package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/code-slammer/slammer-core/internal/agentapi"
	"github.com/code-slammer/slammer-core/internal/agentclient"
	sandboxfirecracker "github.com/code-slammer/slammer-core/internal/firecracker"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

const FIRECRACKER_VERSION = "firecracker-v1.15.0-x86_64"

func main() {
	must(godotenv.Load())
	base_dir := os.Getenv("BASE_DIR")
	if base_dir == "" {
		panic("BASE_DIR is not set (make sure to have a trailing slash)")
	}

	// jailer_sandbox := base_dir + "jailer_sandbox/"
	jailer_sandbox := "/srv/jailer/"
	cleanup(jailer_sandbox)

	kernelImagePath := base_dir + "vmlinux-6.1.155"
	bootImagePath := getenvDefault("BOOT_IMAGE_PATH", base_dir+"boot-init.ext4")
	targetImagePath := os.Getenv("TARGET_IMAGE_PATH")
	if targetImagePath == "" {
		panic("TARGET_IMAGE_PATH is not set")
	}

	uid := 123
	gid := 123

	// Check if kernel image is readable
	// f, err := os.Open(fcCfg.KernelImagePath)
	// if err != nil {
	// 	panic(fmt.Errorf("Failed to open kernel image: %v", err))
	// }
	// f.Close()
	timeFunc(func() {
		wg := sync.WaitGroup{}
		numVMs := 1
		NUM_SHARED_CPU := 1 // measured in # of 1/8 CPU

		if len(os.Args) > 1 {
			// If an argument is passed, use it as the number of VMs to create
			var err error
			numVMs, err = strconv.Atoi(os.Args[1])
			if err != nil {
				panic(fmt.Errorf("Invalid number of VMs: %v", err))
			}
			if len(os.Args) > 2 {
				NUM_SHARED_CPU, err = strconv.Atoi(os.Args[2])
				if err != nil {
					panic(fmt.Errorf("Invalid number of shared CPUs: %v", err))
				}
			}
		}
		NUM_VCPU := int(math.Ceil(float64(NUM_SHARED_CPU) / 8.0))
		cgroup_args := []string{}
		if NUM_SHARED_CPU < 8 {
			cgroup_args = append(cgroup_args, fmt.Sprintf("cpu.max=%d00 100000", 125*NUM_SHARED_CPU)) // 1/8 CPU
		}

		fmt.Printf("Creating %d VMs with %d shared CPUs (%d real CPUs)\n", numVMs, NUM_SHARED_CPU, NUM_VCPU)
		for i := range numVMs {
			wg.Add(1)
			id := uuid.New().String()
			fcCfg := firecracker.Config{
				SocketPath:      "api.socket",
				KernelImagePath: kernelImagePath,
				KernelArgs:      sandboxfirecracker.DefaultKernelArgs,
				Drives:          sandboxfirecracker.Drives(bootImagePath, targetImagePath),
				LogLevel:        "Debug",
				MachineCfg: models.MachineConfiguration{
					VcpuCount:  firecracker.Int64(int64(NUM_VCPU)),
					Smt:        firecracker.Bool(false),
					MemSizeMib: firecracker.Int64(128),
				},
				JailerCfg: &firecracker.JailerConfig{
					UID:            &uid,
					GID:            &gid,
					ID:             id,
					NumaNode:       firecracker.Int(0),
					JailerBinary:   base_dir + "jailer",
					ChrootBaseDir:  jailer_sandbox,
					ChrootStrategy: firecracker.NewNaiveChrootStrategy(kernelImagePath),
					ExecFile:       base_dir + FIRECRACKER_VERSION,
					CgroupVersion:  "2",
					Stdin:          nil,
					Stdout:         io.Discard,
					Stderr:         io.Discard,
					CgroupArgs:     cgroup_args,
				},
				Seccomp:           firecracker.SeccompConfig{Enabled: true},
				NetworkInterfaces: nil,
				VsockDevices: []firecracker.VsockDevice{
					{
						Path: "./vsock.sock",
						CID:  3,
					},
				},
			}

			fmt.Printf("Starting VM %d with ID %s\n", i+1, id)
			go func() {
				defer wg.Done()
				createAndRunVM(fcCfg)
			}()
		}
		wg.Wait()
	})
	// Check each drive is readable and writable
	// for _, drive := range fcCfg.Drives {
	// 	drivePath := firecracker.StringValue(drive.PathOnHost)
	// 	f, err := os.OpenFile(drivePath, os.O_RDWR, 0666)
	// 	if err != nil {
	// 		panic(fmt.Errorf("Failed to open drive with read/write permissions: %v", err))
	// 	}
	// 	f.Close()
	// }

	// time.Sleep(15 * time.Second)
}

func createAndRunVM(fcCfg firecracker.Config) error {
	logrusLogger := logrus.New()
	logrusLogger.SetOutput(os.Stdout)
	logrusLogger.SetLevel(logrus.ErrorLevel)
	logger := logrus.NewEntry(logrusLogger)

	vmmCtx := context.Background()
	m, err := firecracker.NewMachine(vmmCtx, fcCfg, firecracker.WithLogger(logger))
	if err != nil {
		panic(err)
	}

	const VM_TIMEOUT = 30 * time.Second

	if err := m.Start(vmmCtx); err != nil {
		panic(err)
	}
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		// wait 100 ms for the init process to start
		time.Sleep(125 * time.Millisecond)
		jailer_dir := m.Cfg.JailerCfg.ChrootBaseDir
		socket_path := path.Join(jailer_dir, FIRECRACKER_VERSION, m.Cfg.JailerCfg.ID, "root", "vsock.sock")
		// make a new child context with a timeout
		vmClient := agentclient.NewFirecrackerVsock(socket_path, agentclient.DefaultVsockPort)
		vmServiceCtx, cancel := context.WithTimeout(vmmCtx, VM_TIMEOUT)
		if err := waitForAgent(vmServiceCtx, vmClient, 10*time.Millisecond); err != nil {
			cancel()
			fmt.Println("Error:", err)
			return
		}
		contents, err := os.ReadFile("test.py")
		if err != nil {
			cancel()
			fmt.Println("Error:", err)
			return
		}
		uid, gid := 1000, 1000
		out, err := vmClient.Jobs(vmServiceCtx, agentapi.BatchRequest{
			Version:   agentapi.Version,
			Workspace: "/workspace",
			Defaults: agentapi.JobDefaults{
				UID:            &uid,
				GID:            &gid,
				Env:            []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
				TimeoutMillis:  5_000,
				MaxOutputBytes: 1 << 20,
			},
			Jobs: []agentapi.Job{
				{Type: agentapi.JobMkdir, Path: "/workspace", Mode: 0o755},
				{Type: agentapi.JobWriteFile, Path: "/workspace/test.py", Mode: 0o644, ContentsBase64: base64.StdEncoding.EncodeToString(contents)},
				{Type: agentapi.JobExec, Argv: []string{"/usr/bin/python3", "test.py"}, WorkingDir: "/workspace"},
			},
			Shutdown: true,
		})
		cancel()
		if err != nil {
			fmt.Println("Error:", err)
			if out != nil {
				fmt.Printf("Results: %+v\n", out.Results)
			}
			return
		}
		printResults(out)

	}()
	defer m.StopVMM()
	defer wg.Wait()

	// jsonCode, err := json.Marshal(code)
	// must(err)
	// wait for the VMM to exit

	timeout := false
	go func() {
		select {
		case <-time.After(VM_TIMEOUT):
			timeout = true
			m.StopVMM()
		case <-vmmCtx.Done():
			return
		}
	}()

	if err := m.Wait(vmmCtx); err != nil {
		if !timeout {
			fmt.Println(err)
		}
	}

	if timeout {
		fmt.Println("timeout")
	}
	return nil
}

func waitForAgent(ctx context.Context, client *agentclient.Client, sleepDelay time.Duration) error {
	for {
		if err := client.Health(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleepDelay):
		}
	}
}

func printResults(out *agentapi.BatchResponse) {
	for _, result := range out.Results {
		if result.Type != agentapi.JobExec {
			continue
		}
		stdout, _ := base64.StdEncoding.DecodeString(result.StdoutBase64)
		stderr, _ := base64.StdEncoding.DecodeString(result.StderrBase64)
		fmt.Println("Output:", string(stdout))
		fmt.Print("Stderr:", string(stderr))
		fmt.Println("Exit code:", result.ExitCode)
		if result.TimedOut {
			fmt.Println("Timed out")
		}
	}
}

func getenvDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func cleanup(jailer_sandbox string) {
	must(os.RemoveAll(jailer_sandbox + "firecracker-v1.10.1-x86_64"))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func timeFunc(fn func()) {
	start := time.Now()
	fn()
	fmt.Printf("Execution time: %s\n", time.Since(start))
}
