#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
exec python3 tools/bringup/tests/foundation-artifacts.py --build "$@"
