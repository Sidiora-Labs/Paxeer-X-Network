#!/usr/bin/env bash
# Rebuilds pay1-retired-genesis.manifest with the genesis builder so its state
# root tracks the current genesis materialization. The signer is the
# deterministic test key 0x22 repeated, which test_genesis_builder_cli also uses
# to authorize the retired issuance migration of pay1-retired-issuance.lxs.
set -euo pipefail
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$repo_root"
make layerx-genesis-build >/dev/null
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
u16() { printf '%04x' "$1"; }
u32() { printf '%08x' "$1"; }
u64() { printf '%016x' "$1"; }
key32() { local hex; hex=$(printf '%s' "$1" | xxd -p | tr -d '\n'); printf '%s%0*d' "$hex" $((64 - ${#hex})) 0; }
asset=b5a32b12029f8ddfb905f90f280f664b46390de0fc62770fc197dd87b18cd898
record=0003${asset}0354535406020020000000000000000000000000280f664b46390de0fc62770fc197dd87b18cd898000d437573746f647920746f6b656e00000000000000000000000000000000fe4fc96e5b4c2a493ce05fbc952ce511cd558021e4842be0e576b37377abd1c9020000000000000000000000000000000040196a0232105c9be5c8b01016b9ce4d9665dcf53a21fcf816246635faa20d2d
schedule=0002$(printf '%0164d' 0)27100a$(printf '%0320d' 0)
{
    printf '%s' "$(printf 'LXGB' | xxd -p)02$(u16 3)$(u32 77)$(u64 1789229379000)"
    printf '%s' "$(u16 1)$(u16 7)$(key32 parameter-version)$(printf '%063d' 0)1"
    printf '%s' "$(u16 2)"
    printf '%s' 113c1c44731bb06e628a0f31981d846b1de02a50741174e9025ca43da4f7ad2b
    printf '%s' 035829af860e98240be544c313fe9c28f3b84a643f0514ff69aa41b3acaa68cf5c
    printf '%032d' 0
    printf '%s' 480937f89587adc116c3d8ee354b41c632fa491f1e9a049f8cb122b54b615b02
    printf '%s' 02bd21ab8487b1011558aa4a72e99fd008bbd8ace7499fb01470166e6873c62a71
    printf '%032d' 0
    printf '%s' "$asset$(u32 1)"
    for value in 1 1 1 1 1 8 8 64 8 1; do u64 "$value"; done
    printf '01%s' "$(u32 1)"
    for value in 1 1 2 4 1 1 100 100 1 1 10 1 1000; do u64 "$value"; done
    printf '%s' "$(u16 1)$(u16 $((${#record} / 2)))$record"
    printf '%s' "$(u16 $((${#schedule} / 2)))$schedule"
} | xxd -r -p >"$work/request.lxgb"
printf '22%.0s' $(seq 32) | xxd -r -p >"$work/signer.key"
chmod 600 "$work/request.lxgb" "$work/signer.key"
build/bin/layerx-genesis-build "$work/request.lxgb" "$work/signer.key" "$work/out"
cp "$work/out/genesis.manifest" tests/fixtures/pay1-retired-genesis.manifest
