package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/code-slammer/slammer-core/internal/agentapi"
	"github.com/code-slammer/slammer-core/internal/config"
	"github.com/code-slammer/slammer-core/internal/contentstore"
	"github.com/code-slammer/slammer-core/internal/firecracker"
	"github.com/code-slammer/slammer-core/internal/oci"
	"github.com/code-slammer/slammer-core/internal/rootfsbuilder"
	sandboxruntime "github.com/code-slammer/slammer-core/internal/runtime"
)

type Manager struct {
	Config config.Config
}

func New(cfg config.Config) *Manager {
	rt := sandboxruntime.New(cfg)
	return &Manager{Config: rt.Config}
}

func (m *Manager) PrepareImage(ctx context.Context, req PrepareImageRequest) (*PreparedImage, error) {
	rt := sandboxruntime.New(m.Config)
	prepared, err := rt.PrepareImage(ctx, req.ImageRef, req.Platform)
	if err != nil {
		return nil, err
	}
	return managerPreparedImage(prepared), nil
}

func (m *Manager) Run(ctx context.Context, req RunRequest) (*RunResult, error) {
	totalStart := time.Now()
	if req.ImageRef == "" {
		return nil, fmt.Errorf("image ref is required")
	}
	if req.Timeout <= 0 {
		req.Timeout = 60 * time.Second
	}

	preflightStart := time.Now()
	resolvedFirecracker, err := preflightVMRun(firstNonEmpty(req.FirecrackerPath, "firecracker"), firstNonEmpty(req.KernelPath, m.Config.KernelPath), firstNonEmpty(req.BootImagePath, m.Config.BootImagePath))
	if err != nil {
		return nil, err
	}
	preflightDuration := time.Since(preflightStart)

	prepareStart := time.Now()
	prepared, err := m.PrepareImage(ctx, PrepareImageRequest{ImageRef: req.ImageRef, Platform: req.Platform})
	if err != nil {
		return nil, err
	}
	prepareDuration := time.Since(prepareStart)

	configStart := time.Now()
	imageConfig, err := readOCIConfig(prepared.OCIConfigPath)
	if err != nil {
		return nil, err
	}
	readConfigDuration := time.Since(configStart)

	defaults := req.Defaults
	if defaults.UID == nil {
		uid := req.UID
		defaults.UID = &uid
	}
	if defaults.GID == nil {
		gid := req.GID
		defaults.GID = &gid
	}
	if len(defaults.Env) == 0 {
		defaults.Env = imageConfig.Config.Env
	}
	if len(defaults.Env) == 0 {
		defaults.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	}
	if defaults.TimeoutMillis == 0 {
		defaults.TimeoutMillis = int64(req.Timeout.Milliseconds())
	}
	if defaults.MaxOutputBytes == 0 {
		defaults.MaxOutputBytes = 4 << 20
	}

	jobs := req.Jobs
	if len(jobs) == 0 {
		argv := req.Command
		if len(argv) == 0 {
			argv = defaultCommand(imageConfig)
		}
		if len(argv) == 0 {
			return nil, fmt.Errorf("no command provided and image has no Entrypoint/Cmd")
		}
		workspace := firstNonEmpty(req.Workspace, "/workspace")
		workdir := firstNonEmpty(req.Workdir, workspace)
		jobs = []agentapi.Job{
			{Type: agentapi.JobMkdir, Path: workspace, Mode: 0o755},
			{Type: agentapi.JobExec, Argv: argv, WorkingDir: workdir},
		}
	}

	snapshot := req.Snapshot
	snapshotDuration := time.Duration(0)
	if snapshot == nil && req.SnapshotDir != "" {
		snapshot = SnapshotArtifact(req.SnapshotDir, prepared.ChainID)
		if !SnapshotExists(snapshot) {
			snapStart := time.Now()
			created, err := m.CreateSnapshot(ctx, CreateSnapshotRequest{
				ImageRef:        req.ImageRef,
				Platform:        req.Platform,
				KernelPath:      req.KernelPath,
				BootImagePath:   req.BootImagePath,
				FirecrackerPath: resolvedFirecracker,
				Jailer:          req.Jailer,
				MachineConfig:   req.MachineConfig,
				Timeout:         req.Timeout,
				Snapshot:        *snapshot,
			})
			if err != nil {
				return nil, err
			}
			snapshot = created
			snapshotDuration = time.Since(snapStart)
		}
	}

	vm, err := firecracker.StartVM(ctx, firecracker.StartVMRequest{
		StoreDir:        m.Config.StoreDir,
		FirecrackerPath: resolvedFirecracker,
		Jailer:          req.Jailer,
		KernelPath:      firstNonEmpty(req.KernelPath, m.Config.KernelPath),
		BootImagePath:   firstNonEmpty(req.BootImagePath, m.Config.BootImagePath),
		TargetImagePath: prepared.RootfsPath,
		MachineConfig:   req.MachineConfig,
		Defaults:        defaults,
		Workspace:       firstNonEmpty(req.Workspace, "/workspace"),
		Jobs:            jobs,
		Shutdown:        true,
		Timeout:         req.Timeout,
		Snapshot:        snapshot,
	})
	if err != nil {
		return nil, err
	}
	if req.CleanupJailer && req.Jailer != nil {
		_ = os.RemoveAll(firecracker.JailerVMDir(req.Jailer, resolvedFirecracker, vm.VM.ID))
	}
	return &RunResult{
		Image: *prepared,
		VM:    vm,
		Timings: RunTimings{
			PreflightDuration:  preflightDuration,
			PrepareDuration:    prepareDuration,
			ReadConfigDuration: readConfigDuration,
			SnapshotDuration:   snapshotDuration,
			TotalDuration:      time.Since(totalStart),
		},
	}, nil
}

func (m *Manager) ListImages(ctx context.Context) ([]PreparedImage, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	store := contentstore.New(m.Config.StoreDir)
	entries, err := os.ReadDir(filepath.Join(m.Config.StoreDir, "rootfs", "complete"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	images := make([]PreparedImage, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var meta rootfsbuilder.Metadata
		if err := store.ReadJSON(filepath.Join(m.Config.StoreDir, "rootfs", "complete", entry.Name()), &meta); err != nil {
			return nil, err
		}
		images = append(images, PreparedImage{
			ImageRef:       meta.ImageRef,
			ManifestDigest: meta.ManifestDigest,
			ChainID:        meta.ChainID,
			RootfsPath:     store.RootfsPath(meta.ChainID),
			CacheHit:       true,
		})
	}
	return images, nil
}

func (m *Manager) DeleteImage(ctx context.Context, chainID string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := validateChainID(chainID); err != nil {
		return err
	}
	store := contentstore.New(m.Config.StoreDir)
	for _, path := range []string{store.RootfsPath(chainID), store.RootfsMetadataPath(chainID)} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (m *Manager) ListSnapshots(ctx context.Context, snapshotDir string) ([]SnapshotInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	entries, err := os.ReadDir(snapshotDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	snapshots := make([]SnapshotInfo, 0)
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".snapshot" {
			continue
		}
		encoded := strings.TrimSuffix(entry.Name(), ".snapshot")
		chainID := "sha256:" + encoded
		if seen[chainID] {
			continue
		}
		seen[chainID] = true
		artifact := SnapshotArtifact(snapshotDir, chainID)
		snapshots = append(snapshots, SnapshotInfo{
			ChainID:      chainID,
			MemPath:      artifact.MemPath,
			SnapshotPath: artifact.SnapshotPath,
			WorkspaceDir: artifact.WorkspaceDir,
			Exists:       SnapshotExists(artifact),
		})
	}
	return snapshots, nil
}

func (m *Manager) DeleteSnapshot(ctx context.Context, snapshotDir string, chainID string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := validateChainID(chainID); err != nil {
		return err
	}
	artifact := SnapshotArtifact(snapshotDir, chainID)
	for _, path := range []string{artifact.MemPath, artifact.SnapshotPath} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.RemoveAll(artifact.WorkspaceDir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *Manager) CreateSnapshot(ctx context.Context, req CreateSnapshotRequest) (*firecracker.SnapshotArtifact, error) {
	if req.ImageRef == "" {
		return nil, fmt.Errorf("image ref is required")
	}
	if req.Timeout <= 0 {
		req.Timeout = 60 * time.Second
	}
	resolvedFirecracker, err := preflightVMRun(firstNonEmpty(req.FirecrackerPath, "firecracker"), firstNonEmpty(req.KernelPath, m.Config.KernelPath), firstNonEmpty(req.BootImagePath, m.Config.BootImagePath))
	if err != nil {
		return nil, err
	}
	prepared, err := m.PrepareImage(ctx, PrepareImageRequest{ImageRef: req.ImageRef, Platform: req.Platform})
	if err != nil {
		return nil, err
	}
	snapshot := req.Snapshot
	if snapshot.MemPath == "" && req.SnapshotDir != "" {
		snapshot = *SnapshotArtifact(req.SnapshotDir, prepared.ChainID)
	}
	return firecracker.CreateReadySnapshot(ctx, firecracker.SnapshotRequest{
		StoreDir:        m.Config.StoreDir,
		FirecrackerPath: resolvedFirecracker,
		Jailer:          req.Jailer,
		KernelPath:      firstNonEmpty(req.KernelPath, m.Config.KernelPath),
		BootImagePath:   firstNonEmpty(req.BootImagePath, m.Config.BootImagePath),
		TargetImagePath: prepared.RootfsPath,
		MachineConfig:   req.MachineConfig,
		Timeout:         req.Timeout,
		Snapshot:        snapshot,
	})
}

func SnapshotArtifact(snapshotDir string, chainID string) *firecracker.SnapshotArtifact {
	encoded := strings.TrimPrefix(chainID, "sha256:")
	return &firecracker.SnapshotArtifact{
		MemPath:      filepath.Join(snapshotDir, encoded+".mem"),
		SnapshotPath: filepath.Join(snapshotDir, encoded+".snapshot"),
		WorkspaceDir: filepath.Join(snapshotDir, encoded+"-workspace"),
	}
}

func SnapshotExists(snapshot *firecracker.SnapshotArtifact) bool {
	if snapshot == nil {
		return false
	}
	if _, err := os.Stat(snapshot.MemPath); err != nil {
		return false
	}
	if _, err := os.Stat(snapshot.SnapshotPath); err != nil {
		return false
	}
	return true
}

func defaultCommand(cfg *oci.ImageConfig) []string {
	argv := append([]string{}, cfg.Config.Entrypoint...)
	argv = append(argv, cfg.Config.Cmd...)
	return argv
}

func readOCIConfig(path string) (*oci.ImageConfig, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg oci.ImageConfig
	if err := json.Unmarshal(contents, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func preflightVMRun(firecrackerPath, kernelPath, bootImagePath string) (string, error) {
	resolvedFirecracker := firecrackerPath
	if !strings.ContainsRune(firecrackerPath, os.PathSeparator) {
		path, err := exec.LookPath(firecrackerPath)
		if err != nil {
			return "", fmt.Errorf("firecracker binary %q not found in PATH; pass --firecracker-bin /path/to/firecracker", firecrackerPath)
		}
		resolvedFirecracker = path
	}
	for label, p := range map[string]string{"firecracker": resolvedFirecracker, "kernel": kernelPath, "boot image": bootImagePath} {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("%s path %q is not readable: %w", label, p, err)
		}
	}
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return "", fmt.Errorf("/dev/kvm is not available; Firecracker requires KVM access on the host: %w", err)
	}
	return resolvedFirecracker, nil
}

func managerPreparedImage(prepared *sandboxruntime.PreparedImage) *PreparedImage {
	return &PreparedImage{
		ImageRef:       prepared.ImageRef,
		ManifestDigest: prepared.ManifestDigest,
		ChainID:        prepared.ChainID,
		RootfsPath:     prepared.RootfsPath,
		OCIConfigPath:  prepared.OCIConfigPath,
		CacheHit:       prepared.CacheHit,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func validateChainID(chainID string) error {
	encoded := strings.TrimPrefix(chainID, "sha256:")
	if encoded == "" || len(encoded) != 64 {
		return fmt.Errorf("invalid chain id %q", chainID)
	}
	for _, r := range encoded {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return fmt.Errorf("invalid chain id %q", chainID)
		}
	}
	return nil
}
