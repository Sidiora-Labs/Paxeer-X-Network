#!/usr/bin/env bash
# Focused behavior gate for task 21.1: the vault's canonical recipient tests.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
output=$(timeout 10m forge test --root bridge/evm --match-test test_DepositCanonicalRecipient 2>&1)
code=$?
printf '%s\n' "$output"
summary=$(grep -E 'tests? passed, [0-9]+ failed, [0-9]+ skipped' <<<"$output" | tail -n 1)
passed=$(sed -n 's/.*: \([0-9][0-9]*\) tests\{0,1\} passed.*/\1/p' <<<"$summary")
failed=$(sed -n 's/.* \([0-9][0-9]*\) failed.*/\1/p' <<<"$summary")
skipped=$(sed -n 's/.* \([0-9][0-9]*\) skipped.*/\1/p' <<<"$summary")
echo "PAXEER_X_GATE tests=$(( ${passed:-0} + ${failed:-0} )) skipped=${skipped:-0}"
exit "$code"
