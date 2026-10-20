#!/usr/bin/env bash
# Runs the release build of tools/release/build-paxd.sh once into a scratch release directory and checks
# the native library order, the toolchain pins, the recorded sums, the directory contents, the final
# touch and the version the built paxd reports over abci_info.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
cd "$root"
if [ -d /usr/local/go/bin ]; then PATH="/usr/local/go/bin:$PATH"; fi
if [ -d "$HOME/.cargo/bin" ]; then PATH="$HOME/.cargo/bin:$PATH"; fi
export LC_ALL=C
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT

want_version=v6.12.0
want_tag=paxeer-network/$want_version
out=$work/release
build_log=$work/build.log
native_libs=(build/liblayerx.a programs/target/release/liblayerx_programs_sandbox.a)
wasm_libs=(libwasmvm.x86_64.so libwasmvm152.x86_64.so libwasmvm155.x86_64.so)
paxd_name=paxd-$want_version-linux-amd64
sidecar_name=price-sidecar-$want_version-linux-amd64
epoch=$(git log -1 --format=%ct HEAD)
rust_channel=$(awk -F'"' '$1 ~ /^channel *= *$/ { print $2; exit }' rust-toolchain.toml)

# A copy of the files the toolchain pins read, so a pin can be changed without touching the tree.
pin_root() {
  local dir=$1
  mkdir -p "$dir/tools/release"
  cp tools/release/build-paxd.sh tools/release/version.json tools/release/toolchain.env "$dir/tools/release/"
  cp rust-toolchain.toml "$dir/"
  printf 'module pin.check\n\ngo %s\n' "$(awk '$1 == "go" { print $2; exit }' go.mod)" > "$dir/go.mod"
}

# Line number of the first build log line matching the fixed string $1, or 0.
log_line() {
  grep -nF -m 1 -- "$1" "$build_log" | cut -d: -f1 || true
}

go_build_line() {
  log_line "build-paxd: go build $paxd_name"
}

start=$(date +%s)
rm -f -- build/liblayerx.a
build_status=0
env -u SOURCE_DATE_EPOCH -u PAXD_COMMIT PAXD_DIST_DIR="$out" PAXD_RELEASE_LAYERXD=1 \
  bash tools/release/build-paxd.sh > "$build_log" 2>&1 || build_status=$?
echo "build-paxd.sh exit $build_status"
tail -n 40 "$build_log"

require_build() {
  [ "$build_status" -eq 0 ] || { echo "the release build exited $build_status" >&2; return 1; }
}

kernel_library_built_first() {
  local ar_line go_line
  ar_line=$(log_line "rcsD build/liblayerx.a")
  go_line=$(go_build_line)
  [ "${ar_line:-0}" -gt 0 ] || { echo "make paxd-native-libs did not archive build/liblayerx.a" >&2; return 1; }
  [ "${go_line:-0}" -gt 0 ] || { echo "the go build of $paxd_name did not start" >&2; return 1; }
  [ "$ar_line" -lt "$go_line" ] || { echo "build/liblayerx.a archived at log line $ar_line, after the go build at $go_line" >&2; return 1; }
  [ -f build/liblayerx.a ] || { echo "build/liblayerx.a is missing" >&2; return 1; }
  [ "$(stat -c %Y build/liblayerx.a)" -ge "$start" ] || { echo "build/liblayerx.a predates this build" >&2; return 1; }
  [ -f build/generated/lxp_checkpoint_settlement.h ] || { echo "the checkpoint settlement header is missing" >&2; return 1; }
}

sandbox_library_built_first() {
  local cargo_line finished_line go_line
  cargo_line=$(log_line "cargo build --locked --release -p layerx-programs-sandbox --features layerx-programs-sandbox/host-ffi")
  go_line=$(go_build_line)
  [ "${cargo_line:-0}" -gt 0 ] || { echo "programs-release-build did not run" >&2; return 1; }
  [ "${go_line:-0}" -gt 0 ] || { echo "the go build of $paxd_name did not start" >&2; return 1; }
  finished_line=$(awk -v from="$cargo_line" 'NR > from && index($0, "Finished `release` profile") { print NR; exit }' "$build_log")
  [ "${finished_line:-0}" -gt 0 ] || { echo "the sandbox release build did not finish" >&2; return 1; }
  [ "$finished_line" -lt "$go_line" ] || { echo "the sandbox release build finished at log line $finished_line, after the go build at $go_line" >&2; return 1; }
  [ -f programs/target/release/liblayerx_programs_sandbox.a ] || { echo "the release sandbox library is missing" >&2; return 1; }
  [ ! -e programs/target/debug ] || { echo "programs/target/debug exists after the release build" >&2; return 1; }
  [ -x "$out/$paxd_name" ] || { echo "$paxd_name did not link" >&2; return 1; }
  if ldd "$out/$paxd_name" | grep -F liblayerx; then echo "$paxd_name links a liblayerx shared object" >&2; return 1; fi
}

rust_pin_enforced() {
  local dir=$work/pin-rust status=0 err
  pin_root "$dir"
  printf '[toolchain]\nchannel = "1.0.0"\nprofile = "minimal"\n' > "$dir/rust-toolchain.toml"
  err=$(RUSTUP_TOOLCHAIN=$rust_channel RUSTUP_AUTO_INSTALL=0 bash "$dir/tools/release/build-paxd.sh" 2>&1 >/dev/null) || status=$?
  [ "$status" -eq 1 ] || { echo "a rust pin mismatch exited $status, want 1" >&2; return 1; }
  [ "$err" = "build-paxd: rust-toolchain.toml pins 1.0.0, found $rust_channel" ] || { echo "rust pin message: $err" >&2; return 1; }
  # The pin of the tree is the installed channel, so the real build passes the check.
  if grep -qF "rust-toolchain.toml pins" "$build_log"; then echo "the release build failed its own rust pin" >&2; return 1; fi
}

cc_pin_enforced() {
  local dir=$work/pin-cc status=0 err have
  have=$(cc -dumpfullversion)
  pin_root "$dir"
  printf 'CC_VERSION=0.0.0\n' > "$dir/tools/release/toolchain.env"
  err=$(RUSTUP_TOOLCHAIN=$rust_channel RUSTUP_AUTO_INSTALL=0 bash "$dir/tools/release/build-paxd.sh" 2>&1 >/dev/null) || status=$?
  [ "$status" -eq 1 ] || { echo "a cc pin mismatch exited $status, want 1" >&2; return 1; }
  [ "$err" = "build-paxd: toolchain.env pins cc 0.0.0, found $have" ] || { echo "cc pin message: $err" >&2; return 1; }
  [ "$(sed -n 's/^CC_VERSION=//p' tools/release/toolchain.env)" = "$have" ] ||
    { echo "tools/release/toolchain.env does not pin the installed cc $have" >&2; return 1; }
  if grep -qF "toolchain.env pins cc" "$build_log"; then echo "the release build failed its own cc pin" >&2; return 1; fi
}

native_library_sums_recorded() {
  local want listed
  [ -f "$out/native-libs.sha256" ] || { echo "native-libs.sha256 is missing" >&2; return 1; }
  want=$(sha256sum "${native_libs[@]}")
  [ "$(cat "$out/native-libs.sha256")" = "$want" ] || { echo "native-libs.sha256 differs from the libraries: $(cat "$out/native-libs.sha256")" >&2; return 1; }
  [ "$(wc -l < "$out/native-libs.sha256")" -eq 2 ] || { echo "native-libs.sha256 does not hold exactly two lines" >&2; return 1; }
  sha256sum --check --strict "$out/native-libs.sha256" || { echo "native-libs.sha256 does not verify from the repository root" >&2; return 1; }
  [ "$(grep -c native-libs.sha256 "$out/SHA256SUMS")" -eq 1 ] || { echo "SHA256SUMS does not hold one native-libs.sha256 line" >&2; return 1; }
  listed=$(awk '$2 == "native-libs.sha256" { print $1 }' "$out/SHA256SUMS")
  [ "$listed" = "$(sha256sum "$out/native-libs.sha256" | cut -d' ' -f1)" ] || { echo "SHA256SUMS does not list native-libs.sha256" >&2; return 1; }
}

sidecar_artifact_listed() {
  local target listed
  [ -x "$out/$sidecar_name" ] || { echo "$sidecar_name is missing" >&2; return 1; }
  target=$(cargo metadata --locked --format-version 1 --no-deps --manifest-path services/platform/Cargo.toml | jq -er '.target_directory')
  cmp -s "$target/release/price-sidecar" "$out/$sidecar_name" || { echo "$sidecar_name is not the cargo release price-sidecar" >&2; return 1; }
  listed=$(awk -v name="$sidecar_name" '$2 == name { print $1 }' "$out/SHA256SUMS")
  [ "$listed" = "$(sha256sum "$out/$sidecar_name" | cut -d' ' -f1)" ] || { echo "SHA256SUMS does not list $sidecar_name" >&2; return 1; }
  (cd "$out" && grep -F "  $sidecar_name" SHA256SUMS | sha256sum --check --strict) || { echo "$sidecar_name does not verify against SHA256SUMS" >&2; return 1; }
  "$out/$sidecar_name" --version || { echo "$sidecar_name --version failed" >&2; return 1; }
}

release_directory_contents() {
  local want have lib pinned paxd_sum
  require_build
  [ "$(bash tools/release/build-paxd.sh --print-tag)" = "$want_tag" ] || { echo "the release tag is not $want_tag" >&2; return 1; }
  want=$(printf '%s\n' SHA256SUMS layerxd-$want_version-linux-amd64 "${wasm_libs[@]}" native-libs.sha256 \
    "$paxd_name" "$sidecar_name" upgrade-info.json | sort)
  have=$(find "$out" -mindepth 1 -printf '%f\n' | sort)
  [ "$have" = "$want" ] || { echo "release directory holds: $(echo $have)" >&2; return 1; }
  [ "$(awk '{ print $2 }' "$out/SHA256SUMS")" = "$(grep -vx SHA256SUMS <<<"$want")" ] ||
    { echo "SHA256SUMS does not list every other file once" >&2; return 1; }
  (cd "$out" && sha256sum --check --strict --quiet SHA256SUMS) || { echo "SHA256SUMS does not verify" >&2; return 1; }
  for lib in "${wasm_libs[@]}"; do
    pinned=$(awk -v name="$lib" '$2 == name { print $1 }' wasm-runtime/libwasmvm-linux.sha256)
    [ "$(sha256sum "$out/$lib" | cut -d' ' -f1)" = "$pinned" ] || { echo "$lib differs from wasm-runtime/libwasmvm-linux.sha256" >&2; return 1; }
  done
  paxd_sum=$(awk -v name="$paxd_name" '$2 == name { print $1 }' "$out/SHA256SUMS")
  jq -e --arg sum "$paxd_sum" '.name == "v6.12.0" and (.info | contains("checksum=sha256:" + $sum))' "$out/upgrade-info.json" >/dev/null ||
    { echo "upgrade-info.json does not name v6.12.0 with the paxd checksum $paxd_sum" >&2; return 1; }
}

epoch_after_last_write() {
  local file
  require_build
  grep -qxF "build-paxd: $want_tag commit $(git rev-parse HEAD) epoch $epoch -> $out" "$build_log" ||
    { echo "the build did not use the commit time $epoch of HEAD" >&2; return 1; }
  for file in "$out"/*; do
    [ "$(stat -c %Y "$file")" = "$epoch" ] || { echo "$(basename "$file") carries mtime $(stat -c %Y "$file"), want $epoch" >&2; return 1; }
  done
}

binary_reports_v6120() {
  local paxd=$out/$paxd_name home=$work/chain ports rpc p_evm p_ws p_p2p p_grpc p_web node_pid i version
  [ -x "$paxd" ] || { echo "$paxd_name was not built" >&2; return 1; }
  version=$("$paxd" version 2>&1)
  [ "$version" = "$want_version" ] || { echo "$paxd_name version prints '$version', want $want_version" >&2; return 1; }
  # Every port is reserved together and released only once all are chosen.
  ports=$(python3 -c '
import socket
s = [socket.socket() for _ in range(6)]
for x in s: x.bind(("127.0.0.1", 0))
print(" ".join(str(x.getsockname()[1]) for x in s))
for x in s: x.close()')
  read -r p_evm p_ws rpc p_p2p p_grpc p_web <<<"$ports"
  PAXD=$paxd LAYERX_PAXEER_HOME=$home LAYERX_PAXEER_CHAIN_ID=125 \
    LAYERX_PAXEER_DEPLOYER_ADDRESS=0x7e5f4552091a69125d5dfcb7b8c2659029395bdf \
    LAYERX_PAXEER_EVM_PORT=$p_evm LAYERX_PAXEER_EVM_WS_PORT=$p_ws LAYERX_PAXEER_RPC_PORT=$rpc \
    LAYERX_PAXEER_P2P_PORT=$p_p2p LAYERX_PAXEER_GRPC_PORT=$p_grpc LAYERX_PAXEER_GRPC_WEB_PORT=$p_web \
    bash services/platform/hosted/paxeer/init-chain.sh > "$work/init.log" 2>&1 || { cat "$work/init.log" >&2; return 1; }
  "$paxd" start --home "$home" > "$work/node.log" 2>&1 &
  node_pid=$!
  version=
  for i in $(seq 1 180); do
    kill -0 "$node_pid" 2>/dev/null || break
    version=$(curl -fsS --max-time 2 -H 'content-type: application/json' \
      -d '{"jsonrpc":"2.0","id":1,"method":"abci_info","params":{}}' "http://127.0.0.1:$rpc/" 2>/dev/null | jq -r '.result.response.version // empty' 2>/dev/null || true)
    [ -n "$version" ] && break
    sleep 1
  done
  if kill -0 "$node_pid" 2>/dev/null; then
    kill "$node_pid"
  else
    echo "paxd exited before serving abci_info" >&2
  fi
  wait "$node_pid" 2>/dev/null || true
  [ "$version" = "$want_version" ] || { echo "abci_info reports version '${version}', want $want_version" >&2; tail -n 40 "$work/node.log" >&2; return 1; }
}

passed=0
failed=0
for t in kernel_library_built_first sandbox_library_built_first rust_pin_enforced cc_pin_enforced \
  native_library_sums_recorded sidecar_artifact_listed release_directory_contents epoch_after_last_write \
  binary_reports_v6120; do
  if (set -e; "$t"); then
    echo "PASS $t"
    passed=$((passed + 1))
  else
    echo "FAIL $t"
    failed=$((failed + 1))
  fi
done
echo "build: $passed passed, $failed failed"
[ "$failed" -eq 0 ]
