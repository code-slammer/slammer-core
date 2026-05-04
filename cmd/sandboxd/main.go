package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/code-slammer/slammer-core/internal/agentapi"
	"github.com/code-slammer/slammer-core/internal/agentclient"
	"github.com/code-slammer/slammer-core/internal/agentserver"
	"github.com/code-slammer/slammer-core/internal/config"
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
	default:
		usage()
		os.Exit(2)
	}
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

func usage() {
	fmt.Fprintf(os.Stderr, "usage:\n  sandboxd prepare-image [flags] IMAGE_REF\n  sandboxd inspect-rootfs [--ls PATH] [--read PATH] ROOTFS_EXT4\n  sandboxd demo-local [flags]\n")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
