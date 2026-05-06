# Runtime

The host runtime owns a private store under `/var/lib/sandbox-runtime` by default. It does not use Docker, containerd, `/var/lib/docker`, `/run/containerd`, or `/var/lib/containerd`.

Current command:

```sh
go run ./cmd/sandboxd prepare-image --store-dir /var/lib/sandbox-runtime docker.io/library/python:3.12-slim
```

Local demo without Firecracker:

```sh
go run ./cmd/sandboxd demo-local --store-dir ./tmp/sandbox-runtime-demo
```

The demo prepares `alpine:latest`, verifies expected directories inside the generated ext4 image without mounting it, then runs a batched `write_file` plus `exec` request against the agent HTTP API in-process.

Inspect a generated ext4 image without mounting it:

```sh
go run ./cmd/sandboxd inspect-rootfs --ls / ./tmp/sandbox-runtime/rootfs/complete/<chain-id>.ext4
go run ./cmd/sandboxd inspect-rootfs --read /etc/alpine-release ./tmp/sandbox-runtime/rootfs/complete/<chain-id>.ext4
```

Build the trusted boot image:

```sh
CGO_ENABLED=0 go build -o /tmp/init -ldflags "-w -s" ./cmd/init
CGO_ENABLED=0 go build -o /tmp/agent -ldflags "-w -s" ./cmd/agent
go run ./cmd/sandboxd build-boot-image --init /tmp/init --agent /tmp/agent --output ./tmp/boot-init.ext4
```

Run a command in a Firecracker VM:

```sh
go run ./cmd/sandboxd run \
  --store-dir ./tmp/sandbox-runtime \
  --kernel /path/to/vmlinux \
  --boot-image ./tmp/boot-init.ext4 \
  --firecracker-bin /path/to/firecracker \
  docker.io/library/alpine:latest -- /bin/sh -c 'echo hello from vm'
```

`run` accepts the optional `--` separator before the command. It prepares the image if needed, starts Firecracker with the trusted boot image plus prepared OCI rootfs, waits for the guest agent over vsock, sends one batched jobs request, prints stdout/stderr, and reports the exec exit code.
The `sandboxd run` process exits with the guest command's exit code.
Timing breakdown is printed to stderr for preflight, image preparation, Firecracker startup, agent readiness, job execution, shutdown wait, and total command duration.

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
- Can inspect generated ext4 rootfs images in pure Go with `sandboxd inspect-rootfs`.
- Firecracker drive config helpers live in `internal/firecracker`; the host attaches the trusted boot image as the root drive and the prepared OCI rootfs as a read-only secondary drive.
- `sandboxd run` can execute an `argv` command inside the prepared image via the guest agent.

Not implemented yet:
- zstd-compressed layers.
- xattr preservation such as `security.capability`.
- Device node extraction; device and FIFO entries are rejected by default.

Cold build prerequisites:
- Root is not required for local rootfs image creation when the store directory is writable.
- The generated ext4 files are sparse; use apparent-size-aware tools when inspecting disk usage.

VM run prerequisites:
- Firecracker binary available on the host.
- Uncompressed Linux kernel image compatible with Firecracker.
- Host permissions for `/dev/kvm` and creating the Firecracker vsock device.

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
