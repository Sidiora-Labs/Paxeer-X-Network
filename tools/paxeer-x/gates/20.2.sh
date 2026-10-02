#!/usr/bin/env bash
set -euo pipefail
umask 077
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
cd "$root"
: "${PAXEER_X_CANDIDATE_MANIFEST:?candidate manifest required}"
: "${PAXEER_X_STATION_RECOVERY_MATERIAL:?source-bound recovery material required}"
: "${PAXEER_X_EVIDENCE_DIR:?private evidence directory required}"
export PYTHONDONTWRITEBYTECODE=1
exec timeout 20m python3 tools/qualification/paxeer-x/gas-station-contract.py --case station-autonomous-recovery --candidate-manifest "$PAXEER_X_CANDIDATE_MANIFEST"
