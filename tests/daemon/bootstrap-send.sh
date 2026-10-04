#!/usr/bin/env bash
set -euo pipefail
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$repo_root"
[[ $(id -u) == 0 ]] || { echo 'bootstrap-send requires root for the distinct daemon uid' >&2; exit 1; }
native_bin=${LAYERX_TEST_NATIVE_BIN_DIR:-$repo_root/build/bin}
[[ -x "$native_bin/layerxd" && -x "$native_bin/layerx-genesis-build" ]] || {
    echo 'build layerxd and layerx-genesis-build before bootstrap-send' >&2
    exit 1
}
cargo_command=${PLATFORM_CARGO:-cargo}
export LAYERX_TEST_NATIVE_BIN_DIR="$native_bin"
for log_mode in absent empty; do
    echo "bootstrap-send: genesis, replica, sequencer, signed SEND and receipt proofs; $log_mode logs"
    if [[ -n ${LAYERX_TEST_AUTHORITY_REAL_NODE_EXECUTABLE:-} ]]; then
        [[ -x $LAYERX_TEST_AUTHORITY_REAL_NODE_EXECUTABLE ]] || {
            echo 'source-bound real_node executable is not executable' >&2
            exit 1
        }
        LAYERX_TEST_BOOTSTRAP_LOG_MODE=$log_mode "$LAYERX_TEST_AUTHORITY_REAL_NODE_EXECUTABLE" \
            real_node_authority_serves_verified_facts_and_reflects_replica_loss \
            --exact --nocapture --test-threads=1
        continue
    fi
    LAYERX_TEST_BOOTSTRAP_LOG_MODE=$log_mode "$cargo_command" test \
        --offline --manifest-path platform/Cargo.toml --locked \
        -p layerx-platform-authority --test real_node \
        real_node_authority_serves_verified_facts_and_reflects_replica_loss \
        -- --exact --nocapture --test-threads=1
done
