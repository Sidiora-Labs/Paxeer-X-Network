#!/usr/bin/env bash
# Focused behavior gate for task 20.2: autonomous sponsored-transaction recovery
# of the served gas-station binary.
# paxeer-x-services: gas
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
output=$(timeout 20m python3 tools/qualification/paxeer-x/gas-station-contract.py --case station-autonomous-recovery --candidate-manifest "$PAXEER_X_CANDIDATE_MANIFEST" 2>&1)
code=$?
printf '%s\n' "$output"
executed=$(sed -n 's/^cases=[0-9][0-9]* executed=\([0-9][0-9]*\) .*/\1/p' <<<"$output" | tail -n 1)
echo "PAXEER_X_GATE tests=${executed:-0} skipped=0"
exit "$code"
