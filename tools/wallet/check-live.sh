#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'EOF'
usage: tools/wallet/check-live.sh endpoint

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

Environment:
  CHECK_LIVE_ENDPOINT_BASE  base URL of the endpoint, no trailing /rpc
  CHECK_LIVE_TIMEOUT        seconds per request, default 30

Exits 1 when any check fails, 2 on a usage error.
EOF
}

mode="${1:-}"
case "$mode" in
-h | --help)
	usage
	exit 0
	;;
endpoint) ;;
*)
	usage >&2
	exit 2
	;;
esac

if [ "$#" -ne 1 ]; then
	usage >&2
	exit 2
fi

base="${CHECK_LIVE_ENDPOINT_BASE:-}"
if [ -z "$base" ]; then
	echo "check-live: CHECK_LIVE_ENDPOINT_BASE is required" >&2
	usage >&2
	exit 2
fi
base="${base%/}"
timeout="${CHECK_LIVE_TIMEOUT:-30}"

for tool in curl python3; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "check-live: $tool is required" >&2
		exit 2
	fi
done

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
