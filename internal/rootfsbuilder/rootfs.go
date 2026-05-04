package rootfsbuilder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/code-slammer/slammer-core/internal/contentstore"
	filelock "github.com/code-slammer/slammer-core/internal/lock"
	"github.com/code-slammer/slammer-core/internal/oci"
)

type Store struct {
	content *contentstore.Store
	block   BlockImageManager
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
	return &Store{content: contentstore.New(storeDir), block: ShellBlockImageManager{}}
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
	return nil, fmt.Errorf("rootfs cold build for %s is not implemented yet", chainID)
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
