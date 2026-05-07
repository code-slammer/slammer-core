package rootfsbuilder

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type LayerApplier struct {
	Root string
}

func (a LayerApplier) ApplyLayer(ctx context.Context, compressedLayerPath string, expectedDiffID string) error {
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

type readCloser struct {
	io.Reader
	close func() error
}

func (r readCloser) Close() error {
	if r.close == nil {
		return nil
	}
	return r.close()
}

func layerReader(reader io.Reader) (io.ReadCloser, error) {
	bufReader := bufio.NewReader(reader)
	magic, err := bufReader.Peek(2)
	if err != nil {
		return nil, err
	}
	if magic[0] != 0x1f || magic[1] != 0x8b {
		return readCloser{Reader: bufReader}, nil
	}
	gz, err := gzip.NewReader(bufReader)
	if err == nil {
		return gz, nil
	}
	return nil, err
}

func (a LayerApplier) applyEntry(header *tar.Header, reader io.Reader) error {
	name, err := cleanLayerPath(header.Name)
	if err != nil {
		return err
	}
	base := filepath.Base(name)
	dir := filepath.Dir(name)

	if base == ".wh..wh..opq" {
		target, err := a.safePath(dir)
		if err != nil {
			return err
		}
		return removeChildren(target)
	}
	if strings.HasPrefix(base, ".wh.") {
		target, err := a.safePath(filepath.Join(dir, strings.TrimPrefix(base, ".wh.")))
		if err != nil {
			return err
		}
		return os.RemoveAll(target)
	}

	target, err := a.safePath(name)
	if err != nil {
		return err
	}
	if err := ensureNoSymlinkParents(a.Root, target); err != nil {
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

func (a LayerApplier) mkdir(target string, header *tar.Header) error {
	mode := safeFileMode(header.FileInfo().Mode(), 0o755)
	if err := os.MkdirAll(target, mode); err != nil {
		return err
	}
	return applyMetadata(target, header, true)
}

func (a LayerApplier) writeFile(target string, header *tar.Header, reader io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := ensureNoSymlinkParents(a.Root, target); err != nil {
		return err
	}
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to overwrite symlink")
	}
	mode := safeFileMode(header.FileInfo().Mode(), 0o644)
	fd, err := unix.Open(target, unix.O_WRONLY|unix.O_CREAT|unix.O_TRUNC|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(mode))
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), target)
	_, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return applyMetadata(target, header, false)
}

func (a LayerApplier) symlink(target string, header *tar.Header) error {
	if _, err := cleanLinkPath(header.Linkname); err != nil {
		return fmt.Errorf("unsafe symlink target: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	if err := os.Symlink(header.Linkname, target); err != nil {
		return err
	}
	return applyMetadata(target, header, true)
}

func (a LayerApplier) hardlink(target string, header *tar.Header) error {
	linkName, err := cleanLayerPath(header.Linkname)
	if err != nil {
		return fmt.Errorf("unsafe hardlink target: %w", err)
	}
	linkTarget, err := a.safePath(linkName)
	if err != nil {
		return err
	}
	if err := ensureNoSymlinkParents(a.Root, target); err != nil {
		return err
	}
	if err := ensureNoSymlinkParents(a.Root, linkTarget); err != nil {
		return err
	}
	if info, err := os.Lstat(linkTarget); err != nil {
		return err
	} else if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing hardlink to symlink")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	return os.Link(linkTarget, target)
}

func (a LayerApplier) safePath(name string) (string, error) {
	root := filepath.Clean(a.Root)
	target := filepath.Join(root, filepath.FromSlash(name))
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes root")
	}
	return target, nil
}

func cleanLayerPath(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty path")
	}
	if filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("absolute path is not allowed")
	}
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path traversal is not allowed")
	}
	return clean, nil
}

func cleanLinkPath(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty path")
	}
	return filepath.ToSlash(filepath.Clean(name)), nil
}

func ensureNoSymlinkParents(root, target string) error {
	root = filepath.Clean(root)
	dir := filepath.Dir(target)
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return err
	}
	current := root
	if rel == "." {
		return nil
	}
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink parent %q", current)
		}
	}
	return nil
}

func removeChildren(dir string) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func safeFileMode(mode os.FileMode, fallback os.FileMode) os.FileMode {
	if mode == 0 {
		mode = fallback
	}
	return mode.Perm() &^ 0o6000
}

func applyMetadata(target string, header *tar.Header, symlink bool) error {
	if symlink {
		_ = os.Lchown(target, header.Uid, header.Gid)
		return nil
	}
	mode := safeFileMode(header.FileInfo().Mode(), 0o644)
	if err := os.Chmod(target, mode); err != nil {
		return err
	}
	if err := os.Chown(target, header.Uid, header.Gid); err != nil && !errors.Is(err, unix.EPERM) {
		return err
	}
	mtime := header.ModTime
	if mtime.IsZero() {
		mtime = time.Unix(0, 0)
	}
	return os.Chtimes(target, mtime, mtime)
}
