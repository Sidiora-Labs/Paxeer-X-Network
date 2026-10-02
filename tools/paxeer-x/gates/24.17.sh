#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
cd "$root"
exec timeout 15m python3 platform/hosted/node/tests/reset_recovery.py
