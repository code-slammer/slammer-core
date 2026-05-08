package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/code-slammer/slammer-core/internal/agentapi"
	"github.com/code-slammer/slammer-core/internal/config"
	"github.com/code-slammer/slammer-core/internal/firecracker"
	"github.com/code-slammer/slammer-core/internal/manager"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type Server struct {
	Config  Config
	Manager *manager.Manager
}

func New(cfg Config) (*Server, error) {
	applyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Server{Config: cfg, Manager: manager.New(cfg.RuntimeConfig())}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	config := huma.DefaultConfig("sandboxd", "1.0.0")
	config.Servers = []*huma.Server{{URL: "http://" + s.Config.Listen}}
	api := humago.New(mux, config)
	s.Register(api)
	return mux
}

func (s *Server) Register(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "health", Method: http.MethodGet, Path: "/healthz"}, s.health)
	huma.Register(api, huma.Operation{OperationID: "prepare-image", Method: http.MethodPost, Path: "/v1/images/prepare"}, s.prepareImage)
	huma.Register(api, huma.Operation{OperationID: "list-images", Method: http.MethodGet, Path: "/v1/images"}, s.listImages)
	huma.Register(api, huma.Operation{OperationID: "delete-image", Method: http.MethodDelete, Path: "/v1/images/{chain_id}"}, s.deleteImage)
	huma.Register(api, huma.Operation{OperationID: "create-snapshot", Method: http.MethodPost, Path: "/v1/snapshots"}, s.createSnapshot)
	huma.Register(api, huma.Operation{OperationID: "list-snapshots", Method: http.MethodGet, Path: "/v1/snapshots"}, s.listSnapshots)
	huma.Register(api, huma.Operation{OperationID: "delete-snapshot", Method: http.MethodDelete, Path: "/v1/snapshots/{chain_id}"}, s.deleteSnapshot)
	huma.Register(api, huma.Operation{OperationID: "run", Method: http.MethodPost, Path: "/v1/runs"}, s.run)
}

type emptyInput struct{}

type healthOutput struct {
	Body struct {
		OK bool `json:"ok" doc:"Whether the daemon is healthy and able to answer requests." example:"true"`
	}
}

func (s *Server) health(ctx context.Context, input *emptyInput) (*healthOutput, error) {
	out := &healthOutput{}
	out.Body.OK = true
	return out, nil
}

type prepareImageInput struct {
	Body struct {
		ImageRef string `json:"image_ref" doc:"OCI image reference to pull and materialize into the local immutable rootfs cache." minLength:"1" example:"docker.io/library/alpine:latest"`
	}
}

type preparedImageOutput struct {
	Body manager.PreparedImage
}

func (s *Server) prepareImage(ctx context.Context, input *prepareImageInput) (*preparedImageOutput, error) {
	if input.Body.ImageRef == "" {
		return nil, huma.Error400BadRequest("image_ref is required")
	}
	prepared, err := s.Manager.PrepareImage(ctx, manager.PrepareImageRequest{ImageRef: input.Body.ImageRef, Platform: hostPlatform()})
	if err != nil {
		return nil, err
	}
	return &preparedImageOutput{Body: *prepared}, nil
}

type imagesOutput struct {
	Body struct {
		Images []manager.PreparedImage `json:"images" doc:"Prepared rootfs images currently known to the sandbox runtime store."`
	}
}

func (s *Server) listImages(ctx context.Context, input *emptyInput) (*imagesOutput, error) {
	images, err := s.Manager.ListImages(ctx)
	if err != nil {
		return nil, err
	}
	out := &imagesOutput{}
	out.Body.Images = images
	return out, nil
}

type chainIDInput struct {
	ChainID string `path:"chain_id" doc:"Rootfs chain ID. The sha256: prefix is accepted but not required." pattern:"^(sha256:)?[a-f0-9]{64}$" patternDescription:"sha256 chain ID" example:"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`
}

type deletedOutput struct {
	Body struct {
		Deleted bool `json:"deleted" doc:"True when the delete operation completed. Missing artifacts are treated as deleted." example:"true"`
	}
}

func (s *Server) deleteImage(ctx context.Context, input *chainIDInput) (*deletedOutput, error) {
	if err := s.Manager.DeleteImage(ctx, input.ChainID); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	out := &deletedOutput{}
	out.Body.Deleted = true
	return out, nil
}

type snapshotInput struct {
	Body struct {
		ImageRef string `json:"image_ref" doc:"OCI image reference whose prepared rootfs should be booted to agent readiness and snapshotted." minLength:"1" example:"docker.io/library/alpine:latest"`
	}
}

type snapshotOutput struct {
	Body firecracker.SnapshotArtifact
}

func (s *Server) createSnapshot(ctx context.Context, input *snapshotInput) (*snapshotOutput, error) {
	if input.Body.ImageRef == "" {
		return nil, huma.Error400BadRequest("image_ref is required")
	}
	snapshot, err := s.Manager.CreateSnapshot(ctx, manager.CreateSnapshotRequest{
		ImageRef:        input.Body.ImageRef,
		Platform:        hostPlatform(),
		KernelPath:      s.Config.KernelPath,
		BootImagePath:   s.Config.BootImagePath,
		FirecrackerPath: s.Config.FirecrackerPath,
		Jailer:          s.Config.FirecrackerJailer(),
		MachineConfig:   s.Config.Machine,
		Timeout:         time.Duration(s.Config.Limits.MaxTimeoutMillis) * time.Millisecond,
		SnapshotDir:     s.Config.SnapshotDir,
	})
	if err != nil {
		return nil, err
	}
	return &snapshotOutput{Body: *snapshot}, nil
}

type snapshotsOutput struct {
	Body struct {
		Snapshots []manager.SnapshotInfo `json:"snapshots" doc:"Agent-ready snapshots currently present in the configured snapshot directory."`
	}
}

func (s *Server) listSnapshots(ctx context.Context, input *emptyInput) (*snapshotsOutput, error) {
	snapshots, err := s.Manager.ListSnapshots(ctx, s.Config.SnapshotDir)
	if err != nil {
		return nil, err
	}
	out := &snapshotsOutput{}
	out.Body.Snapshots = snapshots
	return out, nil
}

func (s *Server) deleteSnapshot(ctx context.Context, input *chainIDInput) (*deletedOutput, error) {
	if err := s.Manager.DeleteSnapshot(ctx, s.Config.SnapshotDir, input.ChainID); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	out := &deletedOutput{}
	out.Body.Deleted = true
	return out, nil
}

type runInput struct {
	Body RunRequest `json:"body" doc:"One-shot VM run request. Host paths and jailer settings are intentionally server-configured only."`
}

type RunRequest struct {
	ImageRef      string                    `json:"image_ref" doc:"OCI image reference to run. The daemon prepares the image if needed." minLength:"1" example:"docker.io/library/python:3.12-slim"`
	Tasks         []Task                    `json:"tasks" doc:"Ordered guest tasks to execute. A /workspace mkdir task is automatically prepended." minItems:"1" maxItems:"32"`
	Workdir       string                    `json:"workdir,omitempty" doc:"Default guest working directory for exec tasks. Must be under /workspace." default:"/workspace" pattern:"^/workspace(/.*)?$" patternDescription:"absolute path under /workspace" example:"/workspace"`
	UID           int                       `json:"uid,omitempty" doc:"Guest UID used by task defaults when a task does not override it." minimum:"0" default:"0" example:"0"`
	GID           int                       `json:"gid,omitempty" doc:"Guest GID used by task defaults when a task does not override it." minimum:"0" default:"0" example:"0"`
	TimeoutMillis int64                     `json:"timeout_millis,omitempty" doc:"Maximum wall-clock time for the VM run and task defaults. Capped by daemon config." minimum:"1" default:"60000" example:"60000"`
	SnapshotMode  string                    `json:"snapshot_mode,omitempty" doc:"Snapshot behavior for the run. disabled cold-boots; auto restores or creates the configured chain snapshot." enum:"disabled,auto" default:"disabled" example:"auto"`
	Machine       firecracker.MachineConfig `json:"machine,omitempty" doc:"Optional per-run machine sizing. Values are capped by daemon config."`
}

type Task struct {
	Type           string   `json:"type" doc:"Task type to run in the guest agent." enum:"mkdir,write_file,exec" example:"exec"`
	Path           string   `json:"path,omitempty" doc:"Guest path for mkdir and write_file tasks. Must be under /workspace." pattern:"^/workspace(/.*)?$" patternDescription:"absolute path under /workspace" example:"/workspace/main.py"`
	Mode           uint32   `json:"mode,omitempty" doc:"Unix permission bits for mkdir or write_file. Defaults to 0755 for mkdir and 0644 for write_file." minimum:"0" maximum:"511" default:"420" example:"420"`
	ContentsBase64 string   `json:"contents_b64,omitempty" doc:"Base64-encoded file contents for write_file tasks." format:"byte" maxLength:"5592408" example:"cHJpbnQoJ2hpJykK"`
	CreateParents  bool     `json:"create_parents,omitempty" doc:"Create missing parent directories for mkdir or write_file tasks." default:"false" example:"true"`
	Argv           []string `json:"argv,omitempty" doc:"Argument vector for exec tasks. No implicit shell is added." minItems:"1" example:"python3"`
	WorkingDir     string   `json:"working_dir,omitempty" doc:"Guest working directory for this exec task. Must be under /workspace." default:"/workspace" pattern:"^/workspace(/.*)?$" patternDescription:"absolute path under /workspace" example:"/workspace"`
}

type runOutput struct {
	Body struct {
		Image     manager.PreparedImage `json:"image" doc:"Prepared image used for this run." readOnly:"true"`
		Results   []agentapi.JobResult  `json:"results" doc:"Guest agent job results in task order, including the automatically prepended mkdir." readOnly:"true"`
		Timings   RunTimingsDTO         `json:"timings" doc:"High-level manager timing breakdown in milliseconds." readOnly:"true"`
		VM        VMHandleDTO           `json:"vm" doc:"Low-level VM handle metadata for this completed run." readOnly:"true"`
		VMTimings VMTimingsDTO          `json:"vm_timings" doc:"Firecracker and agent timing breakdown in milliseconds." readOnly:"true"`
	}
}

type VMHandleDTO struct {
	ID         string `json:"id" doc:"Server-generated VM identifier." format:"uuid" readOnly:"true" example:"550e8400-e29b-41d4-a716-446655440000"`
	SocketPath string `json:"socket_path" doc:"Firecracker API socket path used internally by the daemon." readOnly:"true"`
}

type RunTimingsDTO struct {
	PreflightDurationMillis  int64 `json:"preflight_duration_ms" doc:"Host preflight duration in milliseconds." minimum:"0" readOnly:"true"`
	PrepareDurationMillis    int64 `json:"prepare_duration_ms" doc:"Image preparation duration in milliseconds." minimum:"0" readOnly:"true"`
	ReadConfigDurationMillis int64 `json:"read_config_duration_ms" doc:"OCI config read duration in milliseconds." minimum:"0" readOnly:"true"`
	SnapshotDurationMillis   int64 `json:"snapshot_duration_ms" doc:"Snapshot creation duration in milliseconds, or zero when no snapshot was created." minimum:"0" readOnly:"true"`
	TotalDurationMillis      int64 `json:"total_duration_ms" doc:"Total manager run duration in milliseconds." minimum:"0" readOnly:"true"`
}

type VMTimingsDTO struct {
	SetupDurationMillis        int64 `json:"setup_duration_ms" doc:"Firecracker setup duration in milliseconds." minimum:"0" readOnly:"true"`
	MachineStartDurationMillis int64 `json:"machine_start_duration_ms" doc:"MicroVM start or snapshot restore duration in milliseconds." minimum:"0" readOnly:"true"`
	AgentReadyDurationMillis   int64 `json:"agent_ready_duration_ms" doc:"Guest agent readiness wait duration in milliseconds when separately measured." minimum:"0" readOnly:"true"`
	JobsDurationMillis         int64 `json:"jobs_duration_ms" doc:"Agent job request duration in milliseconds." minimum:"0" readOnly:"true"`
	ShutdownWaitDurationMillis int64 `json:"shutdown_wait_duration_ms" doc:"VM shutdown wait duration in milliseconds." minimum:"0" readOnly:"true"`
	TotalDurationMillis        int64 `json:"total_duration_ms" doc:"Total low-level VM duration in milliseconds." minimum:"0" readOnly:"true"`
}

func (s *Server) run(ctx context.Context, input *runInput) (*runOutput, error) {
	req, err := s.managerRunRequest(input.Body)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	result, err := s.Manager.Run(ctx, req)
	if err != nil {
		return nil, err
	}
	out := &runOutput{}
	out.Body.Image = result.Image
	out.Body.Results = result.VM.Results
	out.Body.Timings = runTimingsDTO(result.Timings)
	out.Body.VM = VMHandleDTO{ID: result.VM.VM.ID, SocketPath: result.VM.VM.SocketPath}
	out.Body.VMTimings = vmTimingsDTO(result.VM.Timings)
	return out, nil
}

func runTimingsDTO(t manager.RunTimings) RunTimingsDTO {
	return RunTimingsDTO{
		PreflightDurationMillis:  t.PreflightDuration.Milliseconds(),
		PrepareDurationMillis:    t.PrepareDuration.Milliseconds(),
		ReadConfigDurationMillis: t.ReadConfigDuration.Milliseconds(),
		SnapshotDurationMillis:   t.SnapshotDuration.Milliseconds(),
		TotalDurationMillis:      t.TotalDuration.Milliseconds(),
	}
}

func vmTimingsDTO(t firecracker.RunTimings) VMTimingsDTO {
	return VMTimingsDTO{
		SetupDurationMillis:        t.SetupDuration.Milliseconds(),
		MachineStartDurationMillis: t.MachineStartDuration.Milliseconds(),
		AgentReadyDurationMillis:   t.AgentReadyDuration.Milliseconds(),
		JobsDurationMillis:         t.JobsDuration.Milliseconds(),
		ShutdownWaitDurationMillis: t.ShutdownWaitDuration.Milliseconds(),
		TotalDurationMillis:        t.TotalDuration.Milliseconds(),
	}
}

func (s *Server) managerRunRequest(req RunRequest) (manager.RunRequest, error) {
	if req.ImageRef == "" {
		return manager.RunRequest{}, fmt.Errorf("image_ref is required")
	}
	if len(req.Tasks) == 0 {
		return manager.RunRequest{}, fmt.Errorf("at least one task is required")
	}
	if len(req.Tasks) > s.Config.Limits.MaxTasks {
		return manager.RunRequest{}, fmt.Errorf("too many tasks: %d > %d", len(req.Tasks), s.Config.Limits.MaxTasks)
	}
	timeoutMillis := req.TimeoutMillis
	if timeoutMillis <= 0 {
		timeoutMillis = s.Config.Limits.MaxTimeoutMillis
	}
	if timeoutMillis > s.Config.Limits.MaxTimeoutMillis {
		return manager.RunRequest{}, fmt.Errorf("timeout_millis exceeds limit")
	}
	machine := req.Machine
	if machine.VCPUCount <= 0 {
		machine.VCPUCount = s.Config.Machine.VCPUCount
	}
	if machine.MemSizeMiB <= 0 {
		machine.MemSizeMiB = s.Config.Machine.MemSizeMiB
	}
	if machine.VCPUCount > s.Config.Limits.MaxVCPUCount || machine.MemSizeMiB > s.Config.Limits.MaxMemSizeMiB {
		return manager.RunRequest{}, fmt.Errorf("machine config exceeds limits")
	}
	jobs, err := s.tasksToJobs(req.Tasks, firstNonEmpty(req.Workdir, "/workspace"))
	if err != nil {
		return manager.RunRequest{}, err
	}
	snapshotDir := ""
	switch firstNonEmpty(req.SnapshotMode, "disabled") {
	case "disabled":
	case "auto":
		snapshotDir = s.Config.SnapshotDir
	default:
		return manager.RunRequest{}, fmt.Errorf("unsupported snapshot_mode %q", req.SnapshotMode)
	}
	uid, gid := req.UID, req.GID
	return manager.RunRequest{
		ImageRef:        req.ImageRef,
		Platform:        hostPlatform(),
		KernelPath:      s.Config.KernelPath,
		BootImagePath:   s.Config.BootImagePath,
		FirecrackerPath: s.Config.FirecrackerPath,
		Jailer:          s.Config.FirecrackerJailer(),
		MachineConfig:   machine,
		Workspace:       "/workspace",
		Workdir:         firstNonEmpty(req.Workdir, "/workspace"),
		UID:             uid,
		GID:             gid,
		Defaults: agentapi.JobDefaults{
			UID:            &uid,
			GID:            &gid,
			TimeoutMillis:  timeoutMillis,
			MaxOutputBytes: s.Config.Limits.MaxOutputBytes,
		},
		Jobs:          jobs,
		Timeout:       time.Duration(timeoutMillis) * time.Millisecond,
		SnapshotDir:   snapshotDir,
		CleanupJailer: s.Config.CleanupJailer,
	}, nil
}

func (s *Server) tasksToJobs(tasks []Task, defaultWorkdir string) ([]agentapi.Job, error) {
	jobs := make([]agentapi.Job, 0, len(tasks)+1)
	jobs = append(jobs, agentapi.Job{Type: agentapi.JobMkdir, Path: "/workspace", Mode: 0o755})
	for _, task := range tasks {
		switch task.Type {
		case agentapi.JobMkdir:
			if err := validateWorkspacePath(task.Path); err != nil {
				return nil, err
			}
			mode := task.Mode
			if mode == 0 {
				mode = 0o755
			}
			jobs = append(jobs, agentapi.Job{Type: agentapi.JobMkdir, Path: task.Path, Mode: mode, CreateParents: task.CreateParents})
		case agentapi.JobWriteFile:
			if err := validateWorkspacePath(task.Path); err != nil {
				return nil, err
			}
			decodedLen, err := decodedBase64Len(task.ContentsBase64)
			if err != nil {
				return nil, err
			}
			if decodedLen > s.Config.Limits.MaxWriteFileBytes {
				return nil, fmt.Errorf("write_file contents exceed limit")
			}
			mode := task.Mode
			if mode == 0 {
				mode = 0o644
			}
			jobs = append(jobs, agentapi.Job{Type: agentapi.JobWriteFile, Path: task.Path, Mode: mode, ContentsBase64: task.ContentsBase64, CreateParents: task.CreateParents})
		case agentapi.JobExec:
			if len(task.Argv) == 0 {
				return nil, fmt.Errorf("exec argv is required")
			}
			workdir := firstNonEmpty(task.WorkingDir, defaultWorkdir)
			if err := validateWorkspacePath(workdir); err != nil {
				return nil, err
			}
			jobs = append(jobs, agentapi.Job{Type: agentapi.JobExec, Argv: task.Argv, WorkingDir: workdir})
		default:
			return nil, fmt.Errorf("unsupported task type %q", task.Type)
		}
	}
	return jobs, nil
}

func validateWorkspacePath(path string) error {
	clean := filepath.Clean(path)
	if !strings.HasPrefix(clean, "/workspace") {
		return fmt.Errorf("path must be under /workspace: %s", path)
	}
	if clean != "/workspace" && !strings.HasPrefix(clean, "/workspace/") {
		return fmt.Errorf("path must be under /workspace: %s", path)
	}
	return nil
}

func decodedBase64Len(value string) (int64, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return 0, fmt.Errorf("contents_b64 is not valid base64")
	}
	return int64(len(decoded)), nil
}

func hostPlatform() config.Platform {
	return config.Platform{OS: "linux", Architecture: runtime.GOARCH}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
