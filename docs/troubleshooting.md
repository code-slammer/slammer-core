# Troubleshooting

## Start With the Local Demo

If a Firecracker run fails, first verify the non-Firecracker path:

```sh
go test ./...
go run ./cmd/sandboxd demo-local --store-dir ./tmp/sandbox-runtime-demo
```

If this fails, focus on Go dependencies, network access, OCI registry access, or rootfs materialization before debugging KVM or Firecracker.

## `/dev/kvm` Is Missing

Firecracker needs KVM.

Check:

```sh
ls -l /dev/kvm
```

Common causes:

- You are not on Linux.
- CPU virtualization is disabled in BIOS or firmware.
- You are inside a VM that does not expose nested virtualization.
- The KVM kernel modules are not loaded.

## Permission Denied on `/dev/kvm`

Your user needs permission to access `/dev/kvm`. Many systems grant this through the `kvm` group.

Check your distribution's KVM setup docs before changing permissions. Avoid running development commands with `sudo` unless you intentionally want root-owned runtime store files.

## Firecracker Binary Not Found

Use `--firecracker-bin /path/to/firecracker` or put `firecracker` on `PATH`.

The repository sample `sandboxd.json` points to a local release path. That path is only an example.

## Kernel Path Not Found

Use `--kernel /path/to/vmlinux` with an uncompressed Linux kernel image compatible with Firecracker.

Helpful starting point:

- [Firecracker getting started guide](https://github.com/firecracker-microvm/firecracker/blob/main/docs/getting-started.md)

## Boot Image Not Found

Build it first:

```sh
CGO_ENABLED=0 go build -o /tmp/init -ldflags "-w -s" ./cmd/init
CGO_ENABLED=0 go build -o /tmp/agent -ldflags "-w -s" ./cmd/agent
go run ./cmd/sandboxd build-boot-image --init /tmp/init --agent /tmp/agent --output ./tmp/boot-init.ext4
```

Then pass `--boot-image ./tmp/boot-init.ext4`.

## Store Permission Errors

The default store is `/var/lib/sandbox-runtime`, which often requires elevated permissions. For development, use a local store:

```sh
go run ./cmd/sandboxd prepare-image --store-dir ./tmp/sandbox-runtime docker.io/library/alpine:latest
```

If you previously ran commands with `sudo`, files under `./tmp` may be root-owned.

## OCI Pull Fails

Check:

- The image reference is valid, for example `docker.io/library/alpine:latest`.
- The host has network access.
- The registry is reachable and not rate-limiting you.
- The requested `--os` and `--arch` exist for that image.

## Rootfs Build Fails on a Layer

Current known limitations:

- zstd-compressed layers are not implemented yet.
- xattrs such as `security.capability` are not preserved.
- Device nodes and FIFO entries are rejected by default.
- Traversal and symlink-parent escapes are rejected intentionally.

Try a simple image such as `docker.io/library/alpine:latest` to determine whether the failure is image-specific.

## Guest Command Not Found

The command runs inside the target OCI image, not on the host. For example, `python` exists in `python:3.12-slim` but not necessarily in `alpine:latest`.

Use an image that contains the command, or call a shell that exists in that image:

```sh
docker.io/library/alpine:latest -- /bin/sh -c 'echo hello'
```

There is no implicit shell. A command string such as `echo hello` is not split by the agent.

## Guest Writes Do Not Persist

This is expected. The target OCI rootfs is attached read-only. Guest writes go to a tmpfs-backed overlay and disappear when the VM stops.

## Snapshot Restore Does Not Happen

Snapshots are keyed by image chain ID and stored under the directory passed with `--snapshot-dir`. If artifacts are missing for that image, `sandboxd run --snapshot-dir` creates them once by booting to agent readiness.

Confirm you are using the same image content and the same snapshot directory.
