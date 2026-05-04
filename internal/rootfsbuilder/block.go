package rootfsbuilder

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

type BlockImageManager interface {
	CreateSparse(path string, size int64) error
	MakeExt4(ctx context.Context, path string) error
	MountImage(ctx context.Context, path string, target string, readonly bool) (LoopHandle, error)
	Unmount(ctx context.Context, target string) error
}

type LoopHandle interface {
	Close(ctx context.Context) error
}

type ShellBlockImageManager struct{}

func (ShellBlockImageManager) CreateSparse(path string, size int64) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Truncate(size)
}

func (ShellBlockImageManager) MakeExt4(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, "mkfs.ext4", "-F", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mkfs.ext4: %w: %s", err, output)
	}
	return nil
}

func (ShellBlockImageManager) MountImage(ctx context.Context, path string, target string, readonly bool) (LoopHandle, error) {
	args := []string{"--find", "--show"}
	if readonly {
		args = append(args, "--read-only")
	}
	args = append(args, path)
	cmd := exec.CommandContext(ctx, "losetup", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("losetup: %w", err)
	}
	loop := string(trimSpace(output))
	mountArgs := []string{}
	if readonly {
		mountArgs = append(mountArgs, "-o", "ro,noload")
	}
	mountArgs = append(mountArgs, loop, target)
	cmd = exec.CommandContext(ctx, "mount", mountArgs...)
	if output, err := cmd.CombinedOutput(); err != nil {
		_ = exec.CommandContext(ctx, "losetup", "-d", loop).Run()
		return nil, fmt.Errorf("mount: %w: %s", err, output)
	}
	return shellLoopHandle{device: loop}, nil
}

func (ShellBlockImageManager) Unmount(ctx context.Context, target string) error {
	cmd := exec.CommandContext(ctx, "umount", target)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("umount: %w: %s", err, output)
	}
	return nil
}

type shellLoopHandle struct {
	device string
}

func (h shellLoopHandle) Close(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "losetup", "-d", h.device)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("losetup detach: %w: %s", err, output)
	}
	return nil
}

func trimSpace(input []byte) []byte {
	for len(input) > 0 && (input[len(input)-1] == '\n' || input[len(input)-1] == '\r' || input[len(input)-1] == '\t' || input[len(input)-1] == ' ') {
		input = input[:len(input)-1]
	}
	for len(input) > 0 && (input[0] == '\n' || input[0] == '\r' || input[0] == '\t' || input[0] == ' ') {
		input = input[1:]
	}
	return input
}
