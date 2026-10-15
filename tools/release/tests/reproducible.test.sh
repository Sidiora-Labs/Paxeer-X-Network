#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
source "$root/tools/release/reproducible-check.sh"
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
revision=$(git -C "$root" rev-parse HEAD)
epoch=$(git -C "$root" log -1 --format=%ct "$revision")
checkout_a=$work/checkout-a
checkout_b=$work/different/path/checkout-b
outputs=$work/releases

fail() {
  echo "reproducible.test: $*" >&2
  exit 1
}

checkout() {
  local destination=$1
  git -c core.hooksPath=/dev/null clone --quiet --shared --no-checkout "$root" "$destination"
  git -C "$destination" -c core.hooksPath=/dev/null checkout --quiet --detach "$revision"
  cp "$root/tools/release/reproducible-check.sh" "$destination/tools/release/reproducible-check.sh"
}

prepare_builds() {
  [ -f "$root/tools/release/toolchain.env" ] ||
    fail "task 8.4.2 prerequisite missing: tools/release/toolchain.env"
  [ -f "$root/tools/release/upgrade_info.py" ] ||
    fail "release builder prerequisite missing: tools/release/upgrade_info.py"
  checkout "$checkout_a"
  mkdir -p "$outputs" "$checkout_a/build/generated" "$checkout_a/programs/target"
  touch "$checkout_a/build/generated/clean-probe" "$checkout_a/programs/target/clean-probe"
  export PAXD_COMMIT=$revision PAXD_RELEASE_LAYERXD=1
  unset SOURCE_DATE_EPOCH CARGO_TARGET_DIR
  build_twice "$checkout_a" "$outputs"
}

require_builds() {
  [ "$build_status" -eq 0 ] || fail "real release builds failed with exit $build_status (see prepare_builds output)"
}

two_builds_identical() {
  require_builds
  cmp "$outputs/a/SHA256SUMS" "$outputs/b/SHA256SUMS"
  cmp "$outputs/a/native-libs.sha256" "$outputs/b/native-libs.sha256"
  compare_builds "$outputs/a" "$outputs/b" >"$work/identical.out"
  grep -qxF 'reproducible-check: identical' "$work/identical.out"
  (cd "$outputs/a" && sha256sum --check --strict SHA256SUMS)
  (cd "$outputs/b" && sha256sum --check --strict SHA256SUMS)
}

native_libraries_rebuilt_clean() {
  local tree=$work/clean-tree path
  mkdir -p "$tree/build/generated" "$tree/programs/target/debug" "$tree/programs/target/release"
  touch "$tree/build/liblayerx.a"
  touch "$tree/build/generated/probe" "$tree/programs/target/debug/probe" "$tree/programs/target/release/probe"
  touch "$tree/build/keep" "$tree/programs/keep"
  clean_native "$tree"
  for path in build/liblayerx.a build/generated programs/target; do
    [ ! -e "$tree/$path" ] || fail "clean_native retained $path"
  done
  [ -f "$tree/build/keep" ] && [ -f "$tree/programs/keep" ] || fail "clean_native removed unrelated files"
  require_builds
  [ ! -e "$checkout_a/build/generated/clean-probe" ] || fail "generated files survived the second build"
  [ ! -e "$checkout_a/programs/target/clean-probe" ] || fail "program artifacts survived the second build"
  [ -s "$checkout_a/build/generated/lxp_checkpoint_settlement.h" ] || fail "checkpoint header was not rebuilt"
  [ -s "$checkout_a/build/liblayerx.a" ] || fail "kernel library was not rebuilt"
  [ -s "$checkout_a/programs/target/release/liblayerx_programs_sandbox.a" ] || fail "sandbox library was not rebuilt"
  (cd "$checkout_a" && sha256sum --check --strict "$outputs/b/native-libs.sha256")
  cmp "$outputs/a/native-libs.sha256" "$outputs/b/native-libs.sha256"
}

paths_remapped() {
  local directory binary cargo_home=${CARGO_HOME:-$HOME/.cargo}
  require_builds
  checkout "$checkout_b"
  (
    export PAXD_COMMIT=$revision PAXD_RELEASE_LAYERXD=1
    unset SOURCE_DATE_EPOCH CARGO_TARGET_DIR
    PAXD_DIST_DIR="$outputs/c" "$checkout_b/tools/release/build-paxd.sh"
  )
  cmp "$outputs/a/SHA256SUMS" "$outputs/c/SHA256SUMS"
  cmp "$outputs/a/native-libs.sha256" "$outputs/c/native-libs.sha256"
  for directory in "$outputs/a" "$outputs/b" "$outputs/c"; do
    binary=$directory/paxd-v6.12.0-linux-amd64
    [ -x "$binary" ] || fail "missing release binary: $binary"
    strings "$binary" >"$work/binary.strings"
    if grep -F -e "$checkout_a" -e "$checkout_b" -e "$cargo_home" "$work/binary.strings"; then
      fail "absolute build path embedded in $binary"
    fi
  done
}

epoch_from_commit() {
  local directory file count
  require_builds
  for directory in "$outputs/a" "$outputs/b"; do
    [ -f "$directory/upgrade-info.json" ] || fail "missing upgrade-info.json"
    [ -f "$directory/SHA256SUMS" ] || fail "missing SHA256SUMS"
    count=0
    for file in "$directory"/*; do
      [ -f "$file" ] || fail "release entry is not a file: $file"
      [ "$(stat -c %Y "$file")" = "$epoch" ] || fail "wrong commit timestamp: $file"
      count=$((count + 1))
    done
    [ "$count" -eq 9 ] || fail "expected 9 release files, found $count"
  done
}

sum_inputs() {
  local directory digest
  digest=$(sha256sum "$root/tools/release/reproducible-check.sh")
  digest=${digest%% *}
  for directory in "$work/sums-a" "$work/sums-b"; do
    mkdir -p "$directory"
    printf '%s  build/liblayerx.a\n%s  programs/target/release/liblayerx_programs_sandbox.a\n' \
      "$digest" "$digest" >"$directory/native-libs.sha256"
    printf '%s  paxd-test\n' "$digest" >"$directory/SHA256SUMS"
    (cd "$directory" && sha256sum native-libs.sha256 >>SHA256SUMS)
  done
}

expect_refusal() {
  local expected=$1 status=0
  compare_builds "$work/sums-a" "$work/sums-b" >"$work/refusal.out" 2>"$work/refusal.err" || status=$?
  [ "$status" -eq 1 ] || fail "expected refusal exit 1, found $status"
  [ "$(cat "$work/refusal.err")" = "$expected" ] || fail "wrong refusal: $(cat "$work/refusal.err")"
  if grep -qxF 'reproducible-check: identical' "$work/refusal.out"; then
    fail "refusal also reported success"
  fi
}

missing_sums_refused() {
  local directory
  sum_inputs
  compare_builds "$work/sums-a" "$work/sums-b" >"$work/sums-identical.out"
  grep -qxF 'reproducible-check: identical' "$work/sums-identical.out"
  for directory in "$work/sums-a" "$work/sums-b"; do
    sum_inputs
    rm "$directory/native-libs.sha256"
    expect_refusal 'reproducible-check: no native library sums'
    sum_inputs
    : >"$directory/native-libs.sha256"
    expect_refusal 'reproducible-check: no native library sums'
    sum_inputs
    rm "$directory/SHA256SUMS"
    expect_refusal 'reproducible-check: no native library sums'
    sum_inputs
    sed -i '/  native-libs\.sha256$/d' "$directory/SHA256SUMS"
    expect_refusal 'reproducible-check: no native library sums'
    sum_inputs
    sed -i 's/  native-libs\.sha256$/  native-libs.sha256.backup/' "$directory/SHA256SUMS"
    expect_refusal 'reproducible-check: no native library sums'
    sum_inputs
    sed -i '/  paxd-/d' "$directory/SHA256SUMS"
    expect_refusal 'reproducible-check: no paxd binary built'
  done
  sum_inputs
  printf '\n' >>"$work/sums-b/native-libs.sha256"
  expect_refusal 'reproducible-check: native libraries differ'
  sum_inputs
  printf '\n' >>"$work/sums-b/SHA256SUMS"
  expect_refusal 'reproducible-check: builds differ'
}

set +e
(set -e; prepare_builds)
build_status=$?
set -e

passed=0
failed=0
for test in two_builds_identical native_libraries_rebuilt_clean paths_remapped epoch_from_commit missing_sums_refused; do
  set +e
  (set -e; "$test")
  status=$?
  set -e
  if [ "$status" -eq 0 ]; then
    echo "PASS $test"
    passed=$((passed + 1))
  else
    echo "FAIL $test (exit $status)"
    failed=$((failed + 1))
  fi
done
echo "reproducible: $passed passed, $failed failed"
[ "$failed" -eq 0 ]
