#!/usr/bin/env bash
# paxeer-x-services: human-kms
# Focused gate for task 104.38.4: the production LXKP gateway terminates mutual
# TLS and relays to the real layerx-human-kms backend, and the retained LXKP
# contract and custody suites (the absent make human-test-custody) still pass.
set -uo pipefail
if (($#)); then
    echo "usage: $0 (no arguments)" >&2
    exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
export CARGO_TARGET_DIR=${CARGO_TARGET_DIR:-$PWD/human/target}
tests=0
skipped=0
code=0

suite() {
    local output status passed ignored
    output=$(timeout 15m sh tools/runtime/run-with-clock.sh cargo test --locked --manifest-path human/Cargo.toml "$@" 2>&1)
    status=$?
    printf '%s\n' "$output"
    passed=$(sed -n 's/^test result: [a-zA-Z]*\. \([0-9][0-9]*\) passed; \([0-9][0-9]*\) failed; .*/\1 \2/p' <<<"$output" | awk '{n += $1 + $2} END {print n + 0}')
    ignored=$(sed -n 's/^test result: .* \([0-9][0-9]*\) ignored; .*/\1/p' <<<"$output" | awk '{n += $1} END {print n + 0}')
    tests=$((tests + passed))
    skipped=$((skipped + ignored))
    if ((status != 0)); then
        code=$status
    elif ((passed == 0)); then
        echo "empty corpus: $*" >&2
        code=9
    fi
}

if ! timeout 15m cargo build --locked --manifest-path human/Cargo.toml \
    -p layerx-human-kms --bin layerx-human-kms \
    -p layerx-human-service --bin layerx-human-kms-gateway; then
    echo "PAXEER_X_GATE tests=0 skipped=0"
    exit 8
fi
export LAYERX_HUMAN_KMS_GATEWAY_TEST_BACKEND=$CARGO_TARGET_DIR/debug/layerx-human-kms
export LAYERX_HUMAN_KMS_GATEWAY_TEST_BINARY=$CARGO_TARGET_DIR/debug/layerx-human-kms-gateway
suite -p layerx-human-service --bin layerx-human-kms-gateway -- --include-ignored --test-threads=1
suite -p layerx-human-kms --test provider
suite -p layerx-human-service --test custody
echo "PAXEER_X_GATE tests=${tests} skipped=${skipped}"
exit "$code"
