#!/usr/bin/env bash
# Exercises assets_match_build of tools/release/verify-release.sh on a fixture release directory of the
# nine release files and its published copy, and the refusals for a differing sum, a missing asset and a
# published asset whose bytes differ from its SHA256SUMS line.
set -euo pipefail
export LC_ALL=C

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
# shellcheck source=../verify-release.sh
. "$root/tools/release/verify-release.sh"
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT

version=v6.12.0
files=(
  "paxd-$version-linux-amd64"
  "layerxd-$version-linux-amd64"
  "price-sidecar-$version-linux-amd64"
  libwasmvm.x86_64.so
  libwasmvm152.x86_64.so
  libwasmvm155.x86_64.so
  native-libs.sha256
  upgrade-info.json
)

# A build host release directory $1 of the eight files and their SHA256SUMS, and its published copy $2.
fixture() {
  local dist=$1 release=$2 f
  mkdir -p "$dist"
  for f in "${files[@]}"; do printf '%s contents\n' "$f" > "$dist/$f"; done
  (cd "$dist" && printf '%s\n' "${files[@]}" | sort | xargs sha256sum > SHA256SUMS)
  cp -a "$dist" "$release"
}

# Runs assets_match_build on $1 $2; sets out, err and code.
run_match() {
  code=0
  assets_match_build "$1" "$2" > "$work/out" 2> "$work/err" || code=$?
  out=$(cat "$work/out")
  err=$(cat "$work/err")
}

assets_match_build_on_fixture() {
  local dir=$work/match
  fixture "$dir/dist" "$dir/release"
  run_match "$dir/release" "$dir/dist"
  [ "$code" = 0 ] || { echo "exit $code, want 0: $err" >&2; return 1; }
  [ "$out" = "verify-release: 9 assets match" ] || { echo "printed '$out'" >&2; return 1; }
  [ -z "$err" ] || { echo "stderr '$err'" >&2; return 1; }
}

mismatched_sum_refused() {
  local dir=$work/sum name="paxd-$version-linux-amd64"
  fixture "$dir/dist" "$dir/release"
  printf 'rebuilt on the publish runner\n' > "$dir/release/$name"
  (cd "$dir/release" && printf '%s\n' "${files[@]}" | sort | xargs sha256sum > SHA256SUMS)
  local build publish
  build=$(sha256sum "$dir/dist/$name" | cut -d' ' -f1)
  publish=$(sha256sum "$dir/release/$name" | cut -d' ' -f1)
  run_match "$dir/release" "$dir/dist"
  [ "$code" = 1 ] || { echo "exit $code, want 1" >&2; return 1; }
  [ -z "$out" ] || { echo "printed '$out' on a mismatch" >&2; return 1; }
  [ "$err" = "verify-release: $name sum differs: build $build, publish $publish" ] ||
    { echo "stderr '$err'" >&2; return 1; }
}

missing_asset_refused() {
  local dir=$work/missing name=upgrade-info.json
  fixture "$dir/dist" "$dir/release"
  rm "$dir/release/$name"
  run_match "$dir/release" "$dir/dist"
  [ "$code" = 1 ] || { echo "exit $code, want 1" >&2; return 1; }
  [ -z "$out" ] || { echo "printed '$out' with a missing asset" >&2; return 1; }
  grep -qxF "verify-release: $name is not a release asset" <<<"$err" || { echo "stderr '$err'" >&2; return 1; }
}

corrupt_asset_refused() {
  local dir=$work/corrupt name=libwasmvm152.x86_64.so
  fixture "$dir/dist" "$dir/release"
  printf 'truncated upload\n' > "$dir/release/$name"
  run_match "$dir/release" "$dir/dist"
  [ "$code" = 1 ] || { echo "exit $code, want 1" >&2; return 1; }
  [ -z "$out" ] || { echo "printed '$out' with a corrupt asset" >&2; return 1; }
  grep -qxF "$name: FAILED" <<<"$err" || { echo "stderr '$err'" >&2; return 1; }
  grep -qxF "verify-release: files of $dir/release do not match its SHA256SUMS" <<<"$err" ||
    { echo "stderr '$err'" >&2; return 1; }
}

usage_refused() {
  local c=0
  bash "$root/tools/release/verify-release.sh" --tag "paxeer-network/$version" > /dev/null 2>&1 || c=$?
  [ "$c" = 2 ] || { echo "missing --dist exited $c, want 2" >&2; return 1; }
  c=0
  bash "$root/tools/release/verify-release.sh" --tag "paxeer-network/$version" --dist "$work" --sums x > /dev/null 2>&1 || c=$?
  [ "$c" = 2 ] || { echo "unknown flag exited $c, want 2" >&2; return 1; }
}

passed=0
failed=0
for t in assets_match_build_on_fixture mismatched_sum_refused missing_asset_refused corrupt_asset_refused usage_refused; do
  if "$t"; then
    echo "ok $t"
    passed=$((passed + 1))
  else
    echo "FAIL $t"
    failed=$((failed + 1))
  fi
done
echo "verify-release.test: $passed passed, $failed failed"
[ "$failed" = 0 ]
