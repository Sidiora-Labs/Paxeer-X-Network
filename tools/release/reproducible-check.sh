#!/usr/bin/env bash
# Build the release twice into separate dirs (second build on an empty Go cache) and require identical sha256 per binary.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT

PAXD_DIST_DIR="$work/a" "$root/tools/release/build-paxd.sh"
GOCACHE="$work/gocache" PAXD_DIST_DIR="$work/b" "$root/tools/release/build-paxd.sh"

echo "build a:"
cat "$work/a/SHA256SUMS"
echo "build b:"
cat "$work/b/SHA256SUMS"
if ! cmp -s "$work/a/SHA256SUMS" "$work/b/SHA256SUMS"; then
  diff "$work/a/SHA256SUMS" "$work/b/SHA256SUMS" || true
  echo "reproducible-check: builds differ" >&2
  exit 1
fi
if ! grep -q '  paxd-' "$work/a/SHA256SUMS"; then
  echo "reproducible-check: no paxd binary built" >&2
  exit 1
fi
echo "reproducible-check: identical"
