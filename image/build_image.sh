#!/bin/bash

IMAGE_TAG=my_image
TMP_DIR=/tmp/myfs

set -e

docker build -t "$IMAGE_TAG" .

echo "Container $IMAGE_TAG built!"

docker run --rm -it -v "$TMP_DIR:/myfs" "$IMAGE_TAG"

# Build trusted guest binaries from the root module so the boot image uses the
# same source tree as the host runtime.
CGO_ENABLED=0 go build -o init -ldflags "-w -s" ../cmd/init
CGO_ENABLED=0 go build -o agent -ldflags "-w -s" ../cmd/agent
sudo cp init "$TMP_DIR/init"
sudo cp agent "$TMP_DIR/agent"

sudo mksquashfs "$TMP_DIR" ~/rootfs/testing/image.img -noappend

sudo rm -rf "$TMP_DIR"
