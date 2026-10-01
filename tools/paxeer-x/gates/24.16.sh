#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
cd "$root"
case "${1:-verify}" in
build)
    : "${LAYERX_CUSTODY_ARTIFACT_MANIFEST:?private artifact manifest output required}"
    make -j5 LXP_REVISION="$(git rev-parse HEAD)" PAXEER_GO_JOBS=5 paxeer-build custody-proof-build build/tests/bridge/sign-credit
    cargo build --locked --manifest-path platform/Cargo.toml -p layerx-platform-paxeer-boundary --bin layerx-paxeer-boundary
    forge build contracts/GuarantorBond.sol contracts/CheckpointRegistry.sol \
        platform/hosted/paxeer/contracts/BetaUsdl.sol contracts/challenge/CheckpointChallengeManager.sol \
        contracts/governance/LayerXBetaTimelock.sol contracts/custody/AssetRegistry.sol \
        contracts/custody/LayerXVault.sol loadtest/contracts/evm/lib/solmate/src/tokens/WETH.sol \
        --threads 5 --out build/withdraw-contracts/artifacts --cache-path build/withdraw-contracts/cache
    python3 tests/daemon/custody-chain-contract.py --build-dir build --record-build
    ;;
verify)
    exec timeout 15m python3 tests/daemon/custody-chain-contract.py --build-dir build
    ;;
*) echo 'usage: 24.16.sh build|verify' >&2; exit 2 ;;
esac
