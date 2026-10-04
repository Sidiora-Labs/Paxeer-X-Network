#!/usr/bin/env bash
# LayerX beta node supervisor.
#
# One supervisor runs per daemon container and both share the run directory:
#
#   supervisor.sh --role sequencer --data-dir DIR --run-dir DIR [--layerxd P] \
#       -- <bootstrap.sh arguments except --data-dir and --run-dir>
#   supervisor.sh --role replica --data-dir DIR --run-dir DIR [--layerxd P]
#
# The sequencer supervisor bootstraps the data directory on first start (or
# reuses it when node.env is present), publishes the generation number to the
# run directory, waits for the replica supervisor to report its daemon up for
# that generation, starts `layerxd --serve`, and answers requests on the
# pod-local unix socket RUN_DIR/supervisor.sock:
#
# Versioned requests use reset_state.py to retain caller-bound reset identities
# and original outcomes on the persistent volume. Legacy reset/status lines
# remain accepted. Bootstrapping or activation interrupted without a durable
# outcome blocks recovery instead of repeating destructive work.
#
# The replica supervisor starts `layerxd --authority-replica` for every
# generation the sequencer supervisor publishes, restarts it against the new
# generation whenever the published generation changes while it is running,
# and stops it when the sequencer supervisor asks, via files in the run
# directory:
#
#   generation                 current generation, written by the sequencer side
#   replica-ready.<gen>        replica daemon running for <gen>
#   reset.<id>.stop-replica    sequencer side asks the replica side to stop
#   reset.<id>.replica-stopped replica side has stopped its daemon
#
# When the bootstrap arguments carry --treasury-signer-socket PATH the
# sequencer supervisor waits for PATH (logging every 30 seconds, up to
# LAYERX_NODE_TREASURY_SIGNER_WAIT_SECONDS, default 600) before it bootstraps,
# because the treasury signer owns the key the bootstrap binds, and publishes
# PATH to the admin plane as LAYERX_CORE_TREASURY_SIGNER_SOCKET in core.env.
#
# When bootstrap.sh ran with --settlement-env FILE, sequencer.env names FILE
# as LAYERX_NODE_SETTLEMENT_ENV; the sequencer supervisor waits for FILE
# (logging every 30 seconds, up to LAYERX_NODE_SETTLEMENT_WAIT_SECONDS,
# default 3600), validates it with bootstrap.sh --check-settlement and exports
# its five values to `layerxd --serve` before starting it.
#
# Environment files are never sourced: every KEY=VALUE line is validated and
# exported one at a time, and a LAYERX_NODE_SEQUENCER_PRIVATE_KEY line is
# refused. sequencer.env names the sequencer seed file as
# LAYERX_NODE_SEQUENCER_KEY_FILE (the bootstrap --sequencer-key path). Before a
# generation is published the sequencer supervisor checks that the file's
# public key equals LAYERX_NODE_SEQUENCER_PUBLIC_KEY; when it starts
# `layerxd --serve` it reads the seed through a read-only descriptor it opens
# itself and exports LAYERX_NODE_SEQUENCER_PRIVATE_KEY only into the
# environment of the daemon process it execs. The seed is never copied into
# the data directory and never appears on a command line.
#
# A daemon that exits on its own ends the supervisor with status 1 so the pod
# restarts it against the retained data directory.
set -euo pipefail

log() { printf 'supervisor[%s]: %s\n' "${ROLE:-handler}" "$*" >&2; }
fail() { log "$*"; exit 1; }

ROLE=""
DATA_DIR=""
RUN_DIR=""
STATE_DIR=""
LAYERXD=""
SOCAT=""
BOOTSTRAP_ARGS=()
SUPERVISOR_ARGS=("$@")
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)

while [ $# -gt 0 ]; do
    case "$1" in
        --role) ROLE=$2; shift 2 ;;
        --data-dir) DATA_DIR=$2; shift 2 ;;
        --run-dir) RUN_DIR=$2; shift 2 ;;
        --state-dir) STATE_DIR=$2; shift 2 ;;
        --layerxd) LAYERXD=$2; shift 2 ;;
        --socat) SOCAT=$2; shift 2 ;;
        --) shift; BOOTSTRAP_ARGS=("$@"); break ;;
        *) fail "unknown argument $1" ;;
    esac
done

[ -n "$DATA_DIR" ] || fail "--data-dir is required"
[ -n "$RUN_DIR" ] || fail "--run-dir is required"
mkdir -p "$DATA_DIR" "$RUN_DIR"
DATA_DIR=$(readlink -f "$DATA_DIR")
RUN_DIR=$(readlink -f "$RUN_DIR")
STATE_DIR=${STATE_DIR:-$(dirname "$DATA_DIR")/supervisor-state}
RESET_HELPER="$SCRIPT_DIR/reset_state.py"
SUPERVISOR_SOCKET="$RUN_DIR/supervisor.sock"
PID_FILE="$RUN_DIR/supervisor.pid"
GENERATION_FILE="$RUN_DIR/generation"

[ "$ROLE" = sequencer ] || [ "$ROLE" = replica ] || fail "--role must be sequencer or replica"

if [ "$ROLE" = sequencer ]; then
    if [ -z "${LAYERX_RESET_OWNER_FD:-}" ]; then
        exec python3 "$RESET_HELPER" own-supervisor --state-dir "$STATE_DIR" \
            --data-dir "$DATA_DIR" --run-dir "$RUN_DIR" -- bash "$0" "${SUPERVISOR_ARGS[@]}"
    fi
    python3 "$RESET_HELPER" assert-owner --state-dir "$STATE_DIR" \
        --data-dir "$DATA_DIR" --run-dir "$RUN_DIR" || fail "durable supervisor ownership refused"
fi

resolve_binary() {
    local given=$1 name=$2 candidate
    if [ -n "$given" ]; then
        [ -x "$given" ] || fail "$name is not executable: $given"
        printf '%s' "$given"
        return
    fi
    for candidate in "$PWD/build/bin/$name" "/usr/local/bin/$name"; do
        if [ -x "$candidate" ]; then printf '%s' "$candidate"; return; fi
    done
    command -v "$name" || fail "$name not found"
}
LAYERXD=$(resolve_binary "$LAYERXD" layerxd)

DAEMON_PID=""
SOCAT=${SOCAT:-$(command -v socat || true)}
[ -n "$SOCAT" ] && [ -x "$SOCAT" ] || fail "socat is required for daemon readiness and the supervisor socket"
if [ "$ROLE" = sequencer ]; then
    command -v openssl >/dev/null || fail "openssl is required to bind the sequencer seed"
    command -v od >/dev/null || fail "od is required to bind the sequencer seed"
fi

bin_to_hex() { od -An -v -tx1 | tr -d ' \n'; }

hex_to_bin() {
    local hex=$1 i
    for ((i = 0; i < ${#hex}; i += 2)); do
        printf "\\$(printf '%03o' "0x${hex:i:2}")"
    done
}

public_key_hex() {
    # ed25519 public key from a 32-byte seed via the PKCS#8 wrapper openssl reads.
    { hex_to_bin "302e020100300506032b657004220420"; hex_to_bin "$1"; } \
        | openssl pkey -inform DER -pubout -outform DER | tail -c 32 | bin_to_hex
}

load_environment() {
    # load_environment NAME < lines -> exports every validated KEY=VALUE line
    local name=$1 line key
    while IFS= read -r line || [ -n "$line" ]; do
        [ -n "$line" ] || continue
        [[ $line =~ ^(LAYERX_[A-Z0-9_]+)=([^[:cntrl:]]*)$ ]] \
            || fail "$name carries a line that is not a LAYERX_* KEY=VALUE pair: ${line%%=*}"
        key=${BASH_REMATCH[1]}
        [ "$key" != LAYERX_NODE_SEQUENCER_PRIVATE_KEY ] \
            || fail "$name must not carry LAYERX_NODE_SEQUENCER_PRIVATE_KEY; the supervisor delivers the seed from LAYERX_NODE_SEQUENCER_KEY_FILE"
        export "$line"
    done
}

SEQUENCER_SEED=""

sequencer_seed_hex() {
    # sequencer_seed_hex FILE -> SEQUENCER_SEED holds the 64 hex character seed
    # read through a private read-only descriptor; FILE holds 32 raw bytes or
    # 64 hex characters, the same forms bootstrap.sh --sequencer-key accepts.
    local file=$1 size descriptor
    SEQUENCER_SEED=""
    [[ $file = /* ]] || fail "LAYERX_NODE_SEQUENCER_KEY_FILE must be an absolute path"
    [ -f "$file" ] || fail "sequencer key file is not a regular file: $file"
    [ -r "$file" ] || fail "sequencer key file is not readable: $file"
    size=$(stat -L -c %s "$file")
    [ "$size" -le 128 ] || fail "sequencer key file must hold 32 raw bytes or 64 hex characters: $file"
    exec {descriptor}<"$file" || fail "sequencer key file could not be opened: $file"
    if [ "$size" -eq 32 ]; then
        SEQUENCER_SEED=$(bin_to_hex <&"$descriptor")
    else
        SEQUENCER_SEED=$(tr -d ' \t\r\n' <&"$descriptor" | tr 'A-F' 'a-f')
    fi
    exec {descriptor}<&-
    [[ $SEQUENCER_SEED =~ ^[0-9a-f]{64}$ ]] \
        || fail "sequencer key file must hold 32 raw bytes or 64 hex characters: $file"
}

check_sequencer_environment() {
    # check_sequencer_environment ENV_FILE -> refuses a seed carried in the
    # file and binds the named key file to the published sequencer public key
    local env_file=$1 key_file public_key derived
    [ -r "$env_file" ] || fail "environment file missing: $env_file"
    if grep -q '^LAYERX_NODE_SEQUENCER_PRIVATE_KEY=' "$env_file"; then
        fail "$env_file must not carry LAYERX_NODE_SEQUENCER_PRIVATE_KEY; the supervisor delivers the seed from LAYERX_NODE_SEQUENCER_KEY_FILE"
    fi
    key_file=$(sed -n 's/^LAYERX_NODE_SEQUENCER_KEY_FILE=//p' "$env_file" | tail -n 1)
    [ -n "$key_file" ] || fail "LAYERX_NODE_SEQUENCER_KEY_FILE missing from $env_file"
    public_key=$(sed -n 's/^LAYERX_NODE_SEQUENCER_PUBLIC_KEY=//p' "$env_file" | tail -n 1)
    [[ $public_key =~ ^[0-9a-f]{64}$ ]] || fail "LAYERX_NODE_SEQUENCER_PUBLIC_KEY missing from $env_file"
    sequencer_seed_hex "$key_file"
    derived=$(public_key_hex "$SEQUENCER_SEED")
    SEQUENCER_SEED=""
    wait_for_settlement "$env_file"
    (
        local verifier
        local -a arguments
        load_environment "$env_file" < "$env_file"
        if [ -n "${SETTLEMENT_LINES:-}" ]; then
            load_environment "the settlement environment" <<< "$SETTLEMENT_LINES"
        fi
        verifier=$(resolve_binary "" layerx-handover)
        arguments=(--verify-key "${LAYERX_NODE_GENESIS_MANIFEST:?}" \
            "${LAYERX_NODE_CHECKPOINT_DIRECTORY:?}/da-bodies.log" "$derived")
        if [ -n "${LAYERX_NODE_HANDOVER_ACTIVITY:-}" ]; then
            arguments+=("$LAYERX_NODE_HANDOVER_ACTIVITY")
        fi
        "$verifier" "${arguments[@]}"
    ) || fail "the sequencer key file does not match the bound sequencer public key or an authorized key in finalized handover history"
    log "sequencer seed bound from $key_file"
}

publish_core_environment() {
    local sequencer_id asset_id lni_gid temporary signer_socket
    sequencer_id=$(sed -n 's/^LAYERX_NODE_SEQUENCER_ID=//p' "$DATA_DIR/node.env")
    asset_id=$(sed -n 's/^LAYERX_NODE_ASSET_ID=//p' "$DATA_DIR/node.env")
    [[ $sequencer_id =~ ^[0-9a-f]{64}$ ]] || fail "invalid generated sequencer identity"
    [[ $asset_id =~ ^[0-9a-f]{64}$ ]] || fail "invalid generated treasury asset"
    lni_gid=$(sed -n 's/^LAYERX_NODE_LNI_ALLOWED_GID=//p' "$DATA_DIR/sequencer.env")
    [[ $lni_gid =~ ^[0-9]+$ ]] || fail "invalid generated LNI group"
    signer_socket=$(sed -n 's/^LAYERX_NODE_TREASURY_SIGNER_SOCKET=//p' "$DATA_DIR/node.env" | tail -n 1)
    chgrp "$lni_gid" "$RUN_DIR"
    chmod 0750 "$RUN_DIR"
    temporary="$RUN_DIR/core.env.$$"
    printf 'LAYERX_CORE_SEQUENCER_ID=%s\nLAYERX_CORE_TREASURY_ASSET=%s\n' \
        "$sequencer_id" "$asset_id" > "$temporary"
    if [ -n "$signer_socket" ]; then
        [[ $signer_socket = /* ]] || fail "invalid generated treasury signer socket"
        printf 'LAYERX_CORE_TREASURY_SIGNER_SOCKET=%s\n' "$signer_socket" >> "$temporary"
    fi
    chmod 0644 "$temporary"
    mv "$temporary" "$RUN_DIR/core.env"
}

settlement_env_file() {
    sed -n 's/^LAYERX_NODE_SETTLEMENT_ENV=//p' "$1" | tail -n 1
}

wait_for_settlement() {
    # wait_for_settlement ENV_FILE -> the validated settlement lines in SETTLEMENT_LINES
    local env_file=$1 file waited=0 limit
    SETTLEMENT_LINES=""
    file=$(settlement_env_file "$env_file")
    [ -n "$file" ] || return 0
    limit=${LAYERX_NODE_SETTLEMENT_WAIT_SECONDS:-3600}
    [[ $limit =~ ^[0-9]+$ ]] || fail "LAYERX_NODE_SETTLEMENT_WAIT_SECONDS must be decimal"
    while [ ! -e "$file" ]; do
        if [ "$waited" -ge "$limit" ]; then
            fail "settlement environment $file did not appear within ${limit}s"
        fi
        if [ $((waited % 30)) -eq 0 ]; then
            log "waiting for the settlement environment $file (deployed contract addresses)"
        fi
        sleep 1
        waited=$((waited + 1))
    done
    SETTLEMENT_LINES=$("$SCRIPT_DIR/bootstrap.sh" --check-settlement "$file") || fail "settlement environment $file was refused"
    log "settlement environment $file validated"
}

start_daemon() {
    # start_daemon ENV_FILE MODE CONFIG
    local env_file=$1 mode=$2 config=$3
    [ -r "$env_file" ] || fail "environment file missing: $env_file"
    [ -r "$config" ] || fail "configuration missing: $config"
    if [ "$mode" = --serve ]; then
        publish_core_environment
    fi
    wait_for_settlement "$env_file"
    (
        load_environment "$env_file" < "$env_file"
        if [ -n "${SETTLEMENT_LINES:-}" ]; then
            load_environment "the settlement environment" <<< "$SETTLEMENT_LINES"
        fi
        if [ "$mode" = --serve ]; then
            sequencer_seed_hex "${LAYERX_NODE_SEQUENCER_KEY_FILE:-}"
            export LAYERX_NODE_SEQUENCER_PRIVATE_KEY="$SEQUENCER_SEED"
            SEQUENCER_SEED=""
        fi
        if [ "$mode" = --authority-replica ]; then
            export LAYERX_AUTHORITY_STATUS_GENESIS_MANIFEST="$DATA_DIR/genesis/genesis.manifest"
        fi
        exec python3 "$RESET_HELPER" exec-daemon -- "$LAYERXD" "$mode" "$config"
    ) &
    DAEMON_PID=$!
    log "started layerxd $mode pid $DAEMON_PID"
}

stop_daemon() {
    if [ -n "$DAEMON_PID" ] && kill -0 "$DAEMON_PID" 2>/dev/null; then
        kill -TERM "$DAEMON_PID" 2>/dev/null || true
        local waited=0
        while kill -0 "$DAEMON_PID" 2>/dev/null && [ "$waited" -lt 100 ]; do
            sleep 0.1
            waited=$((waited + 1))
        done
        if kill -0 "$DAEMON_PID" 2>/dev/null; then
            kill -KILL "$DAEMON_PID" 2>/dev/null || true
        fi
        wait "$DAEMON_PID" 2>/dev/null || true
        log "stopped layerxd pid $DAEMON_PID"
    fi
    DAEMON_PID=""
}

daemon_alive() { [ -n "$DAEMON_PID" ] && kill -0 "$DAEMON_PID" 2>/dev/null; }

wait_for_file() {
    # wait_for_file PATH SECONDS
    local deadline=$(( $(date +%s) + $2 ))
    while [ ! -e "$1" ]; do
        [ "$(date +%s)" -lt "$deadline" ] || return 1
        sleep 0.2
    done
}

wait_for_daemon_ready() (
    local env_file=$1 mode=$2 seconds=$3 address port bearer path expected response deadline unknown body record page
    load_environment "$env_file" < "$env_file"
    if [ "$mode" = --serve ]; then
        address=$LAYERX_NODE_PROGRAM_ADDRESS
        port=$LAYERX_NODE_PROGRAM_PORT
        bearer=$LAYERX_NODE_PROGRAM_BEARER_TOKEN
        path=/v1/programs/account-state/changes?after_sequence=0
        expected='HTTP/1.1 200 '
    else
        address=$LAYERX_AUTHORITY_ADDRESS
        port=$LAYERX_AUTHORITY_PORT
        bearer=$LAYERX_AUTHORITY_BEARER_TOKEN
        unknown=$(printf '%064d' 0)
        path="/v1/batches/$unknown/receipt-authority?receipt_digest=$unknown"
        expected='HTTP/1.1 404 '
    fi
    [ "$address" = 127.0.0.1 ] || return 1
    record='\{"sequence":[0-9]+,"ordinal":[0-9]+,"program_id":"[0-9a-f]{64}","activity_type":[0-9]+,"event_type":[0-9]+,"receipt_digest":"[0-9a-f]{64}"\}'
    page='^\{"records":\[('
    page+="$record(,$record)*"
    page+=')?\],"complete_through":\{"sequence":[0-9]+,"ordinal":0\},"scanned_through_sequence":[0-9]+,"caught_up":(true|false)\}$'
    deadline=$(( $(date +%s) + seconds ))
    while [ "$(date +%s)" -lt "$deadline" ]; do
        daemon_alive || return 1
        if [ "$mode" = --serve ] && [ ! -S "$LAYERX_NODE_LNI_SOCKET" ]; then
            sleep 0.2
            continue
        fi
        if response=$(printf 'GET %s HTTP/1.1\r\nHost: 127.0.0.1:%s\r\nAuthorization: Bearer %s\r\nConnection: close\r\n\r\n' \
            "$path" "$port" "$bearer" | "$SOCAT" -T 2 - "TCP4:127.0.0.1:$port,connect-timeout=2" 2>/dev/null); then
            if [[ $response == "$expected"* ]] && daemon_alive; then
                if [ "$mode" != --serve ]; then return 0; fi
                body=${response#*$'\r\n\r\n'}
                if [[ $body =~ $page ]]; then return 0; fi
            fi
        fi
        sleep 0.2
    done
    return 1
)

# --- replica role -----------------------------------------------------------
if [ "$ROLE" = replica ]; then
    trap 'stop_daemon; exit 0' TERM INT
    current=""
    while :; do
        stop_request=$(ls "$RUN_DIR"/reset.*.stop-replica 2>/dev/null | head -n 1 || true)
        if [ -n "$stop_request" ]; then
            id=${stop_request##*/reset.}
            id=${id%.stop-replica}
            stop_daemon
            rm -f "$stop_request" "$RUN_DIR/replica-ready.$current"
            : > "$RUN_DIR/reset.$id.replica-stopped"
        fi
        if [ ! -e "$GENERATION_FILE" ]; then
            sleep 0.2
            continue
        fi
        generation=$(cat "$GENERATION_FILE")
        if [ -z "$generation" ] || [ "$generation" = "$current" ]; then
            sleep 0.2
            continue
        fi
        if ! python3 "$RESET_HELPER" replica-generation --state-dir "$STATE_DIR" --generation "$generation" >/dev/null; then
            sleep 0.2
            continue
        fi
        wait_for_file "$DATA_DIR/replica.env" 60 || fail "replica.env missing for generation $generation"
        start_daemon "$DATA_DIR/replica.env" --authority-replica "$DATA_DIR/replica.conf"
        if ! wait_for_daemon_ready "$DATA_DIR/replica.env" --authority-replica 120; then
            stop_daemon
            fail "replica listener did not become ready for generation $generation"
        fi
        current=$generation
        : > "$RUN_DIR/replica-ready.$generation"
        while :; do
            if ! daemon_alive; then
                wait "$DAEMON_PID" && status=0 || status=$?
                fail "layerxd --authority-replica exited with status $status"
            fi
            stop_request=$(ls "$RUN_DIR"/reset.*.stop-replica 2>/dev/null | head -n 1 || true)
            if [ -n "$stop_request" ]; then
                id=${stop_request##*/reset.}
                id=${id%.stop-replica}
                log "reset $id: stopping the replica"
                stop_daemon
                rm -f "$stop_request" "$RUN_DIR/replica-ready.$current"
                : > "$RUN_DIR/reset.$id.replica-stopped"
                break
            fi
            published=$(cat "$GENERATION_FILE" 2>/dev/null || true)
            if [ -n "$published" ] && [ "$published" != "$current" ]; then
                log "generation $current superseded by $published: restarting the replica"
                stop_daemon
                rm -f "$RUN_DIR/replica-ready.$current"
                break
            fi
            if [ ! -e "$RUN_DIR/replica-ready.$current" ]; then
                wait_for_daemon_ready "$DATA_DIR/replica.env" --authority-replica 120 \
                    || fail "replica listener did not reaffirm generation $current"
                : > "$RUN_DIR/replica-ready.$current"
            fi
            sleep 0.2
        done
    done
fi

# --- sequencer role ---------------------------------------------------------
[ -x "$SCRIPT_DIR/bootstrap.sh" ] || fail "bootstrap.sh missing next to supervisor.sh"

trap 'true' USR1
SOCAT_PID=""
OWNS_RUNTIME=0

cleanup() {
    trap - TERM INT EXIT
    stop_daemon
    if [ -n "$SOCAT_PID" ]; then kill "$SOCAT_PID" 2>/dev/null || true; fi
    if [ "$OWNS_RUNTIME" -eq 1 ]; then rm -f "$PID_FILE" "$SUPERVISOR_SOCKET"; fi
}
trap 'cleanup; exit 0' TERM INT
trap cleanup EXIT

treasury_signer_socket() {
    local index
    for ((index = 0; index + 1 < ${#BOOTSTRAP_ARGS[@]}; index++)); do
        if [ "${BOOTSTRAP_ARGS[index]}" = --treasury-signer-socket ]; then
            printf '%s' "${BOOTSTRAP_ARGS[index + 1]}"
            return 0
        fi
    done
}

wait_for_treasury_signer() {
    local socket waited=0 limit
    socket=$(treasury_signer_socket)
    [ -n "$socket" ] || return 0
    limit=${LAYERX_NODE_TREASURY_SIGNER_WAIT_SECONDS:-600}
    [[ $limit =~ ^[0-9]+$ ]] || fail "LAYERX_NODE_TREASURY_SIGNER_WAIT_SECONDS must be decimal"
    while [ ! -S "$socket" ]; do
        if [ "$waited" -ge "$limit" ]; then
            fail "the treasury signer socket $socket did not appear within ${limit}s"
        fi
        if [ $((waited % 30)) -eq 0 ]; then
            log "waiting for the treasury signer socket $socket"
        fi
        sleep 1
        waited=$((waited + 1))
    done
    log "treasury signer socket $socket available"
}

run_bootstrap() {
    wait_for_treasury_signer
    local -a authority=()
    if [ -n "${LAYERX_NODE_HANDOVER_AUTHORITY_PUBLIC_KEY:-}" ]; then
        authority=(--handover-authority "$LAYERX_NODE_HANDOVER_AUTHORITY_PUBLIC_KEY")
    fi
    python3 "$RESET_HELPER" exec-daemon -- "$SCRIPT_DIR/bootstrap.sh" --data-dir "$DATA_DIR" \
        --run-dir "$RUN_DIR" --layerxd "$LAYERXD" "${authority[@]}" "$@"
}

publish_generation() {
    local generation=$1
    rm -f "$RUN_DIR"/replica-ready.* 2>/dev/null || true
    printf '%s' "$generation" > "$GENERATION_FILE.tmp"
    mv "$GENERATION_FILE.tmp" "$GENERATION_FILE"
    wait_for_file "$RUN_DIR/replica-ready.$generation" 120 || fail "replica did not come up for generation $generation"
}

RESET_BINDINGS="$RUN_DIR/reset-bindings.json"
NETWORK_ID=""
for ((index = 0; index + 1 < ${#BOOTSTRAP_ARGS[@]}; index++)); do
    if [ "${BOOTSTRAP_ARGS[index]}" = --network-id ]; then NETWORK_ID=${BOOTSTRAP_ARGS[index + 1]}; fi
done
[[ $NETWORK_ID =~ ^[1-9][0-9]*$ ]] || fail "versioned reset requires the explicit bootstrap network"

write_reset_bindings() {
    python3 - "$SCRIPT_DIR" "$LAYERXD" "$RESET_BINDINGS" \
        "$(resolve_binary '' layerx-genesis-build)" "$(resolve_binary '' layerx-handover)" "${BOOTSTRAP_ARGS[@]}" <<'BINDINGS'
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

node, daemon, output = map(Path, sys.argv[1:4])
args = sys.argv[6:]
if len(args) % 2 or len(args) > 128:
    raise ValueError('bounded option/value bootstrap arguments required')
options = {}
allowed = {'--network-id', '--asset', '--sequencer-key', '--treasury-key',
           '--treasury-signer-socket', '--genesis-metadata', '--handover-authority',
           '--withdrawal-fee', '--module-fees', '--treasury-balance', '--program-port',
           '--replica-port', '--lni-uid', '--lni-gid', '--program-token-file',
           '--replica-token-file', '--replica-id', '--genesis-timestamp-ms',
           '--enable-module', '--migrations', '--genesis-build', '--custody-profile', '--custody-registry',
           '--settlement-env', '--settlement-document'}
for option, value in zip(args[::2], args[1::2]):
    if (option not in allowed or not value or len(value) > 4096
            or any(ord(character) < 32 or ord(character) == 127 for character in value)):
        raise ValueError('invalid bootstrap option')
    if option in options and option != '--enable-module':
        raise ValueError('duplicate bootstrap option')
    options.setdefault(option, []).append(value)
public_files = ['--genesis-metadata', '--module-fees', '--migrations',
                '--settlement-document', '--custody-profile', '--custody-registry', '--settlement-env']
files = [node / name for name in ('bootstrap.sh', 'genesis_fees.py', 'genesis-modules.conf')]
files += [daemon, Path(sys.argv[4]), Path(sys.argv[5])]
for option in public_files:
    files += [Path(value) for value in options.get(option, [])]
if '--settlement-document' not in options:
    configured = os.environ.get('LAYERX_PAXEER_SETTLEMENT_JSON')
    local = (node / '../../../contracts/config/checkpoint-settlement.json').resolve()
    files += [Path(configured) if configured else local if local.is_file() else Path('/opt/layerx/checkpoint-settlement.json')]
if '--migrations' not in options:
    local = Path.cwd() / 'migrations/0007_history_index.sql'
    files += [local if local.is_file() else Path('/opt/layerx/migrations/0007_history_index.sql')]
files += [Path(options['--genesis-build'][0])] if '--genesis-build' in options else []
identities = {}
for option in ('--sequencer-key', '--treasury-key'):
    if option not in options:
        continue
    key_path = Path(options[option][0])
    with key_path.open('rb') as handle:
        seed = handle.read(129)
    if len(seed) != 32:
        try:
            seed = bytes.fromhex(seed.decode().strip())
        except (ValueError, UnicodeError):
            raise ValueError('invalid signing identity') from None
    if len(seed) != 32:
        raise ValueError('invalid signing identity')
    encoded = subprocess.run(['openssl', 'pkey', '-inform', 'DER', '-pubout', '-outform', 'DER'],
                             input=bytes.fromhex('302e020100300506032b657004220420') + seed,
                             stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=True).stdout
    if len(encoded) != 44:
        raise ValueError('invalid public identity')
    identities[option] = encoded[-32:].hex()
if '--treasury-signer-socket' in options:
    public = subprocess.check_output([sys.executable, str(node / 'signer/client.py'), '--socket',
                                      options['--treasury-signer-socket'][0], 'public-key'], timeout=10).decode().strip()
    if len(public) != 64 or any(c not in '0123456789abcdef' for c in public):
        raise ValueError('invalid signer identity')
    identities['--treasury-signer-socket'] = public
fingerprints = {}
for path in files:
    if not path.exists() and str(path) in options.get('--genesis-metadata', []):
        fingerprints[str(path.absolute())] = None
        continue
    digest = hashlib.sha256()
    with path.open('rb') as handle:
        for block in iter(lambda: handle.read(1048576), b''):
            digest.update(block)
    fingerprints[str(path.resolve())] = digest.hexdigest()
public_environment = {}
for key in ('LAYERX_NODE_HANDOVER_AUTHORITY_PUBLIC_KEY', 'LAYERX_NODE_PAXEER_CHAIN_ID',
            'LAYERX_NODE_PAXEER_RPC_URL', 'LAYERX_NODE_PAXEER_RPC_ADDRESS',
            'LAYERX_NODE_PAXEER_RPC_PORT', 'LAYERX_NODE_REGISTRY_PRECOMPILE',
            'LAYERX_NODE_CUSTODY_PRECOMPILE', 'LAYERX_NODE_ANCHOR_PRECOMPILE',
            'LAYERX_NODE_SETTLEMENT_CONTRACT', 'LAYERX_NODE_CHECKPOINT_REGISTRY'):
    if key in os.environ:
        value = os.environ[key]
        if len(value) > 256 or '@' in value or any(ord(c) < 32 or ord(c) == 127 for c in value):
            raise ValueError('invalid public bootstrap binding')
        if key == 'LAYERX_NODE_PAXEER_RPC_URL':
            from urllib.parse import urlsplit
            parsed = urlsplit(value)
            if (parsed.scheme != 'http' or parsed.hostname != '127.0.0.1'
                    or parsed.port is None or not 1 <= parsed.port <= 65535
                    or parsed.username is not None or parsed.password is not None
                    or parsed.path not in ('', '/') or parsed.query or parsed.fragment):
                raise ValueError('invalid public bootstrap RPC binding')
        elif key == 'LAYERX_NODE_PAXEER_RPC_ADDRESS':
            if value != '127.0.0.1':
                raise ValueError('invalid public bootstrap RPC address')
        elif key in ('LAYERX_NODE_PAXEER_CHAIN_ID', 'LAYERX_NODE_PAXEER_RPC_PORT'):
            if not value.isascii() or not value.isdecimal():
                raise ValueError('invalid public numeric binding')
        else:
            encoded = value.removeprefix('0x')
            required = 64 if key == 'LAYERX_NODE_HANDOVER_AUTHORITY_PUBLIC_KEY' else 40
            if len(encoded) != required or any(c not in '0123456789abcdefABCDEF' for c in encoded):
                raise ValueError('invalid public authority binding')
        public_environment[key] = value
value = {'arguments': options, 'files': fingerprints, 'public_keys': identities,
         'public_environment': public_environment}
temporary = output.with_name(output.name + '.tmp.' + str(os.getpid()))
fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, 'w') as handle:
    json.dump(value, handle, sort_keys=True, separators=(',', ':'))
    handle.flush()
    os.fsync(handle.fileno())
os.replace(temporary, output)
BINDINGS
}

reset_state() {
    local result
    if ! result=$(python3 "$RESET_HELPER" "$@" --state-dir "$STATE_DIR" --data-dir "$DATA_DIR" \
        --run-dir "$RUN_DIR" --network "$NETWORK_ID" --bindings "$RESET_BINDINGS" \
        --initial-generation "${INITIAL_GENERATION:-1}" --allowed-gid "${LNI_GID:-$(id -g)}"); then
        log "durable reset state refused: $result"
        return 1
    fi
    printf '%s\n' "$result"
}

record_field() { python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"; }

genesis_binding() {
    python3 - "$DATA_DIR" "${1:-}" <<'GENESIS'
import hashlib
import json
import os
from pathlib import Path
import stat
import sys
root = Path(sys.argv[1])
names = ('genesis/genesis.manifest', 'genesis/genesis-request.lxgb',
         'genesis/genesis.registration', 'genesis/00000000000000000000.lxs',
         'genesis/paxeer-registration-request.lxrr', 'genesis/paxeer-deployment-descriptor.lxgd')
result = {}
for name in names:
    path = root / name
    if path.is_symlink() or not path.is_file():
        raise ValueError('missing canonical genesis artifact')
    result[name] = hashlib.sha256(path.read_bytes()).hexdigest()
if sys.argv[2]:
    if result != json.loads(sys.argv[2])['genesis']:
        raise ValueError('retained canonical genesis differs from durable operation')
else:
    for directory, dirs, files in os.walk(root, followlinks=False):
        for name in files:
            path = Path(directory) / name
            if path.is_symlink() or not stat.S_ISREG(path.stat().st_mode):
                raise ValueError('unexpected bootstrap output')
            fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
            try: os.fsync(fd)
            finally: os.close(fd)
        fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try: os.fsync(fd)
        finally: os.close(fd)
print(json.dumps({'genesis': result}, sort_keys=True, separators=(',', ':')))
GENESIS
}

perform_durable_reset() {
    local record=$1 phase id timestamp target extra
    id=$(record_field reset_id <<< "$record")
    phase=$(record_field phase <<< "$record")
    timestamp=$(record_field genesis_timestamp_ms <<< "$record")
    target=$(record_field generation <<< "$record")
    write_reset_bindings || fail "reset producer inputs unavailable"
    if [ "$phase" = admitted ]; then
        reset_state phase --reset-id "$id" --expected-phase admitted --new-phase stopping >/dev/null
        phase=stopping
    fi
    if [ "$phase" = stopping ]; then
        stop_daemon
        : > "$RUN_DIR/reset.$id.stop-replica"
        wait_for_file "$RUN_DIR/reset.$id.replica-stopped" 120 || fail "replica stop is ambiguous"
        rm -f "$RUN_DIR/reset.$id.replica-stopped" "$RUN_DIR/reset.$id.stop-replica"
        reset_state phase --reset-id "$id" --expected-phase stopping --new-phase bootstrapping >/dev/null
        local -a frozen=()
        local index
        for ((index = 0; index < ${#BOOTSTRAP_ARGS[@]}; index += 2)); do
            [ "${BOOTSTRAP_ARGS[index]}" = --genesis-timestamp-ms ] && continue
            frozen+=("${BOOTSTRAP_ARGS[index]}" "${BOOTSTRAP_ARGS[index + 1]}")
        done
        run_bootstrap --force "${frozen[@]}" --genesis-timestamp-ms "$timestamp" \
            || fail "bootstrap interrupted; durable operation requires reconciliation"
        check_sequencer_environment "$DATA_DIR/sequencer.env"
        extra=$(genesis_binding)
        record=$(reset_state phase --reset-id "$id" --expected-phase bootstrapping --new-phase prepared --extra-json "$extra")
        phase=prepared
    fi
    if [ "$phase" = prepared ]; then
        genesis_binding "$record" >/dev/null
        reset_state phase --reset-id "$id" --expected-phase prepared --new-phase activating >/dev/null
        GENERATION=$target
        publish_generation "$GENERATION"
        start_daemon "$DATA_DIR/sequencer.env" --serve "$DATA_DIR/sequencer.conf"
        wait_for_daemon_ready "$DATA_DIR/sequencer.env" --serve 120 || fail "activation outcome is ambiguous"
        reset_state complete --reset-id "$id" --expected-phase activating >/dev/null
        log "reset $id: durably complete at generation $GENERATION"
    else
        fail "reset $id requires explicit reconciliation: $phase"
    fi
}

if [ -r "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
    fail "another sequencer supervisor still owns the run directory"
fi
OWNS_RUNTIME=1
INITIAL_GENERATION=1
if [ -r "$GENERATION_FILE" ]; then
    INITIAL_GENERATION=$(cat "$GENERATION_FILE")
    [[ $INITIAL_GENERATION =~ ^[1-9][0-9]*$ ]] || fail "invalid retained generation"
fi
wait_for_treasury_signer
write_reset_bindings
reset_state initialize >/dev/null
record=$(reset_state active)
GENERATION=$(reset_state generation)
if [ "$record" != null ]; then
    perform_durable_reset "$record"
else
    completed=$(reset_state completed)
    if [ "$completed" != null ]; then
        [ -r "$DATA_DIR/node.env" ] || fail "completed reset generation is missing; refusing bootstrap"
        genesis_binding "$completed" >/dev/null || fail "completed reset genesis is inconsistent"
    fi
    if [ ! -r "$DATA_DIR/node.env" ]; then
        log "bootstrapping $DATA_DIR"
        run_bootstrap --force "${BOOTSTRAP_ARGS[@]}"
        write_reset_bindings
    fi
    if [ -n "${LAYERX_NODE_HANDOVER_AUTHORITY_PUBLIC_KEY:-}" ]; then
        configured_handover=$(sed -n 's/^LAYERX_NODE_HANDOVER_AUTHORITY_PUBLIC_KEY=//p' "$DATA_DIR/node.env")
        [ "$configured_handover" = "$LAYERX_NODE_HANDOVER_AUTHORITY_PUBLIC_KEY" ] \
            || fail "configured handover authority differs from committed genesis"
    fi
    check_sequencer_environment "$DATA_DIR/sequencer.env"
    publish_generation "$GENERATION"
    start_daemon "$DATA_DIR/sequencer.env" --serve "$DATA_DIR/sequencer.conf"
    wait_for_daemon_ready "$DATA_DIR/sequencer.env" --serve 120 || fail "sequencer did not become ready"
fi

printf '%s' "$$" > "$PID_FILE"
LNI_GID=$(sed -n 's/^LAYERX_NODE_LNI_ALLOWED_GID=//p' "$DATA_DIR/sequencer.env")
[ -n "$LNI_GID" ] || fail "LAYERX_NODE_LNI_ALLOWED_GID missing from sequencer.env"
python3 - "$SUPERVISOR_SOCKET" <<'SOCKET'
import os, stat, sys
try:
    value = os.lstat(sys.argv[1])
except FileNotFoundError:
    pass
else:
    if not stat.S_ISSOCK(value.st_mode) or value.st_uid != os.getuid():
        raise ValueError('supervisor socket pathname is not owned')
    os.unlink(sys.argv[1])
SOCKET
python3 "$RESET_HELPER" serve --socket "$SUPERVISOR_SOCKET" --state-dir "$STATE_DIR" \
    --data-dir "$DATA_DIR" --run-dir "$RUN_DIR" --network "$NETWORK_ID" \
    --bindings "$RESET_BINDINGS" --allowed-gid "$LNI_GID" &
SOCAT_PID=$!
log "supervisor socket $SUPERVISOR_SOCKET"

while :; do
    record=$(reset_state active)
    if [ "$record" != null ]; then perform_durable_reset "$record"; fi
    if ! daemon_alive; then
        wait "$DAEMON_PID" && status=0 || status=$?
        fail "layerxd --serve exited with status $status"
    fi
    if ! kill -0 "$SOCAT_PID" 2>/dev/null; then fail "supervisor socket listener exited"; fi
    sleep 0.2
done
