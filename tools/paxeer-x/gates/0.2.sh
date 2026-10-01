#!/usr/bin/env bash
# Focused behavior gate for task 0.2: the dispatcher and release runner tests.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
output=$(timeout 5m python3 tools/paxeer-x/tests/gate_dispatch_test.py 2>&1)
code=$?
printf '%s\n' "$output"
tests=$(sed -n 's/^Ran \([0-9][0-9]*\) tests\{0,1\} in .*/\1/p' <<<"$output" | tail -n 1)
skipped=$(sed -n 's/.*skipped=\([0-9][0-9]*\).*/\1/p' <<<"$output" | tail -n 1)
echo "PAXEER_X_GATE tests=${tests:-0} skipped=${skipped:-0}"
exit "$code"
