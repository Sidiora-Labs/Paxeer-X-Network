#!/usr/bin/env bash
set -euo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
exec python3 tools/qualification/paxeer-x/program-bindings.py \
    --manifest "${PAXEER_X_BINDINGS_ARTIFACTS:?set the prebuilt bindings artifact directory}/manifest.json"
