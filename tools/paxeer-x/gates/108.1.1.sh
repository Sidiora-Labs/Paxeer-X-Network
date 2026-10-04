#!/usr/bin/env bash
# paxeer-x-services: kernel paxeer-boundaries
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)
: "${PAXEER_X_NODE_ARTIFACT_MANIFEST:?source-bound node artifact manifest required}"
: "${LAYERX_CUSTODY_ARTIFACT_MANIFEST:?source-bound custody artifact manifest required}"
: "${LAYERX_NODE_PAXEER_RPC_URL:?synced loopback Paxeer RPC required for the precompile settlement case}"
[[ $LAYERX_NODE_PAXEER_RPC_URL =~ ^http://127\.0\.0\.1:[1-9][0-9]{0,4}$ ]] || {
    printf '%s\n' '108.1.1: precompile settlement requires a loopback RPC' >&2
    exit 1
}
exec make -C "$ROOT" platform-test-node
