#!/usr/bin/env bash
set -euo pipefail
# The state directory is setgid, a directory created under it inherits that bit, and an octal
# chmod leaves it set on a directory. The producer accepts a publication inputs directory of
# exactly 0700, so the special bits are cleared by name.
private_directory() {
    mkdir -p "$1"
    chmod u=rwx,go=,ug-s,-t "$1"
}
if [ "${1:-}" = --publication-inputs-dir ]; then
    [ "$#" = 2 ] || exit 2
    umask 077
    private_directory "$2"
    exit 0
fi
if [ "${1:-}" = --checkpoint-authority-public ]; then
    [ "$#" = 2 ] || exit 2
    exec python3 "$(dirname "$0")/checkpoint-authority.py" "$2"
fi
: "${LAYERX_GUARANTOR_IDENTITY_DIR:?identity directory is required}"
: "${LAYERX_GUARANTOR_LNI_SOCKET:?LNI socket is required}"
: "${LAYERX_GUARANTOR_STATE_DIR:?state directory is required}"
: "${LAYERX_GUARANTOR_SETTLEMENT_ENV:?settlement environment is required}"
: "${LAYERX_GUARANTOR_SUBMITTER_KEY_FILE:?submitter key is required}"
state_root=$LAYERX_GUARANTOR_STATE_DIR
submitter_source=$LAYERX_GUARANTOR_SUBMITTER_KEY_FILE
# Every batch that replays an owner balance, a withdrawal or a deposit publishes settlement evidence,
# and the producer refuses to publish it without the owner and checkpoint-authority signatures for
# that checkpoint. They arrive as <checkpoint-id>.json in the publication inputs directory, so that
# directory exists on every run and not only in the cluster: the cluster mounts the signing policy
# the producer then runs itself (cmd/layerx-guarantor/authorization.py against the treasury and
# recipient signer sockets), while an operator running the guarantor by hand either points
# LAYERX_GUARANTOR_PUBLICATION_AUTHORIZATION_FILE at their own copy of that policy or delivers the
# signed files into LAYERX_GUARANTOR_PUBLICATION_INPUTS_DIR themselves.
inputs_override=${LAYERX_GUARANTOR_PUBLICATION_INPUTS_DIR:-}
if [ -n "${LAYERX_GUARANTOR_PUBLICATION_AUTHORIZATION_FILE:-}" ] && \
   [ -z "${LAYERX_GUARANTOR_PUBLICATION_AUTHORIZATION_SOURCE:-}" ]; then
    [ -r "$LAYERX_GUARANTOR_PUBLICATION_AUTHORIZATION_FILE" ] || {
        echo "publication authorization policy is not readable: $LAYERX_GUARANTOR_PUBLICATION_AUTHORIZATION_FILE" >&2
        exit 2
    }
fi
# The producer settles through cmd/layerx-guarantor/settlement.py: certificates and checkpoints
# go to the anchor precompile's submitCheckpoint and finalize and their state is read back with
# statusOf, encoded by these signatures, so the ABI shipped from precompiles/layerxanchor must
# declare them before the producer starts.
guarantor_anchor_abi() {
    anchor_abi=${LAYERX_GUARANTOR_ANCHOR_ABI:-/opt/layerx/precompiles/layerxanchor/abi.json}
    [ -r "$anchor_abi" ] || { echo "anchor precompile ABI is not readable: $anchor_abi" >&2; exit 2; }
    for signature in 'submitCheckpoint(bytes,bytes,bytes)' 'finalize(uint64)' 'statusOf(uint64)'; do
        jq -e --arg signature "$signature" \
            'any(.[] | select(.type == "function") | .name + "(" + ([.inputs[].type] | join(",")) + ")"; . == $signature)' \
            "$anchor_abi" > /dev/null || { echo "anchor precompile ABI $anchor_abi does not declare $signature" >&2; exit 2; }
    done
}
guarantor_anchor_abi
guarantor_binary=${LAYERX_GUARANTOR_BINARY:-/usr/local/bin/layerx-guarantor}
[ -x "$guarantor_binary" ] || { echo "guarantor binary is not executable: $guarantor_binary" >&2; exit 2; }
child=""
stop_child() {
    if [ -n "$child" ]; then
        kill -TERM "$child" 2>/dev/null || true
        wait "$child" 2>/dev/null || true
        child=""
    fi
}
trap 'stop_child; exit 0' TERM INT
trap stop_child EXIT
while :; do
    producer_env="$LAYERX_GUARANTOR_IDENTITY_DIR/producer.env"
    manifest="$LAYERX_GUARANTOR_IDENTITY_DIR/genesis.manifest"
    registration="$LAYERX_GUARANTOR_IDENTITY_DIR/genesis.registration"
    if [ "${LAYERX_GENERATION_FD_MODE:-0}" = 1 ]; then
        producer_env=${LAYERX_GUARANTOR_PRODUCER_ENV_FILE:?generation producer environment is required}
        manifest=${LAYERX_NODE_GENESIS_MANIFEST:?generation manifest is required}
        registration=${LAYERX_NODE_GENESIS_REGISTRATION:?generation registration is required}
        : "${LAYERX_GUARANTOR_KEY_FILE:?generation identity key is required}"
        : "${LAYERX_NODE_SNAPSHOT:?generation snapshot is required}"
        : "${LAYERX_NODE_IDENTITIES:?generation identity inventory is required}"
        : "${LAYERX_GUARANTOR_NODE_CONFIG:?generation node config is required}"
    fi
    while [ ! -r "$producer_env" ] || \
          [ ! -r "$manifest" ] || \
          [ ! -r "$registration" ] || \
          [ ! -r "$LAYERX_GUARANTOR_SETTLEMENT_ENV" ] || \
          { [ -n "${LAYERX_GUARANTOR_PUBLICATION_AUTHORIZATION_SOURCE:-}" ] && \
            [ ! -r "$LAYERX_GUARANTOR_PUBLICATION_AUTHORIZATION_SOURCE" ]; } || \
          [ ! -S "$LAYERX_GUARANTOR_LNI_SOCKET" ]; do
        sleep 1
    done
    if [ "${LAYERX_GENERATION_FD_MODE:-0}" = 1 ]; then
        generation=$(stat -Lc %i "$producer_env")
    else
        generation=$(stat -c %i "$producer_env")
    fi
    settlement=$("$(dirname "$0")/bootstrap.sh" --check-settlement "$LAYERX_GUARANTOR_SETTLEMENT_ENV")
    set -a
    . "$producer_env"
    eval "$settlement"
    set +a
    [[ $LAYERX_GUARANTOR_ID =~ ^[0-9a-f]{64}$ ]]
    export LAYERX_GUARANTOR_STATE_DIR="$state_root/$LAYERX_GUARANTOR_ID"
    if [ "${LAYERX_GENERATION_FD_MODE:-0}" != 1 ]; then
        export LAYERX_GUARANTOR_KEY_FILE="$LAYERX_GUARANTOR_IDENTITY_DIR/key.pem"
        export LAYERX_NODE_SNAPSHOT="$LAYERX_GUARANTOR_IDENTITY_DIR/genesis.lxs"
        export LAYERX_NODE_GENESIS_MANIFEST="$LAYERX_GUARANTOR_IDENTITY_DIR/genesis.manifest"
        export LAYERX_NODE_GENESIS_REGISTRATION="$LAYERX_GUARANTOR_IDENTITY_DIR/genesis.registration"
        export LAYERX_NODE_IDENTITIES="$LAYERX_GUARANTOR_IDENTITY_DIR/identities.txt"
        export LAYERX_GUARANTOR_NODE_CONFIG="$LAYERX_GUARANTOR_IDENTITY_DIR/node.conf"
    fi
    umask 077
    mkdir -p "$LAYERX_GUARANTOR_STATE_DIR/signer"
    chmod 0700 "$LAYERX_GUARANTOR_STATE_DIR/signer"
    install -m 0600 "$submitter_source" "$LAYERX_GUARANTOR_STATE_DIR/signer/submitter.key"
    export LAYERX_GUARANTOR_SUBMITTER_KEY_FILE="$LAYERX_GUARANTOR_STATE_DIR/signer/submitter.key"
    export LAYERX_GUARANTOR_PUBLICATION_INPUTS_DIR="${inputs_override:-$LAYERX_GUARANTOR_STATE_DIR/publication-inputs}"
    private_directory "$LAYERX_GUARANTOR_PUBLICATION_INPUTS_DIR"
    if [ -n "${LAYERX_GUARANTOR_PUBLICATION_AUTHORIZATION_SOURCE:-}" ]; then
        install -m 0600 "$LAYERX_GUARANTOR_PUBLICATION_AUTHORIZATION_SOURCE" \
            "$LAYERX_GUARANTOR_STATE_DIR/signer/publication-authorization.json"
        export LAYERX_GUARANTOR_PUBLICATION_AUTHORIZATION_FILE="$LAYERX_GUARANTOR_STATE_DIR/signer/publication-authorization.json"
    fi
    if [ -n "${LAYERX_GUARANTOR_PUBLICATION_AUTHORIZATION_FILE:-}" ]; then
        export PYTHONPATH=${PYTHONPATH:-/opt/layerx:/opt/layerx/human}
    fi
    "$guarantor_binary" &
    child=$!
    current=$generation
    while kill -0 "$child" 2>/dev/null; do
        if [ "${LAYERX_GENERATION_FD_MODE:-0}" != 1 ]; then
            current=$(stat -c %i "$producer_env")
            [ "$current" = "$generation" ] || break
        fi
        sleep 1
    done
    if [ "$current" != "$generation" ]; then
        stop_child
        continue
    fi
    status=0
    wait "$child" || status=$?
    child=""
    exit "$status"
done
