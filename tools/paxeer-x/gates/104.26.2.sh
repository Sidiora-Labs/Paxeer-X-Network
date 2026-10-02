#!/usr/bin/env bash
set -euo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
: "${PAXEER_X_EVIDENCE_DIR:?private evidence directory required}"
: "${PAXEER_X_EXTERNAL_CUSTODY_MANIFEST:?private external-custody build manifest required}"
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
exec python3 tools/qualification/paxeer-x/external_custody.py --verify
