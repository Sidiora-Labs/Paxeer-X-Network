#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)
exec python3 "${root}/scripts/paxeer-x/qualify-scan-load.py" "$@"
