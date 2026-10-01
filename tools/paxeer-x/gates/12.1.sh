#!/usr/bin/env bash
# Focused behavior gate for task 12.1: the real Human KMS provider tests,
# including the one-time primary export transition.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
output=$(timeout 15m tools/runtime/run-with-clock.sh sh -c 'cd human && RUST_TEST_THREADS=1 cargo test -p layerx-human-kms --test provider' 2>&1)
code=$?
printf '%s\n' "$output"
counts=$(sed -n 's/^test result: [a-zA-Z]*\. \([0-9][0-9]*\) passed; \([0-9][0-9]*\) failed; \([0-9][0-9]*\) ignored;.*/\1 \2 \3/p' <<<"$output" | tail -n 1)
read -r passed failed ignored <<<"${counts:-0 0 0}"
echo "PAXEER_X_GATE tests=$((passed + failed)) skipped=${ignored}"
exit "$code"
