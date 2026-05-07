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
	guestVsockPath := vsockPath
	clientVsockPath := vsockPath
	_ = os.Remove(vsockPath)
	if req.Jailer != nil {
		guestVsockPath = "vsock"
		clientVsockPath = filepath.Join(jailerRootfsDir(req.Jailer, req.FirecrackerPath, req.ID), guestVsockPath)
		if err := validateUnixSocketPath(clientVsockPath); err != nil {
			return nil, err
		}
		_ = os.Remove(clientVsockPath)
	}

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
	jailerStdoutPath := filepath.Join(logDir, req.ID+".snapshot.jailer.stdout.log")
	jailerStderrPath := filepath.Join(logDir, req.ID+".snapshot.jailer.stderr.log")
	guestAPISocket := apiSocket
	guestLogPath := logPath
	_ = os.Remove(apiSocket)
	var jailerStdout io.Writer = io.Discard
	var jailerStderr io.Writer = io.Discard
	if req.Jailer != nil {
		guestAPISocket = "api.socket"
		guestLogPath = ""
		clientAPISocket := filepath.Join(jailerRootfsDir(req.Jailer, req.FirecrackerPath, req.ID), guestAPISocket)
		if err := validateUnixSocketPath(clientAPISocket); err != nil {
			return nil, err
		}
		_ = os.Remove(clientAPISocket)
		stdoutFile, err := os.Create(jailerStdoutPath)
		if err != nil {
			return nil, err
		}
		defer stdoutFile.Close()
		stderrFile, err := os.Create(jailerStderrPath)
		if err != nil {
			return nil, err
		}
		defer stderrFile.Close()
		jailerStdout = stdoutFile
		jailerStderr = stderrFile
	}

	fcCfg := sdk.Config{
		SocketPath:      guestAPISocket,
		LogPath:         guestLogPath,
		LogLevel:        "Info",
		KernelImagePath: req.KernelPath,
		KernelArgs:      DefaultKernelArgs,
		Drives:          Drives(req.BootImagePath, req.TargetImagePath),
		MachineCfg:      MachineConfiguration(req.MachineConfig),
		Seccomp:         sdk.SeccompConfig{Enabled: true},
		VsockDevices: []sdk.VsockDevice{{
			Path: guestVsockPath,
			CID:  req.VsockCID,
		}},
	}
	if req.Jailer != nil {
		fcCfg.JailerCfg = sdkJailerConfig(req.Jailer, req.FirecrackerPath, req.KernelPath, req.ID, jailerStdout, jailerStderr)
	}
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	options := []sdk.Opt{sdk.WithLogger(logrus.NewEntry(logger))}
	if req.Jailer == nil {
		cmd := sdk.VMCommandBuilder{}.
			WithBin(req.FirecrackerPath).
			WithSocketPath(apiSocket).
			WithStdout(io.Discard).
			WithStderr(io.Discard).
			Build(ctx)
		options = append(options, sdk.WithProcessRunner(cmd))
	}
	machine, err := sdk.NewMachine(ctx, fcCfg, options...)
	if err != nil {
		return nil, err
	}
	if err := machine.Start(ctx); err != nil {
		return nil, err
	}
	defer machine.StopVMM()

	readyCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	client := agentclient.NewFirecrackerVsock(clientVsockPath, agentclient.DefaultVsockPort)
	if err := waitForAgent(readyCtx, client, 10*time.Millisecond); err != nil {
		return nil, err
	}
	if err := machine.PauseVM(ctx); err != nil {
		return nil, err
	}
	memPath := req.Snapshot.MemPath
	snapshotPath := req.Snapshot.SnapshotPath
	jailedMemPath := ""
	jailedSnapshotPath := ""
	if req.Jailer != nil {
		memPath = "snapshot.mem"
		snapshotPath = "snapshot.state"
		rootfs := jailerRootfsDir(req.Jailer, req.FirecrackerPath, req.ID)
		jailedMemPath = filepath.Join(rootfs, memPath)
		jailedSnapshotPath = filepath.Join(rootfs, snapshotPath)
		_ = os.Remove(jailedMemPath)
		_ = os.Remove(jailedSnapshotPath)
	}
	if err := machine.CreateSnapshot(ctx, memPath, snapshotPath); err != nil {
		return nil, err
	}
	if req.Jailer != nil {
		if err := linkFile(jailedMemPath, req.Snapshot.MemPath); err != nil {
			return nil, err
		}
		if err := linkFile(jailedSnapshotPath, req.Snapshot.SnapshotPath); err != nil {
			return nil, err
		}
	}
	_ = os.Remove(clientVsockPath)
	return &req.Snapshot, nil
}

func linkFile(src string, dst string) error {
	_ = os.Remove(dst)
	return os.Link(src, dst)
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
