package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/code-slammer/slammer-core/internal/config"
	sandboxruntime "github.com/code-slammer/slammer-core/internal/runtime"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "prepare-image":
		prepareImage(os.Args[2:])
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

func usage() {
	fmt.Fprintf(os.Stderr, "usage:\n  sandboxd prepare-image [flags] IMAGE_REF\n")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
