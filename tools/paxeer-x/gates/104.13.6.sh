#!/usr/bin/env bash
set -euo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)
exec python3 "$root/tools/qualification/paxeer-x/human-api-boundary.py" \
    --manifest "${PAXEER_X_HUMAN_API_MANIFEST:?set the private production boundary manifest}"
