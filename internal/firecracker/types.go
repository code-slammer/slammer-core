package firecracker

import (
	"time"

	"github.com/code-slammer/slammer-core/internal/agentapi"
	sdk "github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
)

const DefaultKernelArgs = "console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw init=/init target_drive=/dev/vdb"

type StartVMRequest struct {
	KernelPath      string
	BootImagePath   string
	TargetImagePath string
	MachineConfig   MachineConfig
	VsockCID        uint32
	Jobs            []agentapi.Job
	Shutdown        bool
	Timeout         time.Duration
}

type MachineConfig struct {
	VCPUCount  int
	MemSizeMiB int
}

type VMHandle struct {
	ID         string
	SocketPath string
}

func Drives(bootImagePath, targetImagePath string) []models.Drive {
	return []models.Drive{
		{
			DriveID:      sdk.String("boot"),
			PathOnHost:   sdk.String(bootImagePath),
			IsRootDevice: sdk.Bool(true),
			IsReadOnly:   sdk.Bool(true),
		},
		{
			DriveID:      sdk.String("target"),
			PathOnHost:   sdk.String(targetImagePath),
			IsRootDevice: sdk.Bool(false),
			IsReadOnly:   sdk.Bool(true),
		},
	}
}

func MachineConfiguration(cfg MachineConfig) models.MachineConfiguration {
	if cfg.VCPUCount <= 0 {
		cfg.VCPUCount = 1
	}
	if cfg.MemSizeMiB <= 0 {
		cfg.MemSizeMiB = 128
	}
	return models.MachineConfiguration{
		VcpuCount:  sdk.Int64(int64(cfg.VCPUCount)),
		Smt:        sdk.Bool(false),
		MemSizeMib: sdk.Int64(int64(cfg.MemSizeMiB)),
	}
}
