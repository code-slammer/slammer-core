package contentstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Store struct {
	Dir string
}

type RefMetadata struct {
	ImageRef       string    `json:"image_ref"`
	ManifestDigest string    `json:"manifest_digest"`
	Platform       Platform  `json:"platform"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Platform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
}

func New(dir string) *Store {
	return &Store{Dir: dir}
}

func (s *Store) Init() error {
	for _, dir := range []string{
		s.BlobDir("sha256"),
		filepath.Join(s.Dir, "images", "refs"),
		filepath.Join(s.Dir, "images", "manifests", "sha256"),
		filepath.Join(s.Dir, "images", "configs", "sha256"),
		filepath.Join(s.Dir, "rootfs", "complete"),
		filepath.Join(s.Dir, "rootfs", "building"),
		filepath.Join(s.Dir, "locks"),
		filepath.Join(s.Dir, "firecracker", "sockets"),
		filepath.Join(s.Dir, "firecracker", "logs"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) BlobDir(algorithm string) string {
	return filepath.Join(s.Dir, "content", "blobs", algorithm)
}

func (s *Store) BlobPath(digest string) string {
	algorithm, encoded := splitDigest(digest)
	return filepath.Join(s.Dir, "content", "blobs", algorithm, encoded)
}

func (s *Store) ManifestPath(digest string) string {
	_, encoded := splitDigest(digest)
	return filepath.Join(s.Dir, "images", "manifests", "sha256", encoded+".json")
}

func (s *Store) ConfigPath(digest string) string {
	_, encoded := splitDigest(digest)
	return filepath.Join(s.Dir, "images", "configs", "sha256", encoded+".json")
}

func (s *Store) RefPath(ref string) string {
	return filepath.Join(s.Dir, "images", "refs", EscapeRef(ref)+".json")
}

func (s *Store) RootfsPath(chainID string) string {
	_, encoded := splitDigest(chainID)
	return filepath.Join(s.Dir, "rootfs", "complete", encoded+".ext4")
}

func (s *Store) RootfsMetadataPath(chainID string) string {
	_, encoded := splitDigest(chainID)
	return filepath.Join(s.Dir, "rootfs", "complete", encoded+".json")
}

func (s *Store) RootfsBuildingPath(chainID string) string {
	_, encoded := splitDigest(chainID)
	return filepath.Join(s.Dir, "rootfs", "building", encoded+".ext4.tmp")
}

func (s *Store) LockPath(chainID string) string {
	_, encoded := splitDigest(chainID)
	return filepath.Join(s.Dir, "locks", encoded+".lock")
}

func (s *Store) WriteBlob(digest string, reader io.Reader) (string, error) {
	path := s.BlobPath(digest)
	if exists(path) {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".blob-*.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	hash := sha256.New()
	if _, err := io.Copy(tmp, io.TeeReader(reader, hash)); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	actual := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if actual != digest {
		return "", fmt.Errorf("blob digest mismatch: got %s want %s", actual, digest)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if exists(path) {
			return path, nil
		}
		return "", err
	}
	return path, nil
}

func (s *Store) WriteJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".json-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func EscapeRef(ref string) string {
	replacer := strings.NewReplacer("/", "_", ":", "_", "@", "_", " ", "_")
	return replacer.Replace(ref)
}

func splitDigest(digest string) (string, string) {
	algorithm, encoded, ok := strings.Cut(digest, ":")
	if !ok {
		return "sha256", digest
	}
	return algorithm, encoded
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
