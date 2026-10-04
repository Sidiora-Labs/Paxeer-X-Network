#!/usr/bin/env bash
set -euo pipefail
if (($#)); then
    printf 'usage: %s (no arguments)\n' "$0" >&2
    exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
: "${PAXEER_X_REFERENCE_APP_ARTIFACTS:?set the genuine prebuilt reference artifact manifest}"
: "${PAXEER_X_EVIDENCE_DIR:?set the private qualification evidence directory}"
exec node platform/examples/qualify-reference-apps.mjs
