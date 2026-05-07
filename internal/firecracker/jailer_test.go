package firecracker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJailedBackingFilePathPreservesSnapshotPath(t *testing.T) {
	rootfs := filepath.Join(string(filepath.Separator), "srv", "jailer", "fc", "vm", "root")
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "relative path",
			path: "./tmp/boot-init.ext4",
			want: filepath.Join(rootfs, "tmp", "boot-init.ext4"),
		},
		{
			name: "absolute path",
			path: "/home/user/slammer-core/tmp/rootfs.ext4",
			want: filepath.Join(rootfs, "home", "user", "slammer-core", "tmp", "rootfs.ext4"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jailedBackingFilePath(rootfs, tt.path); got != tt.want {
				t.Fatalf("jailedBackingFilePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLinkJailedDriveProvidesMappedPathAndBasename(t *testing.T) {
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src", "tmp")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(srcDir, "boot-init.ext4")
	if err := os.WriteFile(src, []byte("drive"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootfs := filepath.Join(dir, "jail", "root")
	if err := linkJailedDrive(src, rootfs); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		jailedBackingFilePath(rootfs, src),
		filepath.Join(rootfs, filepath.Base(src)),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected linked drive at %s: %v", path, err)
		}
	}
}
