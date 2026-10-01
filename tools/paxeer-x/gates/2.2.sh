#!/usr/bin/env bash
# Focused behavior gate for task 2.2: recovered finality against authenticated historical membership.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
output=$(PYTHONDONTWRITEBYTECODE=1 timeout 1800s python3 tests/daemon/paxeer_x_historical_finality.py 2>&1)
code=$?
printf '%s\n' "$output"
tests=$(grep -cE ' (passed|refused)$' <<<"$output")
echo "PAXEER_X_GATE tests=${tests:-0} skipped=0"
exit "$code"
