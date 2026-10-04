#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
if [[ "${1:-}" == "--recordings" ]]; then
    [[ $# -eq 3 ]]
    exec timeout 600 python3 "$ROOT/tests/daemon/finality-authority-chain.py" "$@"
fi
exec timeout 600 python3 "$ROOT/tests/daemon/finality-authority-chain.py" "${1:-$ROOT/build/tests/lxp_test_daemon_finality_authority}"
