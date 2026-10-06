#!/usr/bin/env bash
# Render the governance window kit with fixed inputs and assert every proposal
# exists and passes the paxd offline dry run. No network.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
command -v go >/dev/null || PATH=$PATH:/usr/local/go/bin
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

PAXD=${PAXD:-}
if [ -z "$PAXD" ]; then
  (cd "$ROOT" && go build -tags netgo -o "$WORK/paxd" ./daemon/paxd)
  PAXD=$WORK/paxd
fi

OUT=$WORK/out
"$ROOT/tools/paxeer-x/gov/render.sh" --out "$OUT" --paxd "$PAXD" \
  --height 27000000 \
  --release-url https://example.invalid/paxd-v6.11.0-linux-amd64 \
  --release-sha256 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef \
  --deposit 10000000uhpx --pax-usd 0.02 \
  --pax-asset-id 1111111111111111111111111111111111111111111111111111111111111111 \
  --sid-asset-id 2222222222222222222222222222222222222222222222222222222222222222 \
  --market-pax-sid 3333333333333333333333333333333333333333333333333333333333333333 \
  --market-btc-usd 4444444444444444444444444444444444444444444444444444444444444444 \
  --market-eth-usd 5555555555555555555555555555555555555555555555555555555555555555 \
  --custody-network-id 125 \
  --deposit-authority d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a \
  --sequencer-id 6666666666666666666666666666666666666666666666666666666666666666 \
  --sequencer-pubkey 3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c \
  --first-batch 1 \
  --bridge-config "$ROOT/bridge/deploy/proposals/testdata/solana.json" \
  --bridge-manifest "$ROOT/bridge/deploy/proposals/testdata/attestors.json"

fail() { echo "test.sh: $*" >&2; exit 1; }
for f in 01-software-upgrade 02-fee-token-params 03-xweb-fee 04-exchange-markets 05-custody \
  bridge/solana/proposals/04-proposal-open-chain bridge/solana/proposals/05-proposal-sidiora-cap; do
  [ -s "$OUT/$f.json" ] || fail "$f.json missing"
  [ -s "$OUT/$f.json.tx.json" ] || fail "$f.json was not dry-run"
  jq -e '.body.messages[0]["@type"] == "/cosmos.gov.v1beta1.MsgSubmitProposal"' "$OUT/$f.json.tx.json" >/dev/null \
    || fail "$f.json.tx.json does not carry a MsgSubmitProposal"
done
[ -s "$OUT/06-anchor-sequencer-authorization.json" ] || fail "anchor calldata missing"

jq -e '.name == "v6.11" and .height == "27000000" and (.info | fromjson | .binaries["linux/amd64"] | endswith("?checksum=sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"))' \
  "$OUT/01-software-upgrade.json" >/dev/null || fail "upgrade plan"
jq -e '.changes == [
  {subspace: "evm", key: "KeyFeeTokenEnabled", value: true},
  {subspace: "evm", key: "KeyAllowedFeeDenoms", value: [{denom: "usid", rate: "3114000", rate_update_height: "27000000"}]},
  {subspace: "evm", key: "KeyMaxFeeTokenRateAge", value: "3400000"}]' "$OUT/02-fee-token-params.json" >/dev/null || fail "fee-token params"
jq -e '.messages[0].fee == "50000"' "$OUT/03-xweb-fee.json" >/dev/null || fail "xweb fee is not 0.001 USD at 0.02 USD/PAX"
jq -e '[.messages[] | select(.["@type"] == "/paxprotocol.paxchain.layerxexchange.MsgSetMarket")] | length == 3' \
  "$OUT/04-exchange-markets.json" >/dev/null || fail "exchange markets"
jq -e '.messages[2].params.deposit_root_authority == "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"
  and ([.messages[0:2][].asset.denom] == ["uhpx", "usid"])' "$OUT/05-custody.json" >/dev/null || fail "custody proposal"
jq -e '[.[] | select(.type == "function" and .name == "setSequencerAuthorization") | .inputs[].type] == ["bytes32", "bytes32", "uint64", "uint64"]' \
  "$ROOT/precompiles/layerxanchor/abi.json" >/dev/null || fail "anchor ABI changed"
jq -e '.data == "0xc2661383" + ("6" * 64) + "3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c"
  + ("0" * 63) + "1" + ("0" * 48) + ("f" * 16)' "$OUT/06-anchor-sequencer-authorization.json" >/dev/null || fail "anchor calldata"
echo "governance window kit: ok"
