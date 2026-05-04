# Agent Control Plane

The guest control plane is a small `net/http` API served by `cmd/agent` over vsock.

Endpoints:
- `GET /healthz` returns `ok`.
- `POST /jobs` accepts one batched request and then refuses further job batches.

The API intentionally exposes typed operations instead of arbitrary RPC methods or shell strings.

```json
{
  "version": 1,
  "workspace": "/workspace",
  "defaults": {
    "uid": 1000,
    "gid": 1000,
    "env": ["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"],
    "timeout_ms": 5000,
    "max_output_bytes": 1048576
  },
  "jobs": [
    {
      "type": "write_file",
      "path": "/workspace/main.py",
      "mode": 420,
      "contents_b64": "cHJpbnQoImhlbGxvIikK"
    },
    {
      "type": "exec",
      "argv": ["python", "/workspace/main.py"],
      "working_dir": "/workspace"
    }
  ],
  "shutdown": true
}
```

Supported job types:
- `mkdir`: creates a directory under the workspace.
- `write_file`: writes base64 content under the workspace.
- `exec`: executes an `argv` array with no implicit shell.

File operation rules:
- Paths must be absolute and must stay under the configured workspace.
- Parent symlinks are rejected to avoid escaping the workspace.
- `write_file` refuses to overwrite symlink targets.
- setuid and setgid mode bits are rejected.
- Device nodes and general file-server behavior are not exposed.

Exec rules:
- `argv` is required; command strings are not accepted.
- The working directory must be under the workspace.
- The process runs with an explicit UID/GID when requested.
- Timeout kills the process group.
- stdout and stderr are captured with size limits and returned as base64.

Boot flow:
- `cmd/init` is the trusted PID 1 for the boot image.
- It mounts proc/sys/dev/run/tmp, mounts the target rootfs drive read-only at `/mnt/target`, and creates a tmpfs-backed overlay at `/mnt/overlay/merged`.
- It bind-mounts trusted `/agent` into the merged root as `/.sandbox/agent`.
- It chroots into the merged root, starts the agent, reaps children, and powers off when the agent exits.

Build current guest binaries:

```sh
CGO_ENABLED=0 go build -o /tmp/init -ldflags "-w -s" ./cmd/init
CGO_ENABLED=0 go build -o /tmp/agent -ldflags "-w -s" ./cmd/agent
```

Current host prototype:
- `main.go` still reads `.env` through `godotenv` and requires `BASE_DIR`.
- `BOOT_IMAGE_PATH` defaults to `${BASE_DIR}boot-init.ext4`.
- `TARGET_IMAGE_PATH` is required and is attached as the read-only secondary drive.
- Kernel args expect the trusted boot image to provide `/init` and the target image to appear as `/dev/vdb`.
- The host waits for `GET /healthz`, then sends one `POST /jobs` request through `internal/agentclient`.
