package rootfsbuilder

import (
	"context"
	"os"
	"path/filepath"
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
