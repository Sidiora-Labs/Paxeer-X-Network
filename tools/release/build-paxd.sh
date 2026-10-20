#!/usr/bin/env bash
# Reproducible paxd release build: dist/paxeer-network/<version>/ with SHA256SUMS.
# Usage: build-paxd.sh [--print-tag]
# Env: PAXD_DIST_DIR (output dir), PAXD_COMMIT, SOURCE_DATE_EPOCH, PAXD_RELEASE_LAYERXD=1 (also build layerxd).
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$root"

version=$(jq -er '.version' tools/release/version.json)
tag="paxeer-network/$version"
case "$#:${1-}" in
  0:) ;;
  1:--print-tag)
    echo "$tag"
    exit 0
    ;;
  *)
    echo "build-paxd: usage: build-paxd.sh [--print-tag]" >&2
    exit 2
    ;;
esac

if [ -d /usr/local/go/bin ]; then PATH="/usr/local/go/bin:$PATH"; fi
if [ -d "$HOME/.cargo/bin" ]; then PATH="$HOME/.cargo/bin:$PATH"; fi

export GOTOOLCHAIN=local
want_go=$(awk '$1 == "go" { print "go" $2; exit }' go.mod)
have_go=$(go env GOVERSION)
if [ "$have_go" != "$want_go" ]; then
  echo "build-paxd: go.mod pins $want_go, found $have_go" >&2
  exit 1
fi

rust_pin() {
  local want have
  want=$(awk -F'"' '$1 ~ /^channel *= *$/ { print $2; exit }' rust-toolchain.toml)
  have=$(rustc --version 2>/dev/null | awk '{ print $2 }' || true)
  if [ -z "$want" ] || [ "$have" != "$want" ]; then
    echo "build-paxd: rust-toolchain.toml pins $want, found ${have:-no rustc}" >&2
    exit 1
  fi
}

cc_pin() {
  local want have
  want=$(sed -n 's/^CC_VERSION=//p' tools/release/toolchain.env)
  have=$(cc -dumpfullversion 2>/dev/null || true)
  if [ -z "$want" ] || [ "$have" != "$want" ]; then
    echo "build-paxd: toolchain.env pins cc $want, found ${have:-no cc}" >&2
    exit 1
  fi
}

rust_pin
cc_pin

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

link_dir=/tmp/paxeer-release-build
mkdir -p "$link_dir"
exec 9>"$link_dir/lock"
flock 9
src=$link_dir/src
ln -sfn "$root" "$src"

version_pkg=github.com/Sidiora-Labs/Paxeer-X-Network/sdk/version
ldflags="-buildid= -checklinkname=0 -extld=$root/tools/release/extld.sh \
-X $version_pkg.Name=paxeer \
-X $version_pkg.AppName=paxd \
-X $version_pkg.Version=$version \
-X $version_pkg.Commit=$commit \
-X $version_pkg.BuildTags=netgo,ledger"

export RUSTFLAGS="--remap-path-prefix=$root=. --remap-path-prefix=$src=. --remap-path-prefix=${CARGO_HOME:-$HOME/.cargo}=/cargo"
native_libs=(build/liblayerx.a programs/target/release/liblayerx_programs_sandbox.a)
echo "build-paxd: native libraries ${native_libs[*]}"
make --no-print-directory PROGRAMS_DIR="$src/programs" EXTRA_CFLAGS="-ffile-prefix-map=$root=." paxd-native-libs
sha256sum "${native_libs[@]}" > "$out/native-libs.sha256"

paxd="paxd-$version-$goos-$goarch"
echo "build-paxd: go build $paxd"
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

cargo build --locked --release --manifest-path "$src/services/platform/Cargo.toml" -p layerx-oracle-feeder --bin price-sidecar
platform_target=$(cargo metadata --locked --format-version 1 --no-deps --manifest-path "$src/services/platform/Cargo.toml" | jq -er '.target_directory')
cp "$platform_target/release/price-sidecar" "$out/price-sidecar-$version-$goos-$goarch"

if [ "${PAXD_RELEASE_LAYERXD:-0}" = 1 ]; then
  build_dir=$(mktemp -d)
  trap 'rm -rf -- "$build_dir"' EXIT
  make --no-print-directory PROGRAMS_DIR="$src/programs" BUILD_DIR="$build_dir" LXP_REVISION="$commit" \
    EXTRA_CFLAGS="-ffile-prefix-map=$root=. -ffile-prefix-map=$build_dir=build" layerxd
  cp "$build_dir/bin/layerxd" "$out/layerxd-$version-$goos-$goarch"
fi

write_sums() {
  (cd "$out" && find . -maxdepth 1 -type f ! -name SHA256SUMS -printf '%f\n' | sort | xargs sha256sum > SHA256SUMS)
}

touch -d "@$SOURCE_DATE_EPOCH" "$out"/*
write_sums
python3 tools/release/upgrade_info.py release --sums "$out/SHA256SUMS" --tag "$tag" --out "$out/upgrade-info.json"
write_sums
touch -d "@$SOURCE_DATE_EPOCH" "$out"/*
echo "build-paxd: $tag commit $commit epoch $SOURCE_DATE_EPOCH -> $out"
cat "$out/SHA256SUMS"
