package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"time"

	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

const DEFAULT_MAC = "06:00:AC:10:00:02"

type CodeOutput struct {
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

type Code struct {
	Code string `json:"code"`
	Type string `json:"type"`
}

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
	outputMatcher := regexp.MustCompile("====>([^\n]+)")

	output := bytes.NewBuffer(make([]byte, 0, 10))
	mw := io.MultiWriter(output, os.Stdout)

	create_interface("0")
	defer delete_interface("0")
	// dump_interface("0")
	fcCfg := firecracker.Config{
		SocketPath:      "api.socket",
		KernelImagePath: kernelImagePath,
		//console=ttyS0 quiet
		KernelArgs: "console=ttyS0 quiet reboot=k panic=1 pci=off overlay_root=ram init=/sbin/overlay-init",
		// KernelArgs: "console=ttyS0 reboot=k panic=1 pci=off init=/sbin/overlay-init",
		Drives:   firecracker.NewDrivesBuilder(base_dir + "rootfs/testing/image.img").Build(),
		LogLevel: "Error",
		MachineCfg: models.MachineConfiguration{
			VcpuCount:  firecracker.Int64(1),
			Smt:        firecracker.Bool(false),
			MemSizeMib: firecracker.Int64(256),
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
			Stdin:          os.Stdin,
			Stdout:         mw,
			Stderr:         mw,
		},
		Seccomp:     firecracker.SeccompConfig{Enabled: true},
		MmdsVersion: firecracker.MMDSv2,
		MmdsAddress: net.ParseIP("169.254.169.254"),
		NetworkInterfaces: firecracker.NetworkInterfaces{
			firecracker.NetworkInterface{
				StaticConfiguration: &firecracker.StaticNetworkConfiguration{
					MacAddress:  DEFAULT_MAC,
					HostDevName: "tap0",
				},
				AllowMMDS: true,
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

	// Check each drive is readable and writable
	// for _, drive := range fcCfg.Drives {
	// 	drivePath := firecracker.StringValue(drive.PathOnHost)
	// 	f, err := os.OpenFile(drivePath, os.O_RDWR, 0666)
	// 	if err != nil {
	// 		panic(fmt.Errorf("Failed to open drive with read/write permissions: %v", err))
	// 	}
	// 	f.Close()
	// }

	logrusLogger := logrus.New()
	logrusLogger.SetOutput(os.Stdout)
	logrusLogger.SetLevel(logrus.ErrorLevel)
	logger := logrus.NewEntry(logrusLogger)

	vmmCtx := context.Background()
	m, err := firecracker.NewMachine(vmmCtx, fcCfg, firecracker.WithLogger(logger))
	if err != nil {
		panic(err)
	}

	if err := m.Start(vmmCtx); err != nil {
		panic(err)
	}
	defer m.StopVMM()

	code := Code{Code: "echo hello, world!", Type: "bash"}

	// jsonCode, err := json.Marshal(code)
	// must(err)
	// wait for the VMM to exit
	must(m.SetMetadata(vmmCtx, code))

	timeout := false
	go func() {
		select {
		case <-time.After(10 * time.Second):
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

	codeOut := CodeOutput{}
	if timeout {
		codeOut.Error = "Code Timeout"
		codeOut.ExitCode = 1
	} else {
		match := outputMatcher.FindSubmatch(output.Bytes())
		if match != nil || len(match) >= 1 {
			err := json.Unmarshal(match[1], &codeOut)
			if err != nil {
				fmt.Println("Error unmarshalling output")
			}
		}
	}

	fmt.Println("Exit code:", codeOut.ExitCode)
	fmt.Println("Stdout:")
	fmt.Println(codeOut.Stdout)
	fmt.Println("Stderr:")
	fmt.Println(codeOut.Stderr)

}

func dump_interface(id string) {
	// Dump the tap interface
	// equivalent to `sudo ip tuntap show dev "$TAP_DEV"`
	link, err := netlink.LinkByName("tap" + id)
	must(err)

	for _, attr := range []byte(link.Attrs().HardwareAddr) {
		fmt.Printf("%02x:", attr)
	}
	fmt.Println()
	// out, err := json.MarshalIndent(link.Attrs(), "", "\t")
	// if err != nil {
	// 	panic(err)
	// }
	// link.Attrs()
	// os.WriteFile("interface_attr_correct.json", out, 0644)
}
func create_interface(id string) {
	// Create a tap interface for the firecracker instance
	// equivalent to `sudo ip tuntap add dev "$TAP_DEV" mode tap`

	// Create a new TAP interface
	mac, err := net.ParseMAC(DEFAULT_MAC)
	must(err)
	link := &netlink.Tuntap{
		LinkAttrs: netlink.LinkAttrs{
			Name:         "tap" + id,
			Flags:        net.FlagUp,
			HardwareAddr: mac,
		},
		Mode:       netlink.TUNTAP_MODE_TAP,
		Owner:      uint32(123),
		Group:      uint32(123),
		NonPersist: false,
	}
	must(netlink.LinkAdd(link))
	// Set the interface ip address
	// equivalent to `sudo ip addr add
	_, ipnet, err := net.ParseCIDR("172.16.0.2/24")
	must(err)
	addr := &netlink.Addr{
		IPNet: ipnet,
	}
	must(netlink.AddrAdd(link, addr))
	must(netlink.LinkSetUp(link))

}
func delete_interface(id string) {
	// Delete the tap interface
	// equivalent to `sudo ip link del "$TAP_DEV"`
	link, err := netlink.LinkByName("tap" + id)
	must(err)
	must(netlink.LinkDel(link))
}

func cleanup(jailer_sandbox string) {
	must(os.RemoveAll(jailer_sandbox + "firecracker-v1.10.1-x86_64"))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
