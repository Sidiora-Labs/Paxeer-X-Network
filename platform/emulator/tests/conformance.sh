#!/bin/sh
set -eu
emulator_url=${1:?emulator URL is required}
testnet_url=${2:?testnet URL is required}
corpus=${3:?closed conformance corpus is required}
: "${PAXEER_X_CANDIDATE_MANIFEST:?published candidate manifest is required}"
root=$(CDPATH= cd -- "$(dirname -- "$0")/../../.." && pwd)
exec python3 "$root/tools/qualification/paxeer-x/registry-contract.py" \
    --case emulator-conformance --candidate-manifest "$PAXEER_X_CANDIDATE_MANIFEST" \
    --corpus "$corpus" --emulator-url "$emulator_url" --hosted-url "$testnet_url"
