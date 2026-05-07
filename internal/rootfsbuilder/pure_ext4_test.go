package rootfsbuilder

import (
	"archive/tar"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/code-slammer/slammer-core/internal/oci"
	"github.com/diskfs/go-diskfs/backend/file"
	"github.com/diskfs/go-diskfs/filesystem/ext4"
)

func TestPureGoExt4BuilderBuildsReadableImage(t *testing.T) {
	layer, diffID := writeTestLayer(t, []testEntry{{path: "etc/hello.txt", body: "hello"}})
	imagePath := filepath.Join(t.TempDir(), "rootfs.ext4")
	const imageSize = 512 << 20
	img := &oci.PulledImage{
		Config: oci.ImageConfig{RootFS: oci.RootFS{DiffIDs: []string{diffID}}},
		Layers: []oci.Layer{{DiffID: diffID, CompressedBlobPath: layer, Size: 1024}},
	}

	if err := (PureGoExt4Builder{}).Build(context.Background(), img, imagePath, imageSize); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(imagePath); err != nil {
		t.Fatal(err)
	} else if info.Size() != imageSize {
		t.Fatalf("image size = %d", info.Size())
	}

	storage, err := file.OpenFromPath(imagePath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	fsys, err := ext4.Read(storage, imageSize, 0, 512)
	if err != nil {
		t.Fatal(err)
	}
	defer fsys.Close()
	contents, err := fsys.ReadFile("etc/hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "hello" {
		t.Fatalf("contents = %q", contents)
	}
}

func TestPureGoExt4BuilderWritesLargeFile(t *testing.T) {
	body := strings.Repeat("x", 1<<20)
	layer, diffID := writeTestLayer(t, []testEntry{{path: "usr/bin/coreutils", body: body}})
	imagePath := filepath.Join(t.TempDir(), "rootfs.ext4")
	const imageSize = 512 << 20
	img := &oci.PulledImage{
		Config: oci.ImageConfig{RootFS: oci.RootFS{DiffIDs: []string{diffID}}},
		Layers: []oci.Layer{{DiffID: diffID, CompressedBlobPath: layer, Size: 1024}},
	}

	if err := (PureGoExt4Builder{}).Build(context.Background(), img, imagePath, imageSize); err != nil {
		t.Fatal(err)
	}

	storage, err := file.OpenFromPath(imagePath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	fsys, err := ext4.Read(storage, imageSize, 0, 512)
	if err != nil {
		t.Fatal(err)
	}
	defer fsys.Close()
	contents, err := fsys.ReadFile("usr/bin/coreutils")
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != body {
		t.Fatalf("large file length = %d, want %d", len(contents), len(body))
	}
}

func TestPureGoExt4BuilderMaterializesHardlinkAsFile(t *testing.T) {
	layer, diffID := writeTestLayer(t, []testEntry{
		{path: "usr/bin/perl", body: "#!/usr/bin/perl\n"},
		{path: "usr/bin/perl5.40.1", typeflag: tar.TypeLink, linkname: "usr/bin/perl"},
	})
	imagePath := filepath.Join(t.TempDir(), "rootfs.ext4")
	const imageSize = 512 << 20
	img := &oci.PulledImage{
		Config: oci.ImageConfig{RootFS: oci.RootFS{DiffIDs: []string{diffID}}},
		Layers: []oci.Layer{{DiffID: diffID, CompressedBlobPath: layer, Size: 1024}},
	}

	if err := (PureGoExt4Builder{}).Build(context.Background(), img, imagePath, imageSize); err != nil {
		t.Fatal(err)
	}

	storage, err := file.OpenFromPath(imagePath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	fsys, err := ext4.Read(storage, imageSize, 0, 512)
	if err != nil {
		t.Fatal(err)
	}
	defer fsys.Close()
	contents, err := fsys.ReadFile("usr/bin/perl5.40.1")
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "#!/usr/bin/perl\n" {
		t.Fatalf("hardlink contents = %q", contents)
	}
}

func TestPureGoExt4BuilderHandlesDuplicateSymlink(t *testing.T) {
	layer, diffID := writeTestLayer(t, []testEntry{
		{path: "bin", typeflag: tar.TypeSymlink, linkname: "usr/bin", mode: 0o777},
		{path: "bin", typeflag: tar.TypeSymlink, linkname: "usr/bin", mode: 0o777},
	})
	imagePath := filepath.Join(t.TempDir(), "rootfs.ext4")
	const imageSize = 512 << 20
	img := &oci.PulledImage{
		Config: oci.ImageConfig{RootFS: oci.RootFS{DiffIDs: []string{diffID}}},
		Layers: []oci.Layer{{DiffID: diffID, CompressedBlobPath: layer, Size: 1024}},
	}

	if err := (PureGoExt4Builder{}).Build(context.Background(), img, imagePath, imageSize); err != nil {
		t.Fatal(err)
	}
}
