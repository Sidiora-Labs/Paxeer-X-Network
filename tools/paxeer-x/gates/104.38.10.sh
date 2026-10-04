#!/usr/bin/env bash
set -euo pipefail
[[ $# == 0 ]] || exit 2
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)
cd "$root"
exec python3 scripts/qualification/paxeer-x/program-event-bounds.py verify --manifest "${PAXEER_X_EVENT_BOUNDS_ARTIFACTS:?set source-bound event bounds artifacts}/manifest.json"
