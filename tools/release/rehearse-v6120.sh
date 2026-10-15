#!/usr/bin/env bash
# Rehearses plan v6.12.0 on a local network of four validators built from the real chain state.
#
# The previous binary exports a copy of a stopped full node's data at height E (paxd export
# --height E), rehearsal/rewrite_genesis.py gives the four bonded validators local consensus keys
# and the previous binary's validate-genesis checks the result. Four local homes run the previous
# binary with --halt-height HR until each logs "halting node per configuration", every home is
# archived with snapshot.sh, receives the node upgrade file of upgrade_info.py for HR+1 and restarts
# on the release candidate, which must log "applying upgrade" once. The assertions then read the
# applied plan, the ABCI version, the precompile presence switch, the launchpad pool parity, the
# markets, the feed keys and fresh prices of four local price sidecars, the block hashes of
# HR+1..HR+10 and the node logs. The rollback leg halts the nodes after HR+10, runs rollback.sh
# --execute on the rehearsal env, and requires the restored chain to carry the application hash
# recorded at HR and to pass HR+5 on the previous binary.
#
# Every host-specific value comes from flags or from an untracked env file passed with --env-file
# (a filled copy of tools/release/env/rehearsal.env.example); nothing here contacts a remote host.
# Precedence: flags > env file > built-in defaults. The env file holds KEY=VALUE lines (blank lines,
# `#` comments and an optional `export ` prefix allowed); keys this script does not read are ignored.
# Keys read:
#   SOURCE_DATA SOURCE_GENESIS OLD_BIN NEW_BIN WORK PORT_BASE COSMOS_CHAIN_ID EXPORT_HEIGHT
#   REHEARSAL_HALT_HEIGHT CHAINFLOW_KEY_FILE PRICE_SIDECAR_CONFIG OPERATOR_KEY_NAMES
#   KEYRING_BACKEND CREDENTIAL_DIR
#
# --check validates this file's syntax, the given flags and keys and the required tools, and
# touches nothing.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
release=$root/tools/release
plan=v6.12.0
release_id=rehearsal
nodes=4
launchpad=0x0000000000000000000000000000000000001017
pricefeed=0x0000000000000000000000000000000000001020
get_feed_key=0x2d6f477c

env_file=""
source_data=""
source_genesis=""
old_bin=""
new_bin=""
work=""
port_base=36000
chain_id=hyperpax_125-1
export_height=""
halt_height=""
chainflow_key_file=""
sidecar_config=""
operator_keys=""
keyring_backend=test
keyring_dir=""
receipt=""
stop_after=""
wait_seconds=1800
check=0
passed=0

unit_dir=/run/systemd/system
units=()
sidecar_pids=()

log() {
	printf '%s rehearse-v6120: %s\n' "$(date -u +%H:%M:%SZ)" "$*" >&2
}

fail() {
	echo "rehearse-v6120: $*" >&2
	exit 1
}

usage() {
	cat >&2 <<'USAGE'
usage: rehearse-v6120.sh --env-file FILE [flags] [--receipt PATH]
       rehearse-v6120.sh --check [--env-file FILE] [flags]

flags (each overrides the env file key in brackets):
  --source-data DIR          data directory copy of a STOPPED synced full node, never modified [SOURCE_DATA]
  --source-genesis FILE      genesis of that node [SOURCE_GENESIS]
  --old-bin PATH             running release binary [OLD_BIN]
  --new-bin PATH             release candidate paxd-v6.12.0-linux-amd64 inside its release directory [NEW_BIN]
  --work DIR                 empty or absent directory for the four local homes [WORK]
  --port-base P              node i listens at P+20i+0 rpc, +1 p2p, +2 evm, +3 evm ws, +4 grpc,
                             +5 grpc-web, +6 sidecar health (default 36000) [PORT_BASE]
  --chain-id ID              cosmos chain id of the copied state (default hyperpax_125-1) [COSMOS_CHAIN_ID]
  --export-height E          height passed to paxd export (default: the last committed height) [EXPORT_HEIGHT]
  --halt-height HR           halt height of the local network (default E + 20) [REHEARSAL_HALT_HEIGHT]
  --chainflow-key-file FILE  Chainflow feed API key [CHAINFLOW_KEY_FILE]
  --sidecar-config FILE      filled tools/price-sidecar/config.template.json; validator and
                             feed_account are set per sidecar [PRICE_SIDECAR_CONFIG]
  --operator-keys "A B C D"  keyring names of the four bonded validators' operator keys [OPERATOR_KEY_NAMES]
  --keyring-backend NAME     backend of that keyring (default test) [KEYRING_BACKEND]
  --keyring-dir DIR          directory of that keyring [CREDENTIAL_DIR]
options:
  --env-file FILE            untracked KEY=VALUE file supplying any bracketed key
  --receipt PATH             write the JSON receipt of contracts/release.md#post-rollout-verification
  --stop-after STEP          stop after the export or halt step and keep WORK for inspection
  --wait-seconds S           limit on each wait for the network (default 1800)
  --check                    validate syntax, flags, keys and tools, then exit without touching state
USAGE
}

need_value() {
	[ "$#" -ge 2 ] && [ -n "$2" ] || fail "$1 needs a value"
}

args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do
	if [ "${args[$i]}" = "--env-file" ]; then
		[ $((i + 1)) -lt ${#args[@]} ] && [ -n "${args[$((i + 1))]}" ] || fail "--env-file needs a value"
		env_file=${args[$((i + 1))]}
	fi
done

load_env_file() {
	local file=$1 line key value lineno=0
	[ -r "$file" ] || fail "env file $file is not readable"
	while IFS= read -r line || [ -n "$line" ]; do
		lineno=$((lineno + 1))
		line=${line#"${line%%[![:space:]]*}"}
		[ -z "$line" ] || [ "${line:0:1}" = "#" ] && continue
		line=${line#export }
		[[ "$line" =~ ^([A-Z_][A-Z0-9_]*)=(.*)$ ]] || fail "env file $file line $lineno is not KEY=VALUE"
		key=${BASH_REMATCH[1]}
		value=${BASH_REMATCH[2]}
		value=${value%"${value##*[![:space:]]}"}
		if [[ "$value" =~ ^\"(.*)\"$ ]] || [[ "$value" =~ ^\'(.*)\'$ ]]; then
			value=${BASH_REMATCH[1]}
		fi
		[ -n "$value" ] || continue
		case "$key" in
			SOURCE_DATA) source_data=$value ;;
			SOURCE_GENESIS) source_genesis=$value ;;
			OLD_BIN) old_bin=$value ;;
			NEW_BIN) new_bin=$value ;;
			WORK) work=$value ;;
			PORT_BASE) port_base=$value ;;
			COSMOS_CHAIN_ID) chain_id=$value ;;
			EXPORT_HEIGHT) export_height=$value ;;
			REHEARSAL_HALT_HEIGHT) halt_height=$value ;;
			CHAINFLOW_KEY_FILE) chainflow_key_file=$value ;;
			PRICE_SIDECAR_CONFIG) sidecar_config=$value ;;
			OPERATOR_KEY_NAMES) operator_keys=$value ;;
			KEYRING_BACKEND) keyring_backend=$value ;;
			CREDENTIAL_DIR) keyring_dir=$value ;;
			*) ;;
		esac
	done <"$file"
}
[ -z "$env_file" ] || load_env_file "$env_file"

while [ "$#" -gt 0 ]; do
	case "$1" in
		--env-file) need_value "$@"; shift 2 ;;
		--source-data) need_value "$@"; source_data=$2; shift 2 ;;
		--source-genesis) need_value "$@"; source_genesis=$2; shift 2 ;;
		--old-bin) need_value "$@"; old_bin=$2; shift 2 ;;
		--new-bin) need_value "$@"; new_bin=$2; shift 2 ;;
		--work) need_value "$@"; work=$2; shift 2 ;;
		--port-base) need_value "$@"; port_base=$2; shift 2 ;;
		--chain-id) need_value "$@"; chain_id=$2; shift 2 ;;
		--export-height) need_value "$@"; export_height=$2; shift 2 ;;
		--halt-height) need_value "$@"; halt_height=$2; shift 2 ;;
		--chainflow-key-file) need_value "$@"; chainflow_key_file=$2; shift 2 ;;
		--sidecar-config) need_value "$@"; sidecar_config=$2; shift 2 ;;
		--operator-keys) need_value "$@"; operator_keys=$2; shift 2 ;;
		--keyring-backend) need_value "$@"; keyring_backend=$2; shift 2 ;;
		--keyring-dir) need_value "$@"; keyring_dir=$2; shift 2 ;;
		--receipt) need_value "$@"; receipt=$2; shift 2 ;;
		--stop-after) need_value "$@"; stop_after=$2; shift 2 ;;
		--wait-seconds) need_value "$@"; wait_seconds=$2; shift 2 ;;
		--check) check=1; shift ;;
		-h | --help) usage; exit 0 ;;
		*) usage; fail "unknown argument $1" ;;
	esac
done

bash -n "${BASH_SOURCE[0]}" || fail "syntax check of ${BASH_SOURCE[0]} failed"
for key_value in "SOURCE_DATA=$source_data" "SOURCE_GENESIS=$source_genesis" "OLD_BIN=$old_bin" "NEW_BIN=$new_bin" "WORK=$work"; do
	[ -n "${key_value#*=}" ] || fail "${key_value%%=*} is required (flag or env file key)"
done
[[ "$chain_id" =~ ^[a-z][a-z0-9_-]*_[1-9][0-9]*-[1-9][0-9]*$ ]] || fail "COSMOS_CHAIN_ID must look like name_EVMID-VERSION"
[[ "$port_base" =~ ^[1-9][0-9]{3,4}$ ]] && [ "$port_base" -ge 1024 ] && [ "$port_base" -le 65000 ] \
	|| fail "PORT_BASE must be 1024 through 65000"
[ -z "$export_height" ] || [[ "$export_height" =~ ^[1-9][0-9]{0,15}$ ]] || fail "EXPORT_HEIGHT must be a positive height"
[ -z "$halt_height" ] || [[ "$halt_height" =~ ^[1-9][0-9]{0,15}$ ]] || fail "REHEARSAL_HALT_HEIGHT must be a positive height"
if [ -n "$export_height" ] && [ -n "$halt_height" ]; then
	[ "$halt_height" -gt $((export_height + 1)) ] || fail "REHEARSAL_HALT_HEIGHT must be above EXPORT_HEIGHT + 1"
fi
case "$stop_after" in '' | export | halt) ;; *) fail "--stop-after takes export or halt" ;; esac
[[ "$wait_seconds" =~ ^[1-9][0-9]{0,5}$ ]] || fail "--wait-seconds must be a positive integer"
[[ "$keyring_backend" =~ ^(test|file|os)$ ]] || fail "KEYRING_BACKEND must be test, file or os"
[ -d "$source_data" ] || fail "SOURCE_DATA $source_data is not a directory"
[ -r "$source_genesis" ] || fail "SOURCE_GENESIS $source_genesis is not readable"
command -v "$old_bin" >/dev/null 2>&1 || fail "OLD_BIN $old_bin is not executable"
command -v "$new_bin" >/dev/null 2>&1 || fail "NEW_BIN $new_bin is not executable"
if [ -e "$work" ]; then
	[ -d "$work" ] && [ -z "$(ls -A "$work")" ] || fail "WORK $work must be empty or absent"
fi
[ -z "$chainflow_key_file" ] || [ -r "$chainflow_key_file" ] || fail "CHAINFLOW_KEY_FILE $chainflow_key_file is not readable"
[ -z "$sidecar_config" ] || jq -e 'type == "object"' "$sidecar_config" >/dev/null 2>&1 \
	|| fail "PRICE_SIDECAR_CONFIG $sidecar_config is not a JSON object"
[ -z "$keyring_dir" ] || [ -d "$keyring_dir" ] || fail "CREDENTIAL_DIR $keyring_dir is not a directory"
if [ -n "$operator_keys" ]; then
	read -ra key_names <<<"$operator_keys"
	[ "${#key_names[@]}" -eq "$nodes" ] || fail "OPERATOR_KEY_NAMES must name $nodes keys"
fi
for tool in jq curl sha256sum awk sed grep cp mv tar zstd python3 systemctl pgrep; do
	command -v "$tool" >/dev/null 2>&1 || fail "$tool is not available"
done
# The steps up to --stop-after need only their own helpers; --check and a full run need all of them.
case "$check:$stop_after" in
	0:export) prerequisites=() ;;
	0:halt) prerequisites=(rehearsal/rewrite_genesis.py) ;;
	*) prerequisites=(rehearsal/rewrite_genesis.py rehearsal/pool_parity.py upgrade_info.py fleet_check.py feed_key.py
		fleet-views.json snapshot.sh rollback.sh) ;;
esac
for script in "${prerequisites[@]}"; do
	[ -r "$release/$script" ] || fail "tools/release/$script is missing"
	case "$script" in *.sh) bash -n "$release/$script" || fail "syntax check of tools/release/$script failed" ;; esac
done
if [ "$check" -eq 1 ] || [ "$stop_after" != export ]; then
	[ "$(id -u)" = 0 ] || fail "the rehearsal runs its nodes as runtime units in $unit_dir and needs root"
fi

if [ "$check" -eq 1 ]; then
	echo "rehearse-v6120: check passed (syntax, flags, keys, tools); no state touched" >&2
	exit 0
fi

source_data=$(cd "$source_data" && pwd)
source_config=$(dirname "$source_data")/config
old_bin=$(command -v "$old_bin")
new_bin=$(command -v "$new_bin")
release_dir=$(cd "$(dirname "$new_bin")" && pwd)
old_sha=$(sha256sum "$old_bin" | cut -d' ' -f1)
new_sha=$(sha256sum "$new_bin" | cut -d' ' -f1)
sidecar_bin=$release_dir/price-sidecar-$plan-linux-amd64
if [ -z "$stop_after" ]; then
	[ "$old_sha" != "$new_sha" ] || fail "OLD_BIN and NEW_BIN are the same binary"
	[ "$(basename "$new_bin")" = "paxd-$plan-linux-amd64" ] || fail "NEW_BIN must be paxd-$plan-linux-amd64 of the release directory"
	sums_sha=$(awk -v f="paxd-$plan-linux-amd64" '$2 == f || $2 == "*" f { print $1 }' "$release_dir/SHA256SUMS" 2>/dev/null || true)
	[ "$sums_sha" = "$new_sha" ] || fail "NEW_BIN sha256 $new_sha is not the paxd line of $release_dir/SHA256SUMS"
	[ -r "$release_dir/upgrade-info.json" ] || fail "$release_dir/upgrade-info.json is missing"
	[ -x "$sidecar_bin" ] || fail "$sidecar_bin is missing"
	sidecar_sha=$(sha256sum "$sidecar_bin" | cut -d' ' -f1)
	grep -q "^$sidecar_sha [ *]price-sidecar-$plan-linux-amd64$" "$release_dir/SHA256SUMS" \
		|| fail "$sidecar_bin is not the price-sidecar line of SHA256SUMS"
	for key_value in "CHAINFLOW_KEY_FILE=$chainflow_key_file" "PRICE_SIDECAR_CONFIG=$sidecar_config" \
		"OPERATOR_KEY_NAMES=$operator_keys" "CREDENTIAL_DIR=$keyring_dir"; do
		[ -n "${key_value#*=}" ] || fail "${key_value%%=*} is required for the upgrade leg"
	done
fi

mkdir -p "$work"
work=$(cd "$work" && pwd)
logs=$work/logs
bin_dir=$work/bin
snapshots=$work/snapshots
credentials=$work/credentials
receipts=$work/receipts
rehearsal_env=$work/rehearsal.env
mkdir -p "$logs" "$bin_dir" "$snapshots" "$credentials" "$receipts"
unit_tag=$(printf '%s' "$work" | sha256sum | cut -c1-8)

homes=()
for ((i = 0; i < nodes; i++)); do
	homes+=("$work/node$i")
	units+=("paxd-rehearsal-$unit_tag-$i")
done
rpc_port() { echo $((port_base + 20 * $1)); }
p2p_port() { echo $((port_base + 20 * $1 + 1)); }
evm_port() { echo $((port_base + 20 * $1 + 2)); }
rpc_url() { echo "http://127.0.0.1:$(rpc_port "$1")"; }
evm_url() { echo "http://127.0.0.1:$(evm_port "$1")"; }

write_receipt() {
	local result=$1
	[ -n "$receipt" ] || return 0
	mkdir -p "$(dirname "$receipt")"
	if [ "$result" = pass ]; then
		jq -n --arg version "$plan" --argjson nodes "$nodes" --argjson served "$precompiles_served" \
			--argjson pools "$pools" --argjson markets "$markets" --argjson fresh "$oracle_fresh" \
			--argjson feed_keys "$feed_keys" --arg sha "$new_sha" \
			'{version: $version, plan: $version, nodes_checked: $nodes, nodes_passed: $nodes,
			  precompiles_served: $served, pools: $pools, markets: $markets, oracle_fresh: $fresh,
			  feed_keys: $feed_keys, rollback_rehearsed: true, new_bin_sha256: $sha, result: "pass"}' >"$receipt"
	else
		jq -n --arg version "$plan" --arg sha "$new_sha" \
			'{version: $version, plan: $version, rollback_rehearsed: false, new_bin_sha256: $sha, result: "fail"}' >"$receipt"
	fi
}

cleanup() {
	local pid unit
	for pid in "${sidecar_pids[@]}"; do
		kill -TERM "$pid" 2>/dev/null || true
		wait "$pid" 2>/dev/null || true
	done
	for unit in "${units[@]}"; do
		if [ -e "$unit_dir/$unit.service" ]; then
			systemctl stop "$unit" 2>/dev/null || true
			systemctl reset-failed "$unit" 2>/dev/null || true
			rm -f "$unit_dir/$unit.service"
		fi
	done
	systemctl daemon-reload 2>/dev/null || true
	[ "$passed" -eq 1 ] || [ -n "$stop_after" ] || write_receipt fail
}
trap cleanup EXIT

# set_toml <file> <section> <key> <value>: sets key inside [section] (top level when section is
# empty), adding it at the end of the section when the template does not carry it.
set_toml() {
	local file=$1 section=$2 key=$3 value=$4
	awk -v section="$section" -v key="$key" -v value="$value" '
		BEGIN { inside = (section == "") }
		/^\[/ {
			if (inside && !done) { print key " = " value; done = 1 }
			inside = ($0 == "[" section "]")
		}
		inside && !done && index($0, key " = ") == 1 { print key " = " value; done = 1; next }
		{ print }
		END { if (inside && !done) print key " = " value }
	' "$file" >"$file.tmp" || fail "$file: $key was not set"
	mv -f "$file.tmp" "$file"
	grep -qxF "$key = $value" "$file" || fail "$file: $key was not set"
}

# write_unit <i> <log> [extra start flags]: the runtime unit of home i over $bin_dir/paxd.
write_unit() {
	local i=$1 logfile=$2
	shift 2
	cat >"$unit_dir/${units[$i]}.service" <<UNIT
[Unit]
Description=Paxeer X Network $plan rehearsal node $i

[Service]
Type=simple
ExecStart=$bin_dir/paxd start --home ${homes[$i]}$(printf ' %s' "$@")
StandardOutput=append:$logfile
StandardError=append:$logfile
Restart=no
LimitNOFILE=65536
UNIT
}

jget() {
	curl -sf --max-time 5 "$1" 2>/dev/null | jq -r "(.result // .) | $2 // empty" 2>/dev/null || true
}

height() {
	jget "$(rpc_url "$1")/status" '.sync_info.latest_block_height'
}

# wait_height_above <node> <h>: prints the first height above h the node reports.
wait_height_above() {
	local node=$1 target=$2 deadline=$((SECONDS + wait_seconds)) current
	while [ "$SECONDS" -lt "$deadline" ]; do
		current=$(height "$node")
		if [[ "$current" =~ ^[0-9]+$ ]] && [ "$current" -gt "$target" ]; then
			echo "$current"
			return 0
		fi
		systemctl is-active --quiet "${units[$node]}" || fail "node $node stopped before height $((target + 1)); see $logs"
		sleep 1
	done
	fail "node $node did not pass height $target within ${wait_seconds}s; see $logs"
}

# wait_log <file> <pattern> <what>: waits until the extended regex appears in the file.
wait_log() {
	local file=$1 pattern=$2 what=$3 deadline=$((SECONDS + wait_seconds))
	until grep -Eq "$pattern" "$file" 2>/dev/null; do
		[ "$SECONDS" -lt "$deadline" ] || fail "no $what within ${wait_seconds}s; see $file"
		sleep 1
	done
}

wait_stopped() {
	local i=$1 deadline=$((SECONDS + wait_seconds))
	while systemctl is-active --quiet "${units[$i]}"; do
		[ "$SECONDS" -lt "$deadline" ] || fail "node $i did not exit within ${wait_seconds}s"
		sleep 1
	done
	if pgrep -a -x paxd | grep -qF -- "--home ${homes[$i]}"; then
		fail "a paxd process still runs on ${homes[$i]}"
	fi
	systemctl reset-failed "${units[$i]}" 2>/dev/null || true
}

start_units() {
	local i
	systemctl daemon-reload
	for i in "$@"; do
		systemctl start "${units[$i]}" || fail "unit ${units[$i]} did not start"
		log "started ${units[$i]} ($(basename "$(readlink -f "$bin_dir/paxd")"))"
	done
}

stop_units() {
	local i
	for ((i = 0; i < nodes; i++)); do
		systemctl stop "${units[$i]}" 2>/dev/null || true
	done
	for ((i = 0; i < nodes; i++)); do
		wait_stopped "$i"
	done
}

# eth_call_at <node> <to> <data> <height>: prints the result hex, or fails on an RPC error.
eth_call_at() {
	local body response
	body=$(jq -cn --arg to "$2" --arg data "$3" --arg block "$(printf '0x%x' "$4")" \
		'{"jsonrpc":"2.0","id":1,"method":"eth_call","params":[{"to":$to,"data":$data},$block]}')
	response=$(curl -sf --max-time 10 -H 'content-type: application/json' --data "$body" "$(evm_url "$1")") \
		|| fail "eth_call to $2 got no response from node $1"
	if [ "$(jq -r '.error == null' <<<"$response")" != true ]; then
		echo "revert"
		return 0
	fi
	jq -r '.result' <<<"$response"
}

get_code_at() {
	local body
	body=$(jq -cn --arg a "$2" --arg block "$(printf '0x%x' "$3")" \
		'{"jsonrpc":"2.0","id":1,"method":"eth_getCode","params":[$a,$block]}')
	curl -sf --max-time 10 -H 'content-type: application/json' --data "$body" "$(evm_url "$1")" | jq -r '.result'
}

# --- 1. export the copied state with the previous binary --------------------------------------
export_home=$work/export-home
log "copying $source_data into $export_home (the source node must be stopped)"
"$old_bin" init export-source --chain-id "$chain_id" --home "$export_home" --overwrite >/dev/null 2>&1 \
	|| fail "OLD_BIN could not initialise $export_home"
cp "$source_genesis" "$export_home/config/genesis.json"
for file in app.toml config.toml; do
	if [ -r "$source_config/$file" ]; then
		cp "$source_config/$file" "$export_home/config/$file"
	fi
done
rm -rf "$export_home/data"
cp -a --reflink=auto "$source_data" "$export_home/data"

export_raw=$work/export.raw
export_args=(export --home "$export_home")
[ -z "$export_height" ] || export_args+=(--height "$export_height")
log "exporting state with $old_bin ${export_height:+at height $export_height}"
"$old_bin" "${export_args[@]}" >"$export_raw" 2>&1 || fail "export failed; see $export_raw"
exported=$work/exported-genesis.json
grep -E '^\{"app_hash"' "$export_raw" | tail -n 1 >"$exported"
jq -e --arg chain "$chain_id" '.chain_id == $chain and (.app_state | type == "object")' "$exported" >/dev/null \
	|| fail "export did not produce a $chain_id genesis; see $export_raw"
rm -rf "$export_home/data"
initial_height=$(jq -r '.initial_height' "$exported")
[[ "$initial_height" =~ ^[1-9][0-9]{0,15}$ ]] || fail "exported initial height '$initial_height' is not a height"
if [ -n "$export_height" ]; then
	[ "$initial_height" -eq $((export_height + 1)) ] || fail "export at $export_height starts at $initial_height, expected $((export_height + 1))"
else
	export_height=$((initial_height - 1))
fi
[ -n "$halt_height" ] || halt_height=$((export_height + 20))
[ "$halt_height" -gt "$initial_height" ] || fail "REHEARSAL_HALT_HEIGHT $halt_height must be above the first height $initial_height"
h1=$((halt_height + 1))
log "exported state at height $export_height; rehearsal halts at $halt_height"
[ "$stop_after" != export ] || { log "stopped after the export step; work kept in $work"; exit 0; }

# --- 2. four local homes and the four-validator genesis ----------------------------------------
for ((i = 0; i < nodes; i++)); do
	"$old_bin" init "rehearsal-$i" --chain-id "$chain_id" --home "${homes[$i]}" --overwrite >/dev/null 2>&1 \
		|| fail "OLD_BIN could not initialise ${homes[$i]}"
	jq -e '.pub_key.type == "tendermint/PubKeyEd25519"' "${homes[$i]}/config/priv_validator_key.json" >/dev/null \
		|| fail "the consensus key of ${homes[$i]} is not Ed25519"
done
keys_json=$work/keys.json
for ((i = 0; i < nodes; i++)); do
	jq '{address, pub_key}' "${homes[$i]}/config/priv_validator_key.json"
done | jq -s . >"$keys_json"
genesis=$work/genesis.json
python3 "$release/rehearsal/rewrite_genesis.py" --export "$exported" --keys "$keys_json" --out "$genesis" \
	>"$logs/rewrite-genesis.log" 2>&1 || fail "rewrite_genesis.py failed; see $logs/rewrite-genesis.log"
for ((i = 0; i < nodes; i++)); do
	cp "$genesis" "${homes[$i]}/config/genesis.json"
done
"$old_bin" validate-genesis --home "${homes[0]}" >"$logs/validate-genesis.log" 2>&1 \
	|| fail "the rewritten genesis does not validate; see $logs/validate-genesis.log"
for ((i = 0; i < nodes; i++)); do
	key=$(jq -r '.pub_key.value' "${homes[$i]}/config/priv_validator_key.json")
	jq -e --arg k "$key" '[.validators[] | select(.pub_key.value == $k)] | length == 1' "$genesis" >/dev/null \
		|| fail "the consensus key of ${homes[$i]} is not in the rewritten validator list"
done

peers=()
for ((i = 0; i < nodes; i++)); do
	peers+=("$("$old_bin" tendermint show-node-id --home "${homes[$i]}")@127.0.0.1:$(p2p_port "$i")")
done
for ((i = 0; i < nodes; i++)); do
	config=${homes[$i]}/config/config.toml
	app=${homes[$i]}/config/app.toml
	others=()
	for ((j = 0; j < nodes; j++)); do
		[ "$j" -eq "$i" ] || others+=("${peers[$j]}")
	done
	set_toml "$config" "" mode '"validator"'
	set_toml "$config" rpc laddr "\"tcp://127.0.0.1:$(rpc_port "$i")\""
	set_toml "$config" rpc pprof-laddr '""'
	set_toml "$config" p2p laddr "\"tcp://127.0.0.1:$(p2p_port "$i")\""
	set_toml "$config" p2p external-address '""'
	set_toml "$config" p2p bootstrap-peers '""'
	set_toml "$config" p2p persistent-peers "\"$(IFS=,; echo "${others[*]}")\""
	set_toml "$config" p2p pex false
	set_toml "$config" p2p allow-duplicate-ip true
	set_toml "$config" instrumentation prometheus false
	set_toml "$app" api enable false
	set_toml "$app" grpc address "\"127.0.0.1:$((port_base + 20 * i + 4))\""
	set_toml "$app" grpc-web enable false
	set_toml "$app" grpc-web address "\"127.0.0.1:$((port_base + 20 * i + 5))\""
	set_toml "$app" evm http_enabled true
	set_toml "$app" evm http_address '"127.0.0.1"'
	set_toml "$app" evm http_port "$(evm_port "$i")"
	set_toml "$app" evm ws_enabled false
	set_toml "$app" evm ws_port "$((port_base + 20 * i + 3))"
done

# --- 3. run the previous binary to the halt line ----------------------------------------------
cp -p "$old_bin" "$bin_dir/paxd.pre-$release_id"
cp -p "$old_bin" "$bin_dir/paxd.new"
mv -f "$bin_dir/paxd.new" "$bin_dir/paxd"
for ((i = 0; i < nodes; i++)); do
	write_unit "$i" "$logs/node$i-old.log" --halt-height "$halt_height"
done
start_units 0 1 2 3
halt_pattern="halting node per configuration.*height=${halt_height}([^0-9]|$)"
for ((i = 0; i < nodes; i++)); do
	wait_log "$logs/node$i-old.log" "$halt_pattern" "halting node per configuration at height $halt_height on node $i"
done
for ((i = 0; i < nodes; i++)); do
	wait_stopped "$i"
	grep -c panic "$logs/node$i-old.log" | grep -qx 0 || fail "the previous binary log of node $i carries panic"
done
log "the four nodes halted at $halt_height"
[ "$stop_after" != halt ] || { log "stopped after the halt step; work kept in $work"; exit 0; }

# The rollout-shaped env of the local network, read by snapshot.sh, rollback.sh and fleet_check.py.
inventory=$work/inventory.json
for ((i = 0; i < nodes; i++)); do
	jq -n --arg name "rehearsal-$i" --arg home "${homes[$i]}" --arg unit "${units[$i]}" \
		--arg rpc "$(rpc_url "$i")" --arg evm "$(evm_url "$i")" \
		'{name: $name, role: "validator", host: "localhost", home: $home, unit: $unit, consensus_rpc: $rpc, evm_rpc: $evm}'
done | jq -s '{nodes: .}' >"$inventory"
read -ra key_names <<<"$operator_keys"
operators=()
ordered_keys=()
for ((i = 0; i < nodes; i++)); do
	key=$(jq -r '.pub_key.value' "${homes[$i]}/config/priv_validator_key.json")
	operator=$(jq -r --arg k "$key" '.app_state.staking.validators[] | select(.consensus_pubkey.key == $k) | .operator_address' "$genesis")
	[ -n "$operator" ] || fail "no bonded validator of the rewritten genesis carries the key of ${homes[$i]}"
	name=""
	for candidate in "${key_names[@]}"; do
		if [ "$("$old_bin" keys show "$candidate" --bech val -a --keyring-backend "$keyring_backend" --keyring-dir "$keyring_dir" 2>/dev/null)" = "$operator" ]; then
			name=$candidate
		fi
	done
	[ -n "$name" ] || fail "no key of OPERATOR_KEY_NAMES is the operator $operator of node $i"
	operators+=("$operator")
	ordered_keys+=("$name")
done
{
	printf 'RELEASE_ID=%s\n' "$release_id"
	printf 'RELEASE_TAG=%s\n' "paxeer-network/$plan"
	printf 'RELEASE_SHA256=%s\n' "$new_sha"
	printf 'SIDECAR_SHA256=%s\n' "$sidecar_sha"
	printf 'HALT_HEIGHT=%s\n' "$halt_height"
	printf 'CHAIN_ID=%s\n' "$chain_id"
	printf 'KEYRING_BACKEND=%s\n' "$keyring_backend"
	printf 'BIN_DIR=%s\n' "$bin_dir"
	printf 'VALIDATOR_HOSTS="%s"\n' "localhost localhost localhost localhost"
	printf 'VALIDATOR_HOMES="%s"\n' "${homes[*]}"
	printf 'VALIDATOR_UNITS="%s"\n' "${units[*]}"
	printf 'OPERATOR_KEY_NAMES="%s"\n' "${ordered_keys[*]}"
	printf 'FULLNODE_HOSTS=\nFULLNODE_HOMES=\nFULLNODE_UNITS=\n'
	printf 'SNAPSHOT_DIR=%s\n' "$snapshots"
	printf 'CREDENTIAL_DIR=%s\n' "$credentials"
	printf 'CHAINFLOW_KEY_FILE=%s\n' "$chainflow_key_file"
	printf 'CONSENSUS_RPC=%s\n' "$(rpc_url 0)"
	printf 'EVM_RPC=%s\n' "$(evm_url 0)"
	printf 'INVENTORY_FILE=%s\n' "$inventory"
	printf 'RECEIPT_DIR=%s\n' "$receipts"
} >"$rehearsal_env"
chmod 600 "$rehearsal_env"

bash "$release/snapshot.sh" --env-file "$rehearsal_env" >"$logs/snapshot.log" 2>&1 \
	|| fail "snapshot.sh failed; see $logs/snapshot.log"
bash "$release/snapshot.sh" --env-file "$rehearsal_env" --verify >>"$logs/snapshot.log" 2>&1 \
	|| fail "snapshot.sh --verify failed; see $logs/snapshot.log"
log "every home archived under $snapshots"

# --- 4. upgrade files, the release candidate and the applied plan ------------------------------
for ((i = 0; i < nodes; i++)); do
	python3 "$release/upgrade_info.py" node --home "${homes[$i]}" --halt-height "$halt_height" \
		--release-doc "$release_dir/upgrade-info.json" --sha256 "$new_sha" >>"$logs/upgrade-info.log" 2>&1 \
		|| fail "upgrade_info.py node failed for ${homes[$i]}; see $logs/upgrade-info.log"
	jq -e --arg name "$plan" --argjson h "$h1" '.name == $name and .height == $h' "${homes[$i]}/data/upgrade-info.json" >/dev/null \
		|| fail "${homes[$i]}/data/upgrade-info.json does not name $plan at $h1"
done
cp -p "$new_bin" "$bin_dir/paxd.new"
mv -f "$bin_dir/paxd.new" "$bin_dir/paxd"
[ "$(sha256sum "$bin_dir/paxd" | cut -d' ' -f1)" = "$new_sha" ] || fail "the swapped binary is not NEW_BIN"
if ldd "$bin_dir/paxd" 2>&1 | grep -q 'not found'; then
	fail "NEW_BIN has an unresolved shared library: $(ldd "$bin_dir/paxd" | grep 'not found')"
fi
for ((i = 0; i < nodes; i++)); do
	write_unit "$i" "$logs/node$i-new.log"
done
# Node 0 alone cannot commit, so its ABCI info holds the committed state of the halt height.
start_units 0
deadline=$((SECONDS + wait_seconds))
hash_at_halt=""
while [ -z "$hash_at_halt" ]; do
	[ "$SECONDS" -lt "$deadline" ] || fail "node 0 did not answer abci_info at $halt_height; see $logs/node0-new.log"
	if [ "$(jget "$(rpc_url 0)/abci_info" '.response.last_block_height')" = "$halt_height" ]; then
		hash_at_halt=$(jget "$(rpc_url 0)/abci_info" '.response.last_block_app_hash')
	fi
	sleep 1
done
log "application hash at $halt_height: $hash_at_halt"
start_units 1 2 3
for ((i = 0; i < nodes; i++)); do
	wait_log "$logs/node$i-new.log" "applying upgrade.*name=${plan//./\\.}([^0-9.]|$)" "applying upgrade on node $i"
done
for ((i = 0; i < nodes; i++)); do
	wait_height_above "$i" "$((h1 + 1))" >/dev/null
	count=$(grep -Ec "applying upgrade.*name=${plan//./\\.}([^0-9.]|$)" "$logs/node$i-new.log")
	[ "$count" -eq 1 ] || fail "node $i logged applying upgrade $count times"
	applied=$("$new_bin" q upgrade applied "$plan" --node "tcp://127.0.0.1:$(rpc_port "$i")" -o json 2>&1) \
		|| fail "q upgrade applied $plan failed on node $i: $applied"
	[ "$(jq -r '.header.height' <<<"$applied")" = "$h1" ] || fail "node $i applied $plan at $(jq -r '.header.height' <<<"$applied"), expected $h1"
	version=$(jget "$(rpc_url "$i")/abci_info" '.response.version')
	[ "$version" = "$plan" ] || fail "node $i reports ABCI version '$version', expected $plan"
done
[ "$(jget "$(rpc_url 0)/block?height=$h1" '.block.header.app_hash')" = "$hash_at_halt" ] \
	|| fail "block $h1 does not carry the application hash recorded at $halt_height"
log "plan $plan applied at $h1 on the four nodes"

# --- 5. presence switch, pool parity and the feed keys -----------------------------------------
precompiles_served=0
while IFS=$'\t' read -r address calldata length; do
	before=$(eth_call_at 0 "$address" "$calldata" "$halt_height")
	if [ "$address" != "$launchpad" ] && [ "$address" != 0x0000000000000000000000000000000000001015 ]; then
		[ "$before" = 0x ] || fail "$address answered $before at $halt_height, before activation"
	fi
	after=$(eth_call_at 0 "$address" "$calldata" "$h1")
	[ "$after" != revert ] || fail "$address reverted its view call at $h1"
	[ $(((${#after} - 2) / 2)) -eq "$length" ] || fail "$address returned $(((${#after} - 2) / 2)) bytes at $h1, expected $length"
	precompiles_served=$((precompiles_served + 1))
done < <(jq -r '.served[] | [.address, .calldata, (.return_length | tostring)] | @tsv' "$release/fleet-views.json")
for address in $(jq -r '.no_code[]' "$release/fleet-views.json"); do
	for at in "$halt_height" "$h1"; do
		[ "$(get_code_at 0 "$address" "$at")" = 0x ] || fail "$address carries code at $at"
	done
done
log "precompiles served at $h1: $precompiles_served"

pools_file=$work/pools-at-halt.json
python3 "$release/rehearsal/pool_parity.py" record --rpc "$(evm_url 0)" --block "$halt_height" --out "$pools_file" \
	>"$logs/pool-parity.log" 2>&1 || fail "pool_parity.py record failed; see $logs/pool-parity.log"
python3 "$release/rehearsal/pool_parity.py" check --rpc "$(evm_url 0)" --block "$h1" --record "$pools_file" \
	>>"$logs/pool-parity.log" 2>&1 || fail "pool parity failed: $(tail -n 1 "$logs/pool-parity.log")"
pools=$(sed -nE 's/^pools ([0-9]+) of [0-9]+$/\1/p' "$logs/pool-parity.log")
[[ "$pools" =~ ^[0-9]+$ ]] || fail "pool_parity.py printed no pool count; see $logs/pool-parity.log"
log "launchpad pools $pools of $pools, reserves equal"

feed_accounts=()
for ((i = 0; i < nodes; i++)); do
	dir=$credentials/${units[$i]}
	mkdir -p "$dir"
	chmod 700 "$dir"
	python3 "$release/feed_key.py" new --out "$dir/feed.key" >"$dir/feed.pub" || fail "feed_key.py new failed for node $i"
	pubkey=$(tr -d '[:space:]' <"$dir/feed.pub")
	pubkey=${pubkey#0x}
	[[ "$pubkey" =~ ^0[23][0-9a-f]{64}$ ]] || fail "feed_key.py printed '$pubkey', not a compressed public key"
	tx_flags=(--from "${ordered_keys[$i]}" --keyring-backend "$keyring_backend" --keyring-dir "$keyring_dir"
		--home "${homes[$i]}" --chain-id "$chain_id" --evm-rpc "$(evm_url "$i")")
	if ! out=$("$new_bin" tx evm associate-address "${tx_flags[@]}" 2>&1); then
		grep -q 'already associated' <<<"$out" || fail "associate-address failed for ${ordered_keys[$i]}: $out"
		log "operator ${ordered_keys[$i]} is already associated"
	fi
	payload=$(python3 "$release/feed_key.py" payload "$pubkey") || fail "feed_key.py payload failed for node $i"
	out=$("$new_bin" tx evm call-contract "$pricefeed" "${payload#0x}" "${tx_flags[@]}" 2>&1) \
		|| fail "registerFeedKey failed for ${ordered_keys[$i]}: $out"
	validator_hex=$("$new_bin" keys parse "${operators[$i]}" --output json | jq -r '.bytes' | tr 'A-F' 'a-f')
	[[ "$validator_hex" =~ ^[0-9a-f]{40}$ ]] || fail "operator ${operators[$i]} does not parse to 20 bytes"
	deadline=$((SECONDS + 120))
	account=""
	while [ -z "$account" ]; do
		[ "$SECONDS" -lt "$deadline" ] || fail "getFeedKey of ${operators[$i]} did not return the registered key"
		current=$(height 0)
		result=$(eth_call_at 0 "$pricefeed" "${get_feed_key}000000000000000000000000$validator_hex" "$current")
		if [ "$result" != revert ] && [ "${#result}" -ge $((2 + 64 * 4)) ] && [ "${result:2+64*3:66}" = "$pubkey" ]; then
			account=${result:2+64+24:40}
		else
			sleep 1
		fi
	done
	feed_accounts+=("$account")
	log "feed key of ${operators[$i]} registered, feed account 0x$account"
done

for ((i = 0; i < nodes; i++)); do
	dir=$credentials/${units[$i]}
	jq --arg chain "$chain_id" --arg a "0x${feed_accounts[$i]}" \
		--arg v "0x$("$new_bin" keys parse "${operators[$i]}" --output json | jq -r '.bytes' | tr 'A-F' 'a-f')" \
		'del(.note) | .chain_id = $chain | .validator = $v | .feed_account = $a' "$sidecar_config" >"$dir/config.json"
	mkdir -p "$dir/state"
	chmod 700 "$dir/state"
	PRICE_SIDECAR_CONFIG=$dir/config.json \
		PRICE_SIDECAR_FEED_KEY_FILE=$dir/feed.key \
		PRICE_FEED_REST_API_KEY_FILE=$chainflow_key_file \
		PRICE_SIDECAR_STATE_DIR=$dir/state \
		PRICE_SIDECAR_HEALTH_ADDR=127.0.0.1:$((port_base + 20 * i + 6)) \
		PRICE_SIDECAR_CONSENSUS_RPC_URL=$(rpc_url "$i") \
		PRICE_SIDECAR_EVM_RPC_URL=$(evm_url "$i") \
		"$sidecar_bin" >"$logs/sidecar$i.log" 2>&1 &
	sidecar_pids+=("$!")
done
for ((i = 0; i < nodes; i++)); do
	deadline=$((SECONDS + wait_seconds))
	until curl -sf --max-time 5 -o /dev/null "http://127.0.0.1:$((port_base + 20 * i + 6))/readyz"; do
		kill -0 "${sidecar_pids[$i]}" 2>/dev/null || fail "sidecar $i exited; see $logs/sidecar$i.log"
		[ "$SECONDS" -lt "$deadline" ] || fail "sidecar $i was not ready within ${wait_seconds}s; see $logs/sidecar$i.log"
		sleep 1
	done
done
log "four price sidecars ready"

# --- 6. markets, oracle and precompiles through fleet_check.py ---------------------------------
for phase in markets precompiles oracle; do
	deadline=$((SECONDS + wait_seconds))
	until python3 "$release/fleet_check.py" --env-file "$rehearsal_env" --phase "$phase" \
		--receipt "$receipts/$phase.json" >"$logs/fleet-check-$phase.log" 2>&1; do
		[ "$phase" = oracle ] && [ "$SECONDS" -lt "$deadline" ] || fail "fleet_check.py --phase $phase failed; see $logs/fleet-check-$phase.log"
		sleep 5
	done
done
markets=$(jq -r '.markets' "$receipts/markets.json")
[ "$markets" = 18 ] || fail "fleet_check.py reports $markets markets, expected 18"
[ "$(jq -r '.precompiles_served' "$receipts/precompiles.json")" = "$precompiles_served" ] \
	|| fail "fleet_check.py serves $(jq -r '.precompiles_served' "$receipts/precompiles.json") precompiles, the presence check $precompiles_served"
feed_keys=$(jq -r '.feed_keys' "$receipts/oracle.json")
[ "$feed_keys" = "$nodes" ] || fail "fleet_check.py reports $feed_keys feed keys, expected $nodes"
oracle_fresh=$(jq -r '.oracle_fresh' "$receipts/oracle.json")
[ "$oracle_fresh" = true ] || [ "$oracle_fresh" = 18 ] || fail "fleet_check.py reports oracle_fresh $oracle_fresh"

# --- 7. block hashes, the final height and the logs --------------------------------------------
final_height=$(wait_height_above 0 "$((halt_height + 10))")
for ((h = h1; h <= halt_height + 10; h++)); do
	first=""
	for ((i = 0; i < nodes; i++)); do
		hash=$(jget "$(rpc_url "$i")/block?height=$h" '.block_id.hash')
		[ -n "$hash" ] || fail "node $i returned no block hash at $h"
		[ -n "$first" ] || first=$hash
		[ "$hash" = "$first" ] || fail "block $h differs between node 0 ($first) and node $i ($hash)"
	done
done
for ((i = 0; i < nodes; i++)); do
	grep -c panic "$logs/node$i-new.log" | grep -qx 0 || fail "the release candidate log of node $i carries panic"
done
log "blocks $h1..$((halt_height + 10)) agree on the four nodes; final height $final_height"

# --- 8. rollback leg ---------------------------------------------------------------------------
for pid in "${sidecar_pids[@]}"; do
	kill -TERM "$pid" 2>/dev/null || true
	wait "$pid" 2>/dev/null || true
done
sidecar_pids=()
stop_units
for ((i = 0; i < nodes; i++)); do
	write_unit "$i" "$logs/node$i-rollback.log"
done
systemctl daemon-reload
bash "$release/rollback.sh" --env-file "$rehearsal_env" --execute >"$logs/rollback.log" 2>&1 \
	|| fail "rollback.sh --execute failed; see $logs/rollback.log"
[ "$(sha256sum "$bin_dir/paxd" | cut -d' ' -f1)" = "$old_sha" ] || fail "rollback did not restore the previous binary"
for ((i = 0; i < nodes; i++)); do
	[ ! -e "${homes[$i]}/data/upgrade-info.json" ] || fail "rollback left ${homes[$i]}/data/upgrade-info.json"
	[ -d "${homes[$i]}/data.$plan-$release_id" ] || fail "rollback did not set aside the data of ${homes[$i]}"
	wait_height_above "$i" "$((halt_height + 5))" >/dev/null
done
restored_hash=$(jget "$(rpc_url 0)/block?height=$h1" '.block.header.app_hash')
[ "$restored_hash" = "$hash_at_halt" ] \
	|| fail "the restored chain carries application hash $restored_hash at $halt_height, recorded $hash_at_halt"
for ((i = 0; i < nodes; i++)); do
	grep -c panic "$logs/node$i-rollback.log" | grep -qx 0 || fail "the restored log of node $i carries panic"
done
stop_units
log "rollback leg passed: application hash at $halt_height restored, chain past $((halt_height + 5)) on the previous binary"

{
	printf 'upgrade=%s\n' "$plan"
	printf 'halt_height=%s\n' "$halt_height"
	printf 'export_height=%s\n' "$export_height"
	printf 'final_height=%s\n' "$final_height"
	printf 'old_bin_sha256=%s\n' "$old_sha"
	printf 'new_bin_sha256=%s\n' "$new_sha"
	printf 'pools=%s\n' "$pools"
	printf 'markets=%s\n' "$markets"
	printf 'oracle_fresh=%s\n' "$oracle_fresh"
	printf 'feed_keys=%s\n' "$feed_keys"
	printf 'precompiles_served=%s\n' "$precompiles_served"
	printf 'rollback_rehearsed=%s\n' true
} >"$work/rehearsal-result.env"
passed=1
write_receipt pass
log "rehearsal passed; result in $work/rehearsal-result.env"
