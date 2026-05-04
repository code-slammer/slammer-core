package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	targetMount = "/mnt/target"
	overlayBase = "/mnt/overlay"
	mergedRoot  = "/mnt/overlay/merged"
)

func main() {
	log.SetPrefix("init: ")
	log.SetFlags(0)
	os.Setenv("PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")

	if err := run(); err != nil {
		log.Printf("fatal: %v", err)
		time.Sleep(250 * time.Millisecond)
	}
	poweroff()
}

func run() error {
	args := kernelArgs()
	targetDrive := args["target_drive"]
	if targetDrive == "" {
		targetDrive = "/dev/vdb"
	}

	if err := basicMounts(); err != nil {
		return err
	}
	if err := setupOverlay(targetDrive); err != nil {
		return err
	}
	if err := prepareMergedRoot(); err != nil {
		return err
	}
	return superviseAgent()
}

func basicMounts() error {
	for _, dir := range []string{"/proc", "/sys", "/dev", "/run", "/tmp", targetMount, overlayBase} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := mountIgnoreBusy("proc", "/proc", "proc", unix.MS_NOSUID|unix.MS_NOEXEC|unix.MS_NODEV, ""); err != nil {
		return err
	}
	if err := mountIgnoreBusy("sysfs", "/sys", "sysfs", unix.MS_NOSUID|unix.MS_NOEXEC|unix.MS_NODEV, ""); err != nil {
		return err
	}
	if err := mountIgnoreBusy("devtmpfs", "/dev", "devtmpfs", 0, "mode=0755"); err != nil {
		if err := mountIgnoreBusy("tmpfs", "/dev", "tmpfs", unix.MS_NOSUID, "mode=0755"); err != nil {
			return err
		}
	}
	if err := mountIgnoreBusy("tmpfs", "/run", "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, "mode=0755,size=64m"); err != nil {
		return err
	}
	return mountIgnoreBusy("tmpfs", "/tmp", "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, "mode=1777,size=128m")
}

func setupOverlay(targetDrive string) error {
	if err := unix.Mount(targetDrive, targetMount, "ext4", unix.MS_RDONLY|unix.MS_NODEV, "ro,noload"); err != nil {
		return fmt.Errorf("mount target drive %s: %w", targetDrive, err)
	}
	if err := unix.Mount("tmpfs", overlayBase, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, "mode=0755,size=512m"); err != nil {
		return fmt.Errorf("mount overlay tmpfs: %w", err)
	}
	for _, dir := range []string{"upper", "work", "merged"} {
		if err := os.MkdirAll(overlayBase+"/"+dir, 0o755); err != nil {
			return err
		}
	}
	data := "lowerdir=/mnt/target,upperdir=/mnt/overlay/upper,workdir=/mnt/overlay/work"
	if err := unix.Mount("overlay", mergedRoot, "overlay", 0, data); err != nil {
		return fmt.Errorf("mount overlay: %w", err)
	}
	return nil
}

func prepareMergedRoot() error {
	for _, dir := range []string{"proc", "sys", "dev", "run", "tmp", ".sandbox"} {
		mode := os.FileMode(0o755)
		if dir == "tmp" {
			mode = 0o1777
		}
		if err := os.MkdirAll(mergedRoot+"/"+dir, mode); err != nil {
			return err
		}
	}
	for _, mount := range []struct{ source, target string }{
		{"/proc", mergedRoot + "/proc"},
		{"/sys", mergedRoot + "/sys"},
		{"/dev", mergedRoot + "/dev"},
	} {
		if err := unix.Mount(mount.source, mount.target, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
			return fmt.Errorf("bind %s: %w", mount.source, err)
		}
	}
	if err := unix.Mount("tmpfs", mergedRoot+"/run", "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, "mode=0755,size=64m"); err != nil {
		return err
	}
	if err := unix.Mount("tmpfs", mergedRoot+"/tmp", "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, "mode=1777,size=128m"); err != nil {
		return err
	}
	agentTarget := mergedRoot + "/.sandbox/agent"
	file, err := os.OpenFile(agentTarget, os.O_CREATE|os.O_RDONLY, 0o755)
	if err != nil {
		return err
	}
	_ = file.Close()
	if err := unix.Mount("/agent", agentTarget, "", unix.MS_BIND, ""); err != nil {
		return fmt.Errorf("bind /agent: %w", err)
	}
	return nil
}

func superviseAgent() error {
	cmd := exec.Command("/.sandbox/agent", "-shutdown-after-jobs=true")
	cmd.Dir = "/"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Chroot: mergedRoot, Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start agent: %w", err)
	}
	agentPID := cmd.Process.Pid
	for {
		var status unix.WaitStatus
		pid, err := unix.Wait4(-1, &status, 0, nil)
		if err != nil {
			if err == unix.ECHILD {
				return nil
			}
			continue
		}
		if pid == agentPID {
			if status.Exited() && status.ExitStatus() != 0 {
				log.Printf("agent exited with status %d", status.ExitStatus())
			}
			if status.Signaled() {
				log.Printf("agent killed by signal %s", status.Signal())
			}
			return nil
		}
	}
}

func kernelArgs() map[string]string {
	contents, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return map[string]string{}
	}
	result := make(map[string]string)
	for _, field := range strings.Fields(string(bytes.TrimSpace(contents))) {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			result[field] = ""
			continue
		}
		result[key] = value
	}
	return result
}

func mountIgnoreBusy(source, target, fstype string, flags uintptr, data string) error {
	if err := unix.Mount(source, target, fstype, flags, data); err != nil && err != unix.EBUSY {
		return fmt.Errorf("mount %s: %w", target, err)
	}
	return nil
}

func poweroff() {
	unix.Sync()
	_ = unix.Reboot(unix.LINUX_REBOOT_CMD_POWER_OFF)
	_ = unix.Reboot(unix.LINUX_REBOOT_CMD_RESTART)
}
