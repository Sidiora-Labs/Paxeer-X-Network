#!/usr/bin/env bash
# Render the first post-upgrade governance window into an output directory and
# dry-run every proposal through paxd (--generate-only --offline), failing on
# any rejection.
set -euo pipefail

usage() {
  cat <<'USAGE'
usage: render.sh --out DIR --height H --release-url URL --release-sha256 HEX
                 --deposit COINS --pax-usd PRICE
                 --pax-asset-id HEX64 --sid-asset-id HEX64
                 --market-pax-sid HEX64 --market-btc-usd HEX64 --market-eth-usd HEX64
                 --custody-network-id N --deposit-authority HEX64
                 --sequencer-id HEX64 --sequencer-pubkey HEX64
                 [--perp-margin pax|sid] [--rate-update-height H]
                 [--sid-pointer 0xADDR] [--first-batch N] [--last-batch N]
                 [--bridge-config FILE]... [--bridge-manifest FILE]
                 [--paxd BIN] [--from BECH32] [--chain-id ID]
USAGE
  exit 2
}

ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
TEMPLATES="$ROOT/tools/paxeer-x/gov/templates"
GOV_AUTHORITY=pax10d07y265gmmuvt4z0w9aw880jnsr700jxdwa9m
CHAIN_ID=hyperpax_125-1
FROM=$GOV_AUTHORITY
PAXD=${PAXD:-paxd}
PERP_MARGIN=pax
FIRST_BATCH=0
LAST_BATCH=18446744073709551615
SID_POINTER=""
BRIDGE_MANIFEST="$ROOT/bridge/deploy/attestors.json"
BRIDGE_CONFIGS=()
OUT="" HEIGHT="" RELEASE_URL="" RELEASE_SHA256="" DEPOSIT="" PAX_USD="" RATE_UPDATE_HEIGHT=""
PAX_ASSET_ID="" SID_ASSET_ID="" MARKET_PAX_SID="" MARKET_BTC_USD="" MARKET_ETH_USD=""
CUSTODY_NETWORK_ID="" DEPOSIT_AUTHORITY="" SEQUENCER_ID="" SEQUENCER_PUBKEY=""

while [ $# -gt 0 ]; do
  [ $# -ge 2 ] || usage
  case "$1" in
    --out) OUT=$2 ;;
    --height) HEIGHT=$2 ;;
    --release-url) RELEASE_URL=$2 ;;
    --release-sha256) RELEASE_SHA256=$2 ;;
    --deposit) DEPOSIT=$2 ;;
    --pax-usd) PAX_USD=$2 ;;
    --rate-update-height) RATE_UPDATE_HEIGHT=$2 ;;
    --pax-asset-id) PAX_ASSET_ID=$2 ;;
    --sid-asset-id) SID_ASSET_ID=$2 ;;
    --sid-pointer) SID_POINTER=$2 ;;
    --market-pax-sid) MARKET_PAX_SID=$2 ;;
    --market-btc-usd) MARKET_BTC_USD=$2 ;;
    --market-eth-usd) MARKET_ETH_USD=$2 ;;
    --perp-margin) PERP_MARGIN=$2 ;;
    --custody-network-id) CUSTODY_NETWORK_ID=$2 ;;
    --deposit-authority) DEPOSIT_AUTHORITY=$2 ;;
    --sequencer-id) SEQUENCER_ID=$2 ;;
    --sequencer-pubkey) SEQUENCER_PUBKEY=$2 ;;
    --first-batch) FIRST_BATCH=$2 ;;
    --last-batch) LAST_BATCH=$2 ;;
    --bridge-config) BRIDGE_CONFIGS+=("$2") ;;
    --bridge-manifest) BRIDGE_MANIFEST=$2 ;;
    --paxd) PAXD=$2 ;;
    --from) FROM=$2 ;;
    --chain-id) CHAIN_ID=$2 ;;
    *) usage ;;
  esac
  shift 2
done

die() { echo "render.sh: $*" >&2; exit 1; }
for name in OUT HEIGHT RELEASE_URL RELEASE_SHA256 DEPOSIT PAX_USD PAX_ASSET_ID SID_ASSET_ID \
  MARKET_PAX_SID MARKET_BTC_USD MARKET_ETH_USD CUSTODY_NETWORK_ID DEPOSIT_AUTHORITY SEQUENCER_ID SEQUENCER_PUBKEY; do
  [ -n "${!name}" ] || die "--$(echo "$name" | tr 'A-Z_' 'a-z-') is required"
done
RATE_UPDATE_HEIGHT=${RATE_UPDATE_HEIGHT:-$HEIGHT}
[[ $HEIGHT =~ ^[1-9][0-9]*$ ]] || die "--height $HEIGHT is not a positive integer"
[[ $RATE_UPDATE_HEIGHT =~ ^[1-9][0-9]*$ ]] || die "--rate-update-height $RATE_UPDATE_HEIGHT is not a positive integer"
[[ $CUSTODY_NETWORK_ID =~ ^[1-9][0-9]*$ ]] || die "--custody-network-id $CUSTODY_NETWORK_ID is not a positive integer"
[[ $FIRST_BATCH =~ ^[0-9]+$ && $LAST_BATCH =~ ^[0-9]+$ ]] || die "batch numbers must be unsigned integers"
[[ $RELEASE_SHA256 =~ ^[0-9a-f]{64}$ ]] || die "--release-sha256 is not 64 lowercase hex characters"
for name in SEQUENCER_ID SEQUENCER_PUBKEY; do
  [[ ${!name} =~ ^[0-9a-f]{64}$ ]] || die "--$(echo "$name" | tr 'A-Z_' 'a-z-') is not 64 lowercase hex characters"
done
case "$PERP_MARGIN" in
  pax) PERP_MARGIN_ASSET_ID=$PAX_ASSET_ID ;;
  sid) PERP_MARGIN_ASSET_ID=$SID_ASSET_ID ;;
  *) die "--perp-margin must be pax or sid" ;;
esac
OUT=$(realpath -m "$OUT")
BRIDGE_MANIFEST=$(realpath "$BRIDGE_MANIFEST")
for i in "${!BRIDGE_CONFIGS[@]}"; do BRIDGE_CONFIGS[$i]=$(realpath "${BRIDGE_CONFIGS[$i]}"); done
command -v "$PAXD" >/dev/null || die "paxd binary $PAXD not found (pass --paxd or PAXD)"

# 0.001 USD in uhpx (1 PAX = 10^6 uhpx), rounded half up.
XWEB_FEE=$(python3 - "$PAX_USD" <<'PY'
import sys
from decimal import Decimal, ROUND_HALF_UP, InvalidOperation
try:
    price = Decimal(sys.argv[1])
except InvalidOperation:
    sys.exit("--pax-usd %r is not a decimal" % sys.argv[1])
if not price.is_finite() or price <= 0:
    sys.exit("--pax-usd must be positive")
fee = (Decimal("0.001") / price * 1000000).quantize(Decimal(1), rounding=ROUND_HALF_UP)
if fee < 1:
    sys.exit("--pax-usd %s prices 0.001 USD below one uhpx" % price)
print(fee)
PY
)

# setSequencerAuthorization(bytes32,bytes32,uint64,uint64), selector 0xc2661383.
ANCHOR_CALLDATA=$(python3 - "$SEQUENCER_ID" "$SEQUENCER_PUBKEY" "$FIRST_BATCH" "$LAST_BATCH" <<'PY'
import sys
seq, key, first, last = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
if int(seq, 16) == 0 or int(key, 16) == 0:
    sys.exit("sequencer id and public key must be nonzero")
for n in (first, last):
    if n >= 1 << 64:
        sys.exit("batch number %d exceeds uint64" % n)
if first > last:
    sys.exit("first batch %d is after last batch %d" % (first, last))
print("0xc2661383" + seq + key + "%064x" % first + "%064x" % last)
PY
)

UPGRADE_INFO=$(jq -cn --arg url "$RELEASE_URL" --arg sha "$RELEASE_SHA256" \
  '{binaries: {"linux/amd64": ($url + "?checksum=sha256:" + $sha)}}')

VARS=$(jq -n \
  --arg HEIGHT "$HEIGHT" --arg UPGRADE_INFO "$UPGRADE_INFO" --arg DEPOSIT "$DEPOSIT" \
  --arg FEE_TOKEN_RATE "3114000" --arg RATE_UPDATE_HEIGHT "$RATE_UPDATE_HEIGHT" \
  --arg MAX_FEE_TOKEN_RATE_AGE "3400000" --arg GOV_AUTHORITY "$GOV_AUTHORITY" \
  --arg XWEB_FEE "$XWEB_FEE" --argjson XWEB_MAX_PAYLOAD_BYTES 8192 \
  --arg XWEB_MAX_CALLBACK_GAS "500000" --arg XWEB_TIMEOUT_BLOCKS "3600" \
  --arg MARKET_PAX_SID "$MARKET_PAX_SID" --arg MARKET_BTC_USD "$MARKET_BTC_USD" \
  --arg MARKET_ETH_USD "$MARKET_ETH_USD" --arg PERP_MARGIN_ASSET_ID "$PERP_MARGIN_ASSET_ID" \
  --arg PAX_ASSET_ID "$PAX_ASSET_ID" --arg SID_ASSET_ID "$SID_ASSET_ID" --arg SID_POINTER "$SID_POINTER" \
  --arg PAX_MINIMUM_DEPOSIT "1000000" --arg PAX_CUSTODY_CAP "" \
  --arg SID_MINIMUM_DEPOSIT "1000000" --arg SID_CUSTODY_CAP "" \
  --argjson CUSTODY_NETWORK_ID "$CUSTODY_NETWORK_ID" \
  --arg WITHDRAWAL_DELAY_SECONDS "3600" --arg FORCED_EXIT_DELAY_SECONDS "0" \
  --arg LIVENESS_BOUND_SECONDS "86400" --arg DEPOSIT_AUTHORITY "$DEPOSIT_AUTHORITY" \
  --arg SEQUENCER_ID "$SEQUENCER_ID" --arg SEQUENCER_PUBKEY "$SEQUENCER_PUBKEY" \
  --arg FIRST_BATCH "$FIRST_BATCH" --arg LAST_BATCH "$LAST_BATCH" --arg ANCHOR_CALLDATA "$ANCHOR_CALLDATA" \
  '$ARGS.named')

mkdir -p "$OUT"
HOME_DIR=$(mktemp -d)
trap 'rm -rf "$HOME_DIR"' EXIT

for template in "$TEMPLATES"/*.json; do
  jq --argjson v "$VARS" '
    walk(if type == "string" and test("^@[A-Z0-9_]+@$")
         then (.[1:-1]) as $k | if ($v | has($k)) then $v[$k] else error("unset placeholder \($k)") end
         else . end)' "$template" >"$OUT/$(basename "$template")"
done

dry_run() {
  local file=$1; shift
  if ! "$PAXD" "$@" --generate-only --offline --chain-id "$CHAIN_ID" --from "$FROM" \
      --home "$HOME_DIR" --output json >"$file.tx.json" 2>"$file.err"; then
    cat "$file.err" >&2
    die "paxd refused $(basename "$file")"
  fi
  jq -e '.body.messages | length == 1' "$file.tx.json" >/dev/null || die "no transaction for $(basename "$file")"
  rm -f "$file.err"
  echo "validated $(basename "$file")"
}

up="$OUT/01-software-upgrade.json"
dry_run "$up" tx gov submit-proposal software-upgrade "$(jq -r .name "$up")" \
  --upgrade-height "$(jq -r .height "$up")" --upgrade-info "$(jq -r .info "$up")" \
  --title "$(jq -r .title "$up")" --description "$(jq -r .description "$up")" --deposit "$(jq -r .deposit "$up")"
dry_run "$OUT/02-fee-token-params.json" tx gov submit-proposal param-change "$OUT/02-fee-token-params.json"
dry_run "$OUT/03-xweb-fee.json" tx gov submit-proposal layerx-proposal "$OUT/03-xweb-fee.json" --deposit "$DEPOSIT"
dry_run "$OUT/04-exchange-markets.json" tx gov submit-proposal layerx-proposal "$OUT/04-exchange-markets.json" --deposit "$DEPOSIT"
dry_run "$OUT/05-custody.json" tx layerxcustody submit-proposal custody "$OUT/05-custody.json"

anchor="$OUT/06-anchor-sequencer-authorization.json"
jq -e '(.data | test("^0xc2661383[0-9a-f]{256}$")) and .selector == "0xc2661383"' "$anchor" >/dev/null \
  || die "anchor calldata is malformed"
echo "validated $(basename "$anchor")"

for config in "${BRIDGE_CONFIGS[@]}"; do
  name=$(jq -r .name "$config")
  dir="$OUT/bridge/$name"
  rm -rf "$dir"
  (cd "$ROOT" && go run ./bridge/deploy/proposals/cmd/paxeer-bridge-proposals -manifest "$BRIDGE_MANIFEST" \
    -proposals "$dir/proposals" "$config" "$dir/bodies") >/dev/null
  for file in "$dir"/proposals/04-proposal-open-chain.json "$dir"/proposals/05-proposal-sidiora-cap.json; do
    [ -f "$file" ] || continue
    dry_run "$file" tx gov submit-proposal layerxbridge-proposal "$file" --deposit "$DEPOSIT"
  done
done
