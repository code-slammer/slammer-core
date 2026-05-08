# Beginner Concepts

This page explains the main terms used by Slammer Core. You do not need to be a virtualization expert to work on the project, but the words show up throughout the codebase.

## Firecracker

[Firecracker](https://github.com/firecracker-microvm/firecracker) is a virtual machine monitor built for fast, small Linux microVMs. It is commonly used for serverless and sandboxed workloads.

Compared with a full desktop VM, a Firecracker microVM has a deliberately small device model. That makes it easier to start quickly and reduce exposed surface area, but it also means the host must provide a kernel, drives, networking or vsock devices, and machine configuration explicitly.

In this project, Firecracker runs a tiny trusted boot image and attaches a prepared OCI rootfs image as a second read-only drive.

## KVM

[KVM](https://www.linux-kvm.org/page/Main_Page) is the Linux kernel virtualization interface. Firecracker uses `/dev/kvm` to run guest code efficiently on host CPU virtualization features.

If `/dev/kvm` is missing or your user cannot access it, Firecracker VM runs will fail before the guest starts.

## OCI Images

[OCI images](https://github.com/opencontainers/image-spec) are the standard image format used by Docker, Kubernetes, containerd, and many registries. An image reference such as `docker.io/library/alpine:latest` points to metadata and filesystem layers.

Important pieces:

- Manifest: lists the config object and layer blobs for one image.
- Config: includes platform details and ordered `rootfs.diff_ids`.
- Layer: a tar archive containing filesystem changes.
- Diff ID: digest of an uncompressed layer.
- Chain ID: content-derived ID computed from ordered diff IDs.

Slammer Core uses OCI registries and image formats, but it does not use Docker or containerd runtime state.

## Root Filesystem

A root filesystem, often shortened to rootfs, is the directory tree a Linux system sees under `/`. Container images are mostly a portable way to describe a rootfs.

Slammer Core turns OCI layers into an ext4 disk image. Firecracker then attaches that image to the VM as a block device.

## ext4

[ext4](https://www.kernel.org/doc/html/latest/filesystems/ext4/index.html) is a common Linux filesystem. Slammer Core currently materializes target root filesystems as sparse ext4 files using pure Go code through `go-diskfs`.

That means preparing an image does not need `mkfs.ext4`, loop devices, or root privileges when the store directory is writable.

## Trusted Boot Image

The trusted boot image is a small ext4 drive built by `sandboxd build-boot-image`. It contains two static Go binaries:

- `/init`: the first process in the guest, built from `cmd/init`.
- `/agent`: the guest job agent, built from `cmd/agent`.

The boot image is trusted project code. The OCI target image is treated as workload input.

## OverlayFS

[OverlayFS](https://docs.kernel.org/filesystems/overlayfs.html) combines a read-only lower directory with a writable upper directory.

The guest init mounts the target rootfs read-only, creates a tmpfs-backed writable overlay, and chroots into the merged view. This lets the command write files during the run without mutating the cached target rootfs image.

## vsock

[virtio-vsock](https://man7.org/linux/man-pages/man7/vsock.7.html) is a host-to-guest communication channel for VMs. It behaves like a socket, but it does not require normal guest networking.

Slammer Core uses HTTP over vsock so the host can ask the guest agent to run typed jobs.

## Snapshots

Firecracker snapshots save a paused VM's memory and device state. Restoring from a snapshot can be faster than cold booting to the same ready state.

Slammer Core can create an agent-ready snapshot for an image chain ID and reuse it with `sandboxd run --snapshot-dir`.

## Why Not Just Docker?

Docker is excellent for general container workflows. This project is experimenting with a smaller runtime path for a specific sandbox workflow:

- Own the image cache instead of relying on Docker or containerd state.
- Convert OCI layers into immutable rootfs disk artifacts.
- Boot jobs inside microVMs for stronger isolation than a normal container process.
- Keep the guest control plane small and typed instead of exposing a broad file server or shell RPC.
