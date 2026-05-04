package config

const (
	DefaultStoreDir       = "/var/lib/sandbox-runtime"
	DefaultKernelPath     = "/opt/sandbox/vmlinux"
	DefaultBootImagePath  = "/opt/sandbox/boot-init.ext4"
	DefaultPlatformOS     = "linux"
	DefaultArchitecture   = "amd64"
	DefaultRootfsMinBytes = 2 << 30
	DefaultRootfsExtra    = 512 << 20
)

type Config struct {
	StoreDir      string
	KernelPath    string
	BootImagePath string
	Platform      Platform
	Rootfs        RootfsConfig
}

type Platform struct {
	OS           string
	Architecture string
}

type RootfsConfig struct {
	MinSizeBytes int64
	MaxSizeBytes int64
	ExtraBytes   int64
	FS           string
}

func Default() Config {
	return Config{
		StoreDir:      DefaultStoreDir,
		KernelPath:    DefaultKernelPath,
		BootImagePath: DefaultBootImagePath,
		Platform: Platform{
			OS:           DefaultPlatformOS,
			Architecture: DefaultArchitecture,
		},
		Rootfs: RootfsConfig{
			MinSizeBytes: DefaultRootfsMinBytes,
			ExtraBytes:   DefaultRootfsExtra,
			FS:           "ext4",
		},
	}
}
