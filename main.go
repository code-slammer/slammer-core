package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"strconv"
	"sync"
	"time"

	slammer_rpc "github.com/code-slammer/slammer-core/rpc"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

const FIRECRACKER_VERSION = "firecracker-v1.11.0-x86_64"

func main() {
	must(godotenv.Load())
	base_dir := os.Getenv("BASE_DIR")
	if base_dir == "" {
		panic("BASE_DIR is not set (make sure to have a trailing slash)")
	}

	// jailer_sandbox := base_dir + "jailer_sandbox/"
	jailer_sandbox := "/srv/jailer/"
	cleanup(jailer_sandbox)

	kernelImagePath := base_dir + "kernel/vmlinux-6.1.102"

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
				//console=ttyS0 quiet
				KernelArgs: "quiet reboot=k panic=1 pci=off overlay_root=ram init=/sbin/overlay-init",
				// KernelArgs: "console=ttyS0 reboot=k panic=1 pci=off nomodules random.trust_cpu=on i8042.noaux i8042.nomux i8042.nopnp i8042.nokbd overlay_root=ram init=/sbin/overlay-init",
				// KernelArgs: "console=ttyS0 reboot=k panic=1 pci=off init=/sbin/overlay-init",
				Drives:   firecracker.NewDrivesBuilder(base_dir + "rootfs/testing/image.img").Build(),
				LogLevel: "Debug",
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

			// Mark the rootfs as read-only
			fcCfg.Drives[0].IsReadOnly = firecracker.Bool(true)
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
		vmServiceCtx, cancel := context.WithTimeout(vmmCtx, VM_TIMEOUT)
		vmClient := NewVMClient(socket_path)
		defer vmClient.Close()
		err := vmClient.Connect(vmServiceCtx, 10*time.Millisecond)
		cancel()
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		contents, _ := os.ReadFile("test.py")
		vmClient.UploadFile("/home/user/test.py", contents)
		out, err := vmClient.ExecuteCommand(&slammer_rpc.ExecArgs{
			// Command:        "/bin/bash",
			Command: "/usr/bin/python3",
			// Args:           []string{"-c", "sysbench cpu run"},
			Args:           []string{"test.py"},
			UID:            1000,
			GID:            1000,
			WorkDir:        "/home/user",
			Env:            []string{},
			ShutdownOnExit: true,
		})
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		fmt.Println("Output:", string(out.Stdout))
		fmt.Print("Stderr:", string(out.Stderr))
		fmt.Println("Exit code:", out.ExitCode)

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
