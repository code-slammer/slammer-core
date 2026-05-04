# Agent Notes

## Current Repo Shape
- Root Go module is `github.com/code-slammer/slammer-core`; guest commands now build from this module under `cmd/init` and `cmd/agent`.
- `main.go` is the current host Firecracker prototype entrypoint and reads `.env` via `godotenv`; `BASE_DIR` must be set with a trailing slash, `BOOT_IMAGE_PATH` defaults to `${BASE_DIR}boot-init.ext4`, and `TARGET_IMAGE_PATH` is required.
- `cmd/init` is the trusted guest PID 1 prototype; `cmd/agent` is the guest HTTP-over-vsock job agent.
- Guest control-plane work should use `internal/agentapi`, `internal/agentserver`, and `internal/agentclient`; the old `net/rpc/jsonrpc` prototype has been removed.

## Commands
- Root module verification: `go test ./...` from repo root.
- Build trusted guest init: `CGO_ENABLED=0 go build -o /tmp/init -ldflags "-w -s" ./cmd/init`.
- Build trusted guest agent: `CGO_ENABLED=0 go build -o /tmp/agent -ldflags "-w -s" ./cmd/agent`.
- Prepare-image prototype: `go run ./cmd/sandboxd prepare-image --store-dir /var/lib/sandbox-runtime docker.io/library/python:3.12-slim`; rootfs ext4 materialization is pure Go via `go-diskfs`, so no `mkfs.ext4`, loop device, mount, or root privilege is needed if the store dir is writable.
- `image/build_image.sh` uses Docker, `sudo`, and `mksquashfs`, writes to `~/rootfs/testing/image.img`, and removes `/tmp/myfs`; do not run it casually.
- Agent control-plane details are in `docs/agent-control-plane.md`.
- Runtime pull/cache details are in `docs/runtime.md`.

## Architecture Direction
- Target architecture is a small Go runtime that replaces containerd/Docker state for this workflow: pull/cache OCI blobs, materialize a cached immutable ext4 rootfs by chain ID, then launch Firecracker with a trusted boot/init image plus the cached target image as a read-only secondary drive.
- Desired package layout is `cmd/sandboxd`, `cmd/init`, `cmd/agent`, and internals for `oci`, `contentstore`, `rootfsbuilder`, `firecracker`, `lock`, and `gc`.
- Do not cache prepared rootfs images by tag; compute a content-derived chain ID from ordered OCI config `rootfs.diff_ids`.
- Runtime state should be private to `/var/lib/sandbox-runtime/`; do not depend on or mutate `/var/lib/docker`, `/run/containerd`, or `/var/lib/containerd`.

## Security Direction
- Limit guest control-plane surface area. Prefer a small batched `net/http` API over vsock instead of expanding `net/rpc/jsonrpc`.
- Preferred agent model is one-shot or tightly scoped HTTP endpoints such as `GET /healthz` and `POST /jobs`; use explicit job types like `mkdir`, `write_file`, and `exec` instead of arbitrary RPC methods.
- File operations from the agent must stay under an allowed workspace, reject traversal/symlink escapes, reject setuid/setgid bits, and avoid exposing a general file server.
- Exec jobs should take `argv` arrays, not shell command strings; no implicit shell.
- Target OCI rootfs images are immutable artifacts: host only mounts them writable during initial materialization, Firecracker attaches them read-only, and guest writes must go to tmpfs-backed overlay upper/work dirs.

## Missing Project Infrastructure
- There is no README, CI workflow, Makefile, formatter config, or lint config in the current checkout.
