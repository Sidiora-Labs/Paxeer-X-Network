#!/usr/bin/env bash
# Focused behavior gate for task 0.5: versioned boundary contract vectors.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
output=$(timeout 10m python3 tools/paxeer-x/tests/versioned_boundaries.py 2>&1)
code=$?
printf '%s\n' "$output"
tests=$(sed -n 's/^VECTORS tests=\([0-9][0-9]*\) failures=.*/\1/p' <<<"$output" | tail -n 1)
echo "PAXEER_X_GATE tests=${tests:-0} skipped=0"
exit "$code"
