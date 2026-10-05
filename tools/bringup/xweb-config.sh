#!/usr/bin/env bash
set -euo pipefail

# The host map, rpc_domain and repo_root are the probe's.
# shellcheck source=tools/bringup/check-live.sh
. "$(dirname "${BASH_SOURCE[0]}")/check-live.sh"

usage() {
	cat <<'EOF'
usage: tools/bringup/xweb-config.sh prices | render serve|attestor <directory> | inventory | register

The x-websearch configs of the bring-up. Reads the host map from
BRINGUP_HOSTS_FILE for the serving RPC names and never prints it; prints no
key and writes none: each key is generated on its own host.

prices    prints one "<symbol> <asset id> <price>" line for SID, PAX, USDC and
          USDL, the price in the asset's base units for one request at the
          approved 0.001 US dollars and 13.44 US dollars per PAX: PAX exactly
          1/13440, SID that PAX amount times XWEB_SID_PER_PAX, USDC and USDL
          1/1000 of their unit, each rounded up to a whole base unit of the
          asset's decimals. Every asset id and its decimals must match the
          record the gateway's lx_getAsset reports: that symbol, those
          decimals, not paused, an authenticated committed snapshot.

inventory prints, one line each, the read-backs the paid search and web
          attestation settle through, and one "input <NAME> absent" line for
          every input it lacks; never invents an amount or an authority:
            receiver <serving name> payTo=<account>  the receiver each serving
                     RPC name's unpaid /search challenge offers, priced at the
                     approved prices
            payer    XWEB_PAYER_ACCOUNT's balance of XWEB_PAYER_ASSET against
                     XWEB_PAYER_MIN_BALANCE
            kernel-did attestor-<N>  XWEB_ATTESTOR_<N>_RECEIVER_DID registered
                     (lx_getAccount), the submitter and payout DID
            signer attestor-<N>  XWEB_ATTESTOR_<N>_SIGNER a member of the
                     precompile's getAttestors, the set holding exactly the
                     four, the threshold XWEB_ATTESTOR_THRESHOLD and a majority
            submitter attestor-<N>  XWEB_ATTESTOR_<N>_SUBMITTER's balance on
                     XWEB_INVENTORY_EVM_ENDPOINT against XWEB_SUBMITTER_MIN_WEI
          Exits 1 on any failed read-back or absent input.

register  registers the Ed25519 key of XWEB_REGISTER_KEY_FILE (32 hex bytes
          as the sidecar reads it, a regular file readable by its owner only,
          never printed) with the gateway's lx_register, signing the beta
          tenant binding, and checks the answer names exactly that key, the
          beta tenant and the subject the key derives; registering a key
          again answers the same principal. Prints the public key and subject.

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
  XWEB_PAX_USD_PRICE      when set, must be the approved 13.44

Exits 1 when a destination does not answer, 2 on a usage error or a missing
or malformed input, or an input the registered asset records contradict.
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

# xweb_rpc_py: the JSON-RPC read every Python step below shares, a POST of
# one call to an endpoint bounded by XWEB_TIMEOUT seconds; status 1 when the
# destination does not answer, a reply's error is returned as an error.
xweb_rpc_py='
import json
import os
import urllib.error
import urllib.request

def rpc(endpoint, method, params):
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode()
    request = urllib.request.Request(endpoint, body, {"content-type": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=int(os.environ.get("XWEB_TIMEOUT", "30"))) as reply:
            answer = json.loads(reply.read(1 << 20))
    except (OSError, ValueError, urllib.error.URLError):
        return None, "unreachable"
    if not isinstance(answer, dict) or answer.get("id") != 1:
        return None, "malformed"
    if "error" in answer:
        return None, "error=%s" % ((answer.get("error") or {}).get("code", "none"))
    return answer.get("result"), None
'

# xweb_prices: one "<symbol> <asset id> <price>" line per asset.
xweb_prices() {
	local sym
	for sym in "${xweb_symbols[@]}"; do
		xweb_need "XWEB_ASSET_ID_$sym"
		xweb_need "XWEB_DECIMALS_$sym"
	done
	python3 -c "$xweb_rpc_py"'
import re
import sys
from decimal import Decimal, InvalidOperation
from fractions import Fraction

fail = lambda what, code=2: (print(f"xweb-config: {what}", file=sys.stderr), sys.exit(code))
USD_PER_REQUEST = Fraction(1, 1000)
USD_PER_PAX = Fraction(1344, 100)
PAX_PER_REQUEST = USD_PER_REQUEST / USD_PER_PAX
assert PAX_PER_REQUEST == Fraction(1, 13440)
gateway, pax_usd, sid_per_pax, *args = sys.argv[1:]
if pax_usd:
    try:
        if Fraction(Decimal(pax_usd)) != USD_PER_PAX:
            fail("XWEB_PAX_USD_PRICE differs from the approved 13.44")
    except (InvalidOperation, ValueError, OverflowError):
        fail("XWEB_PAX_USD_PRICE is malformed")
try:
    sid = Fraction(Decimal(sid_per_pax))
except (InvalidOperation, ValueError, OverflowError):
    fail("XWEB_SID_PER_PAX is malformed")
if sid <= 0:
    fail("XWEB_SID_PER_PAX is malformed")
units = {"SID": PAX_PER_REQUEST * sid, "PAX": PAX_PER_REQUEST, "USDC": USD_PER_REQUEST, "USDL": USD_PER_REQUEST}
rows = []
for i, sym in enumerate(("SID", "PAX", "USDC", "USDL")):
    asset, decimals = args[2 * i].lower().removeprefix("0x"), args[2 * i + 1]
    if not re.fullmatch(r"[0-9a-f]{64}", asset) or len(set(asset)) == 1:
        fail(f"XWEB_ASSET_ID_{sym} is malformed")
    if not re.fullmatch(r"[0-9]|[1-3][0-9]", decimals) or int(decimals) > 38:
        fail(f"XWEB_DECIMALS_{sym} is malformed")
    result, error = rpc(gateway, "lx_getAsset", [asset])
    if error:
        fail(f"lx_getAsset {sym} {error}", 1)
    try:
        record = result["asset"]
        committed = result["verification"] == "authenticated_committed_snapshot"
        registered = (record["asset_id"], record["symbol"], record["decimals"], record["paused"])
    except (KeyError, TypeError):
        fail(f"lx_getAsset {sym} answer is malformed", 1)
    if not committed or registered[0] != asset:
        fail(f"lx_getAsset {sym} is not an authenticated committed record of that id", 1)
    if registered[1] != sym:
        fail(f"XWEB_ASSET_ID_{sym} is the registered asset {registered[1]}")
    if type(registered[2]) is not int or registered[2] != int(decimals):
        fail(f"XWEB_DECIMALS_{sym} differs from the registered decimals {registered[2]}")
    if registered[3] is not False:
        fail(f"XWEB_ASSET_ID_{sym} is a paused asset")
    scaled = units[sym] * 10 ** int(decimals)
    rows.append((sym, asset, max(1, -(-scaled.numerator // scaled.denominator))))
for sym, asset, price in rows:
    print(sym, asset, price)
' "$xweb_gateway" "${XWEB_PAX_USD_PRICE:-}" "$xweb_sid_per_pax" \
		"$XWEB_ASSET_ID_SID" "$XWEB_DECIMALS_SID" "$XWEB_ASSET_ID_PAX" "$XWEB_DECIMALS_PAX" \
		"$XWEB_ASSET_ID_USDC" "$XWEB_DECIMALS_USDC" "$XWEB_ASSET_ID_USDL" "$XWEB_DECIMALS_USDL"
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

# xweb_inventory: the read-backs of the receivers, the payer, the kernel
# submitter and payout DIDs, the web signers and the EVM submitters, one line
# each, and an "input <NAME> absent" line per input it lacks.
xweb_inventory() {
	local listing prices="" sym id decimals ready=1
	local -a names=()
	for sym in "${xweb_symbols[@]}"; do
		id="XWEB_ASSET_ID_$sym"
		decimals="XWEB_DECIMALS_$sym"
		[ -n "${!id:-}" ] && [ -n "${!decimals:-}" ] || ready=0
	done
	if [ "$ready" -eq 1 ]; then
		prices="$(xweb_prices)"
	fi
	listing="$("$(dirname "${BASH_SOURCE[0]}")/search-front.sh" names)" || return 1
	mapfile -t names < <(sed -n 's/^serve //p' <<<"$listing")
	python3 -c "$xweb_rpc_py"'
import base64
import json
import os
import re
import sys
import urllib.error
import urllib.request

gateway, prices, *names = sys.argv[1:]
env = os.environ.get
failures = 0
def line(ok, text):
    global failures
    failures += 0 if ok else 1
    print(("pass " if ok else "fail ") + text)
def need(name, pattern):
    global failures
    value = env(name, "")
    if not value:
        failures += 1
        print("input " + name + " absent")
        return None
    if not re.fullmatch(pattern, value):
        print("xweb-config: " + name + " is malformed", file=sys.stderr)
        sys.exit(2)
    return value
priced = {}
for row in prices.splitlines():
    sym, asset, price = row.split()
    priced[asset] = (sym, price)
if not priced:
    for sym in ("SID", "PAX", "USDC", "USDL"):
        need("XWEB_ASSET_ID_" + sym, r"(0x)?[0-9a-fA-F]{64}")
        need("XWEB_DECIMALS_" + sym, r"[0-9]{1,2}")
payer_did = need("XWEB_PAYER_DID", r"did:[a-z0-9._:-]+")
if not names:
    line(False, "receiver serving=0")
for name in names:
    if not payer_did:
        break
    request = urllib.request.Request("https://" + name + "/search?q=paxeer", headers={"LAYERX-PAYER-DID": payer_did})
    status, header = None, None
    try:
        with urllib.request.urlopen(request, timeout=int(env("XWEB_TIMEOUT", "30"))) as reply:
            status = reply.status
    except urllib.error.HTTPError as error:
        status, header = error.code, error.headers.get("payment-required")
    except OSError:
        pass
    try:
        offers = json.loads(base64.b64decode(header or "", validate=True))["accepts"]
        receivers = {offer["payTo"] for offer in offers}
        amounts = {(offer["asset"], offer["amount"]) for offer in offers}
    except (ValueError, KeyError, TypeError):
        line(False, "receiver https://%s/search http=%s offers=none" % (name, status or "none"))
        continue
    approved = {(asset, price) for asset, (_, price) in priced.items()}
    ok = status == 402 and len(receivers) == 1 and len(offers) == 8 and (not priced or amounts == approved)
    line(ok, "receiver https://%s/search http=402 payTo=%s offers=%d priced=%s" % (name, ",".join(sorted(receivers)), len(offers),
         "unchecked" if not priced else "approved" if amounts == approved else "differs"))
account = need("XWEB_PAYER_ACCOUNT", r"[0-9a-f]{64}")
payer_sym = need("XWEB_PAYER_ASSET", r"SID|PAX|USDC|USDL")
minimum = need("XWEB_PAYER_MIN_BALANCE", r"[1-9][0-9]{0,37}")
def balance_line(label, account, sym, minimum):
    result, error = rpc(gateway, "lx_getBalance", [account])
    want = next((asset for asset, (name, _) in priced.items() if name == sym), None)
    if error or not isinstance(result, dict):
        line(False, "%s account=%s balance=%s" % (label, account, error or "malformed"))
        return
    balance = str(result.get("balance", ""))
    ok = re.fullmatch(r"0|[1-9][0-9]*", balance) is not None and (want is None or result.get("asset_id") == want)
    ok = ok and minimum is not None and int(balance) >= int(minimum)
    line(ok, "%s account=%s asset=%s balance=%s minimum=%s" % (label, account, sym, balance or "none", minimum or "absent"))
if account and payer_sym and minimum:
    balance_line("payer", account, payer_sym, minimum)
fee_sym = need("XWEB_KERNEL_FEE_ASSET", r"SID|PAX|USDC|USDL")
for n in range(1, 5):
    did = need("XWEB_ATTESTOR_%d_RECEIVER_DID" % n, r"did:[a-z0-9._:-]+")
    if did:
        result, error = rpc(gateway, "lx_getSequence", [did, "identity"])
        sequence = (result or {}).get("next_sequence") if isinstance(result, dict) else None
        line(error is None and isinstance(sequence, str) and re.fullmatch(r"0|[1-9][0-9]*", sequence) is not None,
             "kernel-did attestor-%d did=%s next_sequence=%s" % (n, did, sequence if isinstance(sequence, str) else error or "none"))
    payout = need("XWEB_ATTESTOR_%d_PAYOUT_ACCOUNT" % n, r"[0-9a-f]{64}")
    if payout and fee_sym:
        balance_line("payout attestor-%d" % n, payout, fee_sym, "0")
evm = need("XWEB_INVENTORY_EVM_ENDPOINT", r"https?://[^ ]+")
threshold = need("XWEB_ATTESTOR_THRESHOLD", r"[1-9]")
signers = [need("XWEB_ATTESTOR_%d_SIGNER" % n, r"0x[0-9a-fA-F]{40}") for n in range(1, 5)]
if evm:
    try:
        from Crypto.Hash import keccak
    except ImportError:
        print("xweb-config: pycryptodome keccak is required", file=sys.stderr)
        sys.exit(2)
    selector = keccak.new(digest_bits=256, data=b"getAttestors()").hexdigest()[:8]
    result, error = rpc(evm, "eth_call", [{"to": "0x" + "00" * 18 + "1019", "data": "0x" + selector}, "latest"])
    registered, onchain = None, None
    try:
        raw = bytes.fromhex(result[2:])
        word = lambda at: int.from_bytes(raw[at:at + 32], "big")
        onchain = word(32)
        array = word(0)
        count = word(array)
        registered = []
        for index in range(count):
            tuple_at = array + 32 + word(array + 32 + 32 * index)
            registered.append("0x" + raw[tuple_at + 12:tuple_at + 32].hex())
    except (TypeError, ValueError, IndexError, AttributeError):
        registered = None
    if registered is None:
        line(False, "signers getAttestors=%s" % (error or "malformed"))
    else:
        named = [s.lower() for s in signers if s]
        for n, signer in enumerate(signers, 1):
            if signer:
                line(signer.lower() in registered, "signer attestor-%d address=%s member=%s" % (n, signer.lower(), "yes" if signer.lower() in registered else "no"))
        exact = len(named) == 4 and len(set(named)) == 4 and sorted(named) == sorted(registered)
        majority = onchain is not None and len(registered) // 2 < onchain <= len(registered)
        line(exact and majority and threshold is not None and onchain == int(threshold),
             "signers registered=%d named=%d exact=%s threshold=%s approved=%s" % (len(registered), len(set(named)),
             "yes" if exact else "no", onchain, threshold or "absent"))
    minimum_wei = need("XWEB_SUBMITTER_MIN_WEI", r"[1-9][0-9]{0,77}")
    for n in range(1, 5):
        submitter = need("XWEB_ATTESTOR_%d_SUBMITTER" % n, r"0x[0-9a-fA-F]{40}")
        if not submitter:
            continue
        if signers[n - 1] and submitter.lower() == signers[n - 1].lower():
            line(False, "submitter attestor-%d address=%s role=shared-with-signer" % (n, submitter.lower()))
            continue
        result, error = rpc(evm, "eth_getBalance", [submitter, "latest"])
        try:
            wei = int(result, 16)
        except (TypeError, ValueError):
            line(False, "submitter attestor-%d address=%s balance=%s" % (n, submitter.lower(), error or "malformed"))
            continue
        line(minimum_wei is not None and wei >= int(minimum_wei),
             "submitter attestor-%d address=%s balance=%d minimum=%s" % (n, submitter.lower(), wei, minimum_wei or "absent"))
sys.exit(1 if failures else 0)
' "$xweb_gateway" "$prices" ${names[@]+"${names[@]}"}
}

# xweb_register: registers XWEB_REGISTER_KEY_FILE's key with lx_register and
# checks the answer names exactly it.
xweb_register() {
	xweb_need XWEB_REGISTER_KEY_FILE
	python3 -c "$xweb_rpc_py"'
import hashlib
import os
import re
import stat
import sys

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat

gateway, path = sys.argv[1:]
fail = lambda what, code=2: (print("xweb-config: " + what, file=sys.stderr), sys.exit(code))
try:
    info = os.lstat(path)
except OSError:
    fail("XWEB_REGISTER_KEY_FILE is unreadable")
if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077 or info.st_uid != os.geteuid() or info.st_size > 256:
    fail("XWEB_REGISTER_KEY_FILE must be a regular file of its owner, readable by nobody else")
with open(path, "rb") as handle:
    text = handle.read().strip()
if not re.fullmatch(rb"[0-9a-fA-F]{64}", text):
    fail("XWEB_REGISTER_KEY_FILE does not hold 32 hexadecimal bytes")
key = Ed25519PrivateKey.from_private_bytes(bytes.fromhex(text.decode()))
public = key.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
def tagged(domain):
    digest = hashlib.sha256()
    for part in (domain, b"beta", public):
        digest.update(len(part).to_bytes(8, "big"))
        digest.update(part)
    return digest.digest()
subject = "beta." + tagged(b"layerx-register-subject-v1").hex()[:32]
signature = key.sign(tagged(b"layerx-register-binding-v1"))
result, error = rpc(gateway, "lx_register", [public.hex(), signature.hex()])
if error:
    fail("lx_register " + error, 1)
if (not isinstance(result, dict) or result.get("tenant") != "beta" or result.get("sub") != subject
        or result.get("allowed_signer_public_keys") != [public.hex()]):
    fail("lx_register answered a principal that is not this key", 1)
print("registered public_key=%s did=did:layerx:%s sub=%s tenant=beta" % (public.hex(), public.hex(), subject))
' "$xweb_gateway" "$XWEB_REGISTER_KEY_FILE"
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
inventory)
	[ "$#" -eq 1 ] || {
		usage >&2
		exit 2
	}
	load_hosts
	xweb_inventory
	;;
register)
	[ "$#" -eq 1 ] || {
		usage >&2
		exit 2
	}
	xweb_register
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
