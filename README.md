# Slammer Core

Slammer Core is a Go prototype for running short-lived jobs inside Firecracker microVMs using OCI container images as the guest root filesystem.

The project is aimed at a runtime shape where the host can pull an image such as `docker.io/library/python:3.14-alpine`, materialize it into a cached read-only ext4 rootfs, boot a tiny trusted guest image, and ask a small guest agent to run an explicit command.

If you are new to the space, start here:

- [Beginner Concepts](docs/concepts.md): Firecracker, OCI images, root filesystems, vsock, snapshots, and why this project uses them.
- [Getting Started](docs/getting-started.md): install prerequisites, run the local demo, build the boot image, and run a VM.
- [Architecture](docs/architecture.md): how the host runtime, image cache, boot image, guest init, and agent fit together.
- [Command Reference](docs/commands.md): current `sandboxd` commands and common flags.
- [Troubleshooting](docs/troubleshooting.md): common setup and runtime failures.

Existing deeper notes:

- [Runtime](docs/runtime.md)
- [Agent Control Plane](docs/agent-control-plane.md)

## What Works Today

- Pulls OCI images without using Docker or containerd state.
- Caches content blobs, image metadata, and prepared rootfs images under a private store.
- Builds ext4 rootfs images in pure Go without loop devices, mounts, `mkfs.ext4`, or root privileges when the store is writable.
- Builds a trusted boot ext4 image containing the guest `/init` and `/agent` binaries.
- Boots Firecracker with a trusted boot drive plus an immutable target OCI rootfs drive.
- Runs one batched guest agent request containing typed jobs such as `mkdir`, `write_file`, and `exec`.
- Can create and reuse Firecracker snapshots for lower startup latency.
- Can dump the host HTTP API OpenAPI document with `sandboxd openapi`.

## Quick Start Without Firecracker

This path exercises the image preparation and agent API in-process, so it is the best first command on a new machine.

```sh
go test ./...
go run ./cmd/sandboxd demo-local --store-dir ./tmp/sandbox-runtime-demo
```

The demo prepares Alpine Linux, inspects the generated ext4 rootfs, and runs a batched agent job without starting a VM.

## Quick Start With Firecracker

You need Linux with KVM access, a Firecracker binary, and an uncompressed Linux kernel image compatible with Firecracker.

```sh
CGO_ENABLED=0 go build -o /tmp/init -ldflags "-w -s" ./cmd/init
CGO_ENABLED=0 go build -o /tmp/agent -ldflags "-w -s" ./cmd/agent
go run ./cmd/sandboxd build-boot-image --init /tmp/init --agent /tmp/agent --output ./tmp/boot-init.ext4

CGO_ENABLED=0 go build -o sandboxd.bin ./cmd/sandboxd
sudo ./sandboxd.bin run \
  --store-dir ./tmp/sandbox-runtime \
  --kernel /path/to/vmlinux \
  --boot-image ./tmp/boot-init.ext4 \
  --firecracker-bin /path/to/firecracker \
  docker.io/library/alpine:latest -- /bin/sh -c 'echo hello from vm'
```

For full setup detail, see [Getting Started](docs/getting-started.md).

## Repository Layout

```text
cmd/sandboxd/          Host CLI and HTTP server prototype
cmd/init/              Trusted guest PID 1
cmd/agent/             Guest HTTP-over-vsock job agent
internal/oci/          OCI image pull and metadata handling
internal/contentstore/ Content-addressed blob cache
internal/rootfsbuilder Pure-Go ext4 rootfs materialization
internal/firecracker/  Firecracker config, runner, jailer, snapshots
internal/agentapi/     Shared guest API types
internal/agentclient/  Host-side guest agent client
internal/agentserver/  Guest-side job API implementation
docs/                  Project documentation
```

## Project Status

This repository is an active prototype. Interfaces and command flags can change. Prefer the documentation in this repo over assumptions from Docker, containerd, or general VM tooling because Slammer Core intentionally owns its own small runtime store and guest control plane.
