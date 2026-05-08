# Getting Started

This guide takes a new contributor from a clean checkout to the first local demo and, optionally, a Firecracker VM run.

## Prerequisites

Required for all development:

- Linux or a Linux VM.
- Go matching the module version in `go.mod`.
- Network access to pull Go modules and OCI images.
- Enough disk space for sparse rootfs images under `./tmp` or `/var/lib/sandbox-runtime`.

Required for Firecracker runs:

- CPU virtualization enabled in BIOS or firmware.
- `/dev/kvm` present and accessible by your user.
- A Firecracker binary.
- An uncompressed Linux kernel image compatible with Firecracker.
- Host permissions to create the Firecracker vsock device.

Helpful links:

- [Firecracker getting started guide](https://github.com/firecracker-microvm/firecracker/blob/main/docs/getting-started.md)
- [Firecracker releases](https://github.com/firecracker-microvm/firecracker/releases)
- [KVM overview](https://www.linux-kvm.org/page/Main_Page)

## Verify the Codebase

From the repository root:

```sh
go test ./...
```

This is the main project verification command.

## Run the Local Demo

Start here if you are new to the project. This does not boot Firecracker.

```sh
go run ./cmd/sandboxd demo-local --store-dir ./tmp/sandbox-runtime-demo
```

What this does:

- Pulls and prepares `alpine:latest`.
- Builds a cached ext4 rootfs image in a local store.
- Inspects files inside that ext4 image without mounting it.
- Exercises the batched agent HTTP API in-process.

## Prepare an OCI Image

To only pull and materialize an image:

```sh
go run ./cmd/sandboxd prepare-image --store-dir ./tmp/sandbox-runtime docker.io/library/python:3.12-slim
```

The prepared rootfs is cached by chain ID, not by tag. Running the command again should be fast if the image content has not changed.

## Inspect a Prepared Rootfs

List the root directory:

```sh
go run ./cmd/sandboxd inspect-rootfs --ls / ./tmp/sandbox-runtime/rootfs/complete/<chain-id>.ext4
```

Read a file:

```sh
go run ./cmd/sandboxd inspect-rootfs --read /etc/alpine-release ./tmp/sandbox-runtime/rootfs/complete/<chain-id>.ext4
```

Replace `<chain-id>` with the generated filename under `rootfs/complete`.

## Build the Trusted Boot Image

Firecracker runs need a boot image containing the trusted guest init and agent binaries.

```sh
CGO_ENABLED=0 go build -o /tmp/init -ldflags "-w -s" ./cmd/init
CGO_ENABLED=0 go build -o /tmp/agent -ldflags "-w -s" ./cmd/agent
go run ./cmd/sandboxd build-boot-image --init /tmp/init --agent /tmp/agent --output ./tmp/boot-init.ext4
```

The output image contains `/init` and `/agent`.

## Run a Firecracker VM

After installing Firecracker and obtaining a compatible kernel image:

```sh
go run ./cmd/sandboxd run \
  --store-dir ./tmp/sandbox-runtime \
  --kernel /path/to/vmlinux \
  --boot-image ./tmp/boot-init.ext4 \
  --firecracker-bin /path/to/firecracker \
  docker.io/library/alpine:latest -- /bin/sh -c 'echo hello from vm'
```

The command after `--` is passed as an argv array to the guest agent. There is no implicit shell, so use `/bin/sh -c` only when you intentionally want shell behavior.

## Use Snapshots

Create an agent-ready snapshot:

```sh
go run ./cmd/sandboxd create-snapshot \
  --store-dir ./tmp/sandbox-runtime \
  --kernel /path/to/vmlinux \
  --boot-image ./tmp/boot-init.ext4 \
  --firecracker-bin /path/to/firecracker \
  --snapshot-dir ./tmp/snapshots \
  docker.io/library/alpine:latest
```

Run using existing snapshot artifacts when available:

```sh
go run ./cmd/sandboxd run \
  --store-dir ./tmp/sandbox-runtime \
  --kernel /path/to/vmlinux \
  --boot-image ./tmp/boot-init.ext4 \
  --firecracker-bin /path/to/firecracker \
  --snapshot-dir ./tmp/snapshots \
  docker.io/library/alpine:latest -- /bin/sh -c 'echo hello from snapshot'
```

## Next Reading

- [Architecture](architecture.md)
- [Command Reference](commands.md)
- [Troubleshooting](troubleshooting.md)
- [Runtime](runtime.md)
