package rootfsbuilder

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/code-slammer/slammer-core/internal/oci"
	"github.com/diskfs/go-diskfs/backend/file"
	"github.com/diskfs/go-diskfs/filesystem/ext4"
)

type RootfsImageBuilder interface {
	Build(ctx context.Context, img *oci.PulledImage, imagePath string, size int64) error
}

type PureGoExt4Builder struct{}

func (PureGoExt4Builder) Build(ctx context.Context, img *oci.PulledImage, imagePath string, size int64) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("ext4 rootfs build panic: %v", recovered)
		}
	}()
	storage, err := file.CreateFromPath(imagePath, size)
	if err != nil {
		return err
	}
	defer storage.Close()

	filesystem, err := ext4.Create(storage, size, 0, 512, &ext4.Params{
		VolumeName:      "sandbox-rootfs",
		SectorsPerBlock: 8,
	})
	if err != nil {
		return err
	}
	defer filesystem.Close()

	applier := DiskfsLayerApplier{FS: filesystem, Symlinks: map[string]bool{}}
	for _, layer := range img.Layers {
		if err := applier.ApplyLayer(ctx, layer.CompressedBlobPath, layer.DiffID); err != nil {
			return err
		}
	}
	return nil
}

type DiskfsLayerApplier struct {
	FS       *ext4.FileSystem
	Symlinks map[string]bool
}

func (a DiskfsLayerApplier) ApplyLayer(ctx context.Context, compressedLayerPath string, expectedDiffID string) error {
	if a.Symlinks == nil {
		a.Symlinks = map[string]bool{}
	}
	file, err := os.Open(compressedLayerPath)
	if err != nil {
		return err
	}
	defer file.Close()

	reader, err := layerReader(file)
	if err != nil {
		return err
	}
	defer reader.Close()

	hash := sha256.New()
	hashedReader := io.TeeReader(reader, hash)
	tarReader := tar.NewReader(hashedReader)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if err := a.applyEntry(header, tarReader); err != nil {
			return fmt.Errorf("apply %q: %w", header.Name, err)
		}
	}
	if _, err := io.Copy(io.Discard, hashedReader); err != nil {
		return err
	}

	actual := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if actual != expectedDiffID {
		return fmt.Errorf("diffID mismatch: got %s want %s", actual, expectedDiffID)
	}
	return nil
}

func (a DiskfsLayerApplier) applyEntry(header *tar.Header, reader io.Reader) error {
	name, err := cleanLayerPath(header.Name)
	if err != nil {
		return err
	}
	base := path.Base(name)
	dir := path.Dir(name)

	if base == ".wh..wh..opq" {
		return a.removeChildren(fsPath(dir))
	}
	if strings.HasPrefix(base, ".wh.") {
		return a.removeAll(fsPath(path.Join(dir, strings.TrimPrefix(base, ".wh."))))
	}

	target := fsPath(name)
	if err := a.ensureNoSymlinkParents(target); err != nil {
		return err
	}

	switch header.Typeflag {
	case tar.TypeDir:
		return a.mkdir(target, header)
	case tar.TypeReg, tar.TypeRegA:
		return a.writeFile(target, header, reader)
	case tar.TypeSymlink:
		return a.symlink(target, header)
	case tar.TypeLink:
		return a.hardlink(target, header)
	case tar.TypeXGlobalHeader, tar.TypeXHeader, tar.TypeGNULongName, tar.TypeGNULongLink:
		return nil
	case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
		return fmt.Errorf("device and fifo entries are not allowed")
	default:
		return fmt.Errorf("unsupported tar entry type %d", header.Typeflag)
	}
}

func (a DiskfsLayerApplier) mkdir(target string, header *tar.Header) error {
	if err := a.mkdirAll(target); err != nil {
		return err
	}
	return a.applyMetadata(target, header, false)
}

func (a DiskfsLayerApplier) writeFile(target string, header *tar.Header, reader io.Reader) error {
	if err := a.mkdirAll(path.Dir(target)); err != nil {
		return err
	}
	if err := a.ensureNoSymlinkParents(target); err != nil {
		return err
	}
	if a.Symlinks[target] {
		return fmt.Errorf("refusing to overwrite symlink")
	}
	if a.Symlinks[target] {
		delete(a.Symlinks, target)
		if err := a.FS.Remove(target); err != nil {
			return err
		}
	}
	file, err := a.FS.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC)
	if err != nil {
		return err
	}
	contents, readErr := io.ReadAll(io.LimitReader(reader, header.Size))
	if readErr != nil {
		_ = file.Close()
		return readErr
	}
	_, copyErr := file.Write(contents)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return a.applyMetadata(target, header, false)
}

func (a DiskfsLayerApplier) symlink(target string, header *tar.Header) error {
	if _, err := cleanLinkPath(header.Linkname); err != nil {
		return fmt.Errorf("unsafe symlink target: %w", err)
	}
	if err := a.mkdirAll(path.Dir(target)); err != nil {
		return err
	}
	if existing, err := a.FS.ReadLink(target); err == nil {
		a.Symlinks[target] = true
		if existing == header.Linkname {
			return a.applyMetadata(target, header, true)
		}
		return fmt.Errorf("refusing to replace symlink %q -> %q with %q", target, existing, header.Linkname)
	}
	if err := a.removeIfExists(target); err != nil {
		return err
	}
	if err := a.FS.Symlink(header.Linkname, target); err != nil {
		return err
	}
	a.Symlinks[target] = true
	return a.applyMetadata(target, header, true)
}

func (a DiskfsLayerApplier) hardlink(target string, header *tar.Header) error {
	linkName, err := cleanLayerPath(header.Linkname)
	if err != nil {
		return fmt.Errorf("unsafe hardlink target: %w", err)
	}
	linkTarget := fsPath(linkName)
	if err := a.ensureNoSymlinkParents(target); err != nil {
		return err
	}
	if err := a.ensureNoSymlinkParents(linkTarget); err != nil {
		return err
	}
	if a.Symlinks[linkTarget] {
		return fmt.Errorf("refusing hardlink to symlink")
	}
	if _, err := a.FS.Stat(linkTarget); err != nil {
		return err
	}
	if err := a.mkdirAll(path.Dir(target)); err != nil {
		return err
	}
	if err := a.removeIfExists(target); err != nil {
		return err
	}
	contents, err := a.FS.ReadFile(linkTarget)
	if err != nil {
		return err
	}
	file, err := a.FS.OpenFile(target, os.O_CREATE|os.O_RDWR)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(contents)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return a.applyMetadata(target, header, false)
}

func (a DiskfsLayerApplier) mkdirAll(target string) error {
	target = path.Clean(target)
	if target == "/" || target == "." {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(target, "/"), "/")
	current := ""
	for _, part := range parts {
		if current == "" {
			current = part
		} else {
			current = path.Join(current, part)
		}
		if _, err := a.FS.Stat(current); err == nil {
			continue
		}
		if err := a.FS.Mkdir(current); err != nil {
			if _, statErr := a.FS.Stat(current); statErr == nil {
				continue
			}
			return fmt.Errorf("mkdir %q: %w", current, err)
		}
	}
	return nil
}

func (a DiskfsLayerApplier) removeIfExists(target string) error {
	if _, err := a.FS.Stat(target); err == nil {
		return a.removeAll(target)
	}
	if a.Symlinks[target] {
		delete(a.Symlinks, target)
		return a.FS.Remove(target)
	}
	return nil
}

func (a DiskfsLayerApplier) removeAll(target string) error {
	if target == "/" || target == "." {
		return fmt.Errorf("refusing to remove root")
	}
	if a.Symlinks[target] {
		delete(a.Symlinks, target)
		return a.FS.Remove(target)
	}
	info, err := a.FS.Stat(target)
	if errors.Is(err, fs.ErrNotExist) || err != nil && strings.Contains(err.Error(), "file does not exist") {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := a.removeChildren(target); err != nil {
			return err
		}
	}
	return a.FS.Remove(target)
}

func (a DiskfsLayerApplier) removeChildren(target string) error {
	entries, err := a.FS.ReadDir(target)
	if errors.Is(err, fs.ErrNotExist) || err != nil && strings.Contains(err.Error(), "file does not exist") {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := a.removeAll(path.Join(target, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (a DiskfsLayerApplier) ensureNoSymlinkParents(target string) error {
	dir := path.Dir(path.Clean(target))
	if dir == "/" || dir == "." {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(dir, "/"), "/")
	current := ""
	for _, part := range parts {
		if current == "" {
			current = part
		} else {
			current = path.Join(current, part)
		}
		if a.Symlinks[current] {
			return fmt.Errorf("symlink parent %q", current)
		}
	}
	return nil
}

func (a DiskfsLayerApplier) applyMetadata(target string, header *tar.Header, symlink bool) error {
	if symlink {
		return nil
	}
	if err := a.FS.Chmod(target, safeFileMode(header.FileInfo().Mode(), 0o644)); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	if err := a.FS.Chown(target, header.Uid, header.Gid); err != nil {
		return fmt.Errorf("chown: %w", err)
	}
	return nil
}

func fsPath(name string) string {
	clean := path.Clean(filepathSlash(name))
	if clean == "/" || clean == "" {
		return "."
	}
	return strings.TrimPrefix(clean, "/")
}

func filepathSlash(name string) string {
	return strings.ReplaceAll(name, "\\", "/")
}
