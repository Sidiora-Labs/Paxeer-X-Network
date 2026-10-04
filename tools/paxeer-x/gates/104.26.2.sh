#!/usr/bin/env bash
set -euo pipefail
if (( $# != 0 )); then
    exit 2
fi
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)
exec python3 "$root/tools/qualification/paxeer-x/external_custody.py"
