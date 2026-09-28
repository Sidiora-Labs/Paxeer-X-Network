#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'EOF'
usage: tools/wallet/check-live.sh endpoint | attestors | gateway

Checks a deployed wallet service against its live answers.

endpoint  posts JSON-RPC to $CHECK_LIVE_ENDPOINT_BASE/rpc, a shared endpoint
          running with the chain RPC configured and the kernel backends
          unconfigured. CHECK_LIVE_ENDPOINT_BASE is required.

Checks, each printed as "pass <check> <observed>" or "fail <check> <observed>":
  eth_chainId        the chain id is 0x7d
  px_resolveAccount  a well-formed address resolves to a result with a boolean
                     bound field, never a transport or read error
  px_getNetwork      kernel.available is false with a reason other than available
  lx_getAccount      the answer is error -32010 with data.code kernel_unavailable,
                     a named backend and a reason

attestors reads GET <base>/health from every base in
          CHECK_LIVE_ATTESTOR_BASES and prints one line per node, then one
          quorum line:
  node    "pass node <id> ..." when the node answers ready with every peer
          reachable, with its region, share count, refresh epoch, audit
          sequence and head, reachable peers and readiness
  quorum  "pass quorum ready=<n>/<total> need=3" when at least three nodes
          are ready
          Exits 0 only when every node passes and the quorum passes.

gateway   reads a deployed wallet gateway at CHECK_LIVE_GATEWAY_BASE and prints
          one line per check:
  readiness  GET <base>/readyz answers ready with the attestors, nonce_store,
             rpc_pool and identity_provider components each up
  me         GET <base>/v1/wallet/me with CHECK_LIVE_GATEWAY_TOKEN as the bearer
             answers the provisioned wallet: an EVM address, a did:layerx
             identity, a main account id and binding_state bound
          Exits 0 only when both checks pass.

Environment:
  CHECK_LIVE_ENDPOINT_BASE   base URL of the endpoint, no trailing /rpc
  CHECK_LIVE_ATTESTOR_BASES  comma-separated base URLs of the attestor APIs
  CHECK_LIVE_CLIENT_CERT     client certificate presented to https bases
  CHECK_LIVE_CLIENT_KEY      key of the client certificate
  CHECK_LIVE_CA              CA bundle that authenticates https bases
  CHECK_LIVE_GATEWAY_BASE    base URL of the wallet gateway
  CHECK_LIVE_GATEWAY_TOKEN   access token of a provisioned test identity
  CHECK_LIVE_TIMEOUT         seconds per request, default 30

Exits 1 when any check fails, 2 on a usage error.
EOF
}

mode="${1:-}"
case "$mode" in
-h | --help)
	usage
	exit 0
	;;
endpoint | attestors | gateway) ;;
*)
	usage >&2
	exit 2
	;;
esac

if [ "$#" -ne 1 ]; then
	usage >&2
	exit 2
fi

timeout="${CHECK_LIVE_TIMEOUT:-30}"

for tool in curl python3; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "check-live: $tool is required" >&2
		exit 2
	fi
done

attestors() {
	local bases="${CHECK_LIVE_ATTESTOR_BASES:-}" base entry tls=() need_tls=0
	local failures=0 total=0 ready=0 body status verdict
	if [ -z "$bases" ]; then
		echo "check-live: CHECK_LIVE_ATTESTOR_BASES is required" >&2
		usage >&2
		exit 2
	fi
	IFS=',' read -r -a entries <<<"$bases"
	for entry in "${entries[@]}"; do
		entry="${entry// /}"
		if [ -z "$entry" ]; then
			echo "check-live: CHECK_LIVE_ATTESTOR_BASES holds an empty entry" >&2
			exit 2
		fi
		case "$entry" in
		https://*) need_tls=1 ;;
		http://*) ;;
		*)
			echo "check-live: attestor base $entry is not an http or https URL" >&2
			exit 2
			;;
		esac
	done
	if [ "$need_tls" -eq 1 ]; then
		local var
		for var in CHECK_LIVE_CLIENT_CERT CHECK_LIVE_CLIENT_KEY CHECK_LIVE_CA; do
			if [ -z "${!var:-}" ]; then
				echo "check-live: $var is required for https attestor bases" >&2
				usage >&2
				exit 2
			fi
			if [ ! -r "${!var}" ]; then
				echo "check-live: $var does not name a readable file" >&2
				exit 2
			fi
		done
		tls=(--cert "$CHECK_LIVE_CLIENT_CERT" --key "$CHECK_LIVE_CLIENT_KEY" --cacert "$CHECK_LIVE_CA")
	fi
	for entry in "${entries[@]}"; do
		base="${entry// /}"
		base="${base%/}"
		total=$((total + 1))
		status=0
		body="$(curl -sS --max-time "$timeout" ${tls[@]+"${tls[@]}"} "$base/health" 2>&1)" || status=$?
		if [ "$status" -ne 0 ]; then
			verdict="fail $base transport $(printf '%s' "$body" | tr '\n' ' ' | cut -c1-200)"
		else
			verdict="$(printf '%s' "$body" | python3 -c '
import json
import sys

base = sys.argv[1]
raw = sys.stdin.read()
try:
    doc = json.loads(raw)
except ValueError:
    print("fail " + base + " non-json " + " ".join(raw.split())[:200])
    sys.exit(0)
fields = ("node_id", "region", "share_count", "refresh_epoch", "audit_sequence", "audit_head", "peers", "ready")
if not isinstance(doc, dict) or any(f not in doc for f in fields) or not isinstance(doc["peers"], dict):
    print("fail " + base + " not-health " + json.dumps(doc, sort_keys=True)[:200])
    sys.exit(0)
peers = doc["peers"]
reachable = sum(1 for p in peers.values() if isinstance(p, dict) and p.get("reachable") is True)
down = sorted(i for i, p in peers.items() if not (isinstance(p, dict) and p.get("reachable") is True))
ready = doc["ready"] is True
line = "node %s region=%s shares=%s epoch=%s audit=%s:%s peers=%d/%d ready=%s" % (
    doc["node_id"], doc["region"], doc["share_count"], doc["refresh_epoch"],
    doc["audit_sequence"], doc["audit_head"], reachable, len(peers), str(ready).lower())
if down:
    line += " unreachable=" + ",".join(down)
if doc.get("readiness_error"):
    line += " readiness_error=" + " ".join(str(doc["readiness_error"]).split())[:120]
state = "ready" if ready else "unready"
print(("pass " if ready and peers and not down else "fail ") + state + " " + line)
' "$base")"
		fi
		case "$verdict" in
		pass\ *)
			ready=$((ready + 1))
			echo "pass ${verdict#pass ready }"
			;;
		fail\ ready\ *)
			ready=$((ready + 1))
			echo "fail ${verdict#fail ready }"
			failures=$((failures + 1))
			;;
		fail\ unready\ *)
			echo "fail ${verdict#fail unready }"
			failures=$((failures + 1))
			;;
		*)
			echo "$verdict"
			failures=$((failures + 1))
			;;
		esac
	done
	if [ "$ready" -ge 3 ]; then
		echo "pass quorum ready=$ready/$total need=3"
	else
		echo "fail quorum ready=$ready/$total need=3"
		failures=$((failures + 1))
	fi
	if [ "$failures" -ne 0 ]; then
		echo "check-live: $failures check(s) failed"
		exit 1
	fi
	echo "check-live: all checks passed"
	exit 0
}

gateway() {
	local base="${CHECK_LIVE_GATEWAY_BASE:-}" token="${CHECK_LIVE_GATEWAY_TOKEN:-}"
	local failures=0 check path body status verdict
	if [ -z "$base" ]; then
		echo "check-live: CHECK_LIVE_GATEWAY_BASE is required" >&2
		usage >&2
		exit 2
	fi
	if [ -z "$token" ]; then
		echo "check-live: CHECK_LIVE_GATEWAY_TOKEN is required" >&2
		usage >&2
		exit 2
	fi
	base="${base%/}"
	for check in readiness me; do
		status=0
		if [ "$check" = readiness ]; then
			path=/readyz
			body="$(curl -sS --max-time "$timeout" -w '\n%{http_code}' "$base$path" 2>&1)" || status=$?
		else
			path=/v1/wallet/me
			body="$(curl -sS --max-time "$timeout" -w '\n%{http_code}' -H "authorization: Bearer $token" "$base$path" 2>&1)" || status=$?
		fi
		if [ "$status" -ne 0 ]; then
			verdict="fail transport $(printf '%s' "$body" | tr '\n' ' ' | cut -c1-200)"
		else
			verdict="$(printf '%s' "$body" | python3 -c '
import json
import re
import sys

check = sys.argv[1]
raw = sys.stdin.read()
text, _, code = raw.rpartition("\n")
try:
    doc = json.loads(text)
except ValueError:
    print("fail http=" + code + " non-json " + " ".join(text.split())[:200])
    sys.exit(0)


def show(value):
    return json.dumps(value, separators=(",", ":"), sort_keys=True)[:200]


if check == "readiness":
    components = doc.get("components") if isinstance(doc, dict) else None
    names = ("attestors", "nonce_store", "rpc_pool", "identity_provider")
    if not isinstance(components, dict) or any(not isinstance(components.get(n), dict) for n in names):
        print("fail http=" + code + " not-readiness " + show(doc))
        sys.exit(0)
    parts = []
    down = []
    for n in names:
        c = components[n]
        state = c.get("state")
        part = n + "=" + str(state)
        if n == "attestors":
            part += "(%s/%s)" % (c.get("healthy"), c.get("required"))
        elif n == "rpc_pool":
            part += "(%s)" % c.get("healthy")
        elif n == "identity_provider":
            part += "(%s)" % c.get("keys")
        if state != "up":
            down.append(n)
            if c.get("reason"):
                part += " reason=" + " ".join(str(c["reason"]).split())[:120]
        parts.append(part)
    ok = code == "200" and doc.get("ready") is True and not down
    print(("pass " if ok else "fail ") + "http=" + code + " ready=" + str(doc.get("ready") is True).lower() + " " + " ".join(parts))
else:
    wallet = doc.get("wallet") if isinstance(doc, dict) else None
    if code != "200" or not isinstance(wallet, dict):
        print("fail http=" + code + " " + show(doc))
        sys.exit(0)
    address = wallet.get("address")
    did = wallet.get("did")
    main = wallet.get("main_account_id")
    binding = wallet.get("binding_state")
    kernel = doc.get("kernel") if isinstance(doc.get("kernel"), dict) else {}
    checks = [
        ("address", isinstance(address, str) and re.fullmatch(r"0x[0-9a-fA-F]{40}", address) is not None),
        ("did", isinstance(did, str) and re.fullmatch(r"did:layerx:[0-9a-f]{64}", did) is not None),
        ("main_account_id", isinstance(main, str) and re.fullmatch(r"[0-9a-f]{64}", main) is not None),
    ]
    missing = [n for n, ok in checks if not ok]
    line = "http=" + code + " binding_state=" + str(binding) + " " + " ".join(
        n + ("=set" if ok else "=missing") for n, ok in checks
    ) + " kernel=" + str(kernel.get("state"))
    print(("pass " if not missing and binding == "bound" else "fail ") + line)
' "$check")"
		fi
		case "$verdict" in
		pass\ *) echo "pass $check ${verdict#pass }" ;;
		*)
			echo "fail $check ${verdict#fail }"
			failures=$((failures + 1))
			;;
		esac
	done
	if [ "$failures" -ne 0 ]; then
		echo "check-live: $failures check(s) failed"
		exit 1
	fi
	echo "check-live: all checks passed"
	exit 0
}

if [ "$mode" = attestors ]; then
	attestors
fi

if [ "$mode" = gateway ]; then
	gateway
fi

base="${CHECK_LIVE_ENDPOINT_BASE:-}"
if [ -z "$base" ]; then
	echo "check-live: CHECK_LIVE_ENDPOINT_BASE is required" >&2
	usage >&2
	exit 2
fi
base="${base%/}"

failures=0

call() {
	local method="$1" params="$2"
	curl -sS --max-time "$timeout" \
		-H 'content-type: application/json' \
		-X POST \
		--data "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"$method\",\"params\":$params}" \
		"$base/rpc" 2>&1
}

judge() {
	local check="$1" status="$2" body="$3" verdict
	if [ "$status" -ne 0 ]; then
		verdict="fail transport $(printf '%s' "$body" | tr '\n' ' ' | cut -c1-200)"
	else
		verdict="$(printf '%s' "$body" | python3 -c '
import json
import sys

check = sys.argv[1]
raw = sys.stdin.read()
try:
    doc = json.loads(raw)
except ValueError:
    print("fail non-json " + " ".join(raw.split())[:200])
    sys.exit(0)
if not isinstance(doc, dict) or doc.get("jsonrpc") != "2.0":
    print("fail not-jsonrpc " + json.dumps(doc)[:200])
    sys.exit(0)
result, error = doc.get("result"), doc.get("error")


def show(value):
    return json.dumps(value, separators=(",", ":"), sort_keys=True)[:200]


if check == "eth_chainId":
    if result == "0x7d":
        print("pass " + result)
    else:
        print("fail " + show(error if error is not None else result))
elif check == "px_resolveAccount":
    if error is None and isinstance(result, dict) and isinstance(result.get("bound"), bool):
        print("pass bound=" + str(result["bound"]).lower())
    else:
        print("fail " + show(error if error is not None else result))
elif check == "px_getNetwork":
    kernel = result.get("kernel") if error is None and isinstance(result, dict) else None
    if (
        isinstance(kernel, dict)
        and kernel.get("available") is False
        and isinstance(kernel.get("reason"), str)
        and kernel["reason"] not in ("", "available")
    ):
        print("pass kernel.available=false reason=" + kernel["reason"])
    else:
        print("fail " + show(kernel if kernel is not None else (error if error is not None else result)))
elif check == "lx_getAccount":
    data = error.get("data") if isinstance(error, dict) else None
    if (
        result is None
        and isinstance(error, dict)
        and error.get("code") == -32010
        and isinstance(data, dict)
        and data.get("code") == "kernel_unavailable"
        and isinstance(data.get("backend"), str)
        and data["backend"] != ""
        and isinstance(data.get("reason"), str)
        and data["reason"] != ""
    ):
        print("pass code=-32010 backend=" + data["backend"] + " reason=" + data["reason"])
    else:
        print("fail " + show(error if error is not None else result))
else:
    print("fail unknown-check")
' "$check")"
	fi
	case "$verdict" in
	pass\ *) echo "pass $check ${verdict#pass }" ;;
	*)
		echo "fail $check ${verdict#fail }"
		failures=$((failures + 1))
		;;
	esac
}

run() {
	local check="$1" params="$2" body status=0
	body="$(call "$check" "$params")" || status=$?
	judge "$check" "$status" "$body"
}

run eth_chainId '[]'
run px_resolveAccount '["0x000000000000000000000000000000000000dEaD"]'
run px_getNetwork '[]'
run lx_getAccount "[\"$(printf 'ab%.0s' $(seq 32))\"]"

if [ "$failures" -ne 0 ]; then
	echo "check-live: $failures check(s) failed"
	exit 1
fi
echo "check-live: all checks passed"
