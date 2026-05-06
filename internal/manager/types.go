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
	KernelPath      string                        `json:"kernel_path"`
	BootImagePath   string                        `json:"boot_image_path"`
	FirecrackerPath string                        `json:"firecracker_path"`
	MachineConfig   firecracker.MachineConfig     `json:"machine_config"`
	Workspace       string                        `json:"workspace"`
	Defaults        agentapi.JobDefaults          `json:"defaults"`
	Jobs            []agentapi.Job                `json:"jobs"`
	Timeout         time.Duration                 `json:"timeout"`
	Snapshot        *firecracker.SnapshotArtifact `json:"snapshot,omitempty"`
}

type CreateSnapshotRequest struct {
	ImageRef        string                       `json:"image_ref"`
	Platform        config.Platform              `json:"platform"`
	KernelPath      string                       `json:"kernel_path"`
	BootImagePath   string                       `json:"boot_image_path"`
	FirecrackerPath string                       `json:"firecracker_path"`
	MachineConfig   firecracker.MachineConfig    `json:"machine_config"`
	Timeout         time.Duration                `json:"timeout"`
	Snapshot        firecracker.SnapshotArtifact `json:"snapshot"`
}
