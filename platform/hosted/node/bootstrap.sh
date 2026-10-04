#!/usr/bin/env bash
# LayerX beta node bootstrap.
#
# Generates a signed genesis manifest and snapshot with layerx-genesis-build,
# the bootstrap registration the sequencer daemon verifies at first start, the
# identity file that registers the treasury signer, the eight-line layerxd
# configurations for the sequencer and the receipt-authority replica, and the
# environment files the supervisor sources when it starts both daemons.
#
# Usage:
#   bootstrap.sh --data-dir DIR --run-dir DIR --network-id N --sequencer-key FILE \
#       (--treasury-key FILE | --treasury-signer-socket PATH) [options]
#
# Required:
#   --data-dir DIR          Node data directory (created 0700; must be empty
#                           unless --force is given, which discards its contents).
#   --run-dir DIR           Directory holding the LNI socket and the supervisor
#                           socket. Made mode 0750 and owned by the daemon uid
#                           with --lni-gid as its group. Must differ from the
#                           data directory.
#   --network-id N          Decimal network id, 1..4294967295.
#   --genesis-metadata FILE LXGB v2 suffix: canonical Asset records and named fees.
#                           When FILE does not exist yet, bootstrap writes it (mode
#                           0600) before the node starts: one canonical Asset record
#                           for --asset issued by the treasury identity, with a fresh
#                           salt; an existing FILE is used as given.
#   --withdrawal-fee PRICE Commit an explicit v3 withdrawal price, preserving existing fees.
#   --module-fees FILE     Commit the exact v4 native module price configuration.
#   --sequencer-key FILE    Sequencer ed25519 seed: 32 raw bytes or 64 hex
#                           characters. Signs genesis and every batch. FILE
#                           must lie outside DATA_DIR; it is read here once to
#                           sign genesis and its path is recorded in
#                           sequencer.env as LAYERX_NODE_SEQUENCER_KEY_FILE.
#                           The seed itself is never written into DATA_DIR:
#                           the supervisor reads FILE when it starts layerxd.
#   --treasury-key FILE     Treasury ed25519 seed (same format). The treasury
#                           DID did:layerx:<public-key-hex> is registered as
#                           an identity so the admin plane can sign from it.
#                           The seed is read once for its public key and is
#                           never copied into DATA_DIR: components ask the
#                           treasury signer for signatures instead of opening
#                           a key file.
#   --treasury-signer-socket PATH
#                           Treasury signer socket (platform/hosted/node/signer)
#                           in place of --treasury-key: the treasury public key
#                           is read from the signer and no seed reaches this
#                           host. Exactly one of the two is required.
#
# Options:
#   --handover-authority HEX64
#                           Independent governance public key for authenticated
#                           sequencer replacement. Omission disables handover.
#   --asset HEX64           Genesis asset id (32 bytes hex). Default: the beta
#                           asset sha256("layerx-beta-asset:LXT").
#   --treasury-balance N    Treasury balance the genesis carries. The protocol
#                           genesis manifest admits only the three system
#                           accounts at balance zero, so any value other than 0
#                           is refused with a typed error instead of being
#                           silently dropped. Default 0.
#   --program-port P        Daemon program listener port on 127.0.0.1. Default 9401.
#   --replica-port P        Receipt-authority replica port on 127.0.0.1. Default 9402.
#   --lni-uid U             Uid the LNI admits (must differ from the daemon uid).
#                           Default: 4021.
#   --lni-gid G             Gid the LNI admits and the run directory group.
#                           Default: the daemon's primary gid.
#   --program-token-file F  Bearer token for the program listener (32..128
#                           printable bytes). Default: generated.
#   --replica-token-file F  Bearer token for the replica listener (32..128
#                           bytes, distinct from the program token). Default: generated.
#   --replica-id HEX64      Receipt-authority replica id. Default: derived from
#                           the sequencer public key.
#   --genesis-timestamp-ms T  Genesis timestamp in milliseconds. Default: now.
#   --enable-module NAME    Enable escrow, budget, stream, service, perps or spot in
#                           the signed genesis parameters. All six are enabled
#                           by default; explicit names select the enabled rows.
#   --migrations FILE       History migration SQL. Default: repository
#                           migrations/0007_history_index.sql or
#                           /opt/layerx/migrations/0007_history_index.sql.
#   --layerxd PATH          layerxd binary. Default: build/bin/layerxd or
#                           /usr/local/bin/layerxd.
#   --genesis-build PATH    layerx-genesis-build binary. Same lookup.
#   --settlement-env FILE   Defer the Paxeer settlement binding: the
#                           LAYERX_NODE_PAXEER_CHAIN_ID, LAYERX_NODE_PAXEER_RPC_URL,
#                           LAYERX_NODE_REGISTRY_PRECOMPILE, LAYERX_NODE_CUSTODY_PRECOMPILE
#                           and LAYERX_NODE_ANCHOR_PRECOMPILE values are read from FILE
#                           when the sequencer starts (the supervisor validates FILE
#                           with --check-settlement first) instead of from the
#                           bootstrap environment. sequencer.env then carries
#                           LAYERX_NODE_SETTLEMENT_ENV=FILE in place of the settlement
#                           lines.
#   --force                 Discard the data directory contents first.
#
# Settlement inputs (environment or --settlement-env FILE): the chain id defaults
# to 125, LAYERX_NODE_PAXEER_RPC_URL to the loopback JSON-RPC http://127.0.0.1:8545
# of the synced chain node beside the sequencer, and the three precompile
# addresses to the native modules 0x...1004 (registry), 0x...1013 (custody) and
# 0x...1014 (anchor); no other address is accepted. The settlement lines the
# bootstrap writes are those five values plus the pins cmd/layerxd reads:
# LAYERX_NODE_SETTLEMENT_CONTRACT and LAYERX_NODE_CHECKPOINT_REGISTRY are the anchor
# precompile, LAYERX_NODE_PAXEER_RPC_ADDRESS and LAYERX_NODE_PAXEER_RPC_PORT the
# loopback URL split. Given pins must agree with that derivation.
#
# Validation mode:
#   bootstrap.sh --check-settlement FILE
#                           Validate a settlement environment file holding KEY=VALUE
#                           lines of the settlement inputs (and, optionally, the
#                           derived pins) under the same rules the bootstrap applies
#                           to its environment and print the settlement lines.
#
# Outputs under DATA_DIR:
#   genesis/genesis.manifest, genesis/00000000000000000000.lxs,
#   genesis/paxeer-registration-request.lxrr, genesis/paxeer-deployment-descriptor.lxgd,
#   genesis/genesis.registration (LXGR bootstrap registration),
#   genesis/genesis-request.lxgb (the LXGB v2 request layerx-genesis-build consumed),
#   identities.txt, checkpoints/, logs/, replica/, secrets/{program-token,replica-token},
#   sequencer.conf, replica.conf, sequencer.env, replica.env, node.env, treasury.json
set -euo pipefail

usage() {
    sed -n '2,89p' "$0" | sed 's/^# \{0,1\}//'
    exit 2
}

fail() {
    printf 'bootstrap: %s\n' "$*" >&2
    exit 1
}

REGISTRY_PRECOMPILE=0x0000000000000000000000000000000000001004
CUSTODY_PRECOMPILE=0x0000000000000000000000000000000000001013
ANCHOR_PRECOMPILE=0x0000000000000000000000000000000000001014
SETTLEMENT_KEYS=(LAYERX_NODE_PAXEER_CHAIN_ID LAYERX_NODE_PAXEER_RPC_URL LAYERX_NODE_REGISTRY_PRECOMPILE
    LAYERX_NODE_CUSTODY_PRECOMPILE LAYERX_NODE_ANCHOR_PRECOMPILE LAYERX_NODE_SETTLEMENT_CONTRACT
    LAYERX_NODE_CHECKPOINT_REGISTRY LAYERX_NODE_PAXEER_RPC_ADDRESS LAYERX_NODE_PAXEER_RPC_PORT)
declare -A SETTLEMENT=()
SETTLEMENT_LINES=""

settlement_read_environment() {
    # settlement_read_environment -> SETTLEMENT holds the non-empty settlement keys of the environment
    local key
    SETTLEMENT=()
    for key in "${SETTLEMENT_KEYS[@]}"; do
        [ -z "${!key:-}" ] || SETTLEMENT[$key]=${!key}
    done
}

settlement_read_file() {
    # settlement_read_file FILE -> SETTLEMENT holds the KEY=VALUE lines of FILE
    local file=$1 line key
    SETTLEMENT=()
    [ -r "$file" ] || fail "settlement environment file is not readable: $file"
    [ -f "$file" ] || fail "settlement environment file is not a regular file: $file"
    [ "$(stat -c %s "$file")" -le 4096 ] || fail "settlement environment file exceeds 4096 bytes: $file"
    while IFS= read -r line || [ -n "$line" ]; do
        [ -n "$line" ] || continue
        [[ $line =~ ^([A-Z_]+)=([^[:space:]]+)$ ]] || fail "settlement environment line is not KEY=VALUE: ${line%%=*}"
        key=${BASH_REMATCH[1]}
        [[ " ${SETTLEMENT_KEYS[*]} " = *" $key "* ]] || fail "settlement environment file carries an unexpected key $key"
        [ -z "${SETTLEMENT[$key]+set}" ] || fail "$key repeated"
        SETTLEMENT[$key]=${BASH_REMATCH[2]}
    done < "$file"
}

settlement_resolve() {
    # settlement_resolve -> SETTLEMENT_LINES: the settlement inputs of SETTLEMENT with the
    # chain 125 loopback defaults filling what is absent, followed by the daemon pins
    # derived from them; a pin that was given must agree with the derivation.
    local chain_id=${SETTLEMENT[LAYERX_NODE_PAXEER_CHAIN_ID]:-125} rpc_url=${SETTLEMENT[LAYERX_NODE_PAXEER_RPC_URL]:-}
    local address=${SETTLEMENT[LAYERX_NODE_PAXEER_RPC_ADDRESS]:-} port=${SETTLEMENT[LAYERX_NODE_PAXEER_RPC_PORT]:-}
    local url_port binding given
    [[ $chain_id =~ ^[1-9][0-9]{0,19}$ ]] || fail "LAYERX_NODE_PAXEER_CHAIN_ID must be a positive decimal uint64"
    if [ ${#chain_id} -eq 20 ] && [[ $chain_id > 18446744073709551615 ]]; then
        fail "LAYERX_NODE_PAXEER_CHAIN_ID exceeds uint64"
    fi
    [ -n "$rpc_url" ] || rpc_url="http://${address:-127.0.0.1}:${port:-8545}"
    [[ $rpc_url =~ ^http://127\.0\.0\.1:([1-9][0-9]{0,4})$ ]] && [ "${BASH_REMATCH[1]}" -le 65535 ] \
        || fail "LAYERX_NODE_PAXEER_RPC_URL must be the loopback JSON-RPC http://127.0.0.1:PORT"
    url_port=${BASH_REMATCH[1]}
    [ -z "$address" ] || [ "$address" = 127.0.0.1 ] || fail "LAYERX_NODE_PAXEER_RPC_ADDRESS must be 127.0.0.1"
    [ -z "$port" ] || [ "$port" = "$url_port" ] || fail "LAYERX_NODE_PAXEER_RPC_PORT must be the port of LAYERX_NODE_PAXEER_RPC_URL"
    for binding in "LAYERX_NODE_REGISTRY_PRECOMPILE=$REGISTRY_PRECOMPILE" "LAYERX_NODE_CUSTODY_PRECOMPILE=$CUSTODY_PRECOMPILE" \
            "LAYERX_NODE_ANCHOR_PRECOMPILE=$ANCHOR_PRECOMPILE" "LAYERX_NODE_SETTLEMENT_CONTRACT=$ANCHOR_PRECOMPILE" \
            "LAYERX_NODE_CHECKPOINT_REGISTRY=$ANCHOR_PRECOMPILE"; do
        given=${SETTLEMENT[${binding%%=*}]:-}
        [ -z "$given" ] || [ "${given,,}" = "${binding#*=}" ] || fail "${binding%%=*} must be the native precompile ${binding#*=}"
    done
    SETTLEMENT_LINES=$(printf 'LAYERX_NODE_PAXEER_CHAIN_ID=%s\nLAYERX_NODE_PAXEER_RPC_URL=%s\nLAYERX_NODE_REGISTRY_PRECOMPILE=%s\nLAYERX_NODE_CUSTODY_PRECOMPILE=%s\nLAYERX_NODE_ANCHOR_PRECOMPILE=%s\nLAYERX_NODE_SETTLEMENT_CONTRACT=%s\nLAYERX_NODE_CHECKPOINT_REGISTRY=%s\nLAYERX_NODE_PAXEER_RPC_ADDRESS=127.0.0.1\nLAYERX_NODE_PAXEER_RPC_PORT=%s' \
        "$chain_id" "$rpc_url" "$REGISTRY_PRECOMPILE" "$CUSTODY_PRECOMPILE" "$ANCHOR_PRECOMPILE" "$ANCHOR_PRECOMPILE" "$ANCHOR_PRECOMPILE" "$url_port")
}

if [ "${1:-}" = --check-settlement ]; then
    [ $# -eq 2 ] || fail "--check-settlement takes exactly one file argument"
    settlement_read_file "$2"
    settlement_resolve
    printf '%s\n' "$SETTLEMENT_LINES"
    exit 0
fi

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
DATA_DIR=""
RUN_DIR=""
GENERATION_TARGET_DIR=""
GENERATION_RUN_DIR=""
GENERATION_AUTHORIZATION_DIR=""
GENERATION_TRANSPORT=${LAYERX_NODE_GENERATION_TRANSPORT:-0}
NETWORK_ID=""
SEQUENCER_KEY_FILE=""
TREASURY_KEY_FILE=""
TREASURY_SIGNER_SOCKET=""
ASSET_ID="b5a32b12029f8ddfb905f90f280f664b46390de0fc62770fc197dd87b18cd898"
ASSET_SYMBOL=LXT
ASSET_CURRENCY=LXT
ASSET_DECIMALS=18
TREASURY_BALANCE=0
PROGRAM_PORT=9401
REPLICA_PORT=9402
LNI_UID=4021
LNI_GID=""
PROGRAM_TOKEN_FILE=""
REPLICA_TOKEN_FILE=""
REPLICA_ID=""
GENESIS_TIMESTAMP_MS=""
MIGRATIONS=""
LAYERXD=""
GENESIS_BUILD=""
CUSTODY_PROFILE=""
CUSTODY_REGISTRY=""
GENESIS_METADATA=""
WITHDRAWAL_FEE=""
MODULE_FEES=""
GENESIS_MODULES=()
HANDOVER_AUTHORITY=""
HANDOVER_PARAMETER_COUNT=0
ORACLE_TRANSPORT_PARAMETER_COUNT=0
ORDER_TIF_PARAMETER_COUNT=0
SETTLEMENT_ENV=""
SETTLEMENT_DOCUMENT=${LAYERX_PAXEER_SETTLEMENT_JSON:-}
FORCE=0

enable_genesis_module() {
    local module
    case "$1" in
        escrow|budget|stream|service|perps|spot) ;;
        *) fail "--enable-module requires escrow, budget, stream, service, perps or spot" ;;
    esac
    for module in "${GENESIS_MODULES[@]}"; do
        [ "$module" != "$1" ] || fail "--enable-module repeats $1"
    done
    GENESIS_MODULES+=("$1")
}

while [ $# -gt 0 ]; do
    case "$1" in
        --data-dir) DATA_DIR=$2; shift 2 ;;
        --run-dir) RUN_DIR=$2; shift 2 ;;
        --generation-target-dir)
            [ -z "$GENERATION_TARGET_DIR" ] || fail "--generation-target-dir repeats"
            GENERATION_TARGET_DIR=$2; shift 2 ;;
        --generation-run-dir)
            [ -z "$GENERATION_RUN_DIR" ] || fail "--generation-run-dir repeats"
            GENERATION_RUN_DIR=$2; shift 2 ;;
        --generation-authorization-dir)
            [ -z "$GENERATION_AUTHORIZATION_DIR" ] || fail "--generation-authorization-dir repeats"
            GENERATION_AUTHORIZATION_DIR=$2; shift 2 ;;
        --network-id) NETWORK_ID=$2; shift 2 ;;
        --sequencer-key) SEQUENCER_KEY_FILE=$2; shift 2 ;;
        --treasury-key) TREASURY_KEY_FILE=$2; shift 2 ;;
        --treasury-signer-socket) TREASURY_SIGNER_SOCKET=$2; shift 2 ;;
        --asset) ASSET_ID=$2; shift 2 ;;
        --genesis-metadata) GENESIS_METADATA=$2; shift 2 ;;
        --handover-authority)
            [ "$HANDOVER_PARAMETER_COUNT" -eq 0 ] || fail "--handover-authority repeats"
            HANDOVER_PARAMETER_COUNT=1
            HANDOVER_AUTHORITY=${2,,}; shift 2 ;;
        --perps-order-tif)
            [ "$ORDER_TIF_PARAMETER_COUNT" -eq 0 ] && [ "${2:-}" = 1 ] || fail "--perps-order-tif requires one value of 1"
            ORDER_TIF_PARAMETER_COUNT=1; shift 2 ;;
        --perps-oracle-transport)
            [ "$ORACLE_TRANSPORT_PARAMETER_COUNT" -eq 0 ] && [ "${2:-}" = 1 ] || fail "--perps-oracle-transport requires one value of 1"
            ORACLE_TRANSPORT_PARAMETER_COUNT=1; shift 2 ;;
        --withdrawal-fee) WITHDRAWAL_FEE=$2; shift 2 ;;
        --module-fees) MODULE_FEES=$2; shift 2 ;;
        --treasury-balance) TREASURY_BALANCE=$2; shift 2 ;;
        --program-port) PROGRAM_PORT=$2; shift 2 ;;
        --replica-port) REPLICA_PORT=$2; shift 2 ;;
        --lni-uid) LNI_UID=$2; shift 2 ;;
        --lni-gid) LNI_GID=$2; shift 2 ;;
        --program-token-file) PROGRAM_TOKEN_FILE=$2; shift 2 ;;
        --replica-token-file) REPLICA_TOKEN_FILE=$2; shift 2 ;;
        --replica-id) REPLICA_ID=$2; shift 2 ;;
        --genesis-timestamp-ms) GENESIS_TIMESTAMP_MS=$2; shift 2 ;;
        --enable-module)
            enable_genesis_module "${2:-}"
            shift 2 ;;
        --migrations) MIGRATIONS=$2; shift 2 ;;
        --layerxd) LAYERXD=$2; shift 2 ;;
        --genesis-build) GENESIS_BUILD=$2; shift 2 ;;
        --custody-profile) CUSTODY_PROFILE=$2; shift 2 ;;
        --custody-registry) CUSTODY_REGISTRY=$2; shift 2 ;;
        --settlement-env) SETTLEMENT_ENV=$2; shift 2 ;;
        --settlement-document) SETTLEMENT_DOCUMENT=$2; shift 2 ;;
        --force) FORCE=1; shift ;;
        -h|--help) usage ;;
        *) fail "unknown argument $1" ;;
    esac
done

case "$GENERATION_TRANSPORT" in 0|1) ;; *) fail "invalid generation transport profile" ;; esac
if [ "$GENERATION_TRANSPORT" = 1 ]; then
    [[ $GENERATION_AUTHORIZATION_DIR = /* ]] || fail "generation transport requires private authorization directory"
elif [ -n "$GENERATION_AUTHORIZATION_DIR" ]; then
    fail "generation authorization directory requires the admitted transport profile"
fi

if [ "${#GENESIS_MODULES[@]}" -eq 0 ]; then
    [ -f "$SCRIPT_DIR/genesis-modules.conf" ] && [ -r "$SCRIPT_DIR/genesis-modules.conf" ] \
        || fail "public testnet genesis module configuration is unavailable"
    while IFS= read -r module || [ -n "$module" ]; do
        enable_genesis_module "$module"
    done < "$SCRIPT_DIR/genesis-modules.conf"
    [ "${#GENESIS_MODULES[@]}" -eq 6 ] || fail "public testnet genesis requires six configured modules"
fi

[ -n "$GENESIS_METADATA" ] || fail "--genesis-metadata is required: name the LXGB v2 metadata file, or the path where bootstrap writes it"
GENESIS_METADATA_WRITE=0
if [ -e "$GENESIS_METADATA" ] || [ -L "$GENESIS_METADATA" ]; then
    [ -f "$GENESIS_METADATA" ] && [ ! -L "$GENESIS_METADATA" ] && [ -r "$GENESIS_METADATA" ] || fail "--genesis-metadata requires an authoritative LXGB v2 metadata file"
    GENESIS_METADATA=$(readlink -f "$GENESIS_METADATA")
else
    [ -d "$(dirname "$GENESIS_METADATA")" ] && [ -w "$(dirname "$GENESIS_METADATA")" ] \
        || fail "the genesis metadata is absent and its directory is not writable, so bootstrap cannot write it: $GENESIS_METADATA"
    GENESIS_METADATA=$(readlink -m "$GENESIS_METADATA")
    GENESIS_METADATA_WRITE=1
fi
fee_arguments=()
if [ -n "$MODULE_FEES" ]; then
    [ -n "$WITHDRAWAL_FEE" ] || fail "--module-fees requires an explicit --withdrawal-fee"
    [ -f "$MODULE_FEES" ] && [ ! -L "$MODULE_FEES" ] && [ -r "$MODULE_FEES" ] || fail "invalid module fee configuration file"
    MODULE_FEES=$(readlink -f "$MODULE_FEES")
    case "$MODULE_FEES" in "$(readlink -m "$DATA_DIR")"/*) fail "module fees must be outside the data directory" ;; esac
    fee_arguments+=(--module-fees "$MODULE_FEES")
fi
check_genesis_metadata_fees() {
    if [ -n "$WITHDRAWAL_FEE" ]; then
        python3 "$SCRIPT_DIR/genesis_fees.py" "$GENESIS_METADATA" "$WITHDRAWAL_FEE" "${fee_arguments[@]}" --check \
            || fail "invalid withdrawal fee configuration"
    fi
}
[ "$GENESIS_METADATA_WRITE" -eq 1 ] || check_genesis_metadata_fees
case "$GENESIS_METADATA" in "$(readlink -m "$DATA_DIR")"/*) fail "genesis metadata must be outside the data directory" ;; esac
[ -n "$DATA_DIR" ] || fail "--data-dir is required"
[ -n "$RUN_DIR" ] || fail "--run-dir is required"
[ -n "$NETWORK_ID" ] || fail "--network-id is required"
[ -n "$SEQUENCER_KEY_FILE" ] || fail "--sequencer-key is required"
[[ $SEQUENCER_KEY_FILE = /* ]] || SEQUENCER_KEY_FILE="$PWD/$SEQUENCER_KEY_FILE"
[ -f "$SEQUENCER_KEY_FILE" ] && [ -r "$SEQUENCER_KEY_FILE" ] || fail "--sequencer-key must name a readable regular file: $SEQUENCER_KEY_FILE"
[[ $SEQUENCER_KEY_FILE != *[[:cntrl:]]* ]] || fail "--sequencer-key path must not contain control characters"
if [ -n "$TREASURY_KEY_FILE" ]; then
    [ -z "$TREASURY_SIGNER_SOCKET" ] \
        || fail "--treasury-key and --treasury-signer-socket are exclusive"
elif [ -n "$TREASURY_SIGNER_SOCKET" ]; then
    [[ $TREASURY_SIGNER_SOCKET = /* ]] || fail "--treasury-signer-socket must be an absolute path"
    [ ${#TREASURY_SIGNER_SOCKET} -lt 108 ] || fail "--treasury-signer-socket path is too long"
else
    fail "--treasury-key or --treasury-signer-socket is required"
fi
if [ -n "$CUSTODY_PROFILE" ]; then
    [ -f "$CUSTODY_PROFILE" ] && [ ! -L "$CUSTODY_PROFILE" ] && [ -r "$CUSTODY_PROFILE" ] \
        || fail "--custody-profile must name a readable regular file, not a symlink"
    [ "$(stat -c %s "$CUSTODY_PROFILE")" -eq 223 ] \
        || fail "--custody-profile must contain exactly 223 bytes"
    CUSTODY_PROFILE=$(readlink -f "$CUSTODY_PROFILE")
fi
if [ -n "$CUSTODY_REGISTRY" ]; then
    [ -f "$CUSTODY_REGISTRY" ] && [ ! -L "$CUSTODY_REGISTRY" ] && [ -r "$CUSTODY_REGISTRY" ] \
        || fail "--custody-registry must name a readable regular file, not a symlink"
    [ "$(stat -c %s "$CUSTODY_REGISTRY")" -eq 901 ] \
        || fail "--custody-registry must contain exactly 901 bytes"
    CUSTODY_REGISTRY=$(readlink -f "$CUSTODY_REGISTRY")
fi

is_decimal() { [[ $1 =~ ^[0-9]+$ ]]; }
is_hex64() { [[ $1 =~ ^[0-9a-f]{64}$ ]]; }

if [ "$HANDOVER_PARAMETER_COUNT" -eq 1 ]; then
    is_hex64 "$HANDOVER_AUTHORITY" || fail "--handover-authority must be 64 hex characters"
fi

is_decimal "$NETWORK_ID" || fail "--network-id must be decimal"
[ "$NETWORK_ID" -ge 1 ] && [ "$NETWORK_ID" -le 4294967295 ] || fail "--network-id out of range"
is_decimal "$PROGRAM_PORT" && [ "$PROGRAM_PORT" -ge 1 ] && [ "$PROGRAM_PORT" -le 65535 ] || fail "--program-port out of range"
is_decimal "$REPLICA_PORT" && [ "$REPLICA_PORT" -ge 1 ] && [ "$REPLICA_PORT" -le 65535 ] || fail "--replica-port out of range"
[ "$PROGRAM_PORT" != "$REPLICA_PORT" ] || fail "--program-port and --replica-port must differ"
is_decimal "$LNI_UID" || fail "--lni-uid must be decimal"
is_decimal "$TREASURY_BALANCE" || fail "--treasury-balance must be decimal"
ASSET_ID=$(printf '%s' "$ASSET_ID" | tr 'A-F' 'a-f')
is_hex64 "$ASSET_ID" || fail "--asset must be 64 hex characters"
[ "$ASSET_ID" != "$(printf '0%.0s' $(seq 1 64))" ] || fail "--asset must not be zero"
if [ "$TREASURY_BALANCE" != 0 ]; then
    fail "treasury_balance_unsupported: the protocol genesis manifest (src/protocol/lxp_genesis.c validate) admits only the three system accounts at balance zero; the treasury is funded after genesis, not in it"
fi

settlement_read_environment
if [ -n "$SETTLEMENT_ENV" ]; then
    [[ $SETTLEMENT_ENV = /* ]] || fail "--settlement-env must be an absolute path"
    [ ${#SETTLEMENT[@]} -eq 0 ] \
        || fail "--settlement-env excludes the LAYERX_NODE_PAXEER_*, LAYERX_NODE_*_PRECOMPILE and LAYERX_NODE_SETTLEMENT_CONTRACT/CHECKPOINT_REGISTRY environment"
else
    settlement_resolve
fi

DAEMON_UID=$(id -u)
DAEMON_GID=$(id -g)
[ -n "$LNI_GID" ] || LNI_GID=$DAEMON_GID
is_decimal "$LNI_GID" || fail "--lni-gid must be decimal"
[ "$LNI_UID" != "$DAEMON_UID" ] || fail "--lni-uid must differ from the daemon uid $DAEMON_UID"

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
    fail "$name not found; pass --${name#layerx-} or build it with make"
}

LAYERXD=$(resolve_binary "$LAYERXD" layerxd)
GENESIS_BUILD=$(resolve_binary "$GENESIS_BUILD" layerx-genesis-build)
if [ -z "$MIGRATIONS" ]; then
    for candidate in "$PWD/migrations/0007_history_index.sql" /opt/layerx/migrations/0007_history_index.sql; do
        if [ -r "$candidate" ]; then MIGRATIONS=$candidate; break; fi
    done
fi
[ -n "$MIGRATIONS" ] && [ -r "$MIGRATIONS" ] || fail "history migrations SQL not found; pass --migrations"
MIGRATIONS=$(readlink -f "$MIGRATIONS")

command -v openssl >/dev/null || fail "openssl is required"
command -v sha256sum >/dev/null || fail "sha256sum is required"
command -v od >/dev/null || fail "od is required"

if [ -z "$SETTLEMENT_DOCUMENT" ]; then
    SOURCE_ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
    if [ -r "$SOURCE_ROOT/contracts/config/checkpoint-settlement.json" ]; then
        SETTLEMENT_DOCUMENT="$SOURCE_ROOT/contracts/config/checkpoint-settlement.json"
    else
        SETTLEMENT_DOCUMENT=/opt/layerx/checkpoint-settlement.json
    fi
fi
GUARANTOR_COUNT=$(jq -er '.finality_policy.certificate_threshold | select(type == "number" and . == floor and . >= 1 and . <= 32)' "$SETTLEMENT_DOCUMENT") \
    || fail "certificate threshold must be an integer in 1..32 (LXP_GENESIS_MAX_GUARANTORS)"
if [ "$ORDER_TIF_PARAMETER_COUNT" -eq 1 ]; then
    [ "$ORACLE_TRANSPORT_PARAMETER_COUNT" -eq 1 ] || fail "order time in force requires oracle transport"
fi
if [ "$ORACLE_TRANSPORT_PARAMETER_COUNT" -eq 1 ] && [ "${#GENESIS_MODULES[@]}" -gt 0 ]; then
    perps_selected=0
    for module in "${GENESIS_MODULES[@]}"; do [ "$module" != perps ] || perps_selected=1; done
    [ "$perps_selected" -eq 1 ] || fail "oracle transport requires enabled perps module"
fi
GENESIS_METADATA_MAX_BYTES=$((16384 - 380 - 81 * GUARANTOR_COUNT - 66 * (${#GENESIS_MODULES[@]} + HANDOVER_PARAMETER_COUNT + ORACLE_TRANSPORT_PARAMETER_COUNT + ORDER_TIF_PARAMETER_COUNT)))
check_genesis_metadata_bounds() {
    [ -f "$GENESIS_METADATA" ] && [ ! -L "$GENESIS_METADATA" ] && [ -s "$GENESIS_METADATA" ] \
        || fail "the LXGB v2 genesis metadata is absent: $GENESIS_METADATA"
    GENESIS_METADATA_BYTES=$(stat -c %s "$GENESIS_METADATA")
    [ "$GENESIS_METADATA_BYTES" -gt 219 ] && [ "$GENESIS_METADATA_BYTES" -le "$GENESIS_METADATA_MAX_BYTES" ] \
        || fail "genesis metadata length is outside request bounds: $GENESIS_METADATA_BYTES bytes, expected 220..$GENESIS_METADATA_MAX_BYTES with $GUARANTOR_COUNT guarantors"
}
[ "$GENESIS_METADATA_WRITE" -eq 1 ] || check_genesis_metadata_bounds

bin_to_hex() { od -An -v -tx1 | tr -d ' \n'; }

hex_to_bin() {
    local hex=$1 i
    for ((i = 0; i < ${#hex}; i += 2)); do
        printf "\\$(printf '%03o' "0x${hex:i:2}")"
    done
}

be_hex() {
    # be_hex VALUE BYTES -> big-endian hex of VALUE padded to BYTES bytes
    printf "%0$(( $2 * 2 ))x" "$1"
}

sha256_hex() { sha256sum | cut -c1-64; }

load_seed_hex() {
    local file=$1 name=$2 size text
    [ -f "$file" ] && [ -r "$file" ] || fail "$name key file must name a readable regular file: $file"
    size=$(stat -Lc %s -- "$file")
    if [ "$size" -eq 32 ]; then
        bin_to_hex < "$file"
        return
    fi
    text=$(tr -d ' \t\r\n' < "$file" | tr 'A-F' 'a-f')
    is_hex64 "$text" || fail "$name key file must hold 32 raw bytes or 64 hex characters"
    printf '%s' "$text"
}

public_key_hex() {
    # ed25519 public key from a 32-byte seed via the PKCS#8 wrapper openssl reads.
    { hex_to_bin "302e020100300506032b657004220420"; hex_to_bin "$1"; } \
        | openssl pkey -inform DER -pubout -outform DER | tail -c 32 | bin_to_hex
}

signer_public_key() {
    # signer_public_key SOCKET -> the treasury public key the signer holds
    local socket=$1 client="$SCRIPT_DIR/signer/client.py"
    command -v python3 >/dev/null || fail "python3 is required by --treasury-signer-socket"
    [ -r "$client" ] || fail "treasury signer client is missing: $client"
    [ -S "$socket" ] || fail "treasury signer socket is not available: $socket"
    python3 "$client" --socket "$socket" public-key \
        || fail "the treasury signer refused the public key request"
}

load_token() {
    local file=$1 name=$2 token
    [ -r "$file" ] || fail "$name token file is not readable: $file"
    token=$(tr -d '\r\n' < "$file")
    [ ${#token} -ge 32 ] && [ ${#token} -le 128 ] || fail "$name token must be 32..128 bytes"
    [[ $token =~ ^[\!-~]+$ ]] || fail "$name token must be printable ASCII without spaces"
    printf '%s' "$token"
}

SEQUENCER_PRIVATE=$(load_seed_hex "$SEQUENCER_KEY_FILE" sequencer)
SEQUENCER_PUBLIC=$(public_key_hex "$SEQUENCER_PRIVATE")
if [ -n "$TREASURY_SIGNER_SOCKET" ]; then
    TREASURY_PUBLIC=$(signer_public_key "$TREASURY_SIGNER_SOCKET")
else
    TREASURY_PUBLIC=$(public_key_hex "$(load_seed_hex "$TREASURY_KEY_FILE" treasury)")
fi
TREASURY_PUBLIC=$(printf '%s' "$TREASURY_PUBLIC" | tr -d ' \t\r\n')
is_hex64 "$TREASURY_PUBLIC" || fail "the treasury public key must be 64 hex characters"
[ "$SEQUENCER_PUBLIC" != "$TREASURY_PUBLIC" ] || fail "sequencer and treasury keys must differ"
SEQUENCER_ID=$(printf 'layerx-sequencer:%s' "$SEQUENCER_PUBLIC" | sha256_hex)
if [ -z "$REPLICA_ID" ]; then
    REPLICA_ID=$(printf 'layerx-authority-replica:%s' "$SEQUENCER_PUBLIC" | sha256_hex)
fi
REPLICA_ID=$(printf '%s' "$REPLICA_ID" | tr 'A-F' 'a-f')
is_hex64 "$REPLICA_ID" || fail "--replica-id must be 64 hex characters"
[ -n "$GENESIS_TIMESTAMP_MS" ] || GENESIS_TIMESTAMP_MS=$(( $(date +%s) * 1000 ))
is_decimal "$GENESIS_TIMESTAMP_MS" && [ "$GENESIS_TIMESTAMP_MS" -gt 0 ] || fail "--genesis-timestamp-ms must be a positive decimal"

write_genesis_metadata() {
    # The one-asset LXGB v2 metadata: the canonical Asset record of ASSET_ID with the declared
    # symbol and decimals, issued by the treasury identity under a fresh salt, followed by the
    # zero fee schedule; genesis_fees.py adds the withdrawal and module prices below.
    python3 - "$GENESIS_METADATA" "$ASSET_ID" "$TREASURY_PUBLIC" "$ASSET_SYMBOL" "$ASSET_DECIMALS" <<'PYGENESISMETADATA'
import hashlib
import os
import sys

output, asset, issuer_public = sys.argv[1], bytes.fromhex(sys.argv[2]), bytes.fromhex(sys.argv[3])
symbol, decimals = sys.argv[4].encode('ascii'), int(sys.argv[5])
if len(asset) != 32 or len(issuer_public) != 32:
    raise SystemExit('asset and treasury public key must be 32 bytes')
if not 0 < len(symbol) <= 16 or any(byte > 0x7f for byte in symbol) or not 0 <= decimals <= 38:
    raise SystemExit('asset symbol must be 1 to 16 ASCII bytes and decimals 0..38')
reference = bytes(12) + asset[12:]
if not any(reference):
    raise SystemExit('paxeer custody reference must be non-zero')
did = ('did:layerx:' + issuer_public.hex()).encode()
issuer = hashlib.sha256(b'LXP/v1/did-id\0' + len(did).to_bytes(2, 'big') + did).digest()
record = (b'\0\x03' + asset + len(symbol).to_bytes(1, 'big') + symbol + decimals.to_bytes(1, 'big')
          + b'\x02' + len(reference).to_bytes(2, 'big') + reference
          + b'\0\x0dCustody token' + bytes(16) + issuer + b'\x02' + bytes(16) + os.urandom(32))
schedule = b'\0\x02' + bytes(80) + (10000).to_bytes(4, 'big') + b'\x0a' + bytes(160)
metadata = b'\0\x01' + len(record).to_bytes(2, 'big') + record + len(schedule).to_bytes(2, 'big') + schedule
with os.fdopen(os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as handle:
    handle.write(metadata)
    handle.flush()
    os.fsync(handle.fileno())
PYGENESISMETADATA
}

if [ "$GENESIS_METADATA_WRITE" -eq 1 ]; then
    write_genesis_metadata || fail "writing the LXGB v2 genesis metadata failed: $GENESIS_METADATA"
    check_genesis_metadata_bounds
    check_genesis_metadata_fees
fi

TREASURY_DID="did:layerx:$TREASURY_PUBLIC"
TREASURY_DID_HEX=$(printf '%s' "$TREASURY_DID" | bin_to_hex)
TREASURY_ACCOUNT="agent:$TREASURY_DID:main"

if [ -n "$PROGRAM_TOKEN_FILE" ]; then
    PROGRAM_TOKEN=$(load_token "$PROGRAM_TOKEN_FILE" program)
else
    PROGRAM_TOKEN=$(openssl rand -hex 32)
fi
if [ -n "$REPLICA_TOKEN_FILE" ]; then
    REPLICA_TOKEN=$(load_token "$REPLICA_TOKEN_FILE" replica)
else
    REPLICA_TOKEN=$(openssl rand -hex 32)
fi
[ "$PROGRAM_TOKEN" != "$REPLICA_TOKEN" ] || fail "program and replica tokens must differ"

command -v python3 >/dev/null || fail "python3 is required for safe data directory preparation"
DATA_DIR=$(python3 "$SCRIPT_DIR/data_directory.py" prepare "$DATA_DIR") \
    || fail "data directory preparation was refused"
mkdir -p "$RUN_DIR"
RUN_DIR=$(readlink -f "$RUN_DIR")
[ "$DATA_DIR" != "$RUN_DIR" ] || fail "--data-dir and --run-dir must differ"
case "$RUN_DIR" in "$DATA_DIR"/*) fail "--run-dir must not be inside --data-dir" ;; esac
case "$SEQUENCER_KEY_FILE" in "$DATA_DIR"/*) fail "the sequencer key file must be outside the data directory: $SEQUENCER_KEY_FILE" ;; esac
case "$(readlink -f "$SEQUENCER_KEY_FILE")" in "$DATA_DIR"/*) fail "the sequencer key file must be outside the data directory: $SEQUENCER_KEY_FILE" ;; esac
ENV_DATA_DIR=$DATA_DIR
ENV_RUN_DIR=$RUN_DIR
if [ -n "$GENERATION_TARGET_DIR" ] || [ -n "$GENERATION_RUN_DIR" ]; then
    [ -n "$GENERATION_TARGET_DIR" ] && [ -n "$GENERATION_RUN_DIR" ] \
        || fail "generation target and run directories are both required"
    ENV_DATA_DIR=$(python3 "$SCRIPT_DIR/data_directory.py" inspect "$GENERATION_TARGET_DIR") \
        || fail "generation target data directory was refused"
    ENV_RUN_DIR=$(python3 "$SCRIPT_DIR/data_directory.py" inspect "$GENERATION_RUN_DIR") \
        || fail "generation target run directory was refused"
    [ "$ENV_DATA_DIR" != "$DATA_DIR" ] && [ "$ENV_RUN_DIR" != "$RUN_DIR" ] \
        || fail "generation bootstrap requires isolated output directories"
    [ "$ENV_DATA_DIR" != "$ENV_RUN_DIR" ] || fail "generation data and run directories must differ"
    case "$ENV_RUN_DIR" in "$ENV_DATA_DIR"/*) fail "generation run directory must be outside target data" ;; esac
    case "$DATA_DIR/" in "$ENV_DATA_DIR/"*|"$ENV_RUN_DIR/"*) fail "generation staging overlaps active directories" ;; esac
    case "$RUN_DIR/" in "$ENV_DATA_DIR/"*|"$ENV_RUN_DIR/"*) fail "generation run staging overlaps active directories" ;; esac
    for protected_path in "$SEQUENCER_KEY_FILE" "$(readlink -f "$SEQUENCER_KEY_FILE")" \
            "$GENESIS_METADATA" "$MODULE_FEES"; do
        case "$protected_path" in "$ENV_DATA_DIR"/*) fail "canonical input must be outside target data directory" ;; esac
    done
fi
if [ "$FORCE" -eq 1 ]; then
    python3 "$SCRIPT_DIR/data_directory.py" clear "$DATA_DIR" \
        || fail "data directory cleanup was refused"
fi
if [ -n "$(ls -A "$DATA_DIR")" ]; then
    fail "data directory is not empty: $DATA_DIR (pass --force to discard it)"
fi
chgrp "$LNI_GID" "$RUN_DIR" 2>/dev/null || [ "$(stat -c %g "$RUN_DIR")" = "$LNI_GID" ] \
    || fail "cannot set the run directory group to $LNI_GID: $RUN_DIR"
chmod 0750 "$RUN_DIR"
LNI_SOCKET="$ENV_RUN_DIR/layerxd.lni.sock"
SUPERVISOR_SOCKET="$ENV_RUN_DIR/supervisor.sock"
[ ${#LNI_SOCKET} -lt 108 ] || fail "LNI socket path is too long: $LNI_SOCKET"

umask 077
mkdir -p "$DATA_DIR/checkpoints" "$DATA_DIR/logs" "$DATA_DIR/replica" "$DATA_DIR/secrets" "$DATA_DIR/work"
if [ -n "$WITHDRAWAL_FEE" ]; then
    python3 "$SCRIPT_DIR/genesis_fees.py" "$GENESIS_METADATA" "$WITHDRAWAL_FEE" "${fee_arguments[@]}" \
        > "$DATA_DIR/work/withdrawal-metadata.lxgb" || fail "withdrawal metadata generation failed"
    GENESIS_METADATA="$DATA_DIR/work/withdrawal-metadata.lxgb"
    [ "$(stat -c %s "$GENESIS_METADATA")" -le "$GENESIS_METADATA_MAX_BYTES" ] \
        || fail "withdrawal metadata exceeds the genesis request bound"
fi
GUARANTOR_KEY_FILE="$DATA_DIR/secrets/guarantor-key.pem"
GUARANTOR_ENTRIES=()
declare -A GUARANTOR_KEYS=()
for ((index = 0; index < GUARANTOR_COUNT; index++)); do
    key_file="$DATA_DIR/secrets/guarantor-key-$index.pem"
    if [ "$index" -eq 0 ]; then key_file=$GUARANTOR_KEY_FILE; fi
    openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:secp256k1 -out "$key_file"
    chmod 0600 "$key_file"
    public=$(openssl ec -in "$key_file" -pubout -conv_form compressed -outform DER 2>/dev/null | tail -c 33 | bin_to_hex)
    [[ $public =~ ^0[23][0-9a-f]{64}$ ]] || fail "could not derive a compressed secp256k1 guarantor public key"
    id=$(printf 'layerx-beta-guarantor:%s' "$public" | sha256_hex)
    GUARANTOR_ENTRIES+=("$id $public")
    GUARANTOR_KEYS["$id"]=$key_file
done
mapfile -t GUARANTOR_ENTRIES < <(printf '%s\n' "${GUARANTOR_ENTRIES[@]}" | LC_ALL=C sort)
previous_id=""
for entry in "${GUARANTOR_ENTRIES[@]}"; do
    id=${entry%% *}
    [[ $id > $previous_id ]] || fail "guarantor identifiers must be strictly ascending"
    previous_id=$id
done
entry=${GUARANTOR_ENTRIES[0]}
GUARANTOR_ID=${entry%% *}
GUARANTOR_PUBLIC=${entry#* }
GUARANTOR_KEY_FILE=${GUARANTOR_KEYS[$GUARANTOR_ID]}
if [ "$GUARANTOR_COUNT" -gt 1 ]; then
    entry=${GUARANTOR_ENTRIES[1]}
    GUARANTOR_SECOND_ID=${entry%% *}
    GUARANTOR_SECOND_PUBLIC=${entry#* }
    GUARANTOR_SECOND_KEY_FILE=${GUARANTOR_KEYS[$GUARANTOR_SECOND_ID]}
fi

# --- genesis request (LXGB v2) -------------------------------------------
PARAMETER_KEY=$(printf 'parameter-version' | bin_to_hex)
PARAMETER_KEY="$PARAMETER_KEY$(printf '0%.0s' $(seq 1 $(( 64 - ${#PARAMETER_KEY} ))))"
PARAMETER_VALUE="$(printf '0%.0s' $(seq 1 56))00000001"
FEE_AUTHORITY_KEY=$(printf 'native-fee-authority-version' | bin_to_hex)
FEE_AUTHORITY_KEY="$FEE_AUTHORITY_KEY$(printf '0%.0s' $(seq 1 $((64 - ${#FEE_AUTHORITY_KEY}))))"
FEE_AUTHORITY_VALUE="$(printf '0%.0s' $(seq 1 62))02"
if [ "${#GENESIS_MODULES[@]}" -gt 0 ]; then
    mapfile -t GENESIS_MODULES < <(printf '%s\n' "${GENESIS_MODULES[@]}" | LC_ALL=C sort)
fi
REQUEST="$DATA_DIR/work/genesis-request.lxgb"
{
    printf 'LXGB'
    hex_to_bin 02
    hex_to_bin "$(be_hex 3 2)"
    hex_to_bin "$(be_hex "$NETWORK_ID" 4)"
    hex_to_bin "$(be_hex "$GENESIS_TIMESTAMP_MS" 8)"
    hex_to_bin "$(be_hex "$((2 + ${#GENESIS_MODULES[@]} + HANDOVER_PARAMETER_COUNT + ORACLE_TRANSPORT_PARAMETER_COUNT + ORDER_TIF_PARAMETER_COUNT))" 2)"
    if [ "$HANDOVER_PARAMETER_COUNT" -eq 1 ]; then
        handover_key=$(printf 'handover-authority' | bin_to_hex)
        handover_key="$handover_key$(printf '0%.0s' $(seq 1 $((64 - ${#handover_key}))))"
        hex_to_bin "$(be_hex 7 2)"
        hex_to_bin "$handover_key"
        hex_to_bin "$HANDOVER_AUTHORITY"
    fi
    for module in "${GENESIS_MODULES[@]}"; do
        module_key=$(printf 'module-enable:%s' "$module" | bin_to_hex)
        module_key="$module_key$(printf '0%.0s' $(seq 1 $((64 - ${#module_key}))))"
        hex_to_bin "$(be_hex 7 2)"
        hex_to_bin "$module_key"
        hex_to_bin "$PARAMETER_VALUE"
    done
    hex_to_bin "$(be_hex 7 2)"
    hex_to_bin "$FEE_AUTHORITY_KEY"
    hex_to_bin "$FEE_AUTHORITY_VALUE"
    hex_to_bin "$(be_hex 7 2)"
    hex_to_bin "$PARAMETER_KEY"
    hex_to_bin "$PARAMETER_VALUE"
    if [ "$ORACLE_TRANSPORT_PARAMETER_COUNT" -eq 1 ]; then
        oracle_key=$(printf 'perps-oracle-transport' | bin_to_hex)
        oracle_key="$oracle_key$(printf '0%.0s' $(seq 1 $((64 - ${#oracle_key}))))"
        hex_to_bin "$(be_hex 7 2)"
        hex_to_bin "$oracle_key"
        hex_to_bin "$PARAMETER_VALUE"
    fi
    if [ "$ORDER_TIF_PARAMETER_COUNT" -eq 1 ]; then
        tif_key=$(printf 'perps-order-tif' | bin_to_hex)
        tif_key="$tif_key$(printf '0%.0s' $(seq 1 $((64 - ${#tif_key}))))"
        hex_to_bin "$(be_hex 7 2)"
        hex_to_bin "$tif_key"
        hex_to_bin "$PARAMETER_VALUE"
    fi
    hex_to_bin "$(be_hex "$GUARANTOR_COUNT" 2)"
    for entry in "${GUARANTOR_ENTRIES[@]}"; do
        hex_to_bin "${entry%% *}"
        hex_to_bin "${entry#* }"
        hex_to_bin "$(be_hex 0 16)"
    done
    hex_to_bin "$ASSET_ID"
    hex_to_bin "$(be_hex 1 4)"
    for coefficient in 1 1 1 1 1 8 8 64 8; do hex_to_bin "$(be_hex "$coefficient" 8)"; done
    hex_to_bin "$(be_hex 1 8)"
    hex_to_bin 01
    hex_to_bin "$(be_hex 1 4)"
    for price in 1 1 2 4 1 1 100; do hex_to_bin "$(be_hex "$price" 8)"; done
    for demand in 100 1 1 10 1 1000; do hex_to_bin "$(be_hex "$demand" 8)"; done
    cat "$GENESIS_METADATA"
} > "$REQUEST"
[ "$(stat -c %s "$REQUEST")" -eq "$((380 + 81 * GUARANTOR_COUNT + 66 * (${#GENESIS_MODULES[@]} + HANDOVER_PARAMETER_COUNT + ORACLE_TRANSPORT_PARAMETER_COUNT + ORDER_TIF_PARAMETER_COUNT) + $(stat -c %s "$GENESIS_METADATA")))" ] || fail "genesis request has an unexpected length"

SIGNER_KEY="$DATA_DIR/work/genesis-signer.key"
hex_to_bin "$SEQUENCER_PRIVATE" > "$SIGNER_KEY"
chmod 0600 "$SIGNER_KEY" "$REQUEST"
GENESIS_DIR="$DATA_DIR/genesis"
GENESIS_ARGS=("$REQUEST" "$SIGNER_KEY" "$GENESIS_DIR")
if [ -n "$CUSTODY_PROFILE" ]; then
    GENESIS_ARGS+=(--custody-profile "$CUSTODY_PROFILE")
fi
if [ -n "$CUSTODY_REGISTRY" ]; then
    GENESIS_ARGS+=(--custody-registry "$CUSTODY_REGISTRY")
fi
"$GENESIS_BUILD" "${GENESIS_ARGS[@]}" || fail "layerx-genesis-build refused the genesis request"
rm -f "$SIGNER_KEY"
MANIFEST="$GENESIS_DIR/genesis.manifest"
SNAPSHOT="$GENESIS_DIR/00000000000000000000.lxs"
REGISTRATION_REQUEST="$GENESIS_DIR/paxeer-registration-request.lxrr"
for artifact in "$MANIFEST" "$SNAPSHOT" "$REGISTRATION_REQUEST" "$GENESIS_DIR/paxeer-deployment-descriptor.lxgd"; do
    [ -s "$artifact" ] || fail "genesis artifact missing: $artifact"
done
if [ "$HANDOVER_PARAMETER_COUNT" -eq 1 ]; then
    [ -s "$GENESIS_DIR/genesis-handover-trust.lxt" ] || fail "genesis handover trust artifact missing"
fi
RETAINED_REQUEST="$GENESIS_DIR/genesis-request.lxgb"
mv "$REQUEST" "$RETAINED_REQUEST"
[ "$(stat -c %s "$REGISTRATION_REQUEST")" -eq 73 ] || fail "registration request has an unexpected length"
GENESIS_STATE_ROOT=$(tail -c +10 "$REGISTRATION_REQUEST" | head -c 32 | bin_to_hex)
GENESIS_RECEIPT_STATE_ROOT=$(tail -c 32 "$REGISTRATION_REQUEST" | bin_to_hex)

# Bootstrap registration (LXGR v1): the beta anchors genesis to its own
# receipt state root, the same self-registration the conformance node performs.
# layerxd refuses to start without it (-904), so a custody-profile genesis writes it
# too: the anchor module starts every network holding no roots of its own, and these
# are the bytes the cluster's own registration publish derives from the deployment
# descriptor and writes over this one once it has checked the anchor is still empty.
REGISTRATION="$GENESIS_DIR/genesis.registration"
{
    printf 'LXGR'
    hex_to_bin 01
    hex_to_bin "$(be_hex "$NETWORK_ID" 4)"
    hex_to_bin "$(be_hex 0 8)"
    hex_to_bin "$GENESIS_RECEIPT_STATE_ROOT"
    hex_to_bin "$GENESIS_RECEIPT_STATE_ROOT"
    hex_to_bin 01
} > "$REGISTRATION"
[ "$(stat -c %s "$REGISTRATION")" -eq 82 ] || fail "bootstrap registration has an unexpected length"

# --- identities, tokens, configurations ------------------------------------
IDENTITIES="$DATA_DIR/identities.txt"
ENV_SNAPSHOT="$ENV_DATA_DIR/genesis/00000000000000000000.lxs"
ENV_MANIFEST="$ENV_DATA_DIR/genesis/genesis.manifest"
ENV_REGISTRATION="$ENV_DATA_DIR/genesis/genesis.registration"
ENV_IDENTITIES="$ENV_DATA_DIR/identities.txt"
printf '%s:%s:0\n' "$TREASURY_DID_HEX" "$TREASURY_PUBLIC" > "$IDENTITIES"
if [ "$HANDOVER_PARAMETER_COUNT" -eq 1 ] && [ "$HANDOVER_AUTHORITY" != "$TREASURY_PUBLIC" ]; then
    governance_did=$(printf 'did:layerx:%s' "$HANDOVER_AUTHORITY" | bin_to_hex)
    printf '%s:%s:0\n' "$governance_did" "$HANDOVER_AUTHORITY" >> "$IDENTITIES"
fi

for logfile in logs/program-feed.log logs/canonical.log logs/receipt-authority.log logs/batch.log logs/evidence.log replica/receipt-authority.log; do
    : > "$DATA_DIR/$logfile"
    chmod 0600 "$DATA_DIR/$logfile"
done

printf '%s' "$PROGRAM_TOKEN" > "$DATA_DIR/secrets/program-token"
printf '%s' "$REPLICA_TOKEN" > "$DATA_DIR/secrets/replica-token"

write_config() {
    printf 'config_version=2\nrole=%s\nnetwork_id=%s\nstart_sequence=0\nverify_workers=2\nserial_execution=false\n' "$1" "$NETWORK_ID" > "$2"
}
write_config sequencer "$DATA_DIR/sequencer.conf"
write_config replica "$DATA_DIR/replica.conf"
"$LAYERXD" --check-config "$DATA_DIR/sequencer.conf" || fail "layerxd refused the sequencer configuration"
"$LAYERXD" --check-config "$DATA_DIR/replica.conf" || fail "layerxd refused the replica configuration"

LAST_BATCH=18446744073709551615
if [ -n "$SETTLEMENT_ENV" ]; then
    printf 'LAYERX_NODE_SETTLEMENT_ENV=%s\n' "$SETTLEMENT_ENV" > "$DATA_DIR/sequencer.env"
else
    printf '%s\n' "$SETTLEMENT_LINES" > "$DATA_DIR/sequencer.env"
fi
cat >> "$DATA_DIR/sequencer.env" <<EOF
LAYERX_NODE_CHECKPOINT_DIRECTORY=$ENV_DATA_DIR/checkpoints
LAYERX_NODE_SNAPSHOT=$ENV_SNAPSHOT
LAYERX_NODE_GENESIS_MANIFEST=$ENV_MANIFEST
LAYERX_NODE_GENESIS_REGISTRATION=$ENV_REGISTRATION
LAYERX_NODE_IDENTITIES=$ENV_IDENTITIES
LAYERX_NODE_PROGRAM_FEED_LOG=$ENV_DATA_DIR/logs/program-feed.log
LAYERX_NODE_CANONICAL_LOG=$ENV_DATA_DIR/logs/canonical.log
LAYERX_NODE_RECEIPT_AUTHORITY_LOG=$ENV_DATA_DIR/logs/receipt-authority.log
LAYERX_NODE_BATCH_LOG=$ENV_DATA_DIR/logs/batch.log
LAYERX_NODE_EVIDENCE_LOG=$ENV_DATA_DIR/logs/evidence.log
LAYERX_NODE_HISTORY_DATABASE=$ENV_DATA_DIR/history.sqlite
LAYERX_NODE_HISTORY_MIGRATIONS=$MIGRATIONS
LAYERX_NODE_SEQUENCER_ID=$SEQUENCER_ID
LAYERX_NODE_SEQUENCER_PUBLIC_KEY=$SEQUENCER_PUBLIC
LAYERX_NODE_SEQUENCER_KEY_FILE=$SEQUENCER_KEY_FILE
LAYERX_NODE_FIRST_BATCH=1
LAYERX_NODE_LAST_BATCH=$LAST_BATCH
LAYERX_NODE_AUTHORITY_REPLICA_ADDRESS=127.0.0.1
LAYERX_NODE_AUTHORITY_REPLICA_PORT=$REPLICA_PORT
LAYERX_NODE_AUTHORITY_REPLICA_ID=$REPLICA_ID
LAYERX_NODE_AUTHORITY_REPLICA_BEARER_TOKEN=$REPLICA_TOKEN
LAYERX_NODE_PROGRAM_ADDRESS=127.0.0.1
LAYERX_NODE_PROGRAM_PORT=$PROGRAM_PORT
LAYERX_NODE_PROGRAM_BEARER_TOKEN=$PROGRAM_TOKEN
LAYERX_NODE_LNI_SOCKET=$LNI_SOCKET
LAYERX_NODE_LNI_ALLOWED_UID=$LNI_UID
LAYERX_NODE_LNI_ALLOWED_GID=$LNI_GID
LAYERX_NODE_LNI_FRAME_BYTES=1212416
LAYERX_NODE_LNI_DEADLINE_MS=2000
EOF

cat > "$DATA_DIR/replica.env" <<EOF
LAYERX_AUTHORITY_REPLICA_LOG=$ENV_DATA_DIR/replica/receipt-authority.log
LAYERX_AUTHORITY_REPLICA_ID=$REPLICA_ID
LAYERX_AUTHORITY_SEQUENCER_ID=$SEQUENCER_ID
LAYERX_AUTHORITY_SEQUENCER_PUBLIC_KEY=$SEQUENCER_PUBLIC
LAYERX_AUTHORITY_FIRST_BATCH=1
LAYERX_AUTHORITY_LAST_BATCH=$LAST_BATCH
LAYERX_AUTHORITY_BEARER_TOKEN=$REPLICA_TOKEN
LAYERX_AUTHORITY_ADDRESS=127.0.0.1
LAYERX_AUTHORITY_PORT=$REPLICA_PORT
EOF
if [ "$HANDOVER_PARAMETER_COUNT" -eq 1 ]; then
    printf 'LAYERX_AUTHORITY_GENESIS_MANIFEST=%s\nLAYERX_AUTHORITY_AVAILABILITY_LOG=%s/checkpoints/da-bodies.log\n' "$ENV_MANIFEST" "$ENV_DATA_DIR" >> "$DATA_DIR/replica.env"
    if [ -n "$SETTLEMENT_ENV" ]; then
        printf 'LAYERX_NODE_SETTLEMENT_ENV=%s\n' "$SETTLEMENT_ENV" >> "$DATA_DIR/replica.env"
    else
        printf '%s\n' "$SETTLEMENT_LINES" >> "$DATA_DIR/replica.env"
    fi
fi

umask 022
cat > "$DATA_DIR/node.env.tmp" <<EOF
LAYERX_NODE_NETWORK_ID=$NETWORK_ID
LAYERX_NODE_ASSET_ID=$ASSET_ID
LAYERX_NODE_ASSET_SYMBOL=$ASSET_SYMBOL
LAYERX_NODE_ASSET_CURRENCY=$ASSET_CURRENCY
LAYERX_NODE_ASSET_DECIMALS=$ASSET_DECIMALS
LAYERX_NODE_LNI_SOCKET=$LNI_SOCKET
LAYERX_NODE_SUPERVISOR_SOCKET=$SUPERVISOR_SOCKET
LAYERX_NODE_PROGRAM_URL=http://127.0.0.1:$PROGRAM_PORT
LAYERX_NODE_REPLICA_URL=http://127.0.0.1:$REPLICA_PORT
LAYERX_NODE_PROGRAM_BEARER_TOKEN_FILE=$ENV_DATA_DIR/secrets/program-token
LAYERX_NODE_REPLICA_BEARER_TOKEN_FILE=$ENV_DATA_DIR/secrets/replica-token
LAYERX_NODE_SEQUENCER_ID=$SEQUENCER_ID
LAYERX_NODE_SEQUENCER_PUBLIC_KEY=$SEQUENCER_PUBLIC
LAYERX_NODE_REPLICA_ID=$REPLICA_ID
LAYERX_NODE_GENESIS_STATE_ROOT=$GENESIS_STATE_ROOT
LAYERX_NODE_GENESIS_RECEIPT_STATE_ROOT=$GENESIS_RECEIPT_STATE_ROOT
LAYERX_NODE_GENESIS_GUARANTOR_ID=$GUARANTOR_ID
LAYERX_NODE_GENESIS_GUARANTOR_PUBLIC_KEY=$GUARANTOR_PUBLIC
LAYERX_NODE_GENESIS_GUARANTOR_KEY_FILE=$ENV_DATA_DIR/secrets/${GUARANTOR_KEY_FILE##*/}
LAYERX_PAXEER_GENESIS_DIR=$ENV_DATA_DIR/genesis
LAYERX_NODE_TREASURY_DID=$TREASURY_DID
LAYERX_NODE_TREASURY_PUBLIC_KEY=$TREASURY_PUBLIC
LAYERX_NODE_TREASURY_ACCOUNT=$TREASURY_ACCOUNT
LAYERX_NODE_TREASURY_BALANCE=$TREASURY_BALANCE
LAYERX_NODE_SEQUENCER_CONFIG=$ENV_DATA_DIR/sequencer.conf
LAYERX_NODE_REPLICA_CONFIG=$ENV_DATA_DIR/replica.conf
LAYERX_NODE_SEQUENCER_ENV=$ENV_DATA_DIR/sequencer.env
LAYERX_NODE_REPLICA_ENV=$ENV_DATA_DIR/replica.env
EOF
if [ "$HANDOVER_PARAMETER_COUNT" -eq 1 ]; then
    printf 'LAYERX_NODE_GENESIS_HANDOVER_TRUST=%s/genesis-handover-trust.lxt\nLAYERX_NODE_HANDOVER_AUTHORITY_PUBLIC_KEY=%s\n' "$ENV_DATA_DIR/genesis" "$HANDOVER_AUTHORITY" >> "$DATA_DIR/node.env.tmp"
fi
if [ -n "$TREASURY_SIGNER_SOCKET" ]; then
    printf 'LAYERX_NODE_TREASURY_SIGNER_SOCKET=%s\n' "$TREASURY_SIGNER_SOCKET" >> "$DATA_DIR/node.env.tmp"
fi
printf 'LAYERX_NODE_GENESIS_GUARANTOR_COUNT=%s\n' "$GUARANTOR_COUNT" >> "$DATA_DIR/node.env.tmp"
for ((index = 0; index < GUARANTOR_COUNT; index++)); do
    entry=${GUARANTOR_ENTRIES[index]}
    identity_id=${entry%% *}
    identity_public=${entry#* }
    identity_key=${GUARANTOR_KEYS[$identity_id]}
    printf 'LAYERX_NODE_GENESIS_GUARANTOR_ID_%s=%s\nLAYERX_NODE_GENESIS_GUARANTOR_PUBLIC_KEY_%s=%s\n' \
        "$index" "$identity_id" "$index" "$identity_public" >> "$DATA_DIR/node.env.tmp"
done
if [ "$GUARANTOR_COUNT" -gt 1 ]; then
    printf 'LAYERX_NODE_SECOND_GUARANTOR_ID=%s\nLAYERX_NODE_SECOND_GUARANTOR_PUBLIC_KEY=%s\nLAYERX_NODE_SECOND_GUARANTOR_KEY_FILE=%s\n' \
        "$GUARANTOR_SECOND_ID" "$GUARANTOR_SECOND_PUBLIC" "$ENV_DATA_DIR/secrets/${GUARANTOR_SECOND_KEY_FILE##*/}" >> "$DATA_DIR/node.env.tmp"
fi
if [ "$GENERATION_TRANSPORT" = 1 ]; then
    python3 - "$SCRIPT_DIR" "$DATA_DIR" <<'GENERATION_PROOF'
import os
import stat
import sys

sys.path.insert(0, sys.argv[1])
from reset_state import open_directory

data = open_directory(sys.argv[2])
proof = source = None
try:
    os.mkdir('.generation-proof', 0o700, dir_fd=data)
    os.fsync(data)
    proof_root = open_directory(os.path.join(sys.argv[2], '.generation-proof'))
    try:
        os.mkdir('genesis', 0o700, dir_fd=proof_root)
        os.fsync(proof_root)
    finally:
        os.close(proof_root)
    proof = open_directory(os.path.join(sys.argv[2], '.generation-proof/genesis'))
    source = os.open('genesis', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=data)
    for name in ('genesis.manifest', 'genesis-request.lxgb', 'genesis.registration',
                 '00000000000000000000.lxs', 'paxeer-registration-request.lxrr',
                 'paxeer-deployment-descriptor.lxgd'):
        original = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=source)
        target = None
        try:
            info = os.fstat(original)
            if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid()
                    or info.st_nlink != 1 or not 0 < info.st_size <= 64 * 1024 * 1024):
                raise ValueError('canonical generation artifact refused')
            target = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                             0o600, dir_fd=proof)
            copied = 0
            while True:
                block = os.read(original, 1024 * 1024)
                if not block:
                    break
                copied += len(block)
                if copied > info.st_size:
                    raise ValueError('canonical generation artifact changed')
                remaining = memoryview(block)
                while remaining:
                    count = os.write(target, remaining)
                    if count <= 0:
                        raise ValueError('generation proof write unavailable')
                    remaining = remaining[count:]
            if copied != info.st_size:
                raise ValueError('canonical generation artifact changed')
            os.fsync(target)
        finally:
            if target is not None:
                os.close(target)
            os.close(original)
    os.fsync(proof)
finally:
    if source is not None:
        os.close(source)
    if proof is not None:
        os.close(proof)
    os.close(data)
GENERATION_PROOF
fi
if [ -n "$GENERATION_TARGET_DIR" ] || [ "$GENERATION_TRANSPORT" = 1 ]; then
    printf 'LAYERX_NODE_GENESIS_GUARANTOR_PRODUCER_ROOT=%s/producer-generations\nLAYERX_NODE_CORE_ENV=%s/core.env\n' \
        "$ENV_DATA_DIR" "$ENV_DATA_DIR" >> "$DATA_DIR/node.env.tmp"
fi
chmod 0600 "$DATA_DIR/node.env.tmp"
mv "$DATA_DIR/node.env.tmp" "$DATA_DIR/node.env"
for ((index = 0; index < GUARANTOR_COUNT; index++)); do
    identity=$((index + 1))
    entry=${GUARANTOR_ENTRIES[index]}
    identity_id=${entry%% *}
    identity_key=${GUARANTOR_KEYS[$identity_id]}
    producer_dir="$(dirname "$DATA_DIR")/guarantor-$identity"
    if [ -n "$GENERATION_TARGET_DIR" ] || [ "$GENERATION_TRANSPORT" = 1 ]; then
        producer_dir="$DATA_DIR/producer-generations/guarantor-$identity"
    fi
    mkdir -p "$producer_dir/identity" "$producer_dir/state"
    chgrp "$LNI_GID" "$producer_dir" "$producer_dir/identity" "$producer_dir/state"
    chmod 0750 "$producer_dir" "$producer_dir/identity"
    chmod 2770 "$producer_dir/state"
    install -m 0440 "$identity_key" "$producer_dir/identity/key.pem"
    install -m 0440 "$SNAPSHOT" "$producer_dir/identity/genesis.lxs"
    install -m 0440 "$MANIFEST" "$producer_dir/identity/genesis.manifest"
    install -m 0440 "$IDENTITIES" "$producer_dir/identity/identities.txt"
    install -m 0440 "$DATA_DIR/sequencer.conf" "$producer_dir/identity/node.conf"
    if [ -r "$REGISTRATION" ]; then
        install -m 0440 "$REGISTRATION" "$producer_dir/identity/genesis.registration"
    else
        rm -f "$producer_dir/identity/genesis.registration"
    fi
    printf 'LAYERX_GUARANTOR_ID=%s\nLAYERX_NODE_NETWORK_ID=%s\nLAYERX_NODE_ASSET_ID=%s\nLAYERX_NODE_SEQUENCER_ID=%s\nLAYERX_NODE_SEQUENCER_PUBLIC_KEY=%s\nLAYERX_NODE_FIRST_BATCH=1\nLAYERX_NODE_LAST_BATCH=18446744073709551615\n' \
        "$identity_id" "$NETWORK_ID" "$ASSET_ID" "$SEQUENCER_ID" "$SEQUENCER_PUBLIC" > "$producer_dir/identity/producer.env.tmp"
    mv "$producer_dir/identity/producer.env.tmp" "$producer_dir/identity/producer.env"
    chgrp "$LNI_GID" "$producer_dir/identity/"*
    chmod 0440 "$producer_dir/identity/"*
    if [ "$GENERATION_TRANSPORT" = 1 ]; then
        capability_export="$(dirname "$ENV_DATA_DIR")/guarantor-$identity/identity"
        python3 - "$SCRIPT_DIR" "$GENERATION_AUTHORIZATION_DIR" "$capability_export" "$identity" "$LNI_GID" <<'CAPABILITY'
import hmac
import os
from pathlib import Path
import secrets
import stat
import sys

sys.path.insert(0, sys.argv[1])
from reset_state import open_directory

private, exported = map(Path, sys.argv[2:4])
slot, group = map(int, sys.argv[4:6])
if slot not in (1, 2):
    raise ValueError('generation identity slot outside transport bounds')
private_fd = open_directory(str(private), create=True)
root_fd = None
export_fd = None
try:
    name = 'slot-%d.cap' % slot
    try:
        descriptor = os.open(name, os.O_RDONLY | os.O_NOFOLLOW, dir_fd=private_fd)
    except FileNotFoundError:
        descriptor = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                             0o600, dir_fd=private_fd)
        try:
            value = secrets.token_bytes(32)
            if os.write(descriptor, value) != len(value):
                raise ValueError('incomplete generation capability write')
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
        os.fsync(private_fd)
        descriptor = os.open(name, os.O_RDONLY | os.O_NOFOLLOW, dir_fd=private_fd)
    try:
        info = os.fstat(descriptor)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid()
                or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1 or info.st_size != 32):
            raise ValueError('unsafe generation capability')
        value = os.read(descriptor, 33)
        if len(value) != 32:
            raise ValueError('invalid generation capability length')
    finally:
        os.close(descriptor)
    root_fd = os.open('/', os.O_RDONLY | os.O_DIRECTORY)
    for part in exported.parts[1:]:
        created = False
        try:
            os.mkdir(part, 0o750, dir_fd=root_fd)
            created = True
            os.fsync(root_fd)
        except FileExistsError:
            pass
        child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=root_fd)
        if created:
            os.fchown(child, -1, group)
            os.fchmod(child, 0o750)
            os.fsync(child)
        os.close(root_fd)
        root_fd = child
    info = os.fstat(root_fd)
    if info.st_uid != os.geteuid() or stat.S_IMODE(info.st_mode) != 0o750:
        raise ValueError('unsafe generation capability export directory')
    os.fchown(root_fd, -1, group)
    try:
        export_fd = os.open('generation.cap', os.O_RDONLY | os.O_NOFOLLOW, dir_fd=root_fd)
    except FileNotFoundError:
        export_fd = os.open('generation.cap', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                            0o440, dir_fd=root_fd)
        if os.write(export_fd, value) != len(value):
            raise ValueError('incomplete generation capability export')
        os.fchown(export_fd, -1, group)
        os.fchmod(export_fd, 0o440)
        os.fsync(export_fd)
        os.close(export_fd)
        export_fd = None
        os.fsync(root_fd)
        export_fd = os.open('generation.cap', os.O_RDONLY | os.O_NOFOLLOW, dir_fd=root_fd)
    info = os.fstat(export_fd)
    if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or info.st_gid != group
            or stat.S_IMODE(info.st_mode) != 0o440 or info.st_nlink != 1 or info.st_size != 32
            or not hmac.compare_digest(os.read(export_fd, 33), value)):
        raise ValueError('generation capability export differs from original admission')
finally:
    if export_fd is not None:
        os.close(export_fd)
    if root_fd is not None:
        os.close(root_fd)
    os.close(private_fd)
CAPABILITY
    fi
done
CORE_ENV_OUTPUT="$RUN_DIR/core.env"
if [ -n "$GENERATION_TARGET_DIR" ] || [ "$GENERATION_TRANSPORT" = 1 ]; then
    CORE_ENV_OUTPUT="$DATA_DIR/core.env"
fi
printf 'LAYERX_CORE_SEQUENCER_ID=%s\nLAYERX_CORE_TREASURY_ASSET=%s\n' \
    "$SEQUENCER_ID" "$ASSET_ID" > "$CORE_ENV_OUTPUT.tmp"
if [ -n "$TREASURY_SIGNER_SOCKET" ]; then
    printf 'LAYERX_CORE_TREASURY_SIGNER_SOCKET=%s\n' "$TREASURY_SIGNER_SOCKET" >> "$CORE_ENV_OUTPUT.tmp"
fi
chmod 0644 "$CORE_ENV_OUTPUT.tmp"
mv "$CORE_ENV_OUTPUT.tmp" "$CORE_ENV_OUTPUT"

cat > "$DATA_DIR/treasury.json" <<EOF
{"did":"$TREASURY_DID","public_key":"$TREASURY_PUBLIC","account":"$TREASURY_ACCOUNT","asset":"$ASSET_ID","genesis_balance":"$TREASURY_BALANCE","network_id":$NETWORK_ID}
EOF
chmod 0644 "$DATA_DIR/treasury.json"
rm -rf "$DATA_DIR/work"

printf 'bootstrap: network %s genesis state root %s\n' "$NETWORK_ID" "$GENESIS_STATE_ROOT"
printf 'bootstrap: treasury %s\n' "$TREASURY_ACCOUNT"
printf 'bootstrap: data %s run %s\n' "$DATA_DIR" "$RUN_DIR"
