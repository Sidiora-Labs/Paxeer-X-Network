#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
cd "$root"
[[ $# -eq 0 ]] || { echo '103.6.1 accepts no selector arguments' >&2; exit 2; }
exec make --no-print-directory platform-test-node-runtime
