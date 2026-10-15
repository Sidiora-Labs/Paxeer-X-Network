#!/usr/bin/env bash
# Tests of tools/release/rehearse-v6120.sh and tools/release/rehearsal/pool_parity.py.
#
# export_step_produces_genesis and nodes_halt_at_rehearsal_height run against a one-validator chain
# the previous binary (REHEARSE_OLD_BIN, default the paxd on PATH) builds and runs locally.
# plan_applied_at_next_block and result_file_keys drive the whole rehearsal from REHEARSE_ENV_FILE, a
# filled copy of tools/release/env/rehearsal.env.example over a stopped mainnet data copy and the
# release candidate; without it both fail and name the missing input.
set -uo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
driver=$root/tools/release/rehearse-v6120.sh
parity=$root/tools/release/rehearsal/pool_parity.py
scratch=$(mktemp -d)
fixture_pid=""
trap '[ -z "$fixture_pid" ] || kill "$fixture_pid" 2>/dev/null; rm -rf "$scratch"' EXIT
pass=0
failed=0
old_bin=${REHEARSE_OLD_BIN:-$(command -v paxd || true)}
rehearse_env=${REHEARSE_ENV_FILE:-}
port_base=$((20000 + ($$ % 400) * 100))
chain=hyperpax_125-1

ok() { echo "PASS: $1"; pass=$((pass + 1)); }
bad() { echo "FAIL: $1: $2" >&2; failed=$((failed + 1)); }

# run_case <name>: runs case_<name> in a subshell; a non-zero exit or a printed reason fails it.
run_case() {
	local name=$1 reason
	if reason=$("case_$name" 2>&1); then
		ok "$name"
	else
		bad "$name" "$(tail -n 5 <<<"$reason")"
	fi
}

need_old_bin() {
	[ -n "$old_bin" ] && [ -x "$old_bin" ] || { echo "no previous binary: set REHEARSE_OLD_BIN or put paxd on PATH"; return 1; }
}

# fixture: a one-validator chain run in validator mode by the previous binary to height 8 and stopped.
fixture() {
	[ -d "$scratch/source" ] && return 0
	local h=$scratch/source address height=""
	need_old_bin || return 1
	"$old_bin" init fixture --chain-id "$chain" --home "$h" >/dev/null 2>&1 || { echo "init failed"; return 1; }
	"$old_bin" keys add fixture --keyring-backend test --home "$h" >/dev/null 2>&1 || { echo "keys add failed"; return 1; }
	address=$("$old_bin" keys show fixture -a --keyring-backend test --home "$h")
	"$old_bin" add-genesis-account "$address" 1000000000000uhpx --home "$h" >/dev/null 2>&1 || { echo "add-genesis-account failed"; return 1; }
	"$old_bin" gentx fixture 1000000000uhpx --chain-id "$chain" --keyring-backend test --home "$h" >/dev/null 2>&1 \
		|| { echo "gentx failed"; return 1; }
	"$old_bin" collect-gentxs --home "$h" >/dev/null 2>&1 || { echo "collect-gentxs failed"; return 1; }
	sed -i "0,/^\[/ s|^mode = .*|mode = \"validator\"|; s|^laddr = \"tcp://0.0.0.0:26657\"|laddr = \"tcp://127.0.0.1:$((port_base + 90))\"|; s|^laddr = \"tcp://0.0.0.0:26656\"|laddr = \"tcp://127.0.0.1:$((port_base + 91))\"|" "$h/config/config.toml"
	sed -i "s|^http_port = .*|http_port = $((port_base + 92))|; s|^ws_port = .*|ws_port = $((port_base + 93))|; s|^address = \"0.0.0.0:9090\"|address = \"127.0.0.1:$((port_base + 94))\"|; s|^address = \"0.0.0.0:9091\"|address = \"127.0.0.1:$((port_base + 95))\"|; s|^address = \"tcp://0.0.0.0:1317\"|address = \"tcp://127.0.0.1:$((port_base + 96))\"|" "$h/config/app.toml"
	"$old_bin" start --home "$h" >"$scratch/fixture.log" 2>&1 &
	fixture_pid=$!
	for _ in $(seq 1 600); do
		height=$(curl -sf --max-time 2 "http://127.0.0.1:$((port_base + 90))/status" | jq -r '(.result // .).sync_info.latest_block_height' 2>/dev/null || true)
		[[ "$height" =~ ^[0-9]+$ ]] && [ "$height" -ge 8 ] && break
		kill -0 "$fixture_pid" 2>/dev/null || { echo "fixture node exited: $(tail -n 3 "$scratch/fixture.log")"; return 1; }
		sleep 1
	done
	kill -TERM "$fixture_pid"
	wait "$fixture_pid" 2>/dev/null
	fixture_pid=""
	[[ "$height" =~ ^[0-9]+$ ]] && [ "$height" -ge 8 ] || { echo "fixture chain did not reach height 8"; return 1; }
}

case_check_mode_touches_nothing() {
	local out work=$scratch/check-work
	need_old_bin || return 1
	out=$(bash "$driver" --check --source-data "$scratch" --source-genesis "$driver" --new-bin "$old_bin" --work "$work" 2>&1)
	[ $? -eq 1 ] || { echo "check without OLD_BIN did not exit 1"; return 1; }
	grep -q 'OLD_BIN is required' <<<"$out" || { echo "check without OLD_BIN printed: $out"; return 1; }
	[ ! -e "$work" ] || { echo "check without OLD_BIN created WORK"; return 1; }
	out=$(bash "$driver" --check --source-data "$scratch" --source-genesis "$driver" --old-bin "$old_bin" \
		--new-bin "$old_bin" --work "$work" --port-base "$port_base" 2>&1)
	[ $? -eq 0 ] || { echo "check with every key exited non-zero: $out"; return 1; }
	grep -q 'check passed' <<<"$out" || { echo "check printed: $out"; return 1; }
	[ ! -e "$work" ] || { echo "check created WORK"; return 1; }
	ls /run/systemd/system | grep -q '^paxd-rehearsal-' && { echo "check left a rehearsal unit"; return 1; }
	return 0
}

case_pool_parity_units() {
	python3 - "$parity" <<'PY'
import importlib.util
import sys

spec = importlib.util.spec_from_file_location("pool_parity", sys.argv[1])
pp = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pp)

assert pp.check_pool_count([412, 37], 449) == "pools 449 of 449"
try:
    pp.check_pool_count([412, 37], 448)
    raise AssertionError("a short launchpad count passed")
except pp.ParityError as err:
    assert str(err) == "pools 448 of 449: getPoolCount() of 0x0000000000000000000000000000000000001017 differs from the registries", err

pool = "0x00000000000000000000000000000000000000a1"
words = (50000000000, 1250000000, 800000000000000000000000000)
assert pp.check_reserves({pool: words}, {pool: words}) == "reserves 1 of 1"
try:
    pp.check_reserves({pool: words}, {pool: (50000000000, 1250000001, 800000000000000000000000000)})
    raise AssertionError("a changed realUsdl passed")
except pp.ParityError as err:
    assert str(err) == f"pool {pool} realUsdl: 1250000000 at the halt height, 1250000001 after the upgrade", err
try:
    pp.check_reserves({pool: words}, {})
    raise AssertionError("a missing pool passed")
except pp.ParityError as err:
    assert str(err) == f"pool {pool} has no reserves after the upgrade", err

raw = b"".join(value.to_bytes(32, "big") for value in words)
assert pp.decode_words(raw, 3, "getReserves()") == list(words)
try:
    pp.decode_words(raw[:-1], 3, "getReserves()")
    raise AssertionError("a short reserves word passed")
except pp.ParityError as err:
    assert str(err) == "getReserves() returned 95 bytes, expected 96", err

array = (32).to_bytes(32, "big") + (2).to_bytes(32, "big") + bytes(12) + bytes.fromhex("a1" * 20) + bytes(12) + bytes.fromhex("b2" * 20)
assert pp.decode_address_array(array, "getAllPools") == ["0x" + "a1" * 20, "0x" + "b2" * 20]
try:
    pp.decode_address_array(array[:-32], "getAllPools")
    raise AssertionError("a truncated array passed")
except pp.ParityError as err:
    assert str(err) == "getAllPools returned 96 bytes for 2 addresses", err
PY
}

case_export_step_produces_genesis() {
	local work=$scratch/export-work out
	fixture || return 1
	out=$(bash "$driver" --source-data "$scratch/source/data" --source-genesis "$scratch/source/config/genesis.json" \
		--old-bin "$old_bin" --new-bin "$old_bin" --work "$work" --port-base "$port_base" --chain-id "$chain" \
		--export-height 5 --stop-after export 2>&1) || { echo "export step failed: $out"; return 1; }
	jq -e --arg chain "$chain" '.chain_id == $chain and .initial_height == "6" and (.app_state.staking.validators | length) == 1' \
		"$work/exported-genesis.json" >/dev/null || { echo "exported genesis: $(jq -c '{chain_id, initial_height}' "$work/exported-genesis.json")"; return 1; }
	[ ! -e "$work/export-home/data" ] || { echo "the export copy of the data was left behind"; return 1; }
	[ -d "$scratch/source/data" ] || { echo "the source data was moved"; return 1; }
}

case_nodes_halt_at_rehearsal_height() {
	local work=$scratch/halt-work out i
	fixture || return 1
	out=$(bash "$driver" --source-data "$scratch/source/data" --source-genesis "$scratch/source/config/genesis.json" \
		--old-bin "$old_bin" --new-bin "$old_bin" --work "$work" --port-base "$port_base" --chain-id "$chain" \
		--export-height 5 --halt-height 9 --wait-seconds 300 --stop-after halt 2>&1) || { echo "halt step failed: $out"; return 1; }
	for i in 0 1 2 3; do
		grep -Eq 'halting node per configuration.*height=9([^0-9]|$)' "$work/logs/node$i-old.log" \
			|| { echo "node $i did not log the halt line at 9"; return 1; }
		pgrep -a -x paxd | grep -qF -- "--home $work/node$i" && { echo "node $i still runs"; return 1; }
	done
	ls /run/systemd/system | grep -q "^paxd-rehearsal-$(printf '%s' "$work" | sha256sum | cut -c1-8)-" \
		&& { echo "the halt step left its units installed"; return 1; }
	return 0
}

full_run() {
	[ -n "$rehearse_env" ] && [ -r "$rehearse_env" ] \
		|| { echo "REHEARSE_ENV_FILE must name a filled rehearsal env over a stopped mainnet data copy and the release candidate"; return 1; }
	[ -f "$scratch/full.done" ] && return "$(cat "$scratch/full.done")"
	bash "$driver" --env-file "$rehearse_env" --receipt "$scratch/receipt.json" >"$scratch/full.log" 2>&1
	echo $? >"$scratch/full.done"
	return "$(cat "$scratch/full.done")"
}

full_work() {
	sed -nE 's/^(export )?WORK=["'\'']?([^"'\'']*)["'\'']?$/\2/p' "$rehearse_env" | tail -n 1
}

case_plan_applied_at_next_block() {
	local work halt i
	full_run || { echo "the rehearsal failed: $(tail -n 5 "$scratch/full.log" 2>/dev/null)"; return 1; }
	work=$(full_work)
	halt=$(sed -n 's/^halt_height=//p' "$work/rehearsal-result.env")
	for i in 0 1 2 3; do
		[ "$(grep -Ec 'applying upgrade.*name=v6\.12\.0([^0-9.]|$)' "$work/logs/node$i-new.log")" = 1 ] \
			|| { echo "node $i did not log applying upgrade v6.12.0 once"; return 1; }
		[ "$(grep -c panic "$work/logs/node$i-new.log")" = 0 ] || { echo "node $i logged panic"; return 1; }
		jq -e --argjson h "$((halt + 1))" '.name == "v6.12.0" and .height == $h' "$work/node$i/data/upgrade-info.json" >/dev/null 2>&1 \
			&& { echo "rollback left the upgrade file of node $i"; return 1; }
	done
	return 0
}

case_result_file_keys() {
	local work keys sums_sha
	full_run || { echo "the rehearsal failed: $(tail -n 5 "$scratch/full.log" 2>/dev/null)"; return 1; }
	work=$(full_work)
	keys=$(cut -d= -f1 "$work/rehearsal-result.env" | tr '\n' ' ')
	[ "$keys" = "upgrade halt_height export_height final_height old_bin_sha256 new_bin_sha256 pools markets oracle_fresh feed_keys precompiles_served rollback_rehearsed " ] \
		|| { echo "result keys: $keys"; return 1; }
	grep -qx 'upgrade=v6.12.0' "$work/rehearsal-result.env" || { echo "upgrade is not v6.12.0"; return 1; }
	grep -qx 'rollback_rehearsed=true' "$work/rehearsal-result.env" || { echo "rollback_rehearsed is not true"; return 1; }
	grep -qx 'feed_keys=4' "$work/rehearsal-result.env" || { echo "feed_keys is not 4"; return 1; }
	sums_sha=$(awk '$2 ~ /paxd-v6\.12\.0-linux-amd64$/ { print $1 }' "$(dirname "$(sed -nE 's/^(export )?NEW_BIN=["'\'']?([^"'\'']*)["'\'']?$/\2/p' "$rehearse_env")")/SHA256SUMS")
	grep -qx "new_bin_sha256=$sums_sha" "$work/rehearsal-result.env" || { echo "new_bin_sha256 is not the paxd line of SHA256SUMS"; return 1; }
	jq -e --arg sha "$sums_sha" '.result == "pass" and .rollback_rehearsed == true and .new_bin_sha256 == $sha
		and .nodes_checked == 4 and .nodes_passed == 4 and .feed_keys == 4' "$scratch/receipt.json" >/dev/null \
		|| { echo "receipt: $(cat "$scratch/receipt.json")"; return 1; }
}

run_case check_mode_touches_nothing
run_case pool_parity_units
run_case export_step_produces_genesis
run_case nodes_halt_at_rehearsal_height
run_case plan_applied_at_next_block
run_case result_file_keys

echo "rehearse.test.sh: $pass passed, $failed failed"
[ "$failed" -eq 0 ]
