#!/bin/bash
set -e
# If current dir is not the root of the project, change to the root
if [ ! -f "go.mod" ]; then
    cd "$(dirname "$0")/.."
fi
rm openapi.json 2>/dev/null || true

go run ./cmd/sandboxd openapi
rm -rf docs/api
docker run --rm --user "$(id -u):$(id -g)" -v "${PWD}:/local" openapitools/openapi-generator-cli generate \
    -i /local/openapi.json \
    -g markdown \
    -o /local/docs/api 
