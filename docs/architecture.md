# Architecture

Slammer Core is split between host-side runtime code and guest-side trusted code.

## High-Level Flow

```text
OCI registry
  -> internal/oci pulls manifest, config, and layers
  -> internal/contentstore stores blobs by digest
  -> internal/rootfsbuilder materializes an ext4 rootfs by chain ID
  -> internal/firecracker starts a microVM with two drives
  -> cmd/init mounts target rootfs and starts cmd/agent
  -> internal/agentclient sends HTTP-over-vsock jobs
```

## Host Components

`cmd/sandboxd` is the main host entrypoint. It currently provides commands for image preparation, rootfs inspection, boot image construction, VM runs, snapshot creation, and serving an HTTP API.

Key packages:

- `internal/oci`: resolves OCI image indexes and manifests, downloads configs and layers, and selects a target platform.
- `internal/contentstore`: stores content-addressed blobs under the runtime store.
- `internal/rootfsbuilder`: applies OCI tar layers into sparse ext4 images in pure Go.
- `internal/firecracker`: builds Firecracker configs, starts VMs, handles jailer options, and manages snapshots.
- `internal/manager`: coordinates image preparation and VM execution.
- `internal/agentclient`: connects to the guest agent over vsock.
- `internal/server`: exposes the runtime through the `sandboxd serve` HTTP prototype.

## Guest Components

`cmd/init` is the trusted PID 1 inside the boot image. It performs minimal setup:

- Mounts proc, sysfs, dev, run, and tmp.
- Mounts the target OCI rootfs drive read-only at `/mnt/target`.
- Creates a tmpfs-backed OverlayFS upper/work area.
- Mounts the merged root at `/mnt/overlay/merged`.
- Bind-mounts the trusted `/agent` binary into the merged root as `/.sandbox/agent`.
- Chroots into the merged root and starts the agent.
- Reaps children and powers off when the agent exits.

`cmd/agent` is the guest job server. It listens on a vsock port and exposes a small HTTP API:

- `GET /healthz`
- `POST /jobs`

See [Agent Control Plane](agent-control-plane.md) for request and security details.

## Drives

Firecracker receives two main drives:

- Trusted boot drive: built from project binaries and mounted as the root boot drive.
- Target rootfs drive: materialized from an OCI image and attached read-only as a secondary drive.

The target image is immutable during a run. Writes go to guest tmpfs via OverlayFS.

## Runtime Store

The default store is `/var/lib/sandbox-runtime`, but development commands often use `./tmp/sandbox-runtime`.

The store contains content blobs, image refs, image configs, prepared rootfs images, locks, Firecracker sockets, and logs. It intentionally does not use or mutate Docker or containerd directories.

See [Runtime](runtime.md) for the exact store layout.

## Caching Model

Prepared rootfs images are cached by chain ID. The chain ID is derived from ordered OCI config `rootfs.diff_ids`, not from a mutable image tag.

This matters because tags can move. If `alpine:latest` points to new content tomorrow, it should produce a different rootfs cache entry.

## Security Shape

The current design limits what the guest agent can do:

- File writes must stay under the configured workspace.
- Parent symlink escapes are rejected.
- setuid and setgid file bits are rejected.
- Exec uses argv arrays instead of shell command strings.
- Output and request sizes are bounded.
- The target OCI rootfs is attached read-only.

This is still a prototype, not a complete sandbox hardening story. Treat the current code as a foundation for the intended architecture.
