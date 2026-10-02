#!/usr/bin/env bash
set -euo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
exec python3 tools/qualification/paxeer-x/program-storage.py \
    --manifest "${PAXEER_X_STORAGE_ARTIFACTS:?set the prebuilt storage artifact directory}/manifest.json" \
    --balance-evidence "${PAXEER_X_STORAGE_BALANCE_EVIDENCE:?set the credited current-candidate 104.31.2 evidence file}"
