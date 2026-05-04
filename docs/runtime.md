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
- Defines a replaceable block image manager interface with a shell-backed `mkfs.ext4`/`losetup`/`mount` implementation.

Not implemented yet:
- Cold ext4 rootfs materialization from layers.
- Layer extraction and whiteout handling.

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
