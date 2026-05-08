# Command Reference

All commands are run from the repository root unless otherwise noted.

## Test

```sh
go test ./...
```

Runs the Go test suite.

## Local Demo

```sh
go run ./cmd/sandboxd demo-local --store-dir ./tmp/sandbox-runtime-demo
```

Runs the non-Firecracker demo. This is the recommended first command.

## Prepare Image

```sh
go run ./cmd/sandboxd prepare-image --store-dir ./tmp/sandbox-runtime docker.io/library/python:3.12-slim
```

Pulls an OCI image, stores its content, computes its chain ID, and materializes a cached ext4 rootfs if needed.

Useful flags:

- `--store-dir`: runtime store directory.
- `--os`: target platform OS, default `linux`.
- `--arch`: target architecture, default `amd64`.

## Inspect Rootfs

```sh
go run ./cmd/sandboxd inspect-rootfs --ls / ./tmp/sandbox-runtime/rootfs/complete/<chain-id>.ext4
go run ./cmd/sandboxd inspect-rootfs --read /etc/os-release ./tmp/sandbox-runtime/rootfs/complete/<chain-id>.ext4
```

Inspects a generated ext4 rootfs without mounting it.

## Build Boot Image

```sh
CGO_ENABLED=0 go build -o /tmp/init -ldflags "-w -s" ./cmd/init
CGO_ENABLED=0 go build -o /tmp/agent -ldflags "-w -s" ./cmd/agent
go run ./cmd/sandboxd build-boot-image --init /tmp/init --agent /tmp/agent --output ./tmp/boot-init.ext4
```

Builds the trusted guest boot image containing `/init` and `/agent`.

Useful flags:

- `--init`: path to static guest init binary.
- `--agent`: path to static guest agent binary.
- `--output`: output ext4 image path.
- `--size-bytes`: boot image size, default 256 MiB.

## Run VM

```sh
go run ./cmd/sandboxd run \
  --store-dir ./tmp/sandbox-runtime \
  --kernel /path/to/vmlinux \
  --boot-image ./tmp/boot-init.ext4 \
  --firecracker-bin /path/to/firecracker \
  docker.io/library/alpine:latest -- /bin/sh -c 'echo hello from vm'
```

Prepares the image if needed, starts Firecracker, waits for the guest agent, sends one jobs request, prints captured output, and exits with the guest command exit code.

Useful flags:

- `--store-dir`: runtime store directory.
- `--kernel`: uncompressed Linux kernel image path.
- `--boot-image`: trusted boot image path.
- `--firecracker-bin`: Firecracker binary path.
- `--snapshot-dir`: snapshot artifact directory to use or create from.
- `--vcpu`: vCPU count.
- `--mem-mib`: guest memory in MiB.
- `--uid` and `--gid`: guest UID and GID for the exec job.
- `--workdir`: guest working directory under `/workspace`.
- `--timeout`: VM and job timeout.

## Create Snapshot

```sh
go run ./cmd/sandboxd create-snapshot \
  --store-dir ./tmp/sandbox-runtime \
  --kernel /path/to/vmlinux \
  --boot-image ./tmp/boot-init.ext4 \
  --firecracker-bin /path/to/firecracker \
  --snapshot-dir ./tmp/snapshots \
  docker.io/library/alpine:latest
```

Creates an agent-ready snapshot for the image chain ID.

## Serve HTTP API

```sh
go run ./cmd/sandboxd serve --config sandboxd.json
```

Starts the host HTTP server prototype using a JSON config file. The sample `sandboxd.json` in the repository shows the current shape.

## Dump OpenAPI Spec

```sh
go run ./cmd/sandboxd openapi
```

Writes the HTTP API OpenAPI document to `openapi.json` by default.

Useful flags:

- `--output`: output path, or `-` for stdout.
- `--config`: optional `sandboxd.json` path used to set the OpenAPI server URL.

## Older Host Prototype

The root `main.go` is an older host Firecracker prototype that reads `.env` through `godotenv`. New runtime work should generally use `cmd/sandboxd`.
