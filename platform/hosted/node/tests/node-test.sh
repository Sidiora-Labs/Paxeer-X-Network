#!/usr/bin/env bash
# Real-process test for the beta node bring-up: bootstraps a temporary data
# directory, runs the replica and sequencer supervisors against build/bin,
# proves the LNI handshake with layerx-client, reads the treasury balance,
# exercises the supervisor reset socket, and stops everything.
#
# Runs as root (or with CAP_SETUID) so the LNI client can present a uid that
# differs from the daemon uid, as the LNI requires. Override the client
# identity with LAYERX_NODE_TEST_CLIENT_UID / LAYERX_NODE_TEST_CLIENT_GID.
# The supervisors need socat: LAYERX_TEST_SOCAT_BIN names it, otherwise the
# first socat on PATH is used. The genesis metadata the bootstrap requires is
# built from tests/support/lxgb_metadata.py over the beta asset and the
# treasury key generated here.
#
# Settlement inputs: with LAYERX_NODE_PAXEER_RPC_URL unset the bootstrap takes
# the chain 125 defaults (the loopback JSON-RPC http://127.0.0.1:8545 and the
# registry, custody and anchor precompiles) and no chain is contacted. Set to
# the JSON-RPC of a synced node (http://127.0.0.1:PORT, or an https origin
# relayed onto a loopback port the way the pod's paxeer-relay does) the
# precompile settlement case first proves the node is not anvil, answers the
# chain id, follows the chain (its head advances between two reads; the RPC
# has no eth_syncing) and answers the three precompiles' views encoded from
# the ABIs under precompiles/, then bootstraps the sequencer against that
# loopback URL. LAYERX_NODE_TEST_CA_FILE names the trust store of the https
# relay (default: the system bundle).
#
# The treasury's main account opens on its first credit, never at genesis:
# tests/daemon/withdraw-custody.py --export deposits into the custody
# precompile of an owned paxd chain for the treasury, builds the custody
# profile the genesis is bootstrapped with and the credit the treasury signs,
# and the test submits that credit before the operator SEND. The fixture runs
# under BRIDGE_PYTHON (default python3), which must import the packages of
# tests/bridge/requirements.txt, and needs forge, cast, go, cargo,
# build/tests/bridge/sign-credit and PAXD naming the paxd of make paxeer-build
# (an installed release paxd lacks the loopback RPC bind setting); its cargo
# builds use platform/target.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../../../.." && pwd)
NODE_DIR="$ROOT/platform/hosted/node"
NATIVE_BIN_DIR=${LAYERX_TEST_NATIVE_BIN_DIR:-$ROOT/build/bin}
LAYERXD="$NATIVE_BIN_DIR/layerxd"
GENESIS_BUILD="$NATIVE_BIN_DIR/layerx-genesis-build"
CARGO=${PLATFORM_CARGO:-cargo}
BRIDGE_PYTHON=${BRIDGE_PYTHON:-python3}
NETWORK_ID=${LAYERX_NODE_TEST_NETWORK_ID:-4242}
ASSET_ID=b5a32b12029f8ddfb905f90f280f664b46390de0fc62770fc197dd87b18cd898
PROGRAM_PORT=${LAYERX_NODE_TEST_PROGRAM_PORT:-19401}
REPLICA_PORT=${LAYERX_NODE_TEST_REPLICA_PORT:-19402}
PAXEER_CHAIN_ID=${LAYERX_NODE_PAXEER_CHAIN_ID:-125}
RPC_URL=${LAYERX_NODE_PAXEER_RPC_URL:-}
REGISTRY_PRECOMPILE=0x0000000000000000000000000000000000001004
CUSTODY_PRECOMPILE=0x0000000000000000000000000000000000001013
ANCHOR_PRECOMPILE=0x0000000000000000000000000000000000001014

log() { printf 'node-test: %s\n' "$*" >&2; }
fail() { log "FAIL: $*"; exit 1; }

# The kernel opens agent:<did>:main on its first credit; until then an account
# read is refused with class 4 result -208 (LXP_ERR_UNKNOWN_ACCOUNT_NAMESPACE,
# the absent-account code) and the DID account listing is empty, exactly what
# the daemon's onboarding test and the core boundary assert for a fresh genesis.
expect_treasury_unopened() {
    BALANCE=$(as_client "$WORK/probe" balance --socket "$LAYERX_NODE_LNI_SOCKET" --network-id "$NETWORK_ID" \
        --account "$LAYERX_NODE_TREASURY_ACCOUNT" --asset "$LAYERX_NODE_ASSET_ID")
    log "$BALANCE"
    expect_contains "$BALANCE" "\"account\":\"$LAYERX_NODE_TREASURY_ACCOUNT\""
    expect_contains "$BALANCE" "\"asset\":\"$LAYERX_NODE_ASSET_ID\""
    expect_contains "$BALANCE" '"refused":{"class":4,"result":-208}'
    [ "$LAYERX_NODE_TREASURY_BALANCE" = 0 ] || fail "node.env treasury balance is not the genesis zero"
    ACCOUNTS=$(as_client "$WORK/probe" did-accounts --socket "$LAYERX_NODE_LNI_SOCKET" --network-id "$NETWORK_ID" \
        --did "$LAYERX_NODE_TREASURY_DID")
    log "$ACCOUNTS"
    expect_contains "$ACCOUNTS" "\"did\":\"$LAYERX_NODE_TREASURY_DID\",\"count\":0,\"accounts\":[]"
}

[ -x "$LAYERXD" ] || fail "$LAYERXD missing; run make layerxd"
[ -x "$GENESIS_BUILD" ] || fail "$GENESIS_BUILD missing; run make layerx-genesis-build"
for tool in openssl setpriv od sha256sum jq python3; do
    command -v "$tool" >/dev/null || fail "$tool is required"
done
if [ -n "${LAYERX_TEST_SOCAT_BIN:-}" ]; then
    SOCAT=$LAYERX_TEST_SOCAT_BIN
    [ -f "$SOCAT" ] && [ -x "$SOCAT" ] || fail "socat_invalid: LAYERX_TEST_SOCAT_BIN=$SOCAT is not an executable file"
else
    SOCAT=$(command -v socat || true)
    [ -n "$SOCAT" ] || fail "socat_missing: no socat executable on PATH; set LAYERX_TEST_SOCAT_BIN"
fi
[ "$(id -u)" -eq 0 ] || fail "must run as root so the LNI client can present a distinct uid"
CLIENT_UID=${LAYERX_NODE_TEST_CLIENT_UID:-$(id -u nobody)}
CLIENT_GID=${LAYERX_NODE_TEST_CLIENT_GID:-$(id -g nobody)}
[ "$CLIENT_UID" != "$(id -u)" ] || fail "client uid must differ from the daemon uid"

PROBE=${LAYERX_NODE_TEST_PROBE_BIN:-}
[ -n "$PROBE" ] || fail "LAYERX_NODE_TEST_PROBE_BIN must name the source-bound prebuilt probe"
[ -f "$PROBE" ] && [ -x "$PROBE" ] || fail "probe binary missing at $PROBE"
[ -n "${LAYERX_CUSTODY_ARTIFACT_MANIFEST:-}" ] || fail "LAYERX_CUSTODY_ARTIFACT_MANIFEST is required"

WORK=$(mktemp -d /tmp/layerx-node-test.XXXXXX)
chmod 0755 "$WORK"
cp "$PROBE" "$WORK/probe"
cp "$NATIVE_BIN_DIR/layerxctl" "$WORK/layerxctl"
chmod 0755 "$WORK/probe"
chmod 0755 "$WORK/layerxctl"
DATA="$WORK/data"
RUN="$WORK/run"
SEQUENCER_PID=""
REPLICA_PID=""
RELAY_PID=""
KEEP=1

cleanup() {
    local pid
    for pid in "$SEQUENCER_PID" "$REPLICA_PID"; do
        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
            kill -TERM "$pid" 2>/dev/null || true
        fi
    done
    for pid in "$SEQUENCER_PID" "$REPLICA_PID"; do
        [ -n "$pid" ] && wait "$pid" 2>/dev/null || true
    done
    pkill -TERM -f "$LAYERXD --serve $DATA/" 2>/dev/null || true
    pkill -TERM -f "$LAYERXD --authority-replica $DATA/" 2>/dev/null || true
    if [ -n "$RELAY_PID" ]; then
        kill -TERM "$RELAY_PID" 2>/dev/null || true
        wait "$RELAY_PID" 2>/dev/null || true
    fi
    if [ "$KEEP" -eq 1 ]; then
        log "logs retained under $WORK"
        for logfile in "$WORK/sequencer.log" "$WORK/replica.log"; do
            [ -r "$logfile" ] && { printf -- '--- %s\n' "$logfile" >&2; tail -n 40 "$logfile" >&2; }
        done
    else
        rm -rf "$WORK"
    fi
}
trap cleanup EXIT

umask 077
openssl rand 32 > "$WORK/sequencer.key"
openssl rand 32 > "$WORK/treasury.key"
umask 022
TREASURY_PUBLIC=$({ printf '\x30\x2e\x02\x01\x00\x30\x05\x06\x03\x2b\x65\x70\x04\x22\x04\x20'; cat "$WORK/treasury.key"; } \
    | openssl pkey -inform DER -pubout -outform DER | tail -c 32 | od -An -v -tx1 | tr -d ' \n')
[ ${#TREASURY_PUBLIC} -eq 64 ] || fail "could not derive the treasury public key"
METADATA="$WORK/genesis-metadata.lxgb"
python3 - "$ROOT/tests/support/lxgb_metadata.py" "$ASSET_ID" "$TREASURY_PUBLIC" "$METADATA" <<'LXGB' || fail "could not build the genesis metadata"
import importlib.util, os, sys
spec = importlib.util.spec_from_file_location('lxgb_metadata', sys.argv[1])
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
module.check()
with open(sys.argv[4], 'wb') as output:
    output.write(module.metadata(bytes.fromhex(sys.argv[2]), bytes.fromhex(sys.argv[3]), os.urandom(32)))
LXGB
chmod 0644 "$METADATA"

as_client() {
    setpriv --reuid="$CLIENT_UID" --regid="$CLIENT_GID" --clear-groups "$@"
}

wait_for() {
    # wait_for PATH SECONDS
    local deadline=$(( $(date +%s) + $2 ))
    while [ ! -e "$1" ]; do
        if [ -n "$SEQUENCER_PID" ] && ! kill -0 "$SEQUENCER_PID" 2>/dev/null; then
            fail "sequencer supervisor exited while waiting for $1"
        fi
        if [ -n "$REPLICA_PID" ] && ! kill -0 "$REPLICA_PID" 2>/dev/null; then
            fail "replica supervisor exited while waiting for $1"
        fi
        [ "$(date +%s)" -lt "$deadline" ] || fail "timed out waiting for $1"
        sleep 0.2
    done
}

expect_contains() {
    case "$1" in *"$2"*) ;; *) fail "expected $2 in: $1" ;; esac
}

free_port() {
    python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])'
}

json_rpc() {
    # json_rpc URL METHOD PARAMS -> the JSON result
    python3 - "$1" "$2" "$3" <<'RPC'
import json, sys, urllib.request
url, method, params = sys.argv[1:4]
body = json.dumps({'jsonrpc': '2.0', 'id': 1, 'method': method, 'params': json.loads(params)}).encode()
with urllib.request.urlopen(urllib.request.Request(url, body, {'Content-Type': 'application/json'}), timeout=30) as response:
    reply = json.load(response)
if reply.get('error') is not None or 'result' not in reply:
    raise SystemExit('%s answered %s' % (method, json.dumps(reply)))
print(json.dumps(reply['result']))
RPC
}

abi_declares() {
    # abi_declares ABI_FILE SIGNATURE
    jq -e --arg signature "$2" \
        'any(.[] | select(.type == "function") | .name + "(" + ([.inputs[].type] | join(",")) + ")"; . == $signature)' "$1" > /dev/null
}

precompile_view() {
    # precompile_view URL ADDRESS ABI_FILE SIGNATURE [ARGUMENT_WORDS] -> the 32-byte word the view answers, as hex
    local url=$1 address=$2 abi=$3 signature=$4 words=${5:-} selector result
    abi_declares "$abi" "$signature" || fail "$abi does not declare $signature"
    selector=$(python3 - "$ROOT/platform/hosted/paxeer/evm.py" "$signature" <<'SELECTOR'
import importlib.util, sys
spec = importlib.util.spec_from_file_location('evm', sys.argv[1])
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
print('0x' + module.keccak(sys.argv[2].encode())[:4].hex())
SELECTOR
    )
    result=$(json_rpc "$url" eth_call "[{\"to\":\"$address\",\"data\":\"$selector$words\"},\"latest\"]")
    [[ $result =~ ^\"0x[0-9a-f]{64}\"$ ]] || fail "$signature at $address answered $result"
    printf '%s' "${result:3:64}"
}

SETTLEMENT_RPC=""
if [ -n "$RPC_URL" ]; then
    log "precompile settlement case against $RPC_URL"
    if [[ $RPC_URL =~ ^http://127\.0\.0\.1:[1-9][0-9]{0,4}$ ]]; then
        SETTLEMENT_RPC=$RPC_URL
    elif [[ $RPC_URL =~ ^https://([A-Za-z0-9.-]+)(:([1-9][0-9]{0,4}))?/?$ ]]; then
        RELAY_HOST=${BASH_REMATCH[1]}
        RELAY_PORT=${BASH_REMATCH[3]:-443}
        SETTLEMENT_RPC="http://127.0.0.1:$(free_port)"
        "$SOCAT" -T 120 "TCP4-LISTEN:${SETTLEMENT_RPC##*:},bind=127.0.0.1,reuseaddr,fork" \
            "OPENSSL:$RELAY_HOST:$RELAY_PORT,cafile=${LAYERX_NODE_TEST_CA_FILE:-/etc/ssl/certs/ca-certificates.crt},verify=1,commonname=$RELAY_HOST" \
            > "$WORK/relay.log" 2>&1 &
        RELAY_PID=$!
        log "relaying $SETTLEMENT_RPC to $RELAY_HOST:$RELAY_PORT"
    else
        fail "LAYERX_NODE_PAXEER_RPC_URL must be http://127.0.0.1:PORT or an https origin, not $RPC_URL"
    fi
    CLIENT_VERSION=$(json_rpc "$SETTLEMENT_RPC" web3_clientVersion '[]')
    case "${CLIENT_VERSION,,}" in
        *anvil*|*hardhat*) fail "refusing the settlement case against a development chain: $CLIENT_VERSION" ;;
    esac
    CHAIN_ID_HEX=$(json_rpc "$SETTLEMENT_RPC" eth_chainId '[]')
    [ "$CHAIN_ID_HEX" != '"0x7a69"' ] || fail "refusing the settlement case against anvil chain 31337"
    [ "$CHAIN_ID_HEX" = "\"$(printf '0x%x' "$PAXEER_CHAIN_ID")\"" ] || fail "the node answers chain $CHAIN_ID_HEX, not $PAXEER_CHAIN_ID"
    HEAD_BEFORE=$(json_rpc "$SETTLEMENT_RPC" eth_blockNumber '[]')
    HEAD_BEFORE=${HEAD_BEFORE//\"/}
    [[ $HEAD_BEFORE =~ ^0x[0-9a-f]+$ ]] || fail "eth_blockNumber at $RPC_URL answered $HEAD_BEFORE"
    sleep 3
    HEAD_AFTER=$(json_rpc "$SETTLEMENT_RPC" eth_blockNumber '[]')
    HEAD_AFTER=${HEAD_AFTER//\"/}
    [ $((HEAD_AFTER)) -gt $((HEAD_BEFORE)) ] || fail "the node at $RPC_URL is not following the chain: its head stayed at $HEAD_BEFORE"
    THRESHOLD=$(precompile_view "$SETTLEMENT_RPC" "$ANCHOR_PRECOMPILE" "$ROOT/precompiles/layerxanchor/abi.json" 'threshold()')
    [ $((16#$THRESHOLD)) -ge 1 ] || fail "the anchor precompile reports no certificate threshold"
    STATUS=$(precompile_view "$SETTLEMENT_RPC" "$ANCHOR_PRECOMPILE" "$ROOT/precompiles/layerxanchor/abi.json" 'statusOf(uint64)' "$(printf '%064x' 1)")
    DEPOSITS=$(precompile_view "$SETTLEMENT_RPC" "$CUSTODY_PRECOMPILE" "$ROOT/precompiles/layerxcustody/abi.json" 'depositCount()')
    BIND_NONCE=$(precompile_view "$SETTLEMENT_RPC" "$REGISTRY_PRECOMPILE" "$ROOT/precompiles/addr/abi.json" 'layerXBindNonce(address)' "$(printf '%064x' 0)")
    log "chain $PAXEER_CHAIN_ID ($CLIENT_VERSION): anchor threshold $((16#$THRESHOLD)), checkpoint 1 status $((16#$STATUS)), custody deposits $((16#$DEPOSITS)), registry bind nonce $((16#$BIND_NONCE))"
    for signature in 'submitCheckpoint(bytes,bytes,bytes)' 'finalize(uint64)'; do
        abi_declares "$ROOT/precompiles/layerxanchor/abi.json" "$signature" || fail "the anchor ABI does not declare $signature"
    done
    export LAYERX_NODE_PAXEER_CHAIN_ID="$PAXEER_CHAIN_ID" LAYERX_NODE_PAXEER_RPC_URL="$SETTLEMENT_RPC"
    export LAYERX_NODE_REGISTRY_PRECOMPILE="$REGISTRY_PRECOMPILE" LAYERX_NODE_CUSTODY_PRECOMPILE="$CUSTODY_PRECOMPILE" \
        LAYERX_NODE_ANCHOR_PRECOMPILE="$ANCHOR_PRECOMPILE"
else
    SETTLEMENT_RPC=http://127.0.0.1:8545
    log "no LAYERX_NODE_PAXEER_RPC_URL: the bootstrap takes the chain $PAXEER_CHAIN_ID loopback defaults"
fi

log "custody deposit for the treasury on an owned paxd chain, its custody profile and the signed first credit"
mkdir "$WORK/custody"
CARGO_TARGET_DIR="$ROOT/platform/target" "$BRIDGE_PYTHON" "$ROOT/tests/daemon/withdraw-custody.py" "$NATIVE_BIN_DIR/.." \
    --export "$WORK/custody" --network-id "$NETWORK_ID" --sequencer-key "$WORK/sequencer.key" \
    --beneficiary-key "$WORK/treasury.key" >&2 || fail "the custody credit fixture failed"
CREDIT_AMOUNT=$(jq -r '.amount' "$WORK/custody/custody.json")
[[ $CREDIT_AMOUNT =~ ^[1-9][0-9]*$ ]] || fail "custody deposit amount is $CREDIT_AMOUNT"

log "starting the replica supervisor"
bash "$NODE_DIR/supervisor.sh" --role replica --data-dir "$DATA" --run-dir "$RUN" \
    --layerxd "$LAYERXD" --socat "$SOCAT" > "$WORK/replica.log" 2>&1 &
REPLICA_PID=$!

log "starting the sequencer supervisor (bootstraps $DATA)"
bash "$NODE_DIR/supervisor.sh" --role sequencer --data-dir "$DATA" --run-dir "$RUN" \
    --layerxd "$LAYERXD" --socat "$SOCAT" -- \
    --network-id "$NETWORK_ID" --asset "$ASSET_ID" --genesis-metadata "$METADATA" \
    --sequencer-key "$WORK/sequencer.key" --treasury-key "$WORK/treasury.key" \
    --lni-uid "$CLIENT_UID" --lni-gid "$CLIENT_GID" \
    --program-port "$PROGRAM_PORT" --replica-port "$REPLICA_PORT" \
    --migrations "$ROOT/migrations/0007_history_index.sql" \
    --genesis-build "$GENESIS_BUILD" --custody-profile "$WORK/custody/custody.profile" > "$WORK/sequencer.log" 2>&1 &
SEQUENCER_PID=$!

wait_for "$DATA/node.env" 60
wait_for "$RUN/layerxd.lni.sock" 60
wait_for "$RUN/supervisor.sock" 60
set -a
# shellcheck disable=SC1091
. "$DATA/node.env"
set +a
[ "$LAYERX_NODE_NETWORK_ID" = "$NETWORK_ID" ] || fail "node.env network id mismatch"
[ "$LAYERX_NODE_ASSET_ID" = "$ASSET_ID" ] || fail "node.env asset id mismatch"
[ "$LAYERX_NODE_TREASURY_PUBLIC_KEY" = "$TREASURY_PUBLIC" ] || fail "node.env treasury public key mismatch"
[ "$(stat -c %a "$RUN")" = 750 ] || fail "run directory is not mode 0750"
[ "$(stat -c %g "$RUN")" = "$CLIENT_GID" ] || fail "run directory group is not the LNI gid"
[ "$(stat -c %s "$DATA/genesis/genesis.registration")" = 82 ] || fail "bootstrap registration missing"
grep -q "^$(printf '%s' "$LAYERX_NODE_TREASURY_DID" | od -An -v -tx1 | tr -d ' \n'):$LAYERX_NODE_TREASURY_PUBLIC_KEY:0$" "$DATA/identities.txt" \
    || fail "treasury identity not registered"
[ "$LAYERX_NODE_GENESIS_GUARANTOR_COUNT" -eq "$(jq -r '.finality_policy.certificate_threshold' "$ROOT/contracts/config/checkpoint-settlement.json")" ] || fail "genesis guarantor count mismatch"
FIRST_MANIFEST_INODE=$(stat -c %i "$DATA/genesis/genesis.manifest")
exec {FIRST_MANIFEST_FD}<"$DATA/genesis/genesis.manifest"

log "LNI handshake as uid $CLIENT_UID"
HANDSHAKE=$(as_client "$WORK/probe" handshake --socket "$LAYERX_NODE_LNI_SOCKET" --network-id "$NETWORK_ID")
log "$HANDSHAKE"
expect_contains "$HANDSHAKE" "\"network_id\":$NETWORK_ID"
expect_contains "$HANDSHAKE" '"role":"Sequencer"'
expect_contains "$HANDSHAKE" "\"sequencer_public_key\":\"$LAYERX_NODE_SEQUENCER_PUBLIC_KEY\""
expect_contains "$HANDSHAKE" 'AccountRead'

log "sequencer seed reaches only the daemon environment"
SEQUENCER_SEED_HEX=$(od -An -v -tx1 "$WORK/sequencer.key" | tr -d ' \n')
DAEMON_PID=$(pgrep -f "$LAYERXD --serve $DATA/" | head -n 1)
[ -n "$DAEMON_PID" ] || fail "layerxd --serve pid not found"
if grep -q '^LAYERX_NODE_SEQUENCER_PRIVATE_KEY=' "$DATA/sequencer.env"; then
    fail "sequencer.env carries LAYERX_NODE_SEQUENCER_PRIVATE_KEY"
fi
[ "$(sed -n 's/^LAYERX_NODE_SEQUENCER_KEY_FILE=//p' "$DATA/sequencer.env")" = "$WORK/sequencer.key" ] \
    || fail "sequencer.env does not name the sequencer key file"
COPIES=$(grep -rlD skip --exclude=sequencer.key -- "$SEQUENCER_SEED_HEX" "$WORK" || true)
[ -z "$COPIES" ] || fail "the sequencer seed was copied outside its key file: $COPIES"
tr '\0' '\n' < "/proc/$DAEMON_PID/environ" | grep -qx "LAYERX_NODE_SEQUENCER_PRIVATE_KEY=$SEQUENCER_SEED_HEX" \
    || fail "the layerxd --serve environment does not carry the sequencer seed"
if tr '\0' '\n' < "/proc/$DAEMON_PID/cmdline" | grep -q -- "$SEQUENCER_SEED_HEX"; then
    fail "the sequencer seed appears on the layerxd command line"
fi
if tr '\0' '\n' < "/proc/$SEQUENCER_PID/environ" | grep -q '^LAYERX_NODE_SEQUENCER_PRIVATE_KEY='; then
    fail "the sequencer supervisor environment carries the sequencer seed"
fi

log "settlement inputs name the precompiles on chain $PAXEER_CHAIN_ID through $SETTLEMENT_RPC"
settlement_line() { sed -n "s/^$1=//p" "$DATA/sequencer.env" | tail -n 1; }
[ "$(settlement_line LAYERX_NODE_PAXEER_CHAIN_ID)" = "$PAXEER_CHAIN_ID" ] || fail "sequencer.env chain id mismatch"
[ "$(settlement_line LAYERX_NODE_PAXEER_RPC_URL)" = "$SETTLEMENT_RPC" ] || fail "sequencer.env does not name the loopback JSON-RPC $SETTLEMENT_RPC"
[ "$(settlement_line LAYERX_NODE_REGISTRY_PRECOMPILE)" = "$REGISTRY_PRECOMPILE" ] || fail "sequencer.env registry precompile mismatch"
[ "$(settlement_line LAYERX_NODE_CUSTODY_PRECOMPILE)" = "$CUSTODY_PRECOMPILE" ] || fail "sequencer.env custody precompile mismatch"
[ "$(settlement_line LAYERX_NODE_ANCHOR_PRECOMPILE)" = "$ANCHOR_PRECOMPILE" ] || fail "sequencer.env anchor precompile mismatch"
[ "$(settlement_line LAYERX_NODE_SETTLEMENT_CONTRACT)" = "$ANCHOR_PRECOMPILE" ] || fail "the settlement contract pin is not the anchor precompile"
[ "$(settlement_line LAYERX_NODE_CHECKPOINT_REGISTRY)" = "$ANCHOR_PRECOMPILE" ] || fail "the checkpoint registry pin is not the anchor precompile"
[ "$(settlement_line LAYERX_NODE_PAXEER_RPC_ADDRESS)" = 127.0.0.1 ] || fail "the JSON-RPC address pin is not loopback"
[ "$(settlement_line LAYERX_NODE_PAXEER_RPC_PORT)" = "${SETTLEMENT_RPC##*:}" ] || fail "the JSON-RPC port pin is not the port of $SETTLEMENT_RPC"
if grep -q '^LAYERX_NODE_SETTLEMENT_ENV=' "$DATA/sequencer.env"; then
    fail "sequencer.env defers the settlement binding to a file"
fi
for pin in "LAYERX_NODE_ANCHOR_PRECOMPILE=$ANCHOR_PRECOMPILE" "LAYERX_NODE_SETTLEMENT_CONTRACT=$ANCHOR_PRECOMPILE" \
        "LAYERX_NODE_PAXEER_RPC_URL=$SETTLEMENT_RPC" "LAYERX_NODE_PAXEER_RPC_PORT=${SETTLEMENT_RPC##*:}"; do
    tr '\0' '\n' < "/proc/$DAEMON_PID/environ" | grep -qx "$pin" || fail "the layerxd --serve environment does not carry $pin"
done

log "operator state read over the real LNI"
OPERATOR_STATE=$(as_client "$WORK/layerxctl" read-state --socket "$LAYERX_NODE_LNI_SOCKET" \
    --network-id "$NETWORK_ID" --protocol-version 3 --actor "$LAYERX_NODE_TREASURY_DID")
expect_contains "$OPERATOR_STATE" "\"network_id\":$NETWORK_ID"
expect_contains "$OPERATOR_STATE" '"global_sequence":0'
expect_contains "$OPERATOR_STATE" '"evidence":"authenticated_node_snapshot"'

log "treasury balance read: the main account opens on its first credit, so a fresh genesis refuses the read"
expect_treasury_unopened

log "the treasury submits its first custody credit, which opens and funds its main account"
CREDIT=$(as_client "$WORK/layerxctl" submit --socket "$LAYERX_NODE_LNI_SOCKET" \
    --network-id "$NETWORK_ID" --protocol-version 3 --actor "$LAYERX_NODE_TREASURY_DID" \
    --public-key "$LAYERX_NODE_TREASURY_PUBLIC_KEY" --activity "$WORK/custody/custody.activity")
log "$CREDIT"
expect_contains "$CREDIT" '"state":"acknowledged"'
CREDIT_DEADLINE=$(( $(date +%s) + 60 ))
BALANCE=""
while :; do
    HANDSHAKE=$(as_client "$WORK/probe" handshake --socket "$LAYERX_NODE_LNI_SOCKET" --network-id "$NETWORK_ID")
    case "$HANDSHAKE" in *'"latest_sealed_batch":0,'*) ;; *)
        BALANCE=$(as_client "$WORK/probe" balance --socket "$LAYERX_NODE_LNI_SOCKET" --network-id "$NETWORK_ID" \
            --account "$LAYERX_NODE_TREASURY_ACCOUNT" --asset "$LAYERX_NODE_ASSET_ID")
        case "$BALANCE" in *"\"balance\":\"$CREDIT_AMOUNT\""*) break ;; esac ;;
    esac
    [ "$(date +%s)" -lt "$CREDIT_DEADLINE" ] || fail "the credited treasury balance never appeared: ${BALANCE:-$HANDSHAKE}"
    sleep 0.5
done
log "$BALANCE"
expect_contains "$BALANCE" "\"account\":\"$LAYERX_NODE_TREASURY_ACCOUNT\""
expect_contains "$BALANCE" "\"asset\":\"$LAYERX_NODE_ASSET_ID\""
ACCOUNTS=$(as_client "$WORK/probe" did-accounts --socket "$LAYERX_NODE_LNI_SOCKET" --network-id "$NETWORK_ID" \
    --did "$LAYERX_NODE_TREASURY_DID")
log "$ACCOUNTS"
expect_contains "$ACCOUNTS" "\"did\":\"$LAYERX_NODE_TREASURY_DID\",\"count\":1,\"accounts\":[\"$LAYERX_NODE_TREASURY_ACCOUNT\"]"

log "operator submits a real signed SEND once and preserves its idempotency key"
chown "$CLIENT_UID:$CLIENT_GID" "$WORK/treasury.key"
mkdir "$WORK/operator"
chown "$CLIENT_UID:$CLIENT_GID" "$WORK/operator"
ACTIVITY_ID=$(as_client "$WORK/probe" write-send --socket "$LAYERX_NODE_LNI_SOCKET" \
    --network-id "$NETWORK_ID" --seed-file "$WORK/treasury.key" \
    --destination-did "did:layerx:$LAYERX_NODE_SEQUENCER_PUBLIC_KEY" \
    --asset "$LAYERX_NODE_ASSET_ID" --output "$WORK/operator/send.bin")
ADMISSION=$(as_client "$WORK/layerxctl" submit --socket "$LAYERX_NODE_LNI_SOCKET" \
    --network-id "$NETWORK_ID" --protocol-version 3 --actor "$LAYERX_NODE_TREASURY_DID" \
    --public-key "$LAYERX_NODE_TREASURY_PUBLIC_KEY" --activity "$WORK/operator/send.bin")
expect_contains "$ADMISSION" '"state":"acknowledged"'
expect_contains "$ADMISSION" "\"activity_id\":\"$ACTIVITY_ID\""
SEAL_DEADLINE=$(( $(date +%s) + 60 ))
until as_client "$WORK/layerxctl" read-state --socket "$LAYERX_NODE_LNI_SOCKET" \
    --network-id "$NETWORK_ID" --protocol-version 3 --actor "$LAYERX_NODE_TREASURY_DID" >/dev/null 2>&1; do
    [ "$(date +%s)" -lt "$SEAL_DEADLINE" ] || fail "the admitted SEND never sealed: the preparation read stays refused with -903 LXP_ERR_PROJECTION_STALE"
    sleep 0.5
done
REPEATED_ADMISSION=$(as_client "$WORK/layerxctl" submit --socket "$LAYERX_NODE_LNI_SOCKET" \
    --network-id "$NETWORK_ID" --protocol-version 3 --actor "$LAYERX_NODE_TREASURY_DID" \
    --public-key "$LAYERX_NODE_TREASURY_PUBLIC_KEY" --activity "$WORK/operator/send.bin")
[ "$ADMISSION" = "$REPEATED_ADMISSION" ] || fail "repeated canonical submission changed identity"

log "supervisor status"
STATUS=$(as_client "$WORK/probe" supervisor --socket "$LAYERX_NODE_SUPERVISOR_SOCKET" --request status)
log "$STATUS"
expect_contains "$STATUS" '"state":"running","generation":1'
[ "$(stat -c %s "$DATA/checkpoints/.layerxd-lni-admission.log")" -gt 32 ] || fail "generation 1 admitted nothing into its admission journal"
FIRST_CHECKPOINTS_INODE=$(stat -c %i "$DATA/checkpoints")
exec {FIRST_CHECKPOINTS_FD}<"$DATA/checkpoints"

log "supervisor reset"
RESET=$(as_client "$WORK/probe" supervisor --socket "$LAYERX_NODE_SUPERVISOR_SOCKET" --request reset)
log "$RESET"
expect_contains "$RESET" '"state":"reset","reset_id":"'
wait_for "$RUN/layerxd.lni.sock" 60
set -a
# shellcheck disable=SC1091
. "$DATA/node.env"
set +a
[ "$(stat -c %i "$DATA/genesis/genesis.manifest")" != "$FIRST_MANIFEST_INODE" ] || fail "genesis was not rebuilt by the reset"
exec {FIRST_MANIFEST_FD}<&-
[ "$(stat -c %i "$DATA/checkpoints")" != "$FIRST_CHECKPOINTS_INODE" ] || fail "checkpoint directory was not discarded by the reset"
exec {FIRST_CHECKPOINTS_FD}<&-
[ "$(stat -c %s "$DATA/checkpoints/.layerxd-lni-admission.log")" = 32 ] || fail "generation 1 admissions survived the reset in the admission journal"
STATUS=$(as_client "$WORK/probe" supervisor --socket "$LAYERX_NODE_SUPERVISOR_SOCKET" --request status)
expect_contains "$STATUS" '"state":"running","generation":2'
HANDSHAKE=$(as_client "$WORK/probe" handshake --socket "$LAYERX_NODE_LNI_SOCKET" --network-id "$NETWORK_ID")
expect_contains "$HANDSHAKE" "\"network_id\":$NETWORK_ID"
expect_treasury_unopened

log "stopping"
kill -TERM "$SEQUENCER_PID" "$REPLICA_PID"
wait "$SEQUENCER_PID" || true
wait "$REPLICA_PID" || true
SEQUENCER_PID=""
REPLICA_PID=""
if pgrep -f "$LAYERXD --serve $DATA/" >/dev/null || pgrep -f "$LAYERXD --authority-replica $DATA/" >/dev/null; then
    fail "layerxd processes survived the supervisors"
fi
[ ! -e "$RUN/supervisor.sock" ] || fail "supervisor socket was not removed"
KEEP=0
log "PASS"
