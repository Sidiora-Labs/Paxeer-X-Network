#!/usr/bin/env bash
# Focused behavior gate for task 6.3: interface-bearing native lifecycle across guest ABIs.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
output=$(timeout 30m python3 tools/qualification/paxeer-x/programs_native_interfaces.py 2>&1)
code=$?
printf '%s\n' "$output"
tests=$(sed -n 's/^cases=\([0-9][0-9]*\) passed=[0-9]* skipped=0$/\1/p' <<<"$output" | tail -n 1)
echo "PAXEER_X_GATE tests=${tests:-0} skipped=0"
exit "$code"
