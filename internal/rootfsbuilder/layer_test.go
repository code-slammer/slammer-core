package rootfsbuilder

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestLayerApplierWhiteoutsAndOpaqueDirs(t *testing.T) {
	root := t.TempDir()
	layer1, diff1 := writeTestLayer(t, []testEntry{
		{path: "a.txt", body: "hello"},
		{path: "dir/b.txt", body: "old"},
		{path: "dir/c.txt", body: "old"},
	})
	layer2, diff2 := writeTestLayer(t, []testEntry{
		{path: ".wh.a.txt", body: ""},
		{path: "dir/.wh..wh..opq", body: ""},
		{path: "dir/d.txt", body: "new"},
	})

	applier := LayerApplier{Root: root}
	if err := applier.ApplyLayer(context.Background(), layer1, diff1); err != nil {
		t.Fatal(err)
	}
	if err := applier.ApplyLayer(context.Background(), layer2, diff2); err != nil {
		t.Fatal(err)
	}

	assertMissing(t, filepath.Join(root, "a.txt"))
	assertMissing(t, filepath.Join(root, "dir", "b.txt"))
	assertMissing(t, filepath.Join(root, "dir", "c.txt"))
	assertFile(t, filepath.Join(root, "dir", "d.txt"), "new")
}

func TestLayerApplierRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	layer, diffID := writeTestLayer(t, []testEntry{{path: "../escape", body: "bad"}})
	if err := (LayerApplier{Root: root}).ApplyLayer(context.Background(), layer, diffID); err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestLayerApplierRejectsSymlinkParent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	layer, diffID := writeTestLayer(t, []testEntry{{path: "link/file", body: "bad"}})
	if err := (LayerApplier{Root: root}).ApplyLayer(context.Background(), layer, diffID); err == nil {
		t.Fatal("expected symlink parent rejection")
	}
}

func TestLayerApplierVerifiesDiffID(t *testing.T) {
	root := t.TempDir()
	layer, _ := writeTestLayer(t, []testEntry{{path: "file", body: "hello"}})
	bad := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := (LayerApplier{Root: root}).ApplyLayer(context.Background(), layer, bad); err == nil {
		t.Fatal("expected diffID mismatch")
	}
}

type testEntry struct {
	path string
	body string
}

func writeTestLayer(t *testing.T, entries []testEntry) (string, string) {
	t.Helper()
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	for _, entry := range entries {
		header := &tar.Header{
			Name: entry.path,
			Mode: 0o644,
			Size: int64(len(entry.body)),
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw.Bytes())
	diffID := "sha256:" + hex.EncodeToString(hash[:])

	path := filepath.Join(t.TempDir(), "layer.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	if _, err := gz.Write(raw.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path, diffID
}

func assertFile(t *testing.T, path string, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be absent, err=%v", path, err)
	}
}
