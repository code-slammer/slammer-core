package manager

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/code-slammer/slammer-core/internal/oci"
)

func TestSnapshotArtifactUsesChainID(t *testing.T) {
	snapshot := SnapshotArtifact("/tmp/snapshots", "sha256:abc123")
	if snapshot.MemPath != filepath.Join("/tmp/snapshots", "abc123.mem") {
		t.Fatalf("MemPath = %q", snapshot.MemPath)
	}
	if snapshot.SnapshotPath != filepath.Join("/tmp/snapshots", "abc123.snapshot") {
		t.Fatalf("SnapshotPath = %q", snapshot.SnapshotPath)
	}
	if snapshot.WorkspaceDir != filepath.Join("/tmp/snapshots", "abc123-workspace") {
		t.Fatalf("WorkspaceDir = %q", snapshot.WorkspaceDir)
	}
}

func TestSnapshotExistsRequiresMemoryAndState(t *testing.T) {
	dir := t.TempDir()
	snapshot := SnapshotArtifact(dir, "sha256:def456")
	if SnapshotExists(snapshot) {
		t.Fatal("snapshot should not exist before files are created")
	}
	if err := os.WriteFile(snapshot.MemPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if SnapshotExists(snapshot) {
		t.Fatal("snapshot should not exist with only memory file")
	}
	if err := os.WriteFile(snapshot.SnapshotPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !SnapshotExists(snapshot) {
		t.Fatal("snapshot should exist with memory and state files")
	}
}

func TestDefaultCommandCombinesEntrypointAndCmd(t *testing.T) {
	cfg := &oci.ImageConfig{}
	cfg.Config.Entrypoint = []string{"/bin/sh", "-c"}
	cfg.Config.Cmd = []string{"echo hi"}
	got := defaultCommand(cfg)
	want := []string{"/bin/sh", "-c", "echo hi"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaultCommand() = %#v, want %#v", got, want)
	}
	cfg.Config.Entrypoint[0] = "changed"
	if got[0] != "/bin/sh" {
		t.Fatal("defaultCommand should return an independent argv slice")
	}
}
