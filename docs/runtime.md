# Runtime

The host runtime owns a private store under `/var/lib/sandbox-runtime` by default. It does not use Docker, containerd, `/var/lib/docker`, `/run/containerd`, or `/var/lib/containerd`.

Current command:

```sh
go run ./cmd/sandboxd prepare-image --store-dir /var/lib/sandbox-runtime docker.io/library/python:3.12-slim
```

Implemented now:
- Creates the runtime store layout.
- Resolves OCI image indexes and manifests for a selected platform with `go-containerregistry`.
- Caches the selected manifest, config, and compressed layer blobs by digest.
- Writes image ref metadata under `images/refs`.
- Computes the rootfs chain ID from ordered config `rootfs.diff_ids`.
- Returns immediately if `rootfs/complete/<chain-id>.ext4` already exists.
- Uses a chain-ID file lock before entering the cold build path.
- Materializes cold rootfs images as sparse ext4 files in pure Go using `github.com/diskfs/go-diskfs`; no `mkfs.ext4`, loop device, or mount is required for rootfs preparation.
- Applies gzip or uncompressed OCI tar layers, verifies uncompressed diff IDs, handles whiteouts and opaque directories, and rejects traversal/symlink-parent escapes.

Not implemented yet:
- zstd-compressed layers.
- xattr preservation such as `security.capability`.
- Device node extraction; device and FIFO entries are rejected by default.

Cold build prerequisites:
- Root is not required for local rootfs image creation when the store directory is writable.
- The generated ext4 files are sparse; use apparent-size-aware tools when inspecting disk usage.

Store layout:

```text
/var/lib/sandbox-runtime/
  content/blobs/sha256/<digest>
  images/refs/<escaped-image-ref>.json
  images/manifests/sha256/<manifest-digest>.json
  images/configs/sha256/<config-digest>.json
  rootfs/complete/<chain-id>.ext4
  rootfs/complete/<chain-id>.json
  rootfs/building/
  locks/
  firecracker/sockets/
  firecracker/logs/
```

The materialized rootfs cache key is not the tag. It is the chain ID computed from ordered uncompressed OCI diff IDs:

```text
chainID = sha256(join(diffIDs, "\n"))
```
