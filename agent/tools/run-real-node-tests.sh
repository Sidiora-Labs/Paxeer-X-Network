#!/bin/sh
set -eu

case "${1-}" in
    test) test_target=agent-test ;;
    sanitizers) test_target=agent-test-sanitize ;;
    prepare) test_target= ;;
    *) echo "expected test, sanitizers or prepare" >&2; exit 2 ;;
esac
if [ "$#" -ne 1 ] || [ "$(id -u)" -ne 0 ]; then
    echo "real-node tests require root to launch the daemon under its distinct uid" >&2
    exit 2
fi

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"
ownership_helper=$repo_root/scripts/ci/restore-build-ownership.py
checkout_owner=$(python3 "$ownership_helper" --repo "$repo_root" --capture-owner)
native_build_output=${LAYERX_TEST_NATIVE_BUILD_DIR:-${BUILD_DIR:-$repo_root/build}}
cargo_build_output=${CARGO_TARGET_DIR:-$repo_root/.lane-target}
python3 "$ownership_helper" --repo "$repo_root" --owner "$checkout_owner" \
    --output "$native_build_output" --output "$cargo_build_output" \
    --output "$repo_root/build" --validate-only
restore_build_ownership() {
    primary_exit=$?
    trap - 0 HUP INT TERM
    set +e
    python3 "$ownership_helper" --repo "$repo_root" --owner "$checkout_owner" \
        --output "$native_build_output" --output "$cargo_build_output" \
        --output "$repo_root/build"
    ownership_exit=$?
    if [ "$primary_exit" -ne 0 ]; then
        exit "$primary_exit"
    fi
    exit "$ownership_exit"
}
trap restore_build_ownership 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
export CARGO_BUILD_JOBS=4
export TMPDIR=/tmp
for executable in forge cast anvil go; do
    command -v "$executable" >/dev/null
done
python3 -c "from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey; from Crypto.Hash import keccak; from eth_account import Account; from eth_utils import to_checksum_address"
if [ -n "$test_target" ]; then
    make "$test_target"
    exit 0
fi

: "${CARGO_TARGET_DIR:?the build output directory of the agent-test path, exported by the Makefile variable AGENT_TEST_TARGET_DIR}"
export LAYERX_TEST_NATIVE_BUILD_DIR=${LAYERX_TEST_NATIVE_BUILD_DIR:-$repo_root/build}
make -j4 CC=gcc BUILD_DIR="$LAYERX_TEST_NATIVE_BUILD_DIR" \
    LXP_REVISION="$(git rev-parse HEAD)" \
    PROGRAMS_TARGET_DIR="$CARGO_TARGET_DIR" \
    PROGRAMS_RUNTIME_LIB="$CARGO_TARGET_DIR/debug/liblayerx_programs_sandbox.a" \
    "$LAYERX_TEST_NATIVE_BUILD_DIR/bin/layerxd" \
    "$LAYERX_TEST_NATIVE_BUILD_DIR/bin/layerx-genesis-build" \
    "$LAYERX_TEST_NATIVE_BUILD_DIR/tests/lxp_test_daemon_finality_authority" \
    "$LAYERX_TEST_NATIVE_BUILD_DIR/tests/lxp_test_program_admission" \
    "$LAYERX_TEST_NATIVE_BUILD_DIR/tests/lxp_test_metered_allowance" \
    "$LAYERX_TEST_NATIVE_BUILD_DIR/tests/bridge/sign-credit"
make BUILD_DIR="$LAYERX_TEST_NATIVE_BUILD_DIR" PAXEER_GO_JOBS=4 custody-proof-build
GOMAXPROCS=4 GOFLAGS="${GOFLAGS:-} -p=4" make paxeer-build

export LAYERX_TEST_NATIVE_BIN_DIR=$LAYERX_TEST_NATIVE_BUILD_DIR/bin
export LAYERX_CUSTODY_PROOF_BIN=$LAYERX_TEST_NATIVE_BIN_DIR/layerx-custody-proof
export PAXD=$repo_root/build/paxd
sha256sum "$LAYERX_TEST_NATIVE_BIN_DIR/layerxd" \
    "$LAYERX_TEST_NATIVE_BIN_DIR/layerx-genesis-build" "$PAXD"
