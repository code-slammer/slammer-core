package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"sync"
	"time"

	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/joho/godotenv"
	"github.com/k0kubun/pp/v3"
	"github.com/sirupsen/logrus"
)

func main() {
	must(godotenv.Load())
	base_dir := os.Getenv("BASE_DIR")
	if base_dir == "" {
		panic("BASE_DIR is not set (make sure to have a trailing slash)")
	}

	jailer_sandbox := base_dir + "jailer_sandbox/"
	cleanup(jailer_sandbox)

	kernelImagePath := base_dir + "kernel/vmlinux-6.1.102"

	const id = "test"

	uid := 123
	gid := 123

	fcCfg := firecracker.Config{
		SocketPath:      "api.socket",
		KernelImagePath: kernelImagePath,
		//console=ttyS0 quiet
		KernelArgs: "console=ttyS0 quiet reboot=k panic=1 pci=off overlay_root=ram init=/sbin/overlay-init",
		// KernelArgs: "console=ttyS0 quiet reboot=k panic=1 pci=off nomodules random.trust_cpu=on i8042.noaux i8042.nomux i8042.nopnp i8042.nokbd overlay_root=ram init=/sbin/overlay-init",
		// KernelArgs: "console=ttyS0 reboot=k panic=1 pci=off init=/sbin/overlay-init",
		Drives:   firecracker.NewDrivesBuilder(base_dir + "rootfs/testing/image.img").Build(),
		LogLevel: "Error",
		MachineCfg: models.MachineConfiguration{
			VcpuCount:  firecracker.Int64(1),
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
			ExecFile:       base_dir + "firecracker-v1.10.1-x86_64",
			CgroupVersion:  "2",
			Stdin:          nil,
			Stdout:         io.Discard,
			Stderr:         io.Discard,
			CgroupArgs:     []string{},
		},
		Seccomp:           firecracker.SeccompConfig{Enabled: true},
		NetworkInterfaces: nil,
		VsockDevices: []firecracker.VsockDevice{
			{
				// ID:   "2",
				Path: "./vsock.sock",
				CID:  3,
			},
		},
	}

	// Mark the rootfs as read-only
	fcCfg.Drives[0].IsReadOnly = firecracker.Bool(true)

	// Check if kernel image is readable
	// f, err := os.Open(fcCfg.KernelImagePath)
	// if err != nil {
	// 	panic(fmt.Errorf("Failed to open kernel image: %v", err))
	// }
	// f.Close()
	timeFunc(func() {
		createAndRunVM(fcCfg)
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

	// send the code
	const VM_TIMEOUT = 10 * time.Second

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
		socket_path := path.Join(jailer_dir, "firecracker-v1.10.1-x86_64", m.Cfg.JailerCfg.ID, "root", "vsock.sock")
		// make a new child context with a timeout
		vmServiceCtx, cancel := context.WithTimeout(vmmCtx, VM_TIMEOUT)
		err := wait_for_vm_service(vmServiceCtx, socket_path, 10*time.Millisecond)
		cancel()
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		out, err := send_code(socket_path, "/bin/bash", "echo 'Hello, World!'")
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		pp.Println(out)
		fmt.Printf("Output: %v\n", string(out.Stdout))
		fmt.Printf("Error: %v\n", string(out.Stderr))
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
