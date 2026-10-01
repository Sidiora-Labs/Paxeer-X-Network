#!/usr/bin/env bash
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
output=$(timeout 25m go test ./node/ -run 'Activation|Upgrade' -count=1 -v 2>&1)
code=$?
printf '%s\n' "$output"
tests=$(grep -cE '^[[:space:]]*--- (PASS|FAIL): ' <<<"$output")
skipped=$(grep -cE '^[[:space:]]*--- SKIP: ' <<<"$output")
echo "PAXEER_X_GATE tests=${tests} skipped=${skipped}"
exit "$code"
