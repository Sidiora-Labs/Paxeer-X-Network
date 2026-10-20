#!/usr/bin/env bash
# Tests of tools/release/rehearse-v6120.sh and tools/release/rehearsal/pool_parity.py.
#
# export_step_produces_genesis and nodes_halt_at_rehearsal_height run against a four-validator chain
# the previous binary (REHEARSE_OLD_BIN, default build/paxd.previous, else the paxd on PATH) runs locally.
# plan_applied_at_next_block and result_file_keys drive the whole rehearsal from REHEARSAL_ENV_FILE
# (default .env.rehearsal-v6120), a filled copy of tools/release/env/rehearsal.env.example over a
# stopped mainnet data copy and the release candidate, unless its WORK already holds a finished run;
# without it both fail and name the missing input. The receipt is REHEARSAL_RECEIPT (default
# tools/release/rollout/v6.12.0/rehearsal.json).
#
# The cases from applied_height_on_every_node on read that finished run: applied height, seeded
# markets, oracle feed keys and fresh prices, precompile presence at the activation height, block
# hashes, version and logs, the rollback leg and feed-key association. Its WORK holds
# rehearsal-result.env and the raw responses the driver recorded under WORK/evidence:
#   node<i>/upgrade-applied.json       paxd q upgrade applied v6.12.0 --node <node i> -o json
#   node<i>/block-<h>.json             block?height=h for h in HR+1..HR+10
#   node<i>/abci-info.json             abci_info on the new binary after HR+10
#   node<i>/new-bin.log                log of the new binary
#   node<i>/halt-abci-info.json        abci_info at HR on the old binary, before the upgrade
#   node<i>/rollback-abci-info.json    abci_info after the rollback restore, before the next block
#   node<i>/rollback-status.json       status at the end of the rollback leg
#   markets/totalMarkets.json          eth_call totalMarkets() of 0x1015 at HR+1
#   markets/getMarketCount.json        eth_call getMarketCount() of 0x1020 at HR+1
#   markets/getMarket-<m>.json         eth_call getMarket(m) of 0x1020 at HR+1
#   oracle/getPrice-<m>.json           eth_call getPrice(m) of 0x1020 after the sidecars ran
#   oracle/getFeedKey-<validator>.json eth_call getFeedKey(validator) of 0x1020
#   precompiles/<h>/<address>.json     eth_call of the view of each served address at HR and HR+1
#   code/<h>/<address>.json            eth_getCode of 0x1026 and 0x1027 at HR and HR+1
#   feedkey/<validator>/evm-address    operator EVM address of the validator
#   feedkey/<validator>/before-association.out  output of registerFeedKey before associate-address
#   feedkey/<validator>/associate.json          paxd tx evm associate-address result
#   feedkey/<validator>/register.json           eth_call registerFeedKey(key) from the EVM address
#   feedkey/<validator>/getValidatorByFeedAccount.json
# <validator> is the 20 bytes of the operator address as lowercase 0x hex; <address> is lowercase.
set -uo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
cd "$root"
export LC_ALL=C
driver=$root/tools/release/rehearse-v6120.sh
parity=$root/tools/release/rehearsal/pool_parity.py
scratch=$(mktemp -d)
fixture_pids=()
trap '[ ${#fixture_pids[@]} -eq 0 ] || kill "${fixture_pids[@]}" 2>/dev/null; rm -rf "$scratch"' EXIT
pass=0
failed=0
old_bin=${REHEARSE_OLD_BIN:-$([ -x "$root/build/paxd.previous" ] && echo "$root/build/paxd.previous" || command -v paxd || true)}
rehearse_env=${REHEARSAL_ENV_FILE:-.env.rehearsal-v6120}
receipt=${REHEARSAL_RECEIPT:-tools/release/rollout/v6.12.0/rehearsal.json}
port_base=$((20000 + ($$ % 400) * 100))
chain=hyperpax_125-1
want_upgrade=v6.12.0
nodes=(0 1 2 3)
want_symbols=(PAX SID NAS100 ETH SPX500 BTC BNB SOL HYPE TSLA GOOGL XAU NVDA ASTER LINK XRP TRUMP AVAX)
served_existing=(0x0000000000000000000000000000000000001015 0x0000000000000000000000000000000000001017)
served_new=(0x0000000000000000000000000000000000001020 0x0000000000000000000000000000000000001021
	0x0000000000000000000000000000000000001022 0x0000000000000000000000000000000000001023
	0x0000000000000000000000000000000000001024 0x0000000000000000000000000000000000001025)
codeless=(0x0000000000000000000000000000000000001026 0x0000000000000000000000000000000000001027)

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

# Decodes the ABI return words of a view: uint <hex>, address <hex>, market <hex> (prints id and
# symbol), price_fresh <hex>, feed_key <hex> (prints key length, key and feed account), view <hex>.
cat >"$scratch/abi.py" <<'PY'
import sys


def words(data):
    if not data.startswith("0x") or (len(data) - 2) % 64 != 0:
        raise SystemExit(f"abi: return data {data[:80]} is not whole 32-byte words")
    body = bytes.fromhex(data[2:])
    return body, [int.from_bytes(body[i:i + 32], "big") for i in range(0, len(body), 32)]


def word(ws, i):
    if i >= len(ws):
        raise SystemExit(f"abi: word {i} past the {len(ws)} returned")
    return ws[i]


def dyn_bytes(body, offset):
    if offset + 32 > len(body):
        raise SystemExit(f"abi: offset {offset} past the return data")
    n = int.from_bytes(body[offset:offset + 32], "big")
    if offset + 32 + n > len(body):
        raise SystemExit(f"abi: length {n} at {offset} past the return data")
    return body[offset + 32:offset + 32 + n]


def address(value):
    if value >> 160:
        raise SystemExit(f"abi: word {value:#x} is not an address")
    return f"0x{value:040x}"


op, data = sys.argv[1], sys.argv[2]
body, ws = words(data)
if op == "uint":
    print(word(ws, 0))
elif op == "address":
    print(address(word(ws, 0)))
elif op == "market":
    base = word(ws, 0)
    _, tuple_ws = words("0x" + body[base:].hex())
    symbol = dyn_bytes(body, base + word(tuple_ws, 1)).decode("ascii")
    print(word(tuple_ws, 0), symbol)
elif op == "price_fresh":
    fresh = word(ws, 5)
    if fresh not in (0, 1):
        raise SystemExit(f"abi: fresh word {fresh:#x} is not a bool")
    print("true" if fresh else "false")
elif op == "feed_key":
    key = dyn_bytes(body, word(ws, 0))
    print(len(key), key.hex(), address(word(ws, 1)))
elif op == "view":
    if not ws:
        raise SystemExit("abi: the view returned no word")
    print(len(ws))
else:
    raise SystemExit(f"abi: unknown op {op}")
PY

abi() { python3 "$scratch/abi.py" "$@"; }

# key_of <file> <key>: value of the last KEY=VALUE line, an optional export prefix and quotes allowed.
key_of() {
	[ -r "$1" ] || { echo "$1 is missing" >&2; return 1; }
	awk -v k="$2" '
		{ sub(/^[[:space:]]*export[[:space:]]+/, "") }
		index($0, k "=") == 1 { v = substr($0, length(k) + 2); found = 1 }
		END {
			if (!found) exit 1
			if (v ~ /^".*"$/ || v ~ /^\047.*\047$/) v = substr(v, 2, length(v) - 2)
			print v
		}' "$1" || { echo "$1 has no $2" >&2; return 1; }
}

# rpc_result <file>: the result of a recorded JSON-RPC response; refuses an error response.
rpc_result() {
	[ -r "$1" ] || { echo "$1 is missing" >&2; return 1; }
	jq -er 'if .error != null then error("error response: \(.error | tostring)") else .result end' "$1"
}

# json_field <file> <jq path>: one non-empty field of a recorded JSON document.
json_field() {
	[ -r "$1" ] || { echo "$1 is missing" >&2; return 1; }
	jq -er "$2 | select(. != null and . != \"\") | tostring" "$1" || { echo "$1 has no $2" >&2; return 1; }
}

need_old_bin() {
	[ -n "$old_bin" ] && [ -x "$old_bin" ] || { echo "no previous binary: set REHEARSE_OLD_BIN or put paxd on PATH"; return 1; }
}

# fixture: a chain of four validators of equal power run in validator mode by the previous binary
# to height 8 and stopped; $scratch/source is the home of validator 0.
fixture() {
	[ -d "$scratch/source" ] && return 0
	local i j h address height="" peers=() others base
	need_old_bin || return 1
	for i in 0 1 2 3; do
		h=$scratch/fixture$i
		"$old_bin" init "fixture-$i" --chain-id "$chain" --home "$h" >/dev/null 2>&1 || { echo "init $i failed"; return 1; }
		"$old_bin" keys add "fixture-$i" --keyring-backend test --home "$h" >/dev/null 2>&1 || { echo "keys add $i failed"; return 1; }
		address=$("$old_bin" keys show "fixture-$i" -a --keyring-backend test --home "$h")
		"$old_bin" add-genesis-account "$address" 1000000000000uhpx --home "$scratch/fixture0" >/dev/null 2>&1 \
			|| { echo "add-genesis-account $i failed"; return 1; }
	done
	for i in 1 2 3; do cp "$scratch/fixture0/config/genesis.json" "$scratch/fixture$i/config/genesis.json"; done
	for i in 0 1 2 3; do
		h=$scratch/fixture$i
		"$old_bin" gentx "fixture-$i" 1000000000uhpx --chain-id "$chain" --keyring-backend test --home "$h" >/dev/null 2>&1 \
			|| { echo "gentx $i failed"; return 1; }
		[ "$i" = 0 ] || cp "$h"/config/gentx/*.json "$scratch/fixture0/config/gentx/"
	done
	"$old_bin" collect-gentxs --home "$scratch/fixture0" >/dev/null 2>&1 || { echo "collect-gentxs failed"; return 1; }
	for i in 0 1 2 3; do
		[ "$i" = 0 ] || cp "$scratch/fixture0/config/genesis.json" "$scratch/fixture$i/config/genesis.json"
		peers+=("$("$old_bin" tendermint show-node-id --home "$scratch/fixture$i")@127.0.0.1:$((port_base + 80 + 4 * i + 1))")
	done
	for i in 0 1 2 3; do
		h=$scratch/fixture$i
		base=$((port_base + 80 + 4 * i))
		others=""
		for j in 0 1 2 3; do [ "$j" = "$i" ] || others=${others:+$others,}${peers[$j]}; done
		sed -i "0,/^\[/ s|^mode = .*|mode = \"validator\"|; s|^laddr = \"tcp://0.0.0.0:26657\"|laddr = \"tcp://127.0.0.1:$base\"|; s|^laddr = \"tcp://0.0.0.0:26656\"|laddr = \"tcp://127.0.0.1:$((base + 1))\"|; s|^persistent-peers = .*|persistent-peers = \"$others\"|; s|^allow-duplicate-ip = .*|allow-duplicate-ip = true|; s|^pex = .*|pex = false|" "$h/config/config.toml"
		sed -i "s|^http_port = .*|http_port = $((base + 2))|; s|^ws_enabled = .*|ws_enabled = false|; s|^address = \"0.0.0.0:9090\"|address = \"127.0.0.1:$((base + 3))\"|; s|^address = \"tcp://0.0.0.0:1317\"|address = \"tcp://127.0.0.1:$((port_base + 99 - i))\"|" "$h/config/app.toml"
		sed -i "/^\[grpc-web\]/,/^\[/ s|^enable = .*|enable = false|" "$h/config/app.toml"
		"$old_bin" start --home "$h" >"$scratch/fixture$i.log" 2>&1 &
		fixture_pids+=("$!")
	done
	for _ in $(seq 1 600); do
		height=$(curl -sf --max-time 2 "http://127.0.0.1:$((port_base + 80))/status" | jq -r '(.result // .).sync_info.latest_block_height' 2>/dev/null || true)
		[[ "$height" =~ ^[0-9]+$ ]] && [ "$height" -ge 8 ] && break
		for i in 0 1 2 3; do
			kill -0 "${fixture_pids[$i]}" 2>/dev/null || { echo "fixture node $i exited: $(tail -n 3 "$scratch/fixture$i.log")"; return 1; }
		done
		sleep 1
	done
	kill -TERM "${fixture_pids[@]}" 2>/dev/null
	wait "${fixture_pids[@]}" 2>/dev/null
	fixture_pids=()
	[[ "$height" =~ ^[0-9]+$ ]] && [ "$height" -ge 8 ] || { echo "fixture chain did not reach height 8"; return 1; }
	mv "$scratch/fixture0" "$scratch/source"
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
covered = (1250000000, 800000000000000000000000000)
assert pp.check_reserves({pool: words}, {pool: covered}, {pool: words}) == "reserves 1 of 1"
try:
    pp.check_reserves({pool: words}, {pool: covered}, {pool: (50000000000, 1250000001, 800000000000000000000000000)})
    raise AssertionError("a changed realUsdl passed")
except pp.ParityError as err:
    assert str(err) == f"pool {pool} realUsdl: 1250000000 expected from 1250000000 at the halt height, 1250000001 after the upgrade", err
try:
    pp.check_reserves({pool: words}, {pool: covered}, {})
    raise AssertionError("a missing pool passed")
except pp.ParityError as err:
    assert str(err) == f"pool {pool} has no reserves after the upgrade", err
try:
    pp.check_reserves({pool: words}, {}, {pool: words})
    raise AssertionError("a pool without held amounts passed")
except pp.ParityError as err:
    assert str(err) == f"pool {pool} has no held amounts at the halt height", err

short = (1249999900, 799999999999999999999999000)
assert pp.expected_reserves(words, short) == (50000000000, 1249999900, 799999999999999999999999000)
assert pp.expected_reserves(words, (1250000100, 800000000000000000000001000)) == words
capped = (50000000000, 1249999900, 799999999999999999999999000)
assert pp.check_reserves({pool: words}, {pool: short}, {pool: capped}) == "reserves 1 of 1"
try:
    pp.check_reserves({pool: words}, {pool: short}, {pool: words})
    raise AssertionError("book reserves above the held USDL passed")
except pp.ParityError as err:
    assert str(err) == f"pool {pool} realUsdl: 1249999900 expected from 1250000000 at the halt height, 1250000000 after the upgrade", err
try:
    pp.check_reserves({pool: words}, {pool: short}, {pool: (50000000000, 1249999900, 800000000000000000000000000)})
    raise AssertionError("a token reserve above the held tokens passed")
except pp.ParityError as err:
    assert str(err) == (
        f"pool {pool} tokenReserve: 799999999999999999999999000 expected from 800000000000000000000000000"
        " at the halt height, 800000000000000000000000000 after the upgrade"
    ), err

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
	jq -e --arg chain "$chain" '.chain_id == $chain and .initial_height == "6" and (.app_state.staking.validators | length) == 4' \
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
		|| { echo "REHEARSAL_ENV_FILE must name a filled rehearsal env over a stopped mainnet data copy and the release candidate"; return 1; }
	[ -f "$scratch/full.done" ] && return "$(cat "$scratch/full.done")"
	local work
	work=$(key_of "$rehearse_env" WORK) || return 1
	if [ -r "$work/rehearsal-result.env" ] && [ -r "$receipt" ]; then
		echo "the finished run of $work" >"$scratch/full.log"
		echo 0 >"$scratch/full.done"
		return 0
	fi
	bash "$driver" --env-file "$rehearse_env" --receipt "$receipt" >"$scratch/full.log" 2>&1
	echo $? >"$scratch/full.done"
	return "$(cat "$scratch/full.done")"
}

case_plan_applied_at_next_block() {
	local work halt i
	full_run || { echo "the rehearsal failed: $(tail -n 5 "$scratch/full.log" 2>/dev/null)"; return 1; }
	work=$(key_of "$rehearse_env" WORK)
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
	work=$(key_of "$rehearse_env" WORK)
	keys=$(cut -d= -f1 "$work/rehearsal-result.env" | tr '\n' ' ')
	[ "$keys" = "upgrade halt_height export_height final_height old_bin_sha256 new_bin_sha256 pools markets oracle_fresh feed_keys precompiles_served rollback_rehearsed " ] \
		|| { echo "result keys: $keys"; return 1; }
	grep -qx 'upgrade=v6.12.0' "$work/rehearsal-result.env" || { echo "upgrade is not v6.12.0"; return 1; }
	grep -qx 'rollback_rehearsed=true' "$work/rehearsal-result.env" || { echo "rollback_rehearsed is not true"; return 1; }
	grep -qx 'feed_keys=4' "$work/rehearsal-result.env" || { echo "feed_keys is not 4"; return 1; }
	sums_sha=$(awk '$2 ~ /paxd-v6\.12\.0-linux-amd64$/ { print $1 }' "$(dirname "$(key_of "$rehearse_env" NEW_BIN)")/SHA256SUMS")
	grep -qx "new_bin_sha256=$sums_sha" "$work/rehearsal-result.env" || { echo "new_bin_sha256 is not the paxd line of SHA256SUMS"; return 1; }
	jq -e --arg sha "$sums_sha" '.result == "pass" and .rollback_rehearsed == true and .new_bin_sha256 == $sha
		and .nodes_checked == 4 and .nodes_passed == 4 and .feed_keys == 4' "$receipt" >/dev/null \
		|| { echo "receipt: $(cat "$receipt")"; return 1; }
}

work=""
evidence=""
result_env=""
hr=""
load_run() {
	work=$(key_of "$rehearse_env" WORK) || return 1
	[ -n "$work" ] || { echo "$rehearse_env leaves WORK empty" >&2; return 1; }
	evidence=$work/evidence
	result_env=$work/rehearsal-result.env
	hr=$(key_of "$result_env" halt_height) || return 1
	[[ $hr =~ ^[1-9][0-9]*$ ]] || { echo "halt_height '$hr' is not a height" >&2; return 1; }
	local upgrade
	upgrade=$(key_of "$result_env" upgrade) || return 1
	[ "$upgrade" = "$want_upgrade" ] || { echo "the run rehearsed '$upgrade', want $want_upgrade" >&2; return 1; }
}

# require_run: loads the finished run named by the env file, or fails naming what is missing.
require_run() {
	load_run 2>"$scratch/load.log" || { echo "no finished rehearsal run: $(tail -n 1 "$scratch/load.log")" >&2; return 1; }
}

case_applied_height_on_every_node() {
	require_run || return 1
	local i height
	for i in "${nodes[@]}"; do
		height=$(json_field "$evidence/node$i/upgrade-applied.json" '.header.height')
		[ "$height" = "$((hr + 1))" ] || { echo "node $i applied $want_upgrade at $height, want $((hr + 1))" >&2; return 1; }
	done
}

case_eighteen_markets_seeded() {
	require_run || return 1
	local total count m id symbol got want out
	total=$(abi uint "$(rpc_result "$evidence/markets/totalMarkets.json")")
	[ "$total" = 18 ] || { echo "totalMarkets() of 0x1015 is $total, want 18" >&2; return 1; }
	count=$(abi uint "$(rpc_result "$evidence/markets/getMarketCount.json")")
	[ "$count" = 18 ] || { echo "getMarketCount() of 0x1020 is $count, want 18" >&2; return 1; }
	: > "$scratch/symbols"
	for ((m = 0; m < count; m++)); do
		out=$(abi market "$(rpc_result "$evidence/markets/getMarket-$m.json")") || return 1
		read -r id symbol <<<"$out"
		[ "$id" = "$m" ] || { echo "getMarket($m) carries market id $id" >&2; return 1; }
		printf '%s\n' "$symbol" >> "$scratch/symbols"
	done
	got=$(sort "$scratch/symbols" | paste -sd ' ')
	want=$(printf '%s\n' "${want_symbols[@]}" | sort | paste -sd ' ')
	[ "$got" = "$want" ] || { echo "market symbols are '$got', want '$want'" >&2; return 1; }
	[ "$(key_of "$result_env" markets)" = 18 ] || { echo "rehearsal-result.env markets is not 18" >&2; return 1; }
}

case_oracle_set_and_fresh_prices() {
	require_run || return 1
	local files=() f len key account count m out fresh=0
	shopt -s nullglob
	files=("$evidence"/oracle/getFeedKey-0x*.json)
	shopt -u nullglob
	[ "${#files[@]}" -eq "${#nodes[@]}" ] || { echo "${#files[@]} getFeedKey records, want one per validator (${#nodes[@]})" >&2; return 1; }
	for f in "${files[@]}"; do
		out=$(abi feed_key "$(rpc_result "$f")") || return 1
		read -r len key account <<<"$out"
		[ "$len" = 33 ] || { echo "$f: feed key is $len bytes, want 33" >&2; return 1; }
		case $key in 02* | 03*) ;; *) echo "$f: feed key $key is not a compressed secp256k1 key" >&2; return 1 ;; esac
		[ "$account" != 0x0000000000000000000000000000000000000000 ] || { echo "$f: zero feed account" >&2; return 1; }
	done
	[ "$(key_of "$result_env" feed_keys)" = "${#nodes[@]}" ] || { echo "rehearsal-result.env feed_keys is not ${#nodes[@]}" >&2; return 1; }
	count=$(abi uint "$(rpc_result "$evidence/markets/getMarketCount.json")")
	[ "$count" = 18 ] || { echo "getMarketCount() of 0x1020 is $count, want 18" >&2; return 1; }
	for ((m = 0; m < count; m++)); do
		if [ "$(abi price_fresh "$(rpc_result "$evidence/oracle/getPrice-$m.json")")" = true ]; then
			fresh=$((fresh + 1))
		else
			echo "getPrice($m).fresh is false" >&2
		fi
	done
	[ "$fresh" = "$count" ] || { echo "$fresh of $count markets fresh" >&2; return 1; }
	[ "$(key_of "$result_env" oracle_fresh)" = "$count" ] || { echo "rehearsal-result.env oracle_fresh is not $count" >&2; return 1; }
}

case_precompiles_switch_at_activation() {
	require_run || return 1
	local address h out served=0
	for address in "${served_existing[@]}" "${served_new[@]}"; do
		abi view "$(rpc_result "$evidence/precompiles/$((hr + 1))/$address.json")" > /dev/null \
			|| { echo "$address does not answer its view at $((hr + 1))" >&2; return 1; }
		served=$((served + 1))
	done
	for address in "${served_new[@]}"; do
		out=$(rpc_result "$evidence/precompiles/$hr/$address.json")
		[ "$out" = 0x ] || { echo "$address answers $out at $hr, before activation" >&2; return 1; }
	done
	for address in "${codeless[@]}"; do
		for h in "$hr" "$((hr + 1))"; do
			out=$(rpc_result "$evidence/code/$h/$address.json")
			[ "$out" = 0x ] || { echo "eth_getCode $address at $h is $out, want 0x" >&2; return 1; }
			[ ! -e "$evidence/precompiles/$h/$address.json" ] || { echo "a view call was made to $address at $h" >&2; return 1; }
		done
	done
	[ "$served" = 8 ] || { echo "$served served addresses, want 8" >&2; return 1; }
	[ "$(key_of "$result_env" precompiles_served)" = 8 ] || { echo "rehearsal-result.env precompiles_served is not 8" >&2; return 1; }
}

case_block_hashes_agree() {
	require_run || return 1
	local h i hash first header final
	for ((h = hr + 1; h <= hr + 10; h++)); do
		first=""
		for i in "${nodes[@]}"; do
			header=$(json_field "$evidence/node$i/block-$h.json" '.result.block.header.height')
			[ "$header" = "$h" ] || { echo "node $i block-$h.json carries height $header" >&2; return 1; }
			hash=$(json_field "$evidence/node$i/block-$h.json" '.result.block_id.hash')
			if [ -z "$first" ]; then
				first=$hash
			elif [ "$hash" != "$first" ]; then
				echo "block $h: node $i hash $hash, node ${nodes[0]} hash $first" >&2
				return 1
			fi
		done
	done
	final=$(key_of "$result_env" final_height)
	[[ $final =~ ^[0-9]+$ ]] && [ "$final" -gt "$((hr + 10))" ] || { echo "final_height '$final' does not pass $((hr + 10))" >&2; return 1; }
}

case_version_and_clean_logs() {
	require_run || return 1
	local i version height log panics total=0
	for i in "${nodes[@]}"; do
		version=$(json_field "$evidence/node$i/abci-info.json" '.result.response.version')
		[ "$version" = "$want_upgrade" ] || { echo "node $i reports version $version, want $want_upgrade" >&2; return 1; }
		height=$(json_field "$evidence/node$i/abci-info.json" '.result.response.last_block_height')
		[ "$height" -gt "$((hr + 10))" ] || { echo "node $i abci_info read at $height, not past $((hr + 10))" >&2; return 1; }
		log=$evidence/node$i/new-bin.log
		[ -s "$log" ] || { echo "$log is missing or empty" >&2; return 1; }
		panics=$(grep -c panic "$log" || true)
		[ "$panics" = 0 ] || grep -n -m 3 panic "$log" >&2
		total=$((total + panics))
	done
	[ "$total" = 0 ] || { echo "the new-binary logs carry panic $total times" >&2; return 1; }
}

case_rollback_leg_restores_previous_chain() {
	require_run || return 1
	local i halted restored before after old_version new_version latest
	for i in "${nodes[@]}"; do
		halted=$(json_field "$evidence/node$i/halt-abci-info.json" '.result.response.last_block_height')
		[ "$halted" = "$hr" ] || { echo "node $i recorded the halt at $halted, want $hr" >&2; return 1; }
		restored=$(json_field "$evidence/node$i/rollback-abci-info.json" '.result.response.last_block_height')
		[ "$restored" = "$hr" ] || { echo "node $i restored to $restored, want $hr" >&2; return 1; }
		before=$(json_field "$evidence/node$i/halt-abci-info.json" '.result.response.last_block_app_hash')
		after=$(json_field "$evidence/node$i/rollback-abci-info.json" '.result.response.last_block_app_hash')
		[ "$after" = "$before" ] || { echo "node $i application hash at $hr is $after after the restore, $before before the upgrade" >&2; return 1; }
		old_version=$(json_field "$evidence/node$i/halt-abci-info.json" '.result.response.version')
		new_version=$(json_field "$evidence/node$i/rollback-abci-info.json" '.result.response.version')
		[ "$new_version" = "$old_version" ] && [ "$new_version" != "$want_upgrade" ] \
			|| { echo "node $i runs $new_version after the restore, want the previous binary's $old_version" >&2; return 1; }
		latest=$(json_field "$evidence/node$i/rollback-status.json" '.result.sync_info.latest_block_height')
		[ "$latest" -gt "$((hr + 5))" ] || { echo "node $i reached $latest on the previous binary, not past $((hr + 5))" >&2; return 1; }
	done
	[ "$(key_of "$result_env" rollback_rehearsed)" = true ] || { echo "rehearsal-result.env rollback_rehearsed is not true" >&2; return 1; }
	[ -r "$receipt" ] || { echo "$receipt is missing" >&2; return 1; }
	jq -e '.rollback_rehearsed == true' "$receipt" > /dev/null || { echo "$receipt rollback_rehearsed is not true" >&2; return 1; }
}

case_feed_key_needs_association() {
	require_run || return 1
	local dirs=() dir validator evm code account owner key_account out
	shopt -s nullglob
	dirs=("$evidence"/feedkey/0x*/)
	shopt -u nullglob
	[ "${#dirs[@]}" -eq "${#nodes[@]}" ] || { echo "${#dirs[@]} feed-key registrations, want one per validator (${#nodes[@]})" >&2; return 1; }
	for dir in "${dirs[@]}"; do
		dir=${dir%/}
		validator=${dir##*/}
		[[ $validator =~ ^0x[0-9a-f]{40}$ ]] || { echo "$dir is not named by a validator address" >&2; return 1; }
		evm=$(tr -d '[:space:]' < "$dir/evm-address")
		[[ $evm =~ ^0x[0-9a-fA-F]{40}$ ]] || { echo "$dir/evm-address holds '$evm'" >&2; return 1; }
		grep -q LXP_PRICEFEED_NOT_VALIDATOR "$dir/before-association.out" \
			|| { echo "$validator: registerFeedKey before associate-address did not revert LXP_PRICEFEED_NOT_VALIDATOR" >&2; return 1; }
		code=$(json_field "$dir/associate.json" '.code')
		[ "$code" = 0 ] || { echo "$validator: associate-address returned code $code" >&2; return 1; }
		account=$(abi address "$(rpc_result "$dir/register.json")")
		[ "$account" != 0x0000000000000000000000000000000000000000 ] || { echo "$validator: registerFeedKey returned the zero feed account" >&2; return 1; }
		owner=$(abi address "$(rpc_result "$dir/getValidatorByFeedAccount.json")")
		[ "$owner" = "$validator" ] || { echo "getValidatorByFeedAccount($account) is $owner, want $validator" >&2; return 1; }
		out=$(abi feed_key "$(rpc_result "$evidence/oracle/getFeedKey-$validator.json")") || return 1
		read -r _ _ key_account <<<"$out"
		[ "$key_account" = "$account" ] || { echo "getFeedKey($validator) names $key_account, registerFeedKey returned $account" >&2; return 1; }
	done
}

run_case check_mode_touches_nothing
run_case pool_parity_units
run_case export_step_produces_genesis
run_case nodes_halt_at_rehearsal_height
run_case plan_applied_at_next_block
run_case result_file_keys
run_case applied_height_on_every_node
run_case eighteen_markets_seeded
run_case oracle_set_and_fresh_prices
run_case precompiles_switch_at_activation
run_case block_hashes_agree
run_case version_and_clean_logs
run_case rollback_leg_restores_previous_chain
run_case feed_key_needs_association

echo "rehearse.test.sh: $pass passed, $failed failed"
[ "$failed" -eq 0 ]
