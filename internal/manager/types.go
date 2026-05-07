package manager

import (
	"time"

	"github.com/code-slammer/slammer-core/internal/agentapi"
	"github.com/code-slammer/slammer-core/internal/config"
	"github.com/code-slammer/slammer-core/internal/firecracker"
)

type PrepareImageRequest struct {
	ImageRef string          `json:"image_ref"`
	Platform config.Platform `json:"platform"`
}

type RunRequest struct {
	ImageRef        string                        `json:"image_ref"`
	Platform        config.Platform               `json:"platform"`
	Command         []string                      `json:"command"`
	KernelPath      string                        `json:"kernel_path"`
	BootImagePath   string                        `json:"boot_image_path"`
	FirecrackerPath string                        `json:"firecracker_path"`
	Jailer          *firecracker.JailerConfig     `json:"jailer,omitempty"`
	MachineConfig   firecracker.MachineConfig     `json:"machine_config"`
	Workspace       string                        `json:"workspace"`
	Workdir         string                        `json:"workdir"`
	UID             int                           `json:"uid"`
	GID             int                           `json:"gid"`
	Defaults        agentapi.JobDefaults          `json:"defaults"`
	Jobs            []agentapi.Job                `json:"jobs"`
	Timeout         time.Duration                 `json:"timeout"`
	SnapshotDir     string                        `json:"snapshot_dir,omitempty"`
	Snapshot        *firecracker.SnapshotArtifact `json:"snapshot,omitempty"`
	CleanupJailer   bool                          `json:"cleanup_jailer"`
}

type CreateSnapshotRequest struct {
	ImageRef        string                       `json:"image_ref"`
	Platform        config.Platform              `json:"platform"`
	KernelPath      string                       `json:"kernel_path"`
	BootImagePath   string                       `json:"boot_image_path"`
	FirecrackerPath string                       `json:"firecracker_path"`
	Jailer          *firecracker.JailerConfig    `json:"jailer,omitempty"`
	MachineConfig   firecracker.MachineConfig    `json:"machine_config"`
	Timeout         time.Duration                `json:"timeout"`
	SnapshotDir     string                       `json:"snapshot_dir,omitempty"`
	Snapshot        firecracker.SnapshotArtifact `json:"snapshot"`
}

type RunResult struct {
	Image   PreparedImage `json:"image"`
	VM      *firecracker.RunResult
	Timings RunTimings `json:"timings"`
}

type PreparedImage struct {
	ImageRef       string `json:"image_ref"`
	ManifestDigest string `json:"manifest_digest"`
	ChainID        string `json:"chain_id"`
	RootfsPath     string `json:"rootfs_path"`
	OCIConfigPath  string `json:"oci_config_path"`
	CacheHit       bool   `json:"cache_hit"`
}

type SnapshotInfo struct {
	ChainID      string `json:"chain_id"`
	MemPath      string `json:"mem_path"`
	SnapshotPath string `json:"snapshot_path"`
	WorkspaceDir string `json:"workspace_dir"`
	Exists       bool   `json:"exists"`
}

type RunTimings struct {
	PreflightDuration  time.Duration `json:"preflight_duration"`
	PrepareDuration    time.Duration `json:"prepare_duration"`
	ReadConfigDuration time.Duration `json:"read_config_duration"`
	SnapshotDuration   time.Duration `json:"snapshot_duration"`
	TotalDuration      time.Duration `json:"total_duration"`
}
