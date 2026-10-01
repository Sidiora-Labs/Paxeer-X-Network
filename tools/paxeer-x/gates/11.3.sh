#!/usr/bin/env bash
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
output=$(timeout 15m cargo test --manifest-path platform/Cargo.toml -p layerx-platform-identity --test service session_capacity_excludes_expired_after_restart -- --exact 2>&1)
code=$?
printf '%s\n' "$output"
counts=$(sed -n 's/^test result: .*\. \([0-9][0-9]*\) passed; \([0-9][0-9]*\) failed; \([0-9][0-9]*\) ignored;.*/\1 \2 \3/p' <<<"$output" | tail -n 1)
read -r passed failed skipped <<<"${counts:-0 0 0}"
tests=$((passed + failed))
printf 'PAXEER_X_GATE tests=%s skipped=%s\n' "$tests" "$skipped"
if (( code == 0 && (tests == 0 || skipped != 0) )); then
    exit 1
fi
exit "$code"
