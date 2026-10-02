#!/usr/bin/env bash
set -euo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
exec python3 tools/qualification/paxeer-x/deterministic-execution-producer.py \
    --phase gate \
    --manifest "${PAXEER_X_DETERMINISM_ARTIFACTS:?set the prebuilt artifact directory}/manifest.json" \
    --output "${PAXEER_X_DETERMINISM_RESULTS:?set a fresh private result directory}" \
    --repository "${PAXEER_X_GITHUB_REPOSITORY:?set the candidate GitHub repository}" \
    --run-id "${PAXEER_X_DETERMINISM_RUN_ID:?set the genuine five-platform workflow run ID}"
