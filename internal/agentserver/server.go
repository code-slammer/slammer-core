package agentserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/code-slammer/slammer-core/internal/agentapi"
	"golang.org/x/sys/unix"
)

const (
	DefaultWorkspace      = "/workspace"
	DefaultMaxBodyBytes   = 16 << 20
	DefaultMaxFileBytes   = 8 << 20
	DefaultMaxOutputBytes = 1 << 20
	DefaultTimeout        = 30 * time.Second
)

type Config struct {
	Workspace         string
	MaxBodyBytes      int64
	MaxFileBytes      int64
	MaxOutputBytes    int64
	DefaultTimeout    time.Duration
	DefaultUID        int
	DefaultGID        int
	Shutdown          func()
	ShutdownAfterJobs bool
}

type Server struct {
	cfg      Config
	usedJobs atomic.Bool
}

func New(cfg Config) *Server {
	if cfg.Workspace == "" {
		cfg.Workspace = DefaultWorkspace
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if cfg.MaxFileBytes <= 0 {
		cfg.MaxFileBytes = DefaultMaxFileBytes
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = DefaultMaxOutputBytes
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = DefaultTimeout
	}
	if cfg.DefaultUID == 0 && cfg.DefaultGID == 0 {
		cfg.DefaultUID = os.Getuid()
		cfg.DefaultGID = os.Getgid()
	}
	return &Server{cfg: cfg}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("POST /jobs", s.jobs)
	return mux
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) jobs(w http.ResponseWriter, r *http.Request) {
	if !s.usedJobs.CompareAndSwap(false, true) {
		http.Error(w, "jobs endpoint already used", http.StatusGone)
		return
	}

	var req agentapi.BatchRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("decode request: %v", err), http.StatusBadRequest)
		return
	}
	if err := decodeEOF(dec); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer func() {
		if (s.cfg.ShutdownAfterJobs || req.Shutdown) && s.cfg.Shutdown != nil {
			go s.cfg.Shutdown()
		}
	}()

	resp, status := s.runBatch(r.Context(), &req)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

func decodeEOF(dec *json.Decoder) error {
	var extra struct{}
	if err := dec.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	}
	return errors.New("request must contain exactly one JSON document")
}

func (s *Server) runBatch(ctx context.Context, req *agentapi.BatchRequest) (*agentapi.BatchResponse, int) {
	resp := &agentapi.BatchResponse{Results: make([]agentapi.JobResult, 0, len(req.Jobs))}
	if req.Version != agentapi.Version {
		return batchError("", fmt.Errorf("unsupported protocol version %d", req.Version)), http.StatusBadRequest
	}
	workspace, err := s.workspace(req.Workspace)
	if err != nil {
		return batchError("", err), http.StatusBadRequest
	}
	if len(req.Jobs) == 0 {
		return batchError("", errors.New("jobs must not be empty")), http.StatusBadRequest
	}

	var uploaded int64
	for _, job := range req.Jobs {
		result := s.runJob(ctx, workspace, req.Defaults, job, &uploaded)
		resp.Results = append(resp.Results, result)
		if !result.OK {
			return resp, http.StatusBadRequest
		}
	}
	return resp, http.StatusOK
}

func batchError(jobType string, err error) *agentapi.BatchResponse {
	return &agentapi.BatchResponse{Results: []agentapi.JobResult{{Type: jobType, OK: false, Error: err.Error()}}}
}

func (s *Server) runJob(ctx context.Context, workspace string, defaults agentapi.JobDefaults, job agentapi.Job, uploaded *int64) agentapi.JobResult {
	switch job.Type {
	case agentapi.JobMkdir:
		return s.mkdir(workspace, defaults, job)
	case agentapi.JobWriteFile:
		return s.writeFile(workspace, defaults, job, uploaded)
	case agentapi.JobExec:
		return s.exec(ctx, workspace, defaults, job)
	default:
		return fail(job.Type, fmt.Errorf("unsupported job type %q", job.Type))
	}
}

func (s *Server) workspace(requested string) (string, error) {
	if requested == "" {
		requested = s.cfg.Workspace
	}
	clean := filepath.Clean(requested)
	if !filepath.IsAbs(clean) {
		return "", fmt.Errorf("workspace must be absolute")
	}
	configured := filepath.Clean(s.cfg.Workspace)
	if clean != configured {
		return "", fmt.Errorf("workspace %q is not allowed", clean)
	}
	if err := os.MkdirAll(clean, 0o755); err != nil {
		return "", fmt.Errorf("create workspace: %w", err)
	}
	return clean, nil
}

func (s *Server) mkdir(workspace string, defaults agentapi.JobDefaults, job agentapi.Job) agentapi.JobResult {
	target, err := safePath(workspace, job.Path)
	if err != nil {
		return fail(job.Type, err)
	}
	mode, err := safeMode(job.Mode, 0o755)
	if err != nil {
		return fail(job.Type, err)
	}
	if err := ensureSafeParents(workspace, target); err != nil {
		return fail(job.Type, err)
	}
	if err := os.MkdirAll(target, mode); err != nil {
		return fail(job.Type, fmt.Errorf("mkdir: %w", err))
	}
	uid, gid := s.credentials(defaults, job)
	if err := chownIfNeeded(target, uid, gid); err != nil {
		return fail(job.Type, err)
	}
	return ok(job.Type)
}

func (s *Server) writeFile(workspace string, defaults agentapi.JobDefaults, job agentapi.Job, uploaded *int64) agentapi.JobResult {
	target, err := safePath(workspace, job.Path)
	if err != nil {
		return fail(job.Type, err)
	}
	mode, err := safeMode(job.Mode, 0o644)
	if err != nil {
		return fail(job.Type, err)
	}
	contents, err := base64.StdEncoding.DecodeString(job.ContentsBase64)
	if err != nil {
		return fail(job.Type, fmt.Errorf("contents_b64: %w", err))
	}
	if int64(len(contents)) > s.cfg.MaxFileBytes {
		return fail(job.Type, fmt.Errorf("file exceeds max size %d", s.cfg.MaxFileBytes))
	}
	*uploaded += int64(len(contents))
	if *uploaded > s.cfg.MaxBodyBytes {
		return fail(job.Type, fmt.Errorf("total uploaded bytes exceeds max size %d", s.cfg.MaxBodyBytes))
	}

	parent := filepath.Dir(target)
	if job.CreateParents {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fail(job.Type, fmt.Errorf("create parents: %w", err))
		}
	}
	if err := ensureSafeParents(workspace, target); err != nil {
		return fail(job.Type, err)
	}
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fail(job.Type, fmt.Errorf("refusing to overwrite symlink %q", job.Path))
	}

	fd, err := unix.Open(target, unix.O_WRONLY|unix.O_CREAT|unix.O_TRUNC|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(mode))
	if err != nil {
		return fail(job.Type, fmt.Errorf("open file: %w", err))
	}
	file := os.NewFile(uintptr(fd), target)
	_, writeErr := file.Write(contents)
	closeErr := file.Close()
	if writeErr != nil {
		return fail(job.Type, fmt.Errorf("write file: %w", writeErr))
	}
	if closeErr != nil {
		return fail(job.Type, fmt.Errorf("close file: %w", closeErr))
	}
	if err := os.Chmod(target, mode); err != nil {
		return fail(job.Type, fmt.Errorf("chmod file: %w", err))
	}
	uid, gid := s.credentials(defaults, job)
	if err := chownIfNeeded(target, uid, gid); err != nil {
		return fail(job.Type, err)
	}
	return ok(job.Type)
}

func (s *Server) exec(ctx context.Context, workspace string, defaults agentapi.JobDefaults, job agentapi.Job) agentapi.JobResult {
	if len(job.Argv) == 0 || job.Argv[0] == "" {
		return fail(job.Type, errors.New("exec argv must not be empty"))
	}
	workingDir := job.WorkingDir
	if workingDir == "" {
		workingDir = workspace
	}
	cwd, err := safePath(workspace, workingDir)
	if err != nil {
		return fail(job.Type, err)
	}
	if err := ensureSafeParents(workspace, filepath.Join(cwd, ".agent-check")); err != nil {
		return fail(job.Type, err)
	}
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		return fail(job.Type, fmt.Errorf("working_dir must exist and be a directory"))
	}

	timeout := s.timeout(defaults, job)
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, job.Argv[0], job.Argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = append([]string{}, defaults.Env...)
	cmd.Env = append(cmd.Env, job.Env...)
	uid, gid := s.credentials(defaults, job)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if uid != os.Getuid() || gid != os.Getgid() {
		if os.Geteuid() != 0 {
			return fail(job.Type, fmt.Errorf("cannot switch credentials unless running as root"))
		}
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	}

	maxOutput := s.maxOutput(defaults, job)
	stdout := &limitedBuffer{limit: maxOutput}
	stderr := &limitedBuffer{limit: maxOutput}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err = cmd.Run()
	timedOut := cmdCtx.Err() == context.DeadlineExceeded
	if timedOut && cmd.Process != nil {
		_ = unix.Kill(-cmd.Process.Pid, unix.SIGKILL)
	}

	result := agentapi.JobResult{
		Type:         job.Type,
		OK:           true,
		TimedOut:     timedOut,
		StdoutBase64: base64.StdEncoding.EncodeToString(stdout.Bytes()),
		StderrBase64: base64.StdEncoding.EncodeToString(stderr.Bytes()),
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result
		}
		if timedOut {
			result.ExitCode = -1
			return result
		}
		return fail(job.Type, fmt.Errorf("exec: %w", err))
	}
	return result
}

func safePath(workspace, requested string) (string, error) {
	if requested == "" {
		return "", errors.New("path must not be empty")
	}
	if !filepath.IsAbs(requested) {
		return "", fmt.Errorf("path %q must be absolute", requested)
	}
	clean := filepath.Clean(requested)
	rel, err := filepath.Rel(workspace, clean)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
		return "", fmt.Errorf("path %q escapes workspace", requested)
	}
	return clean, nil
}

func ensureSafeParents(workspace, target string) error {
	rel, err := filepath.Rel(workspace, filepath.Dir(target))
	if err != nil {
		return err
	}
	current := workspace
	if rel == "." {
		return rejectSymlink(current)
	}
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		if err := rejectSymlink(current); err != nil {
			return err
		}
	}
	return nil
}

func rejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("unsafe parent %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink parent %q", path)
	}
	return nil
}

func safeMode(mode uint32, fallback os.FileMode) (os.FileMode, error) {
	if mode == 0 {
		mode = uint32(fallback)
	}
	if mode&0o6000 != 0 {
		return 0, errors.New("setuid/setgid mode bits are not allowed")
	}
	return os.FileMode(mode & 0o777), nil
}

func (s *Server) credentials(defaults agentapi.JobDefaults, job agentapi.Job) (int, int) {
	uid := s.cfg.DefaultUID
	gid := s.cfg.DefaultGID
	if defaults.UID != nil {
		uid = *defaults.UID
	}
	if defaults.GID != nil {
		gid = *defaults.GID
	}
	if job.UID != nil {
		uid = *job.UID
	}
	if job.GID != nil {
		gid = *job.GID
	}
	return uid, gid
}

func chownIfNeeded(path string, uid, gid int) error {
	if uid == os.Getuid() && gid == os.Getgid() {
		return nil
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown: %w", err)
	}
	return nil
}

func (s *Server) timeout(defaults agentapi.JobDefaults, job agentapi.Job) time.Duration {
	ms := defaults.TimeoutMillis
	if job.TimeoutMillis > 0 {
		ms = job.TimeoutMillis
	}
	if ms <= 0 {
		return s.cfg.DefaultTimeout
	}
	return time.Duration(ms) * time.Millisecond
}

func (s *Server) maxOutput(defaults agentapi.JobDefaults, job agentapi.Job) int64 {
	maxOutput := defaults.MaxOutputBytes
	if job.MaxOutputBytes > 0 {
		maxOutput = job.MaxOutputBytes
	}
	if maxOutput <= 0 {
		return s.cfg.MaxOutputBytes
	}
	return maxOutput
}

func ok(jobType string) agentapi.JobResult {
	return agentapi.JobResult{Type: jobType, OK: true}
}

func fail(jobType string, err error) agentapi.JobResult {
	return agentapi.JobResult{Type: jobType, OK: false, Error: err.Error()}
}

type limitedBuffer struct {
	buf   bytes.Buffer
	limit int64
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - int64(b.buf.Len())
	if remaining <= 0 {
		return len(p), nil
	}
	if int64(len(p)) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		return len(p), nil
	}
	_, _ = b.buf.Write(p)
	return len(p), nil
}

func (b *limitedBuffer) Bytes() []byte {
	return b.buf.Bytes()
}
