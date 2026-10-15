#!/usr/bin/env bash
# Reads a finished run of tools/release/rehearse-v6120.sh and checks the v6.12.0 upgrade on its four
# nodes: applied height, seeded markets, oracle feed keys and fresh prices, precompile presence at the
# activation height, block hashes, version and logs, the rollback leg and feed-key association.
#
# The run is named by the env file of the rehearsal (REHEARSAL_ENV_FILE, default .env.rehearsal-v6120):
# its WORK holds rehearsal-result.env and the raw responses the driver recorded under WORK/evidence:
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
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
cd "$root"
export LC_ALL=C
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT

want_upgrade=v6.12.0
env_file=${REHEARSAL_ENV_FILE:-.env.rehearsal-v6120}
receipt=${REHEARSAL_RECEIPT:-tools/release/rollout/v6.12.0/rehearsal.json}
nodes=(0 1 2 3)
want_symbols=(PAX SID NAS100 ETH SPX500 BTC BNB SOL HYPE TSLA GOOGL XAU NVDA ASTER LINK XRP TRUMP AVAX)
served_existing=(0x0000000000000000000000000000000000001015 0x0000000000000000000000000000000000001017)
served_new=(0x0000000000000000000000000000000000001020 0x0000000000000000000000000000000000001021
  0x0000000000000000000000000000000000001022 0x0000000000000000000000000000000000001023
  0x0000000000000000000000000000000000001024 0x0000000000000000000000000000000000001025)
codeless=(0x0000000000000000000000000000000000001026 0x0000000000000000000000000000000000001027)

# Decodes the ABI return words of a view: uint <hex>, address <hex>, market <hex> (prints id and
# symbol), price_fresh <hex>, feed_key <hex> (prints key length, key and feed account), view <hex>.
cat > "$tmp/abi.py" <<'PY'
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

abi() { python3 "$tmp/abi.py" "$@"; }

# key_of <file> <key>: value of the last KEY=VALUE line, an optional export prefix allowed.
key_of() {
  [ -r "$1" ] || { echo "$1 is missing" >&2; return 1; }
  awk -v k="$2" '
    { sub(/^[[:space:]]*export[[:space:]]+/, "") }
    index($0, k "=") == 1 { v = substr($0, length(k) + 2); found = 1 }
    END { if (!found) exit 1; print v }' "$1" || { echo "$1 has no $2" >&2; return 1; }
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

work=""
evidence=""
result_env=""
hr=""
load_status=0
load_run() {
  work=$(key_of "$env_file" WORK) || return 1
  [ -n "$work" ] || { echo "$env_file leaves WORK empty" >&2; return 1; }
  evidence=$work/evidence
  result_env=$work/rehearsal-result.env
  hr=$(key_of "$result_env" halt_height) || return 1
  [[ $hr =~ ^[1-9][0-9]*$ ]] || { echo "halt_height '$hr' is not a height" >&2; return 1; }
  local upgrade
  upgrade=$(key_of "$result_env" upgrade) || return 1
  [ "$upgrade" = "$want_upgrade" ] || { echo "the run rehearsed '$upgrade', want $want_upgrade" >&2; return 1; }
}
load_run > "$tmp/load.log" 2>&1 || load_status=$?
cat "$tmp/load.log"

require_run() {
  [ "$load_status" -eq 0 ] || { echo "no finished rehearsal run: $(tail -n 1 "$tmp/load.log")" >&2; return 1; }
}

applied_height_on_every_node() {
  require_run || return 1
  local i height
  for i in "${nodes[@]}"; do
    height=$(json_field "$evidence/node$i/upgrade-applied.json" '.header.height')
    [ "$height" = "$((hr + 1))" ] || { echo "node $i applied $want_upgrade at $height, want $((hr + 1))" >&2; return 1; }
  done
}

eighteen_markets_seeded() {
  require_run || return 1
  local total count m id symbol got want out
  total=$(abi uint "$(rpc_result "$evidence/markets/totalMarkets.json")")
  [ "$total" = 18 ] || { echo "totalMarkets() of 0x1015 is $total, want 18" >&2; return 1; }
  count=$(abi uint "$(rpc_result "$evidence/markets/getMarketCount.json")")
  [ "$count" = 18 ] || { echo "getMarketCount() of 0x1020 is $count, want 18" >&2; return 1; }
  : > "$tmp/symbols"
  for ((m = 0; m < count; m++)); do
    out=$(abi market "$(rpc_result "$evidence/markets/getMarket-$m.json")") || return 1
    read -r id symbol <<<"$out"
    [ "$id" = "$m" ] || { echo "getMarket($m) carries market id $id" >&2; return 1; }
    printf '%s\n' "$symbol" >> "$tmp/symbols"
  done
  got=$(sort "$tmp/symbols" | paste -sd ' ')
  want=$(printf '%s\n' "${want_symbols[@]}" | sort | paste -sd ' ')
  [ "$got" = "$want" ] || { echo "market symbols are '$got', want '$want'" >&2; return 1; }
  [ "$(key_of "$result_env" markets)" = 18 ] || { echo "rehearsal-result.env markets is not 18" >&2; return 1; }
}

oracle_set_and_fresh_prices() {
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

precompiles_switch_at_activation() {
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

block_hashes_agree() {
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

version_and_clean_logs() {
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

rollback_leg_restores_previous_chain() {
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

feed_key_needs_association() {
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

passed=0
failed=0
for t in applied_height_on_every_node eighteen_markets_seeded oracle_set_and_fresh_prices \
  precompiles_switch_at_activation block_hashes_agree version_and_clean_logs \
  rollback_leg_restores_previous_chain feed_key_needs_association; do
  if (set -e; "$t"); then
    echo "PASS $t"
    passed=$((passed + 1))
  else
    echo "FAIL $t"
    failed=$((failed + 1))
  fi
done
echo "rehearse: $passed passed, $failed failed"
[ "$failed" -eq 0 ]
