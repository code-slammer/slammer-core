package server

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/code-slammer/slammer-core/internal/config"
	"github.com/code-slammer/slammer-core/internal/firecracker"
)

const DefaultListen = "127.0.0.1:8080"

type Config struct {
	Listen          string                    `json:"listen"`
	StoreDir        string                    `json:"store_dir"`
	KernelPath      string                    `json:"kernel_path"`
	BootImagePath   string                    `json:"boot_image_path"`
	FirecrackerPath string                    `json:"firecracker_path"`
	SnapshotDir     string                    `json:"snapshot_dir"`
	Machine         firecracker.MachineConfig `json:"machine"`
	Jailer          *JailerConfig             `json:"jailer,omitempty"`
	Limits          Limits                    `json:"limits"`
	CleanupJailer   bool                      `json:"cleanup_jailer"`
}

type JailerConfig struct {
	Binary        string   `json:"binary"`
	ChrootBaseDir string   `json:"chroot_base_dir"`
	UID           int      `json:"uid"`
	GID           int      `json:"gid"`
	NumaNode      int      `json:"numa_node"`
	CgroupVersion string   `json:"cgroup_version"`
	CgroupArgs    []string `json:"cgroup_args"`
}

type Limits struct {
	MaxTimeoutMillis  int64 `json:"max_timeout_millis"`
	MaxOutputBytes    int64 `json:"max_output_bytes"`
	MaxVCPUCount      int   `json:"max_vcpu_count"`
	MaxMemSizeMiB     int   `json:"max_mem_size_mib"`
	MaxTasks          int   `json:"max_tasks"`
	MaxWriteFileBytes int64 `json:"max_write_file_bytes"`
}

func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	contents, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	if err := json.Unmarshal(contents, &cfg); err != nil {
		return Config{}, err
	}
	applyDefaults(&cfg)
	return cfg, cfg.Validate()
}

func DefaultConfig() Config {
	cfg := Config{
		Listen:          DefaultListen,
		StoreDir:        config.DefaultStoreDir,
		KernelPath:      config.DefaultKernelPath,
		BootImagePath:   config.DefaultBootImagePath,
		FirecrackerPath: "firecracker",
		SnapshotDir:     config.DefaultStoreDir + "/snapshots",
		Machine:         firecracker.MachineConfig{VCPUCount: 1, MemSizeMiB: 256},
		Limits: Limits{
			MaxTimeoutMillis:  60_000,
			MaxOutputBytes:    4 << 20,
			MaxVCPUCount:      4,
			MaxMemSizeMiB:     2048,
			MaxTasks:          32,
			MaxWriteFileBytes: 4 << 20,
		},
	}
	applyDefaults(&cfg)
	return cfg
}

func applyDefaults(cfg *Config) {
	if cfg.Listen == "" {
		cfg.Listen = DefaultListen
	}
	if cfg.StoreDir == "" {
		cfg.StoreDir = config.DefaultStoreDir
	}
	if cfg.KernelPath == "" {
		cfg.KernelPath = config.DefaultKernelPath
	}
	if cfg.BootImagePath == "" {
		cfg.BootImagePath = config.DefaultBootImagePath
	}
	if cfg.FirecrackerPath == "" {
		cfg.FirecrackerPath = "firecracker"
	}
	if cfg.SnapshotDir == "" {
		cfg.SnapshotDir = cfg.StoreDir + "/snapshots"
	}
	if cfg.Machine.VCPUCount <= 0 {
		cfg.Machine.VCPUCount = 1
	}
	if cfg.Machine.MemSizeMiB <= 0 {
		cfg.Machine.MemSizeMiB = 256
	}
	if cfg.Limits.MaxTimeoutMillis <= 0 {
		cfg.Limits.MaxTimeoutMillis = int64((60 * time.Second).Milliseconds())
	}
	if cfg.Limits.MaxOutputBytes <= 0 {
		cfg.Limits.MaxOutputBytes = 4 << 20
	}
	if cfg.Limits.MaxVCPUCount <= 0 {
		cfg.Limits.MaxVCPUCount = cfg.Machine.VCPUCount
	}
	if cfg.Limits.MaxMemSizeMiB <= 0 {
		cfg.Limits.MaxMemSizeMiB = cfg.Machine.MemSizeMiB
	}
	if cfg.Limits.MaxTasks <= 0 {
		cfg.Limits.MaxTasks = 32
	}
	if cfg.Limits.MaxWriteFileBytes <= 0 {
		cfg.Limits.MaxWriteFileBytes = 4 << 20
	}
	if cfg.Jailer != nil && cfg.Jailer.CgroupVersion == "" {
		cfg.Jailer.CgroupVersion = "2"
	}
}

func (cfg Config) Validate() error {
	if cfg.Listen == "" {
		return fmt.Errorf("listen is required")
	}
	if cfg.StoreDir == "" || cfg.KernelPath == "" || cfg.BootImagePath == "" || cfg.FirecrackerPath == "" || cfg.SnapshotDir == "" {
		return fmt.Errorf("store_dir, kernel_path, boot_image_path, firecracker_path, and snapshot_dir are required")
	}
	if cfg.Machine.VCPUCount > cfg.Limits.MaxVCPUCount || cfg.Machine.MemSizeMiB > cfg.Limits.MaxMemSizeMiB {
		return fmt.Errorf("default machine config exceeds configured limits")
	}
	if cfg.Jailer != nil && cfg.Jailer.Binary == "" {
		return fmt.Errorf("jailer.binary is required when jailer is configured")
	}
	return nil
}

func (cfg Config) RuntimeConfig() config.Config {
	return config.Config{
		StoreDir:      cfg.StoreDir,
		KernelPath:    cfg.KernelPath,
		BootImagePath: cfg.BootImagePath,
		Platform: config.Platform{
			OS:           "linux",
			Architecture: runtime.GOARCH,
		},
	}
}

func (cfg Config) FirecrackerJailer() *firecracker.JailerConfig {
	if cfg.Jailer == nil {
		return nil
	}
	return &firecracker.JailerConfig{
		Binary:        cfg.Jailer.Binary,
		ChrootBaseDir: cfg.Jailer.ChrootBaseDir,
		UID:           cfg.Jailer.UID,
		GID:           cfg.Jailer.GID,
		NumaNode:      cfg.Jailer.NumaNode,
		CgroupVersion: cfg.Jailer.CgroupVersion,
		CgroupArgs:    cfg.Jailer.CgroupArgs,
	}
}
