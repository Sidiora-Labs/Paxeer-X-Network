#!/usr/bin/env bash
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
output=$(timeout 10m python3 tests/ci/ci-recipe-contract.py 2>&1)
code=$?
printf '%s\n' "$output"
tests=$(sed -n 's/^Ran \([0-9][0-9]*\) tests\{0,1\} in .*/\1/p' <<<"$output" | tail -n 1)
skipped=$(sed -n 's/.*skipped=\([0-9][0-9]*\).*/\1/p' <<<"$output" | tail -n 1)
echo "PAXEER_X_GATE tests=${tests:-0} skipped=${skipped:-0}"
exit "$code"
