#!/usr/bin/env bash
# Focused behavior gate for task 18.3: relay archive freshness from pinned origin observations.
# paxeer-x-services: archive
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
output=$(timeout 20m python3 tools/qualification/paxeer-x/interop-archive-contract.py --case archive-origin-freshness --candidate-manifest "${PAXEER_X_CANDIDATE_MANIFEST:-}" 2>&1)
code=$?
printf "%s\n" "$output"
tests=$(grep -c "^PASS " <<<"$output")
echo "PAXEER_X_GATE tests=${tests} skipped=0"
exit "$code"
