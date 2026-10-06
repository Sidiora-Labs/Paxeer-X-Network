#!/usr/bin/env bash
# Reproducible paxd release build: dist/paxeer-network/<version>/ with SHA256SUMS.
# Env: PAXD_DIST_DIR (output dir), PAXD_COMMIT, SOURCE_DATE_EPOCH, PAXD_RELEASE_LAYERXD=1 (also build layerxd).
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$root"
if [ -d /usr/local/go/bin ]; then PATH="/usr/local/go/bin:$PATH"; fi
if [ -d "$HOME/.cargo/bin" ]; then PATH="$HOME/.cargo/bin:$PATH"; fi

export GOTOOLCHAIN=local
want_go=$(awk '$1 == "go" { print "go" $2; exit }' go.mod)
have_go=$(go env GOVERSION)
if [ "$have_go" != "$want_go" ]; then
  echo "build-paxd: go.mod pins $want_go, found $have_go" >&2
  exit 1
fi

version=$(jq -er '.version' version.json)
tag="paxeer-network/$version"
commit=${PAXD_COMMIT:-$(git rev-parse HEAD)}
export SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct "$commit")}
goos=$(go env GOOS)
goarch=$(go env GOARCH)
out=${PAXD_DIST_DIR:-$root/dist/$tag}

export CGO_ENABLED=1 CC=gcc CXX=g++
export CGO_CFLAGS="-O2 -g" CGO_CXXFLAGS="-O2 -g" CGO_LDFLAGS="-O2 -g"
export GOFLAGS="-mod=readonly"
export LC_ALL=C TZ=UTC

rm -rf -- "$out"
mkdir -p "$out"

version_pkg=github.com/Sidiora-Labs/Paxeer-X-Network/sdk/version
ldflags="-buildid= -checklinkname=0 \
-X $version_pkg.Name=paxeer \
-X $version_pkg.AppName=paxd \
-X $version_pkg.Version=$version \
-X $version_pkg.Commit=$commit \
-X $version_pkg.BuildTags=netgo,ledger"

paxd="paxd-$version-$goos-$goarch"
go build -trimpath -buildvcs=false -tags "netgo ledger" -ldflags "$ldflags" -o "$out/$paxd" ./daemon/paxd

if [ "$goos/$goarch" = linux/amd64 ]; then
  for lib in wasm-runtime/internal/api/libwasmvm.x86_64.so \
    wasm/x/wasm/artifacts/v152/api/libwasmvm152.x86_64.so \
    wasm/x/wasm/artifacts/v155/api/libwasmvm155.x86_64.so; do
    name=$(basename "$lib")
    expected=$(awk -v name="$name" '$2 == name { print $1 }' wasm-runtime/libwasmvm-linux.sha256)
    if [ "${#expected}" -ne 64 ]; then
      echo "build-paxd: no pinned sha256 for $name" >&2
      exit 1
    fi
    cp "$lib" "$out/$name"
    (cd "$out" && printf '%s  %s\n' "$expected" "$name" | sha256sum --check --strict --quiet)
  done
fi

if [ "${PAXD_RELEASE_LAYERXD:-0}" = 1 ]; then
  build_dir=$(mktemp -d)
  trap 'rm -rf -- "$build_dir"' EXIT
  make --no-print-directory BUILD_DIR="$build_dir" LXP_REVISION="$commit" \
    EXTRA_CFLAGS="-ffile-prefix-map=$root=. -ffile-prefix-map=$build_dir=build" layerxd
  cp "$build_dir/bin/layerxd" "$out/layerxd-$version-$goos-$goarch"
fi

touch -d "@$SOURCE_DATE_EPOCH" "$out"/*
(cd "$out" && find . -maxdepth 1 -type f ! -name SHA256SUMS -printf '%f\n' | sort | xargs sha256sum > SHA256SUMS)
echo "build-paxd: $tag commit $commit epoch $SOURCE_DATE_EPOCH -> $out"
cat "$out/SHA256SUMS"
