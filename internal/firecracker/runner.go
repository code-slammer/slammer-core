package firecracker

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/code-slammer/slammer-core/internal/agentapi"
	"github.com/code-slammer/slammer-core/internal/agentclient"
	sdk "github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

const defaultVsockCID = 3

func StartVM(ctx context.Context, req StartVMRequest) (*RunResult, error) {
	totalStart := time.Now()
	if req.FirecrackerPath == "" {
		return nil, fmt.Errorf("firecracker binary path is required")
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
	_ = os.Remove(apiSocket)
	_ = os.Remove(vsockPath)

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
	entry := logrus.NewEntry(logger)
	cmd := sdk.VMCommandBuilder{}.
		WithBin(req.FirecrackerPath).
		WithSocketPath(apiSocket).
		WithStdout(io.Discard).
		WithStderr(io.Discard).
		Build(ctx)
	opts := []sdk.Opt{sdk.WithLogger(entry), sdk.WithProcessRunner(cmd)}
	if req.Snapshot != nil {
		opts = append(opts, sdk.WithSnapshot(req.Snapshot.MemPath, req.Snapshot.SnapshotPath, func(cfg *sdk.SnapshotConfig) {
			cfg.ResumeVM = true
		}))
	}
	machine, err := sdk.NewMachine(ctx, fcCfg, opts...)
	if err != nil {
		return nil, err
	}
	timings := RunTimings{SetupDuration: time.Since(setupStart)}
	machineStart := time.Now()
	if err := machine.Start(ctx); err != nil {
		return nil, err
	}
	timings.MachineStartDuration = time.Since(machineStart)

	vm := VMHandle{ID: req.ID, SocketPath: apiSocket}
	resultCh := make(chan *RunResult, 1)
	errCh := make(chan error, 1)
	runCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()

	go func() {
		localTimings := timings
		client := agentclient.NewFirecrackerVsock(vsockPath, agentclient.DefaultVsockPort)
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
