package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/code-slammer/slammer-core/internal/contentstore"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func Pull(ctx context.Context, store *contentstore.Store, ref string, platform Platform) (*PulledImage, error) {
	parsed, err := name.ParseReference(ref, name.WithDefaultRegistry("docker.io"))
	if err != nil {
		return nil, err
	}

	img, err := remote.Image(parsed, remote.WithContext(ctx), remote.WithPlatform(toRemotePlatform(platform)))
	if err != nil {
		return nil, err
	}

	manifest, err := img.RawManifest()
	if err != nil {
		return nil, err
	}
	manifestHash, err := img.Digest()
	if err != nil {
		return nil, err
	}
	manifestDigest := manifestHash.String()
	if _, err := store.WriteBlob(manifestDigest, bytes.NewReader(manifest)); err != nil {
		return nil, err
	}
	if err := store.WriteJSON(store.ManifestPath(manifestDigest), json.RawMessage(manifest)); err != nil {
		return nil, err
	}

	configBytes, err := img.RawConfigFile()
	if err != nil {
		return nil, err
	}
	configName, err := img.ConfigName()
	if err != nil {
		return nil, err
	}
	configDigest := configName.String()
	if _, err := store.WriteBlob(configDigest, bytes.NewReader(configBytes)); err != nil {
		return nil, err
	}
	if err := store.WriteJSON(store.ConfigPath(configDigest), json.RawMessage(configBytes)); err != nil {
		return nil, err
	}
	var config ImageConfig
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return nil, err
	}

	layers, err := img.Layers()
	if err != nil {
		return nil, err
	}
	if len(layers) != len(config.RootFS.DiffIDs) {
		return nil, fmt.Errorf("layer count %d does not match diffID count %d", len(layers), len(config.RootFS.DiffIDs))
	}

	pulledLayers := make([]Layer, 0, len(layers))
	for i, layer := range layers {
		digest, err := layer.Digest()
		if err != nil {
			return nil, err
		}
		mediaType, err := layer.MediaType()
		if err != nil {
			return nil, err
		}
		size, err := layer.Size()
		if err != nil {
			return nil, err
		}
		compressed, err := layer.Compressed()
		if err != nil {
			return nil, err
		}
		path, err := store.WriteBlob(digest.String(), compressed)
		_ = compressed.Close()
		if err != nil {
			return nil, err
		}
		pulledLayers = append(pulledLayers, Layer{
			Digest:             digest.String(),
			DiffID:             config.RootFS.DiffIDs[i],
			MediaType:          string(mediaType),
			CompressedBlobPath: path,
			Size:               size,
		})
	}

	if err := store.WriteJSON(store.RefPath(ref), contentstore.RefMetadata{
		ImageRef:       ref,
		ManifestDigest: manifestDigest,
		Platform: contentstore.Platform{
			OS:           platform.OS,
			Architecture: platform.Architecture,
		},
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		return nil, err
	}

	return &PulledImage{
		Ref:            ref,
		ManifestDigest: manifestDigest,
		ConfigDigest:   configDigest,
		Config:         config,
		Layers:         pulledLayers,
		Platform:       platform,
	}, nil
}

func toRemotePlatform(platform Platform) v1.Platform {
	return v1.Platform{OS: platform.OS, Architecture: platform.Architecture}
}
