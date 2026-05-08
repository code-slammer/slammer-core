# Documentation

Start with these pages if you are new to the project:

- [Beginner Concepts](concepts.md): the vocabulary used by this repository.
- [Getting Started](getting-started.md): the first commands to run.
- [Architecture](architecture.md): the main components and boot flow.
- [Command Reference](commands.md): command examples and common options.
- [Troubleshooting](troubleshooting.md): common failures and fixes.

More focused technical notes:

- [Runtime](runtime.md): OCI image pull, rootfs cache, VM run flow, snapshots, and store layout.
- [Agent Control Plane](agent-control-plane.md): guest API shape, security rules, and boot flow details.

External background reading:

- [Firecracker documentation](https://github.com/firecracker-microvm/firecracker/blob/main/docs/getting-started.md)
- [Open Container Initiative image spec](https://github.com/opencontainers/image-spec)
- [Linux KVM](https://www.linux-kvm.org/page/Main_Page)
- [Linux virtio-vsock](https://man7.org/linux/man-pages/man7/vsock.7.html)
- [OverlayFS kernel documentation](https://docs.kernel.org/filesystems/overlayfs.html)
