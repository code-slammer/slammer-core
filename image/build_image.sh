#!/bin/bash

IMAGE_TAG=my_image
TMP_DIR=/tmp/myfs

set -e

docker build -t "$IMAGE_TAG" .

echo "Container $IMAGE_TAG built!"

docker run --rm -it -v "$TMP_DIR:/myfs" "$IMAGE_TAG"

# sudo cp start.sh "$TMP_DIR"
CGO_ENABLED=0 go build -o init -ldflags "-w -s" init.go
sudo cp init "$TMP_DIR/init"

sudo mksquashfs "$TMP_DIR" ~/rootfs/testing/image.img -noappend

sudo rm -rf "$TMP_DIR"