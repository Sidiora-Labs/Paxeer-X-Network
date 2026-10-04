#!/usr/bin/env bash
set -euo pipefail
umask 077
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
cd "$root"
case "${1:-verify}" in
build)
    : "${LAYERX_RESET_NATIVE_ARTIFACT_MANIFEST:?private native manifest output required}"
    bash tools/paxeer-x/gates/24.18.sh build
    python3 platform/hosted/node/tests/reset_recovery.py --record-native "$LAYERX_RESET_NATIVE_ARTIFACT_MANIFEST"
    ;;
verify) exec timeout 15m python3 tools/qualification/paxeer-x/supervisor-reset-recovery.py ;;
*) echo 'usage: 24.19.sh build|verify' >&2; exit 2 ;;
esac
