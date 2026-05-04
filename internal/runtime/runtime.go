package runtime

import (
	"context"
	"fmt"

	"github.com/code-slammer/slammer-core/internal/config"
	"github.com/code-slammer/slammer-core/internal/contentstore"
	"github.com/code-slammer/slammer-core/internal/oci"
	"github.com/code-slammer/slammer-core/internal/rootfsbuilder"
)

type Platform = config.Platform

type PreparedImage struct {
	ImageRef       string
	ManifestDigest string
	ChainID        string
	RootfsPath     string
	OCIConfigPath  string
}

type Runtime struct {
	Config config.Config
}

func New(cfg config.Config) *Runtime {
	defaults := config.Default()
	if cfg.StoreDir == "" {
		cfg.StoreDir = defaults.StoreDir
	}
	if cfg.KernelPath == "" {
		cfg.KernelPath = defaults.KernelPath
	}
	if cfg.BootImagePath == "" {
		cfg.BootImagePath = defaults.BootImagePath
	}
	if cfg.Platform.OS == "" {
		cfg.Platform.OS = defaults.Platform.OS
	}
	if cfg.Platform.Architecture == "" {
		cfg.Platform.Architecture = defaults.Platform.Architecture
	}
	if cfg.Rootfs.MinSizeBytes == 0 {
		cfg.Rootfs.MinSizeBytes = defaults.Rootfs.MinSizeBytes
	}
	if cfg.Rootfs.ExtraBytes == 0 {
		cfg.Rootfs.ExtraBytes = defaults.Rootfs.ExtraBytes
	}
	if cfg.Rootfs.FS == "" {
		cfg.Rootfs.FS = defaults.Rootfs.FS
	}
	return &Runtime{Config: cfg}
}

func (r *Runtime) PrepareImage(ctx context.Context, ref string, platform Platform) (*PreparedImage, error) {
	if ref == "" {
		return nil, fmt.Errorf("image ref is required")
	}
	if platform.OS == "" {
		platform.OS = r.Config.Platform.OS
	}
	if platform.Architecture == "" {
		platform.Architecture = r.Config.Platform.Architecture
	}

	store := contentstore.New(r.Config.StoreDir)
	if err := store.Init(); err != nil {
		return nil, err
	}

	pulled, err := oci.Pull(ctx, store, ref, platform)
	if err != nil {
		return nil, err
	}

	rootfsStore := rootfsbuilder.New(r.Config.StoreDir)
	rootfs, err := rootfsStore.EnsureRootfs(ctx, pulled)
	if err != nil {
		return nil, err
	}

	return &PreparedImage{
		ImageRef:       ref,
		ManifestDigest: pulled.ManifestDigest,
		ChainID:        rootfs.ChainID,
		RootfsPath:     rootfs.Path,
		OCIConfigPath:  store.ConfigPath(pulled.ConfigDigest),
	}, nil
}
