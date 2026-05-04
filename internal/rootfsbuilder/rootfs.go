package rootfsbuilder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/code-slammer/slammer-core/internal/contentstore"
	filelock "github.com/code-slammer/slammer-core/internal/lock"
	"github.com/code-slammer/slammer-core/internal/oci"
)

const (
	defaultMinRootfsSize = 2 << 30
	defaultExtraSize     = 512 << 20
)

type Store struct {
	content *contentstore.Store
	builder RootfsImageBuilder
}

type Rootfs struct {
	ChainID string
	Path    string
}

type Metadata struct {
	ChainID        string                `json:"chain_id"`
	ImageRef       string                `json:"image_ref"`
	ManifestDigest string                `json:"manifest_digest"`
	Platform       contentstore.Platform `json:"platform"`
	DiffIDs        []string              `json:"diff_ids"`
	SizeBytes      int64                 `json:"size_bytes"`
	FSType         string                `json:"fs_type"`
	CreatedAt      time.Time             `json:"created_at"`
}

func New(storeDir string) *Store {
	return &Store{content: contentstore.New(storeDir), builder: PureGoExt4Builder{}}
}

func (s *Store) EnsureRootfs(ctx context.Context, img *oci.PulledImage) (*Rootfs, error) {
	chainID, err := ComputeChainID(img.Config.RootFS.DiffIDs)
	if err != nil {
		return nil, err
	}
	path := s.content.RootfsPath(chainID)
	if _, err := os.Stat(path); err == nil {
		return &Rootfs{ChainID: chainID, Path: path}, nil
	}
	lock, err := filelock.Acquire(ctx, s.content.LockPath(chainID))
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if _, err := os.Stat(path); err == nil {
		return &Rootfs{ChainID: chainID, Path: path}, nil
	}
	if err := s.build(ctx, img, chainID, path); err != nil {
		return nil, err
	}
	return &Rootfs{ChainID: chainID, Path: path}, nil
}

func (s *Store) build(ctx context.Context, img *oci.PulledImage, chainID string, completePath string) error {
	if err := os.MkdirAll(filepath.Dir(completePath), 0o755); err != nil {
		return err
	}
	buildingPath := s.content.RootfsBuildingPath(chainID)
	if err := os.MkdirAll(filepath.Dir(buildingPath), 0o755); err != nil {
		return err
	}
	_ = os.Remove(buildingPath)

	size := estimateRootfsSize(img)
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(buildingPath)
		}
	}()

	if err := s.builder.Build(ctx, img, buildingPath, size); err != nil {
		return err
	}

	if err := os.Rename(buildingPath, completePath); err != nil {
		return err
	}
	cleanupTmp = false
	if err := s.WriteMetadata(img, chainID, size); err != nil {
		return err
	}
	return nil
}

func (s *Store) WriteMetadata(img *oci.PulledImage, chainID string, sizeBytes int64) error {
	return s.content.WriteJSON(s.content.RootfsMetadataPath(chainID), Metadata{
		ChainID:        chainID,
		ImageRef:       img.Ref,
		ManifestDigest: img.ManifestDigest,
		Platform: contentstore.Platform{
			OS:           img.Platform.OS,
			Architecture: img.Platform.Architecture,
		},
		DiffIDs:   img.Config.RootFS.DiffIDs,
		SizeBytes: sizeBytes,
		FSType:    "ext4",
		CreatedAt: time.Now().UTC(),
	})
}

func ComputeChainID(diffIDs []string) (string, error) {
	if len(diffIDs) == 0 {
		return "", fmt.Errorf("image config has no rootfs diff_ids")
	}
	for _, diffID := range diffIDs {
		if !strings.HasPrefix(diffID, "sha256:") {
			return "", fmt.Errorf("unsupported diffID %q", diffID)
		}
	}
	hash := sha256.Sum256([]byte(strings.Join(diffIDs, "\n")))
	return "sha256:" + hex.EncodeToString(hash[:]), nil
}

func estimateRootfsSize(img *oci.PulledImage) int64 {
	var compressedTotal int64
	for _, layer := range img.Layers {
		compressedTotal += layer.Size
	}
	size := compressedTotal*4 + defaultExtraSize
	if size < defaultMinRootfsSize {
		return defaultMinRootfsSize
	}
	return size
}
