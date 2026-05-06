package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/code-slammer/slammer-core/internal/agentapi"
	"github.com/code-slammer/slammer-core/internal/agentclient"
	"github.com/code-slammer/slammer-core/internal/agentserver"
	"github.com/code-slammer/slammer-core/internal/config"
	sandboxfirecracker "github.com/code-slammer/slammer-core/internal/firecracker"
	"github.com/code-slammer/slammer-core/internal/oci"
	sandboxruntime "github.com/code-slammer/slammer-core/internal/runtime"
	"github.com/diskfs/go-diskfs/backend/file"
	"github.com/diskfs/go-diskfs/filesystem/ext4"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "prepare-image":
		prepareImage(os.Args[2:])
	case "inspect-rootfs":
		inspectRootfs(os.Args[2:])
	case "demo-local":
		demoLocal(os.Args[2:])
	case "run":
		runVM(os.Args[2:])
	case "create-snapshot":
		createSnapshot(os.Args[2:])
	case "build-boot-image":
		buildBootImage(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func buildBootImage(args []string) {
	fs := flag.NewFlagSet("build-boot-image", flag.ExitOnError)
	initPath := fs.String("init", "", "static guest /init binary")
	agentPath := fs.String("agent", "", "static guest /agent binary")
	outputPath := fs.String("output", "", "output boot ext4 image")
	size := fs.Int64("size-bytes", 256<<20, "boot image size")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if *initPath == "" || *agentPath == "" || *outputPath == "" {
		fatal(fmt.Errorf("usage: sandboxd build-boot-image --init /tmp/init --agent /tmp/agent --output boot-init.ext4"))
	}
	if err := os.MkdirAll(filepath.Dir(*outputPath), 0o755); err != nil && filepath.Dir(*outputPath) != "." {
		fatal(err)
	}
	_ = os.Remove(*outputPath)
	storage, err := file.CreateFromPath(*outputPath, *size)
	if err != nil {
		fatal(err)
	}
	defer storage.Close()
	fsys, err := ext4.Create(storage, *size, 0, 512, &ext4.Params{VolumeName: "sandbox-boot", SectorsPerBlock: 8})
	if err != nil {
		fatal(err)
	}
	defer fsys.Close()
	for _, dir := range []string{"dev", "proc", "sys", "run", "tmp", "mnt", "mnt/target", "mnt/overlay"} {
		if err := fsys.Mkdir(dir); err != nil {
			fatal(err)
		}
	}
	if err := copyIntoExt4(fsys, *initPath, "init", 0o755); err != nil {
		fatal(err)
	}
	if err := copyIntoExt4(fsys, *agentPath, "agent", 0o755); err != nil {
		fatal(err)
	}
	fmt.Println(*outputPath)
}

func runVM(args []string) {
	totalStart := time.Now()
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	storeDir := fs.String("store-dir", config.DefaultStoreDir, "runtime store directory")
	kernelPath := fs.String("kernel", config.DefaultKernelPath, "Firecracker kernel image path")
	bootImagePath := fs.String("boot-image", config.DefaultBootImagePath, "trusted boot/init ext4 image path")
	firecrackerPath := fs.String("firecracker-bin", "firecracker", "Firecracker binary path")
	osName := fs.String("os", config.DefaultPlatformOS, "target platform OS")
	arch := fs.String("arch", config.DefaultArchitecture, "target platform architecture")
	vcpu := fs.Int("vcpu", 1, "vCPU count")
	mem := fs.Int("mem-mib", 256, "memory size in MiB")
	uid := fs.Int("uid", 0, "guest uid for exec job")
	gid := fs.Int("gid", 0, "guest gid for exec job")
	workdir := fs.String("workdir", "/workspace", "guest working directory under /workspace")
	timeout := fs.Duration("timeout", 60*time.Second, "VM/job timeout")
	snapshotDir := fs.String("snapshot-dir", "", "directory for ready snapshot artifacts; existing snapshots are restored")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() < 1 {
		fatal(fmt.Errorf("usage: sandboxd run [flags] IMAGE_REF [-- COMMAND [ARG...]]"))
	}
	imageRef := fs.Arg(0)
	argv := fs.Args()[1:]
	if len(argv) > 0 && argv[0] == "--" {
		argv = argv[1:]
	}
	preflightStart := time.Now()
	resolvedFirecracker, err := preflightVMRun(*firecrackerPath, *kernelPath, *bootImagePath)
	if err != nil {
		fatal(err)
	}
	preflightDuration := time.Since(preflightStart)

	ctx, cancel := context.WithTimeout(context.Background(), *timeout+2*time.Minute)
	defer cancel()
	rt := sandboxruntime.New(config.Config{StoreDir: *storeDir, KernelPath: *kernelPath, BootImagePath: *bootImagePath})
	prepareStart := time.Now()
	prepared, err := rt.PrepareImage(ctx, imageRef, config.Platform{OS: *osName, Architecture: *arch})
	if err != nil {
		fatal(err)
	}
	prepareDuration := time.Since(prepareStart)
	configStart := time.Now()
	imageConfig, err := readOCIConfig(prepared.OCIConfigPath)
	if err != nil {
		fatal(err)
	}
	configDuration := time.Since(configStart)
	jobUID, jobGID := *uid, *gid
	defaults := agentapi.JobDefaults{
		UID:            &jobUID,
		GID:            &jobGID,
		Env:            imageConfig.Config.Env,
		TimeoutMillis:  int64(timeout.Milliseconds()),
		MaxOutputBytes: 4 << 20,
	}
	if len(defaults.Env) == 0 {
		defaults.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	}
	if len(argv) == 0 {
		argv = append([]string{}, imageConfig.Config.Entrypoint...)
		argv = append(argv, imageConfig.Config.Cmd...)
		if len(argv) == 0 {
			fatal(fmt.Errorf("no command provided and image has no Entrypoint/Cmd"))
		}
	}
	jobWorkdir := *workdir
	var snapshot *sandboxfirecracker.SnapshotArtifact
	if *snapshotDir != "" {
		snapshot = snapshotArtifact(*snapshotDir, prepared.ChainID)
		if !snapshotExists(snapshot) {
			snapStart := time.Now()
			if _, err := sandboxfirecracker.CreateReadySnapshot(ctx, sandboxfirecracker.SnapshotRequest{
				StoreDir:        *storeDir,
				FirecrackerPath: resolvedFirecracker,
				KernelPath:      *kernelPath,
				BootImagePath:   *bootImagePath,
				TargetImagePath: prepared.RootfsPath,
				MachineConfig:   sandboxfirecracker.MachineConfig{VCPUCount: *vcpu, MemSizeMiB: *mem},
				Timeout:         *timeout,
				Snapshot:        *snapshot,
			}); err != nil {
				fatal(err)
			}
			fmt.Fprintf(os.Stderr, "snapshot_create: %s\n", time.Since(snapStart))
		}
	}

	result, err := sandboxfirecracker.StartVM(ctx, sandboxfirecracker.StartVMRequest{
		StoreDir:        *storeDir,
		FirecrackerPath: resolvedFirecracker,
		KernelPath:      *kernelPath,
		BootImagePath:   *bootImagePath,
		TargetImagePath: prepared.RootfsPath,
		MachineConfig:   sandboxfirecracker.MachineConfig{VCPUCount: *vcpu, MemSizeMiB: *mem},
		Defaults:        defaults,
		Workspace:       "/workspace",
		Jobs: []agentapi.Job{
			{Type: agentapi.JobMkdir, Path: "/workspace", Mode: 0o755},
			{Type: agentapi.JobExec, Argv: argv, WorkingDir: jobWorkdir},
		},
		Shutdown: true,
		Timeout:  *timeout,
		Snapshot: snapshot,
	})
	if err != nil {
		fatal(err)
	}
	printJobResults(result.Results)
	printRunTimings(prepared.CacheHit, preflightDuration, prepareDuration, configDuration, result.Timings, time.Since(totalStart))
	if code := exitCodeFromResults(result.Results); code != 0 {
		os.Exit(code)
	}
}

func createSnapshot(args []string) {
	fs := flag.NewFlagSet("create-snapshot", flag.ExitOnError)
	storeDir := fs.String("store-dir", config.DefaultStoreDir, "runtime store directory")
	kernelPath := fs.String("kernel", config.DefaultKernelPath, "Firecracker kernel image path")
	bootImagePath := fs.String("boot-image", config.DefaultBootImagePath, "trusted boot/init ext4 image path")
	firecrackerPath := fs.String("firecracker-bin", "firecracker", "Firecracker binary path")
	snapshotDir := fs.String("snapshot-dir", filepath.Join("tmp", "snapshots"), "snapshot artifact directory")
	vcpu := fs.Int("vcpu", 1, "vCPU count")
	mem := fs.Int("mem-mib", 256, "memory size in MiB")
	timeout := fs.Duration("timeout", 60*time.Second, "snapshot creation timeout")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() != 1 {
		fatal(fmt.Errorf("usage: sandboxd create-snapshot [flags] IMAGE_REF"))
	}
	imageRef := fs.Arg(0)
	resolvedFirecracker, err := preflightVMRun(*firecrackerPath, *kernelPath, *bootImagePath)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout+2*time.Minute)
	defer cancel()
	rt := sandboxruntime.New(config.Config{StoreDir: *storeDir, KernelPath: *kernelPath, BootImagePath: *bootImagePath})
	prepared, err := rt.PrepareImage(ctx, imageRef, config.Platform{OS: config.DefaultPlatformOS, Architecture: config.DefaultArchitecture})
	if err != nil {
		fatal(err)
	}
	snapshot := snapshotArtifact(*snapshotDir, prepared.ChainID)
	created, err := sandboxfirecracker.CreateReadySnapshot(ctx, sandboxfirecracker.SnapshotRequest{
		StoreDir:        *storeDir,
		FirecrackerPath: resolvedFirecracker,
		KernelPath:      *kernelPath,
		BootImagePath:   *bootImagePath,
		TargetImagePath: prepared.RootfsPath,
		MachineConfig:   sandboxfirecracker.MachineConfig{VCPUCount: *vcpu, MemSizeMiB: *mem},
		Timeout:         *timeout,
		Snapshot:        *snapshot,
	})
	if err != nil {
		fatal(err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(created); err != nil {
		fatal(err)
	}
}

func snapshotArtifact(snapshotDir string, chainID string) *sandboxfirecracker.SnapshotArtifact {
	encoded := strings.TrimPrefix(chainID, "sha256:")
	return &sandboxfirecracker.SnapshotArtifact{
		MemPath:      filepath.Join(snapshotDir, encoded+".mem"),
		SnapshotPath: filepath.Join(snapshotDir, encoded+".snapshot"),
	}
}

func snapshotExists(snapshot *sandboxfirecracker.SnapshotArtifact) bool {
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

func prepareImage(args []string) {
	fs := flag.NewFlagSet("prepare-image", flag.ExitOnError)
	storeDir := fs.String("store-dir", config.DefaultStoreDir, "runtime store directory")
	osName := fs.String("os", config.DefaultPlatformOS, "target platform OS")
	arch := fs.String("arch", config.DefaultArchitecture, "target platform architecture")
	timeout := fs.Duration("timeout", 10*time.Minute, "prepare timeout")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() != 1 {
		fatal(fmt.Errorf("usage: sandboxd prepare-image [flags] IMAGE_REF"))
	}

	rt := sandboxruntime.New(config.Config{StoreDir: *storeDir})
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	prepared, err := rt.PrepareImage(ctx, fs.Arg(0), config.Platform{OS: *osName, Architecture: *arch})
	if err != nil {
		fatal(err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(prepared); err != nil {
		fatal(err)
	}
}

func inspectRootfs(args []string) {
	fs := flag.NewFlagSet("inspect-rootfs", flag.ExitOnError)
	readFile := fs.String("read", "", "read a file from the ext4 image")
	listDir := fs.String("ls", "", "list a directory from the ext4 image")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}
	if fs.NArg() != 1 {
		fatal(fmt.Errorf("usage: sandboxd inspect-rootfs [--ls PATH] [--read PATH] ROOTFS_EXT4"))
	}
	imagePath := fs.Arg(0)
	fsys, closeFn, err := openExt4(imagePath)
	if err != nil {
		fatal(err)
	}
	defer closeFn()

	if *listDir == "" && *readFile == "" {
		*listDir = "/"
	}
	if *listDir != "" {
		entries, err := fsys.ReadDir(cleanImagePath(*listDir))
		if err != nil {
			fatal(err)
		}
		for _, entry := range entries {
			kind := "file"
			if entry.IsDir() {
				kind = "dir"
			}
			fmt.Printf("%s\t%s\n", kind, entry.Name())
		}
	}
	if *readFile != "" {
		contents, err := fsys.ReadFile(cleanImagePath(*readFile))
		if err != nil {
			fatal(err)
		}
		_, _ = os.Stdout.Write(contents)
	}
}

func demoLocal(args []string) {
	fs := flag.NewFlagSet("demo-local", flag.ExitOnError)
	storeDir := fs.String("store-dir", filepath.Join("tmp", "sandbox-runtime-demo"), "runtime store directory")
	imageRef := fs.String("image", "docker.io/library/alpine:latest", "OCI image ref to prepare")
	timeout := fs.Duration("timeout", 10*time.Minute, "demo timeout")
	if err := fs.Parse(args); err != nil {
		fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	rt := sandboxruntime.New(config.Config{StoreDir: *storeDir})
	prepared, err := rt.PrepareImage(ctx, *imageRef, config.Platform{OS: config.DefaultPlatformOS, Architecture: config.DefaultArchitecture})
	if err != nil {
		fatal(err)
	}
	fmt.Printf("prepared rootfs: %s\n", prepared.RootfsPath)

	fsys, closeFn, err := openExt4(prepared.RootfsPath)
	if err != nil {
		fatal(err)
	}
	defer closeFn()
	for _, p := range []string{"bin", "etc", "lib"} {
		if _, err := fsys.Stat(p); err != nil {
			fatal(fmt.Errorf("missing expected rootfs path %s: %w", p, err))
		}
	}
	fmt.Println("verified ext4 contains expected Alpine directories: bin, etc, lib")

	absStoreDir, err := filepath.Abs(*storeDir)
	if err != nil {
		fatal(err)
	}
	workspace := filepath.Join(absStoreDir, "demo-workspace")
	if err := os.RemoveAll(workspace); err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		fatal(err)
	}
	server := agentserver.New(agentserver.Config{Workspace: workspace, DefaultUID: os.Getuid(), DefaultGID: os.Getgid()})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	client := agentclient.NewHTTP(httpServer.Client(), httpServer.URL)

	uid, gid := os.Getuid(), os.Getgid()
	resp, err := client.Jobs(ctx, agentapi.BatchRequest{
		Version:   agentapi.Version,
		Workspace: workspace,
		Defaults:  agentapi.JobDefaults{UID: &uid, GID: &gid, TimeoutMillis: 5_000, MaxOutputBytes: 1 << 20},
		Jobs: []agentapi.Job{
			{Type: agentapi.JobWriteFile, Path: filepath.Join(workspace, "hello.txt"), Mode: 0o644, ContentsBase64: base64.StdEncoding.EncodeToString([]byte("hello from batched jobs\n"))},
			{Type: agentapi.JobExec, Argv: []string{"/bin/sh", "-c", "cat hello.txt"}, WorkingDir: workspace},
		},
	})
	if err != nil {
		if resp != nil {
			fatal(fmt.Errorf("%w: %+v", err, resp.Results))
		}
		fatal(err)
	}
	if len(resp.Results) != 2 || !resp.Results[0].OK || !resp.Results[1].OK {
		fatal(fmt.Errorf("unexpected job results: %+v", resp.Results))
	}
	stdout, _ := base64.StdEncoding.DecodeString(resp.Results[1].StdoutBase64)
	fmt.Printf("agent job stdout: %s", stdout)
	fmt.Println("demo complete")
}

func openExt4(imagePath string) (*ext4.FileSystem, func() error, error) {
	info, err := os.Stat(imagePath)
	if err != nil {
		return nil, nil, err
	}
	storage, err := file.OpenFromPath(imagePath, true)
	if err != nil {
		return nil, nil, err
	}
	fsys, err := ext4.Read(storage, info.Size(), 0, 512)
	if err != nil {
		_ = storage.Close()
		return nil, nil, err
	}
	return fsys, func() error {
		err1 := fsys.Close()
		err2 := storage.Close()
		if err1 != nil {
			return err1
		}
		return err2
	}, nil
}

func cleanImagePath(p string) string {
	clean := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(p)), "/")
	if clean == "." || clean == "" {
		return "."
	}
	return clean
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

func printJobResults(results []agentapi.JobResult) {
	for _, result := range results {
		if result.Type != agentapi.JobExec {
			continue
		}
		stdout, _ := base64.StdEncoding.DecodeString(result.StdoutBase64)
		stderr, _ := base64.StdEncoding.DecodeString(result.StderrBase64)
		_, _ = os.Stdout.Write(stdout)
		_, _ = os.Stderr.Write(stderr)
		fmt.Fprintf(os.Stderr, "exit_code=%d\n", result.ExitCode)
		if result.TimedOut {
			fmt.Fprintln(os.Stderr, "timed_out=true")
		}
		if !result.OK && result.Error != "" {
			fmt.Fprintln(os.Stderr, result.Error)
		}
	}
}

func printRunTimings(cacheHit bool, preflight time.Duration, prepare time.Duration, readConfig time.Duration, vm sandboxfirecracker.RunTimings, total time.Duration) {
	fmt.Fprintln(os.Stderr, "timings:")
	fmt.Fprintf(os.Stderr, "  prepare_cache_hit: %t\n", cacheHit)
	fmt.Fprintf(os.Stderr, "  preflight: %s\n", preflight)
	fmt.Fprintf(os.Stderr, "  prepare_image: %s\n", prepare)
	fmt.Fprintf(os.Stderr, "  read_oci_config: %s\n", readConfig)
	fmt.Fprintf(os.Stderr, "  vm_setup: %s\n", vm.SetupDuration)
	fmt.Fprintf(os.Stderr, "  firecracker_start: %s\n", vm.MachineStartDuration)
	if vm.AgentReadyDuration > 0 {
		fmt.Fprintf(os.Stderr, "  agent_ready: %s\n", vm.AgentReadyDuration)
	}
	fmt.Fprintf(os.Stderr, "  jobs_request_with_agent_wait: %s\n", vm.JobsDuration)
	fmt.Fprintf(os.Stderr, "  shutdown_wait: %s\n", vm.ShutdownWaitDuration)
	fmt.Fprintf(os.Stderr, "  vm_total: %s\n", vm.TotalDuration)
	fmt.Fprintf(os.Stderr, "  command_total: %s\n", total)
}

func exitCodeFromResults(results []agentapi.JobResult) int {
	for _, result := range results {
		if result.Type == agentapi.JobExec {
			if result.ExitCode < 0 || result.ExitCode > 255 {
				return 1
			}
			return result.ExitCode
		}
	}
	return 0
}

func copyIntoExt4(fsys *ext4.FileSystem, sourcePath string, targetPath string, mode os.FileMode) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := fsys.OpenFile(targetPath, os.O_CREATE|os.O_RDWR)
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		_ = target.Close()
		return err
	}
	if err := target.Close(); err != nil {
		return err
	}
	return fsys.Chmod(targetPath, mode)
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage:\n  sandboxd prepare-image [flags] IMAGE_REF\n  sandboxd inspect-rootfs [--ls PATH] [--read PATH] ROOTFS_EXT4\n  sandboxd build-boot-image --init /tmp/init --agent /tmp/agent --output boot-init.ext4\n  sandboxd run [flags] IMAGE_REF -- COMMAND [ARG...]\n  sandboxd demo-local [flags]\n")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
