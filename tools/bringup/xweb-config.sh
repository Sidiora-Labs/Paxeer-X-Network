#!/usr/bin/env bash
set -euo pipefail

# The host map, rpc_domain and repo_root are the probe's.
# shellcheck source=tools/bringup/check-live.sh
. "$(dirname "${BASH_SOURCE[0]}")/check-live.sh"

usage() {
	cat <<'EOF'
usage: tools/bringup/xweb-config.sh prices | render serve|attestor <directory>

The x-websearch configs of the bring-up. Reads the host map from
BRINGUP_HOSTS_FILE for the serving RPC names and never prints it; reads no
key and writes none: each receiver key is generated on its own host.

prices    prints one "<symbol> <asset id> <price>" line for SID, PAX, USDC and
          USDL, the price in the asset's base units for one request of 0.001
          US dollars: USDC and USDL 0.001 of their unit, PAX 0.001 divided by
          XWEB_PAX_USD_PRICE, SID that PAX amount times XWEB_SID_PER_PAX,
          each rounded up to a whole base unit.

render serve <directory>
          writes <directory>/<apiN name>.json for every serving RPC name that
          tools/bringup/search-front.sh names lists: listen 127.0.0.1:8482,
          gateway.endpoint the router's /rpc with the kernel's sequencer id
          and public key, the four assets with their prices, evm the node's
          loopback JSON-RPC on chain 125, kernel_network_id 125, peers the
          https names of the other serving RPC names, the crawl seeds of
          XWEB_SEEDS and no kernel block.

render attestor <directory>
          writes <directory>/attestor-<N>.json for the four Fly attestor apps
          of interop/deploy/x-websearch/attestor-<N>.toml: listen [::]:8480,
          data_dir /data/state, gateway.endpoint the router's /rpc with the
          kernel's sequencer id and public key, the four assets with their
          prices, evm the https name of a serving RPC name (attestor N takes
          the (N-1)th of the list, wrapping) on chain 125, kernel_network_id
          125, peers the loopback hops http://127.0.0.1:849<M> of the other
          three, the crawl seeds of XWEB_SEEDS and the kernel block: endpoint
          the router's /rpc, the program web request topic, submitter_did
          XWEB_ATTESTOR_<N>_RECEIVER_DID and fee_limit XWEB_KERNEL_FEE_LIMIT.
          Also needs:
  XWEB_ATTESTOR_1_RECEIVER_DID .. XWEB_ATTESTOR_4_RECEIVER_DID
                          the kernel DID each attestor's receiver key posts
                          observations as
  XWEB_KERNEL_FEE_LIMIT   the fee limit of each observation activity, a
                          decimal in base units of the fee asset

Inputs (all required, none printed but the ids and prices):
  XWEB_PAX_USD_PRICE      the owner's PAX price in US dollars, a decimal
  XWEB_ASSET_ID_SID, XWEB_ASSET_ID_PAX, XWEB_ASSET_ID_USDC, XWEB_ASSET_ID_USDL
                          the 64-hex asset ids the kernel genesis registered
  XWEB_DECIMALS_SID, XWEB_DECIMALS_PAX, XWEB_DECIMALS_USDC, XWEB_DECIMALS_USDL
                          the decimals of those genesis asset records
  XWEB_SEQUENCER_ID, XWEB_SEQUENCER_PUBLIC_KEY
                          the kernel's sequencer id and ed25519 public key, 64 hex
  XWEB_SEEDS              the space-separated https URLs the sidecar crawls from
  XWEB_SID_PER_PAX        SID per PAX, default the owner-set 3.114
  XWEB_GATEWAY_ENDPOINT   default https://api-mainnet-beta.paxeer.network/rpc
  XWEB_EVM_ENDPOINT       the node's loopback JSON-RPC, default http://127.0.0.1:8545

Exits 1 when a destination does not answer, 2 on a usage error or a missing
or malformed input.
EOF
}

xweb_symbols=(SID PAX USDC USDL)
xweb_gateway="${XWEB_GATEWAY_ENDPOINT:-https://api-mainnet-beta.paxeer.network/rpc}"
xweb_evm="${XWEB_EVM_ENDPOINT:-http://127.0.0.1:8545}"
xweb_sid_per_pax="${XWEB_SID_PER_PAX:-3.114}"

xweb_need() {
	if [ -z "${!1:-}" ]; then
		echo "xweb-config: $1 is unset" >&2
		exit 2
	fi
}

# xweb_prices: one "<symbol> <asset id> <price>" line per asset.
xweb_prices() {
	local sym
	xweb_need XWEB_PAX_USD_PRICE
	for sym in "${xweb_symbols[@]}"; do
		xweb_need "XWEB_ASSET_ID_$sym"
		xweb_need "XWEB_DECIMALS_$sym"
	done
	python3 - "$XWEB_PAX_USD_PRICE" "$xweb_sid_per_pax" \
		"$XWEB_ASSET_ID_SID" "$XWEB_DECIMALS_SID" "$XWEB_ASSET_ID_PAX" "$XWEB_DECIMALS_PAX" \
		"$XWEB_ASSET_ID_USDC" "$XWEB_DECIMALS_USDC" "$XWEB_ASSET_ID_USDL" "$XWEB_DECIMALS_USDL" <<'PY'
import re
import sys
from decimal import ROUND_CEILING, Decimal, InvalidOperation, getcontext

getcontext().prec = 80
fail = lambda what: (print(f"xweb-config: {what} is malformed", file=sys.stderr), sys.exit(2))
try:
    pax_usd, sid_per_pax = Decimal(sys.argv[1]), Decimal(sys.argv[2])
except InvalidOperation:
    fail("XWEB_PAX_USD_PRICE or XWEB_SID_PER_PAX")
if not pax_usd.is_finite() or pax_usd <= 0:
    fail("XWEB_PAX_USD_PRICE")
if not sid_per_pax.is_finite() or sid_per_pax <= 0:
    fail("XWEB_SID_PER_PAX")
usd = Decimal("0.001")
pax = usd / pax_usd
units = {"SID": pax * sid_per_pax, "PAX": pax, "USDC": usd, "USDL": usd}
args = sys.argv[3:]
for i, sym in enumerate(("SID", "PAX", "USDC", "USDL")):
    asset, decimals = args[2 * i].lower().removeprefix("0x"), args[2 * i + 1]
    if not re.fullmatch(r"[0-9a-f]{64}", asset) or len(set(asset)) == 1:
        fail(f"XWEB_ASSET_ID_{sym}")
    if not re.fullmatch(r"[0-9]|[1-3][0-9]", decimals) or int(decimals) > 38:
        fail(f"XWEB_DECIMALS_{sym}")
    price = (units[sym] * (Decimal(10) ** int(decimals))).to_integral_value(ROUND_CEILING)
    print(sym, asset, int(max(price, 1)))
PY
}

# xweb_render_serve <directory>: the serving config of every serving RPC name.
xweb_render_serve() {
	local dir="$1" listing prices name
	local -a names=()
	xweb_need XWEB_SEQUENCER_ID
	xweb_need XWEB_SEQUENCER_PUBLIC_KEY
	xweb_need XWEB_SEEDS
	prices="$(xweb_prices)"
	listing="$("$(dirname "${BASH_SOURCE[0]}")/search-front.sh" names)" || return 1
	mapfile -t names < <(sed -n 's/^serve //p' <<<"$listing")
	if [ "${#names[@]}" -eq 0 ]; then
		echo "xweb-config: no serving RPC name" >&2
		return 1
	fi
	mkdir -p "$dir"
	for name in "${names[@]}"; do
		python3 - "$name" "$xweb_gateway" "$XWEB_SEQUENCER_ID" "$XWEB_SEQUENCER_PUBLIC_KEY" "$xweb_evm" "$prices" "$XWEB_SEEDS" "${names[@]}" >"$dir/$name.json" <<'PY'
import json
import re
import sys

name, gateway, seq_id, seq_key, evm, prices, seeds, *names = sys.argv[1:]
for label, value in (("XWEB_SEQUENCER_ID", seq_id), ("XWEB_SEQUENCER_PUBLIC_KEY", seq_key)):
    if not re.fullmatch(r"[0-9a-f]{64}", value.lower().removeprefix("0x")):
        print(f"xweb-config: {label} is malformed", file=sys.stderr)
        sys.exit(2)
assets = {}
for line in prices.splitlines():
    sym, asset, price = line.split()
    assets[sym] = {"asset_id": asset, "price": price}
config = {
    "listen": "127.0.0.1:8482",
    "data_dir": "/var/lib/x-websearch",
    "seeds": seeds.split(),
    "crawl": {"pages_per_cycle": 1000, "pages_per_host": 100, "max_depth": 3, "politeness_delay_ms": 1000},
    "fetch": {"connect_timeout_ms": 3000, "total_timeout_ms": 10000, "max_body_bytes": 2097152, "max_redirects": 3, "allow_loopback": False},
    "assets": assets,
    "gateway": {
        "endpoint": gateway,
        "sequencer_id": seq_id.lower().removeprefix("0x"),
        "sequencer_public_key": seq_key.lower().removeprefix("0x"),
    },
    "evm": {"endpoint": evm, "chain_id": 125, "confirmations": 12},
    "kernel_network_id": 125,
    "peers": [f"https://{peer}" for peer in names if peer != name],
}
print(json.dumps(config, indent=2))
PY
		echo "rendered $name"
	done
}

# xweb_render_attestor <directory>: the config of each of the four Fly
# attestor apps, their peers reached through the machine's loopback hops.
xweb_render_attestor() {
	local dir="$1" listing prices n did
	local -a names=() dids=()
	xweb_need XWEB_SEQUENCER_ID
	xweb_need XWEB_SEQUENCER_PUBLIC_KEY
	xweb_need XWEB_SEEDS
	xweb_need XWEB_KERNEL_FEE_LIMIT
	for n in 1 2 3 4; do
		did="XWEB_ATTESTOR_${n}_RECEIVER_DID"
		xweb_need "$did"
		dids+=("${!did}")
	done
	prices="$(xweb_prices)"
	listing="$("$(dirname "${BASH_SOURCE[0]}")/search-front.sh" names)" || return 1
	mapfile -t names < <(sed -n 's/^serve //p' <<<"$listing")
	if [ "${#names[@]}" -eq 0 ]; then
		echo "xweb-config: no serving RPC name" >&2
		return 1
	fi
	mkdir -p "$dir"
	python3 - "$dir" "$xweb_gateway" "$XWEB_SEQUENCER_ID" "$XWEB_SEQUENCER_PUBLIC_KEY" "$prices" "$XWEB_SEEDS" "$XWEB_KERNEL_FEE_LIMIT" "${dids[@]}" "${names[@]}" <<'PY'
import json
import re
import sys

out, gateway, seq_id, seq_key, prices, seeds, fee_limit, *rest = sys.argv[1:]
dids, names = rest[:4], rest[4:]
fail = lambda label: (print(f"xweb-config: {label} is malformed", file=sys.stderr), sys.exit(2))
for label, value in (("XWEB_SEQUENCER_ID", seq_id), ("XWEB_SEQUENCER_PUBLIC_KEY", seq_key)):
    if not re.fullmatch(r"[0-9a-f]{64}", value.lower().removeprefix("0x")):
        fail(label)
if not re.fullmatch(r"[1-9][0-9]{0,37}", fee_limit):
    fail("XWEB_KERNEL_FEE_LIMIT")
for n, did in enumerate(dids, 1):
    if not re.fullmatch(r"did:[a-z0-9._-]+(:[a-z0-9._-]+)+", did) or ":asset:" in did or len(did) > 255:
        fail(f"XWEB_ATTESTOR_{n}_RECEIVER_DID")
assets = {}
for line in prices.splitlines():
    sym, asset, price = line.split()
    assets[sym] = {"asset_id": asset, "price": price}
for n in range(1, 5):
    config = {
        "listen": "[::]:8480",
        "data_dir": "/data/state",
        "seeds": seeds.split(),
        "crawl": {"pages_per_cycle": 1000, "pages_per_host": 100, "max_depth": 3, "politeness_delay_ms": 1000},
        "fetch": {"connect_timeout_ms": 3000, "total_timeout_ms": 10000, "max_body_bytes": 2097152, "max_redirects": 3, "allow_loopback": False},
        "assets": assets,
        "gateway": {
            "endpoint": gateway,
            "sequencer_id": seq_id.lower().removeprefix("0x"),
            "sequencer_public_key": seq_key.lower().removeprefix("0x"),
        },
        "evm": {"endpoint": f"https://{names[(n - 1) % len(names)]}", "chain_id": 125, "confirmations": 12},
        "kernel_network_id": 125,
        "kernel": {
            "endpoint": gateway,
            "poll_interval_ms": 1000,
            "topics": ["PAXEERX_WEB_REQUEST_V1"],
            "submitter_did": dids[n - 1],
            "fee_limit": fee_limit,
        },
        "peers": [f"http://127.0.0.1:{8490 + m}" for m in range(1, 5) if m != n],
    }
    with open(f"{out}/attestor-{n}.json", "w") as handle:
        print(json.dumps(config, indent=2), file=handle)
    print(f"rendered attestor-{n}")
PY
}

mode="${1:-}"
case "$mode" in
-h | --help)
	usage
	exit 0
	;;
prices)
	[ "$#" -eq 1 ] || {
		usage >&2
		exit 2
	}
	xweb_prices
	;;
render)
	# One renderer per sidecar kind: serve on the RPC nodes, attestor on
	# the four Fly attestor apps.
	case "${2:-}" in
	attestor)
		[ "$#" -eq 3 ] || {
			usage >&2
			exit 2
		}
		load_hosts
		xweb_render_attestor "$3"
		;;
	serve)
		[ "$#" -eq 3 ] || {
			usage >&2
			exit 2
		}
		load_hosts
		xweb_render_serve "$3"
		;;
	*)
		usage >&2
		exit 2
		;;
	esac
	;;
*)
	usage >&2
	exit 2
	;;
esac
