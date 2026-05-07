package firecracker

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/code-slammer/slammer-core/internal/agentclient"
	sdk "github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

func CreateReadySnapshot(ctx context.Context, req SnapshotRequest) (*SnapshotArtifact, error) {
	if req.FirecrackerPath == "" || req.KernelPath == "" || req.BootImagePath == "" || req.TargetImagePath == "" {
		return nil, fmt.Errorf("firecracker, kernel, boot image, and target image paths are required")
	}
	if req.ID == "" {
		req.ID = uuid.NewString()
	}
	if req.StoreDir == "" {
		req.StoreDir = filepath.Join(os.TempDir(), "sandbox-runtime")
	}
	if req.VsockCID == 0 {
		req.VsockCID = defaultVsockCID
	}
	if req.Timeout <= 0 {
		req.Timeout = 30 * time.Second
	}
	if req.Snapshot.MemPath == "" || req.Snapshot.SnapshotPath == "" {
		return nil, fmt.Errorf("snapshot memory and state paths are required")
	}
	if err := os.MkdirAll(filepath.Dir(req.Snapshot.MemPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(req.Snapshot.SnapshotPath), 0o755); err != nil {
		return nil, err
	}
	_ = os.Remove(req.Snapshot.MemPath)
	_ = os.Remove(req.Snapshot.SnapshotPath)

	workspaceDir := req.Snapshot.WorkspaceDir
	if workspaceDir == "" {
		workspaceDir = filepath.Join(filepath.Dir(req.Snapshot.MemPath), "workspace")
	}
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return nil, err
	}
	vsockPath := filepath.Join(workspaceDir, "vsock.sock")
	_ = os.Remove(vsockPath)

	socketDir := filepath.Join(req.StoreDir, "firecracker", "sockets")
	logDir := filepath.Join(req.StoreDir, "firecracker", "logs")
	if err := os.MkdirAll(socketDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, err
	}
	apiSocket := filepath.Join(socketDir, req.ID+".snapshot.api.socket")
	logPath := filepath.Join(logDir, req.ID+".snapshot.log")
	_ = os.Remove(apiSocket)

	fcCfg := sdk.Config{
		SocketPath:      apiSocket,
		LogPath:         logPath,
		LogLevel:        "Info",
		KernelImagePath: req.KernelPath,
		KernelArgs:      DefaultKernelArgs,
		Drives:          Drives(req.BootImagePath, req.TargetImagePath),
		MachineCfg:      MachineConfiguration(req.MachineConfig),
		Seccomp:         sdk.SeccompConfig{Enabled: true},
		VsockDevices: []sdk.VsockDevice{{
			Path: vsockPath,
			CID:  req.VsockCID,
		}},
	}
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cmd := sdk.VMCommandBuilder{}.
		WithBin(req.FirecrackerPath).
		WithSocketPath(apiSocket).
		WithStdout(io.Discard).
		WithStderr(io.Discard).
		Build(ctx)
	machine, err := sdk.NewMachine(ctx, fcCfg, sdk.WithLogger(logrus.NewEntry(logger)), sdk.WithProcessRunner(cmd))
	if err != nil {
		return nil, err
	}
	if err := machine.Start(ctx); err != nil {
		return nil, err
	}
	defer machine.StopVMM()

	readyCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	client := agentclient.NewFirecrackerVsock(vsockPath, agentclient.DefaultVsockPort)
	if err := waitForAgent(readyCtx, client, 10*time.Millisecond); err != nil {
		return nil, err
	}
	if err := machine.PauseVM(ctx); err != nil {
		return nil, err
	}
	if err := machine.CreateSnapshot(ctx, req.Snapshot.MemPath, req.Snapshot.SnapshotPath); err != nil {
		return nil, err
	}
	_ = os.Remove(vsockPath)
	return &req.Snapshot, nil
}

func waitForAgent(ctx context.Context, client *agentclient.Client, delay time.Duration) error {
	for {
		if err := client.Health(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}
