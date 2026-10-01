#!/usr/bin/env bash
# Focused behavior gate for task 3.2: metered accrual bounded by stream lifetime.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
output=$(timeout 1800s python3 tests/modules/paxeer_x_metered_end_boundary.py 2>&1)
code=$?
printf '%s\n' "$output"
tests=$(sed -n 's/^cases=\([0-9][0-9]*\)$/\1/p' <<<"$output" | tail -n 1)
echo "PAXEER_X_GATE tests=${tests:-0} skipped=0"
exit "$code"
