package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

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
	CacheHit       bool
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
	if prepared, err := r.preparedFromCache(store, ref, platform); err == nil {
		return prepared, nil
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
		CacheHit:       false,
	}, nil
}

type imageManifest struct {
	Config struct {
		Digest string `json:"digest"`
	} `json:"config"`
}

func (r *Runtime) preparedFromCache(store *contentstore.Store, ref string, platform Platform) (*PreparedImage, error) {
	var refMeta contentstore.RefMetadata
	if err := store.ReadJSON(store.RefPath(ref), &refMeta); err != nil {
		return nil, err
	}
	if refMeta.Platform.OS != platform.OS || refMeta.Platform.Architecture != platform.Architecture {
		return nil, fmt.Errorf("cached ref platform mismatch")
	}
	var manifest imageManifest
	if err := store.ReadJSON(store.ManifestPath(refMeta.ManifestDigest), &manifest); err != nil {
		return nil, err
	}
	if manifest.Config.Digest == "" {
		return nil, fmt.Errorf("cached manifest has no config digest")
	}
	configPath := store.ConfigPath(manifest.Config.Digest)
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var imageConfig oci.ImageConfig
	if err := json.Unmarshal(configBytes, &imageConfig); err != nil {
		return nil, err
	}
	chainID, err := rootfsbuilder.ComputeChainID(imageConfig.RootFS.DiffIDs)
	if err != nil {
		return nil, err
	}
	rootfsPath := store.RootfsPath(chainID)
	if _, err := os.Stat(rootfsPath); err != nil {
		return nil, err
	}
	return &PreparedImage{
		ImageRef:       ref,
		ManifestDigest: refMeta.ManifestDigest,
		ChainID:        chainID,
		RootfsPath:     rootfsPath,
		OCIConfigPath:  configPath,
		CacheHit:       true,
	}, nil
}
