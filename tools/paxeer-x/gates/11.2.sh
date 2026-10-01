#!/usr/bin/env bash
# Focused behavior gate for task 11.2: the identity provider assertion tests.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
output=$(timeout 15m tools/runtime/run-with-clock.sh sh -c 'cd human && cargo test -p layerx-human-identity-provider --test assertion' 2>&1)
code=$?
printf '%s\n' "$output"
summary=$(grep -E '^test result: ' <<<"$output" | tail -n 1)
passed=$(sed -n 's/.* \([0-9][0-9]*\) passed;.*/\1/p' <<<"$summary")
failed=$(sed -n 's/.* \([0-9][0-9]*\) failed;.*/\1/p' <<<"$summary")
ignored=$(sed -n 's/.* \([0-9][0-9]*\) ignored;.*/\1/p' <<<"$summary")
echo "PAXEER_X_GATE tests=$(( ${passed:-0} + ${failed:-0} )) skipped=${ignored:-0}"
exit "$code"
