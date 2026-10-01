#!/usr/bin/env bash
# Focused behavior gate for task 17.1: the webhook-ingress-roles case of the
# event delivery contract against the real layerx-webhooks binary.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
output=$(timeout 20m python3 tools/qualification/paxeer-x/event-delivery-contract.py --case webhook-ingress-roles --candidate-manifest "$PAXEER_X_CANDIDATE_MANIFEST" 2>&1)
code=$?
printf '%s\n' "$output"
tests=$(grep -c '^PASS ' <<<"$output")
failures=$(grep -c '^\(FAIL\|MISSING\) ' <<<"$output")
result=$(sed -n 's/^RESULT case=webhook-ingress-roles assertions=\([0-9][0-9]*\) failures=\([0-9][0-9]*\)$/\1 \2/p' <<<"$output" | tail -n 1)
if [ "$result" != "$((tests + failures)) $failures" ]; then
	echo "event delivery contract reported no consistent result" >&2
	[ "$code" -ne 0 ] || code=1
fi
echo "PAXEER_X_GATE tests=$((tests + failures)) skipped=0"
exit "$code"
