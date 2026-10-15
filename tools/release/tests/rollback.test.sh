#!/usr/bin/env bash
set -euo pipefail
umask 077

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
work=$(mktemp -d)
old_bin=${OLD_BIN:-$root/build/paxd.previous}
new_bin=${NEW_BIN:-$root/dist/paxeer-network/v6.12.0/paxd-v6.12.0-linux-amd64}
rollback=$root/tools/release/rollback.sh
passed=0
failed=0
children=()

cleanup() {
  local rc=$? pid candidate args
  trap - EXIT
  for pid in "${children[@]}"; do
    if [ -r "/proc/$pid/cmdline" ] && tr '\0' '\n' <"/proc/$pid/cmdline" | grep -qxF "$work/validator"; then
      kill -TERM "$pid" 2>/dev/null || true
    fi
    wait "$pid" 2>/dev/null || true
  done
  while read -r pid candidate; do
    [ -n "$pid" ] || continue
    [ -r "/proc/$pid/cmdline" ] || continue
    args=$(tr '\0' '\n' <"/proc/$pid/cmdline")
    if grep -qxF "$work/validator" <<<"$args"; then kill -TERM "$pid" 2>/dev/null || true; fi
  done < <(pgrep -a -x paxd || true)
  if [ "$rc" -eq 0 ]; then
    rm -rf -- "$work"
  else
    printf 'rollback test artifacts: %s\n' "$work" >&2
  fi
  printf 'rollback: %s passed, %s failed\n' "$passed" "$failed"
  exit "$rc"
}
trap cleanup EXIT

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}
sha() { sha256sum -- "$1" | cut -d' ' -f1; }
assert_height() { jq -e --arg height "$2" '.height == $height' "$1" >/dev/null; }
invoke() { bash "$rollback" --env-file "$work/rollout.env" --local --service-manager foreground "$@"; }
wait_halted() {
  local pid=$1 log=$2 height=$3 attempt
  for ((attempt = 0; attempt < 180; attempt++)); do
    if ! kill -0 "$pid" 2>/dev/null; then break; fi
    if [ -r "/proc/$pid/stat" ] && [ "$(awk '{print $3}' "/proc/$pid/stat")" = Z ]; then break; fi
    sleep 1
  done
  [ "$attempt" -lt 180 ] || fail "node did not halt; see $log"
  wait "$pid" || true
  grep -F 'halting node per configuration' "$log" | grep -Eq "height[=:]$height([^0-9]|$)" || fail "missing exact halt height"
  assert_height "$work/validator/data/priv_validator_state.json" "$height"
}

start_to() {
  local height=$1 log=$2
  sed -i -E "s/^halt-height = .*/halt-height = $height/" "$work/validator/config/app.toml"
  "$work/bin/paxd" start --home "$work/validator" >"$log" 2>&1 &
  node_pid=$!
  children+=("$node_pid")
}

write_env() {
  local full=${1:-} full_unit=
  [ -z "$full" ] || full_unit=full.service
  cat >"$work/rollout.env" <<ENV
RELEASE_ID=r1
BIN_DIR=$work/bin
SNAPSHOT_DIR=$work/snapshots
HALT_HEIGHT=20
VALIDATOR_HOMES=$work/validator
VALIDATOR_UNITS=validator.service
FULLNODE_HOMES=$full
FULLNODE_UNITS=$full_unit
ENV
}

setup_chain() {
  [ -x "$old_bin" ] || fail "OLD_BIN must name the real previous paxd binary: $old_bin"
  [ -x "$new_bin" ] || fail "NEW_BIN must name the release candidate: $new_bin"
  [ "$(sha "$old_bin")" != "$(sha "$new_bin")" ] || fail "OLD_BIN and NEW_BIN must be different binaries"
  for tool in jq zstd tar python3 curl pgrep; do command -v "$tool" >/dev/null || fail "$tool is required"; done
  mkdir -p "$work/bin" "$work/snapshots"
  cp -p "$old_bin" "$work/bin/paxd.pre-r1"
  cp -p "$old_bin" "$work/bin/paxd"
  read -r p_evm p_ws p_rpc p_p2p p_grpc p_web < <(
    python3 - <<'PY'
import socket

sockets = [socket.socket() for _ in range(6)]
for connection in sockets:
    connection.bind(("127.0.0.1", 0))
print(" ".join(str(connection.getsockname()[1]) for connection in sockets))
for connection in sockets:
    connection.close()
PY
  )
  PAXD="$work/bin/paxd" LAYERX_PAXEER_HOME="$work/validator" LAYERX_PAXEER_CHAIN_ID=125 \
    LAYERX_PAXEER_DEPLOYER_ADDRESS=0x7e5f4552091a69125d5dfcb7b8c2659029395bdf \
    LAYERX_PAXEER_EVM_PORT="$p_evm" LAYERX_PAXEER_EVM_WS_PORT="$p_ws" \
    LAYERX_PAXEER_RPC_PORT="$p_rpc" LAYERX_PAXEER_P2P_PORT="$p_p2p" \
    LAYERX_PAXEER_GRPC_PORT="$p_grpc" LAYERX_PAXEER_GRPC_WEB_PORT="$p_web" \
    LAYERX_PAXEER_COMMIT_TIMEOUT_NANOSECONDS=1000000000 \
    bash "$root/platform/hosted/paxeer/init-chain.sh" >"$work/init.log" 2>&1
  start_to 20 "$work/old.log"
  wait_halted "$node_pid" "$work/old.log" 20
  tar --zstd -cf "$work/snapshots/validator-pre-v6.12.0.tar.zst" -C "$work/validator" data
  cp -p "$work/validator/data/priv_validator_state.json" "$work/signing-at-h.json"
  "$old_bin" init full --home "$work/full" --chain-id hyperpax_125-1 >"$work/full-init.log" 2>&1
  write_env
}

readiness_requires_material() {
  write_env "$work/full"
  local rc=0
  invoke --readiness --receipt "$work/readiness.json" >"$work/readiness.out" 2>"$work/readiness.err" || rc=$?
  [ "$rc" -eq 1 ]
  [ "$(cat "$work/readiness.err")" = "rollback: no snapshot for $work/full" ]
  jq -e '. == {readiness:false, rollback_executed:false, result:"fail"}' "$work/readiness.json" >/dev/null
  write_env
  invoke --readiness --receipt "$work/readiness.json"
  jq -e '. == {readiness:true, rollback_executed:false, result:"pass"}' "$work/readiness.json" >/dev/null
  mv "$work/bin/paxd.pre-r1" "$work/bin/retained"
  rc=0
  invoke --readiness >"$work/readiness.out" 2>"$work/readiness.err" || rc=$?
  mv "$work/bin/retained" "$work/bin/paxd.pre-r1"
  [ "$rc" -eq 1 ]
  [ "$(cat "$work/readiness.err")" = 'rollback: no previous binary' ]
  write_env
  cmp "$work/signing-at-h.json" "$work/validator/data/priv_validator_state.json"
  [ ! -e "$work/validator/data.v6.12.0-r1" ]
}

advance_candidate() {
  cp -p "$new_bin" "$work/bin/paxd.new"
  mv -f "$work/bin/paxd.new" "$work/bin/paxd"
  printf '{"name":"v6.12.0","height":21}\n' >"$work/validator/data/upgrade-info.json"
  start_to 25 "$work/candidate.log"
  wait_halted "$node_pid" "$work/candidate.log" 25
  cp -p "$work/validator/data/priv_validator_state.json" "$work/signing-after-h.json"
  [ "$(sha "$work/bin/paxd")" = "$(sha "$new_bin")" ]
}

restored_state_is_pre_h1() {
  invoke --execute --phase prepare
  invoke --execute --phase stop
  invoke --execute --phase restore
  assert_height "$work/validator/data/priv_validator_state.json" 20
  assert_height "$work/validator/data.v6.12.0-r1/priv_validator_state.json" 25
  cmp "$work/signing-at-h.json" "$work/validator/data/priv_validator_state.json"
  cmp "$work/signing-after-h.json" "$work/validator/data.v6.12.0-r1/priv_validator_state.json"
  tar --zstd -df "$work/snapshots/validator-pre-v6.12.0.tar.zst" -C "$work/validator"
  [ "$(sha "$work/bin/paxd")" = "$(sha "$old_bin")" ]
  [ ! -e "$work/validator/data/upgrade-info.json" ]
  grep -qx 'halt-height = 0' "$work/validator/config/app.toml"
  local pid args
  while read -r pid args; do
    [[ $args != *"$work/validator/data.v6.12.0-r1"* ]]
  done < <(pgrep -a -x paxd || true)
}

execute_restores_snapshot_and_binary() {
  invoke --execute --phase start --receipt "$work/rollback.json"
  jq -e '. == {readiness:true, rollback_executed:true, result:"pass"}' "$work/rollback.json" >/dev/null
  invoke --verify --receipt "$work/rollback.json"
  jq -e '. == {readiness:true, rollback_executed:true, result:"pass"}' "$work/rollback.json" >/dev/null
  local attempt height rc=0
  for ((attempt = 0; attempt < 120; attempt++)); do
    height=$(curl -fsS --max-time 3 "http://127.0.0.1:$p_rpc/status" | jq -er '.result.sync_info.latest_block_height')
    [ "$height" -gt 22 ] && break
    sleep 1
  done
  [ "$attempt" -lt 120 ]
  curl -fsS --max-time 3 "http://127.0.0.1:$p_rpc/block?height=20" >"$work/restored-block.json"
  jq -e '.result.block.header.height == "20"' "$work/restored-block.json" >/dev/null
  cmp "$work/signing-at-h.json" "$work/validator/.rollback-r1/priv_validator_state.json"
  cmp "$work/signing-after-h.json" "$work/validator/data.v6.12.0-r1/priv_validator_state.json"
  [ "$(sha "$work/bin/paxd")" = "$(sha "$old_bin")" ]
  [ ! -e "$work/validator/data/upgrade-info.json" ]
  invoke --execute >"$work/repeat.out" 2>"$work/repeat.err" || rc=$?
  [ "$rc" -eq 1 ]
  [ "$(cat "$work/repeat.err")" = "rollback: rollback destination already exists for $work/validator" ]
  [ -d "$work/validator/data.v6.12.0-r1" ]
  cp -p "$old_bin" "$work/bin/paxd.pre-r2"
  sed -i 's/^RELEASE_ID=r1$/RELEASE_ID=r2/' "$work/rollout.env"
  invoke --execute --receipt "$work/rollback.json"
  jq -e '. == {readiness:true, rollback_executed:true, result:"pass"}' "$work/rollback.json" >/dev/null
  invoke --verify --receipt "$work/rollback.json"
  cmp "$work/signing-at-h.json" "$work/validator/.rollback-r2/priv_validator_state.json"
  cmp "$work/signing-after-h.json" "$work/validator/data.v6.12.0-r1/priv_validator_state.json"
  [ -d "$work/validator/data.v6.12.0-r2" ]
}

run_case() {
  local name=$1 rc=0
  set +e
  (
    set -e
    "$name"
  )
  rc=$?
  set -e
  if [ "$rc" -eq 0 ]; then
    printf 'PASS %s\n' "$name"
    passed=$((passed + 1))
  else
    printf 'FAIL %s (exit %s)\n' "$name" "$rc"
    failed=$((failed + 1))
  fi
}

setup_chain
run_case readiness_requires_material
[ "$failed" -eq 0 ] || exit 1
advance_candidate
run_case restored_state_is_pre_h1
[ "$failed" -eq 0 ] || exit 1
run_case execute_restores_snapshot_and_binary
[ "$failed" -eq 0 ]
