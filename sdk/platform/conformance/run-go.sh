#!/bin/sh
set -eu

repo_root=${1:-.}
repo_root=$(cd "$repo_root" && pwd)

cargo run --offline --manifest-path "$repo_root/sdk/platform/generators/Cargo.toml" --locked \
	-p layerx-platform-sdkgen -- --check "$repo_root"
cd "$repo_root/sdk/platform/go"
GOPROXY=off go test ./...
GOPROXY=off go run ./cmd/conformance -repo "$repo_root"
