#!/bin/bash

# If current dir is not the root of the project, change to the root
if [ ! -f "go.mod" ]; then
    cd "$(dirname "$0")/.."
fi

CGO_ENABLED=0 go build -o sandboxd.bin ./cmd/sandboxd

docker run --rm -v "${PWD}:/local" openapitools/openapi-generator-cli generate \
    -i /local/openapi.yaml \
    -g markdown \
    -o /local/docs/api