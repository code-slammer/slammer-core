package firecracker

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/code-slammer/slammer-core/internal/agentapi"
	"github.com/code-slammer/slammer-core/internal/agentclient"
	sdk "github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

const defaultVsockCID = 3

func StartVM(ctx context.Context, req StartVMRequest) (*RunResult, error) {
	totalStart := time.Now()
	if req.FirecrackerPath == "" {
		return nil, fmt.Errorf("firecracker binary path is required")
	}
	if len(req.Jobs) == 0 {
		return nil, fmt.Errorf("at least one job is required")
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
	if req.Workspace == "" {
		req.Workspace = "/workspace"
	}

	if req.Snapshot != nil {
		return startFromSnapshot(ctx, req, totalStart)
	}

	if req.KernelPath == "" {
		return nil, fmt.Errorf("kernel path is required")
	}
	if req.BootImagePath == "" {
		return nil, fmt.Errorf("boot image path is required")
	}
	if req.TargetImagePath == "" {
		return nil, fmt.Errorf("target image path is required")
	}

	setupStart := time.Now()
	socketDir := filepath.Join(req.StoreDir, "firecracker", "sockets")
	logDir := filepath.Join(req.StoreDir, "firecracker", "logs")
	if err := os.MkdirAll(socketDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, err
	}
	apiSocket := filepath.Join(socketDir, req.ID+".api.socket")
	vsockPath := filepath.Join(socketDir, req.ID+".vsock")
	logPath := filepath.Join(logDir, req.ID+".log")
	jailerStdoutPath := filepath.Join(logDir, req.ID+".jailer.stdout.log")
	jailerStderrPath := filepath.Join(logDir, req.ID+".jailer.stderr.log")
	guestAPISocket := apiSocket
	clientAPISocket := apiSocket
	guestLogPath := logPath
	guestVsockPath := vsockPath
	clientVsockPath := vsockPath
	_ = os.Remove(apiSocket)
	_ = os.Remove(vsockPath)
	var jailerStdout io.Writer = io.Discard
	var jailerStderr io.Writer = io.Discard
	if req.Jailer != nil {
		guestAPISocket = "api.socket"
		clientAPISocket = filepath.Join(jailerRootfsDir(req.Jailer, req.FirecrackerPath, req.ID), guestAPISocket)
		guestLogPath = ""
		guestVsockPath = "vsock"
		clientVsockPath = filepath.Join(jailerRootfsDir(req.Jailer, req.FirecrackerPath, req.ID), guestVsockPath)
		if err := validateUnixSocketPath(clientAPISocket); err != nil {
			return nil, err
		}
		if err := validateUnixSocketPath(clientVsockPath); err != nil {
			return nil, err
		}
		_ = os.Remove(clientAPISocket)
		_ = os.Remove(clientVsockPath)
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
	entry := logrus.NewEntry(logger)
	options := []sdk.Opt{sdk.WithLogger(entry)}
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
	timings := RunTimings{SetupDuration: time.Since(setupStart)}
	machineStart := time.Now()
	if err := machine.Start(ctx); err != nil {
		return nil, err
	}
	timings.MachineStartDuration = time.Since(machineStart)

	vm := VMHandle{ID: req.ID, SocketPath: clientAPISocket}
	resultCh := make(chan *RunResult, 1)
	errCh := make(chan error, 1)
	runCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()

	go func() {
		localTimings := timings
		client := agentclient.NewFirecrackerVsock(clientVsockPath, agentclient.DefaultVsockPort)
		jobsStart := time.Now()
		request := agentapi.BatchRequest{
			Version:   agentapi.Version,
			Workspace: req.Workspace,
			Defaults:  req.Defaults,
			Jobs:      req.Jobs,
			Shutdown:  true,
		}
		resp, err := retryJobs(runCtx, client, request, 10*time.Millisecond)
		if err != nil {
			if resp != nil {
				errCh <- fmt.Errorf("%w: %+v", err, resp.Results)
				return
			}
			errCh <- err
			return
		}
		localTimings.JobsDuration = time.Since(jobsStart)
		resultCh <- &RunResult{VM: vm, Results: resp.Results, Timings: localTimings}
	}()

	waitCh := make(chan error, 1)
	go func() { waitCh <- machine.Wait(ctx) }()

	var result *RunResult
	for result == nil {
		select {
		case result = <-resultCh:
		case err := <-errCh:
			_ = machine.StopVMM()
			return nil, err
		case <-runCtx.Done():
			_ = machine.StopVMM()
			return &RunResult{VM: vm, TimedOut: true, Timings: timings}, runCtx.Err()
		}
	}

	shutdownStart := time.Now()
	_ = machine.StopVMM()
	select {
	case <-waitCh:
	case <-time.After(100 * time.Millisecond):
	}
	result.Timings.ShutdownWaitDuration = time.Since(shutdownStart)
	result.Timings.TotalDuration = time.Since(totalStart)
	return result, nil
}

func startFromSnapshot(ctx context.Context, req StartVMRequest, totalStart time.Time) (*RunResult, error) {
	setupStart := time.Now()

	tmpDir, err := os.MkdirTemp("", "snapshot-restore-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	apiSocket := filepath.Join(tmpDir, "firecracker.sock")
	vsockPath := filepath.Join(req.Snapshot.WorkspaceDir, "vsock.sock")
	memPath := req.Snapshot.MemPath
	snapshotPath := req.Snapshot.SnapshotPath
	if req.Jailer != nil {
		rootfs := jailerRootfsDir(req.Jailer, req.FirecrackerPath, req.ID)
		apiSocket = filepath.Join(rootfs, "api.socket")
		vsockPath = filepath.Join(rootfs, "vsock")
		memPath = "snapshot.mem"
		snapshotPath = "snapshot.state"
		if err := validateUnixSocketPath(apiSocket); err != nil {
			return nil, err
		}
		if err := validateUnixSocketPath(vsockPath); err != nil {
			return nil, err
		}
	}

	// Clear stale vsock UDS so Firecracker can re-bind on restore.
	_ = os.Remove(vsockPath)

	cmd := sdk.VMCommandBuilder{}.
		WithBin(req.FirecrackerPath).
		WithSocketPath(apiSocket).
		WithStdout(io.Discard).
		WithStderr(io.Discard).
		Build(ctx)
	if req.Jailer != nil {
		cmd = jailerCommand(ctx, req.Jailer, req.FirecrackerPath, req.ID, "api.socket", io.Discard, io.Discard)
	}

	machineStart := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start firecracker for snapshot restore: %w", err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	if err := waitForSocket(ctx, apiSocket); err != nil {
		return nil, fmt.Errorf("firecracker socket not ready for restore: %w", err)
	}
	if req.Jailer != nil {
		rootfs := jailerRootfsDir(req.Jailer, req.FirecrackerPath, req.ID)
		if err := linkFile(req.BootImagePath, filepath.Join(rootfs, filepath.Base(req.BootImagePath))); err != nil {
			return nil, err
		}
		if err := linkFile(req.TargetImagePath, filepath.Join(rootfs, filepath.Base(req.TargetImagePath))); err != nil {
			return nil, err
		}
		jailedMemPath := filepath.Join(rootfs, memPath)
		jailedSnapshotPath := filepath.Join(rootfs, snapshotPath)
		if err := linkFile(req.Snapshot.MemPath, jailedMemPath); err != nil {
			return nil, err
		}
		if err := os.Chown(jailedMemPath, req.Jailer.UID, req.Jailer.GID); err != nil {
			return nil, err
		}
		if err := linkFile(req.Snapshot.SnapshotPath, jailedSnapshotPath); err != nil {
			return nil, err
		}
		if err := os.Chown(jailedSnapshotPath, req.Jailer.UID, req.Jailer.GID); err != nil {
			return nil, err
		}
	}
	timings := RunTimings{
		SetupDuration:        time.Since(setupStart),
		MachineStartDuration: time.Since(machineStart),
	}

	client := sdk.NewClient(apiSocket, nil, false)
	sp := snapshotPath
	mp := memPath
	if _, err := client.LoadSnapshot(ctx, &models.SnapshotLoadParams{
		SnapshotPath: &sp,
		MemFilePath:  mp,
		ResumeVM:     true,
	}); err != nil {
		return nil, fmt.Errorf("failed to load snapshot: %w", err)
	}

	vm := VMHandle{ID: req.ID, SocketPath: apiSocket}
	runCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()

	jobsStart := time.Now()
	agent := agentclient.NewFirecrackerVsock(vsockPath, agentclient.DefaultVsockPort)
	batchReq := agentapi.BatchRequest{
		Version:   agentapi.Version,
		Workspace: req.Workspace,
		Defaults:  req.Defaults,
		Jobs:      req.Jobs,
		Shutdown:  true,
	}
	resp, err := retryJobs(runCtx, agent, batchReq, 10*time.Millisecond)
	if err != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		return nil, fmt.Errorf("agent send_batch failed: %w", err)
	}
	timings.JobsDuration = time.Since(jobsStart)

	shutdownStart := time.Now()
	_ = cmd.Process.Signal(syscall.SIGTERM)

	exitCtx, exitCancel := context.WithTimeout(ctx, 30*time.Second)
	defer exitCancel()
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case <-waitErr:
	case <-exitCtx.Done():
	}

	timings.ShutdownWaitDuration = time.Since(shutdownStart)
	timings.TotalDuration = time.Since(totalStart)

	return &RunResult{VM: vm, Results: resp.Results, Timings: timings}, nil
}

func waitForSocket(ctx context.Context, socketPath string) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(socketPath); err == nil {
			client := sdk.NewClient(socketPath, nil, false)
			if _, err := client.GetMachineConfiguration(); err == nil {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func retryJobs(ctx context.Context, client *agentclient.Client, request agentapi.BatchRequest, delay time.Duration) (*agentapi.BatchResponse, error) {
	var lastErr error
	for {
		resp, err := client.Jobs(ctx, request)
		if err == nil || resp != nil {
			return resp, err
		}
		lastErr = err
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return nil, fmt.Errorf("%w: last job attempt: %v", ctx.Err(), lastErr)
			}
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
}
