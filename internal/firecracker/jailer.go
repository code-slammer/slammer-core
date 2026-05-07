package firecracker

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"

	sdk "github.com/firecracker-microvm/firecracker-go-sdk"
)

const maxUnixSocketPathLen = 107

func sdkJailerConfig(cfg *JailerConfig, firecrackerPath string, kernelPath string, id string, stdout io.Writer, stderr io.Writer) *sdk.JailerConfig {
	uid := cfg.UID
	gid := cfg.GID
	numaNode := cfg.NumaNode
	return &sdk.JailerConfig{
		UID:            &uid,
		GID:            &gid,
		ID:             id,
		NumaNode:       &numaNode,
		ExecFile:       firecrackerPath,
		JailerBinary:   cfg.Binary,
		ChrootBaseDir:  cfg.ChrootBaseDir,
		ChrootStrategy: sdk.NewNaiveChrootStrategy(kernelPath),
		CgroupVersion:  cfg.CgroupVersion,
		CgroupArgs:     cfg.CgroupArgs,
		Stdout:         stdout,
		Stderr:         stderr,
	}
}

func jailerCommand(ctx context.Context, cfg *JailerConfig, firecrackerPath string, id string, apiSocket string, stdout io.Writer, stderr io.Writer) *exec.Cmd {
	return sdk.NewJailerCommandBuilder().
		WithBin(cfg.Binary).
		WithID(id).
		WithUID(cfg.UID).
		WithGID(cfg.GID).
		WithNumaNode(cfg.NumaNode).
		WithExecFile(firecrackerPath).
		WithChrootBaseDir(cfg.ChrootBaseDir).
		WithCgroupVersion(cfg.CgroupVersion).
		WithCgroupArgs(cfg.CgroupArgs...).
		WithFirecrackerArgs("--api-sock", apiSocket).
		WithStdout(stdout).
		WithStderr(stderr).
		Build(ctx)
}

func jailerRootfsDir(cfg *JailerConfig, firecrackerPath string, id string) string {
	return filepath.Join(JailerVMDir(cfg, firecrackerPath, id), "root")
}

func JailerVMDir(cfg *JailerConfig, firecrackerPath string, id string) string {
	base := cfg.ChrootBaseDir
	if base == "" {
		base = "/srv/jailer"
	}
	return filepath.Join(base, filepath.Base(firecrackerPath), id)
}

func validateUnixSocketPath(path string) error {
	if len(path) <= maxUnixSocketPathLen {
		return nil
	}
	return fmt.Errorf("jailer socket path is too long for Unix sockets (%d > %d): %s; use a shorter --jailer-chroot-base-dir such as /tmp/jailer or /srv/jailer", len(path), maxUnixSocketPathLen, path)
}
