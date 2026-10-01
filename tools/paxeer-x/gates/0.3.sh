#!/usr/bin/env bash
# Focused behavior gate for task 0.3: deterministic spec parser, validator and renderer.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
output=$(timeout 240s go test -count=1 -v ./tools/specgen 2>&1)
code=$?
printf '%s\n' "$output"
if [ "$code" -eq 0 ]; then
  timeout 60s go run ./tools/specgen -root . -check
  code=$?
fi
tests=$(grep -c '^--- PASS: ' <<<"$output")
skipped=$(grep -c '^--- SKIP: ' <<<"$output")
echo "PAXEER_X_GATE tests=${tests} skipped=${skipped}"
exit "$code"
