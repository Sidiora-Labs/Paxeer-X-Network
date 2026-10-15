#!/usr/bin/env bash
# Build the release twice into separate dirs (second build on an empty Go cache) and require identical sha256 per binary.
set -euo pipefail

clean_native() {
  local root=$1
  rm -rf -- "$root/build/liblayerx.a" "$root/build/generated" "$root/programs/target"
}

compare_builds() {
  local first=$1 second=$2 directory
  for directory in "$first" "$second"; do
    if [ ! -s "$directory/native-libs.sha256" ] || [ ! -s "$directory/SHA256SUMS" ] ||
      ! grep -qE '^[0-9a-f]{64}  native-libs\.sha256$' "$directory/SHA256SUMS"; then
      echo "reproducible-check: no native library sums" >&2
      return 1
    fi
    if ! grep -qE '^[0-9a-f]{64}  paxd-[^/[:space:]]+$' "$directory/SHA256SUMS"; then
      echo "reproducible-check: no paxd binary built" >&2
      return 1
    fi
  done

  echo "build a:"
  cat "$first/SHA256SUMS"
  echo "build b:"
  cat "$second/SHA256SUMS"
  if ! cmp -s "$first/native-libs.sha256" "$second/native-libs.sha256"; then
    diff "$first/native-libs.sha256" "$second/native-libs.sha256" || true
    echo "reproducible-check: native libraries differ" >&2
    return 1
  fi
  if ! cmp -s "$first/SHA256SUMS" "$second/SHA256SUMS"; then
    diff "$first/SHA256SUMS" "$second/SHA256SUMS" || true
    echo "reproducible-check: builds differ" >&2
    return 1
  fi
  echo "reproducible-check: identical"
}

build_twice() {
  local root=$1 work=$2
  PAXD_DIST_DIR="$work/a" "$root/tools/release/build-paxd.sh" || return "$?"
  clean_native "$root" || return "$?"
  GOCACHE="$work/gocache" PAXD_DIST_DIR="$work/b" "$root/tools/release/build-paxd.sh" || return "$?"
  compare_builds "$work/a" "$work/b"
}

main() (
  root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
  work=$(mktemp -d)
  trap 'rm -rf -- "$work"' EXIT
  build_twice "$root" "$work"
)

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  main "$@"
fi
