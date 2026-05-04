package oci

import "github.com/code-slammer/slammer-core/internal/config"

type Platform = config.Platform

type PulledImage struct {
	Ref            string
	ManifestDigest string
	ConfigDigest   string
	Config         ImageConfig
	Layers         []Layer
	Platform       Platform
}

type ImageConfig struct {
	Architecture string        `json:"architecture"`
	OS           string        `json:"os"`
	Config       RuntimeConfig `json:"config"`
	RootFS       RootFS        `json:"rootfs"`
}

type RuntimeConfig struct {
	User       string   `json:"User,omitempty"`
	Env        []string `json:"Env,omitempty"`
	Entrypoint []string `json:"Entrypoint,omitempty"`
	Cmd        []string `json:"Cmd,omitempty"`
	WorkingDir string   `json:"WorkingDir,omitempty"`
	StopSignal string   `json:"StopSignal,omitempty"`
}

type RootFS struct {
	Type    string   `json:"type"`
	DiffIDs []string `json:"diff_ids"`
}

type Layer struct {
	Digest             string
	DiffID             string
	MediaType          string
	CompressedBlobPath string
	Size               int64
}
