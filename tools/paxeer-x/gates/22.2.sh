#!/usr/bin/env bash
# Focused behavior gate for task 22.2: rollback restores canonical asset and
# account projections in the indexer's SQLite store.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
output=$(timeout 10m cargo test --locked --manifest-path platform/Cargo.toml -p layerx-indexer --test rollback_projection 2>&1)
code=$?
printf '%s\n' "$output"
tests=$(sed -n 's/^test result: [a-zA-Z]*\. \([0-9][0-9]*\) passed; \([0-9][0-9]*\) failed; .*/\1 \2/p' <<<"$output" | awk '{n += $1 + $2} END {print n + 0}')
skipped=$(sed -n 's/^test result: .* \([0-9][0-9]*\) ignored; .*/\1/p' <<<"$output" | awk '{n += $1} END {print n + 0}')
echo "PAXEER_X_GATE tests=${tests} skipped=${skipped}"
exit "$code"
