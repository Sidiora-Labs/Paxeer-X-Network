#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
cd "$root"
case "${1:-verify}" in
verify) exec timeout 15m python3 platform/hosted/core/tests/receipt_retention.py ;;
*) echo 'usage: 24.18.sh verify' >&2; exit 2 ;;
esac
