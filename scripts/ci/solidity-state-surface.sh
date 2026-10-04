#!/bin/sh
set -eu

contracts='CheckpointRegistry
GuarantorBond
AssetRegistry
LayerXVault
CheckpointChallengeManager
WithdrawalNullifierRegistry
WithdrawalClaims
EmergencyExit
ReserveReconciler
LayerXTimelock
Blueprint
ManagerContainer
ManagerMigrator
LayerXCustody'

surface_file=$(mktemp)
trap 'rm -f -- "$surface_file"' EXIT HUP INT TERM

for contract in $contracts; do
    forge inspect "$contract" abi --json | jq -r --arg contract "$contract" '
        .[]
        | select(.type == "function")
        | select(.stateMutability != "view" and .stateMutability != "pure")
        | $contract + "." + .name + "(" + ([.inputs[].type] | join(",")) + ")"
    ' >>"$surface_file"
done

LC_ALL=C sort -u "$surface_file" -o "$surface_file"
if [ ! -s "$surface_file" ]; then
    echo "contract state surface is empty" >&2
    exit 1
fi

missing=0
while IFS= read -r surface; do
    method=${surface#*.}
    method=${method%%(*}
    if ! rg --glob '*.t.sol' -q "\\b${method}\\b" test; then
        echo "untested external state-changing function: $surface" >&2
        missing=1
    fi
done <"$surface_file"

if [ "$missing" -ne 0 ]; then
    exit 1
fi

count=$(wc -l <"$surface_file" | tr -d ' ')
echo "solidity state surface: $count selectors have test evidence"
