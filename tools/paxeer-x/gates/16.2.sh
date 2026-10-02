#!/usr/bin/env bash
# Focused behavior gate for task 16.2: durable source-verification idempotency.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
output=$(timeout 20m python3 tools/qualification/paxeer-x/registry-contract.py --case durable-verification-idempotency --candidate-manifest "$PAXEER_X_CANDIDATE_MANIFEST" 2>&1)
code=$?
printf '%s\n' "$output"
tests=$(sed -n 's/^registry-contract: case durable-verification-idempotency cases=\([0-9][0-9]*\) passed=\1$/\1/p' <<<"$output" | tail -n 1)
echo "PAXEER_X_GATE tests=${tests:-0} skipped=0"
exit "$code"
