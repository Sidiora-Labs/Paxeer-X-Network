#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'EOF'
usage: scripts/wallet/check-live.sh endpoint | attestors | gateway | cutover | human | human-session

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

human     reads a deployed human service at CHECK_LIVE_HUMAN_BASE as the wallet
          origin CHECK_LIVE_HUMAN_ORIGIN and prints one line per check:
  live       GET <base>/livez answers 200 with result.live true and
             result.service layerx-human-service
  preflight  OPTIONS <base>/v1/intents/plan from the origin answers 2xx with
             access-control-allow-origin equal to the origin
  plan       POST <base>/v1/intents/plan from the origin with the golden plan
             request body and no session answers the structured envelope:
             http 401, ok false, error.code unauthenticated and a trace;
             a 5xx or a non-envelope body fails
          Exits 0 only when all three checks pass.

human-session
          plans the golden intent against a deployed human service at
          CHECK_LIVE_HUMAN_BASE as the wallet origin CHECK_LIVE_HUMAN_ORIGIN,
          authenticated by the wallet identity assertion
          CHECK_LIVE_HUMAN_ASSERTION, and prints one line per check:
  preflight  OPTIONS <base>/v1/intents/plan from the origin answers 2xx with
             access-control-allow-origin equal to the origin and
             access-control-allow-headers admitting authorization
  plan       POST <base>/v1/intents/plan from the origin with the assertion as
             its bearer and the golden plan request body, its asset_id set to
             CHECK_LIVE_HUMAN_ASSET, its destination account set to
             CHECK_LIVE_HUMAN_DESTINATION and its deadline ten minutes ahead,
             answers http 200, ok true and the IntentPlan the contract
             declares: a lowercase hex plan_digest, a journey_kind, a
             total_fee, legs indexed from 0 in order each carrying mechanism,
             domain, source, destination, money and fee, and signing
             requirements each naming a leg of the plan
          Exits 0 only when both checks pass.

cutover   reads the public wallet hostname CHECK_LIVE_CUTOVER_HOST over https,
          through whatever proxy serves it, and confirms the new gateway
          answers, one line per check:
  served_by  GET https://<host>/healthz answers 200 with the response header
             x-served-by: paxeer-wallet-gateway
  readiness  GET https://<host>/readyz answers 200 with ready true and the
             same header
          Exits 0 only when both checks pass. The mode runs only after the
          endpoint has been cut over; without the variable it refuses.

Environment:
  CHECK_LIVE_ENDPOINT_BASE   base URL of the endpoint, no trailing /rpc
  CHECK_LIVE_ATTESTOR_BASES  comma-separated base URLs of the attestor APIs
  CHECK_LIVE_CLIENT_CERT     client certificate presented to https bases
  CHECK_LIVE_CLIENT_KEY      key of the client certificate
  CHECK_LIVE_CA              CA bundle that authenticates https bases
  CHECK_LIVE_GATEWAY_BASE    base URL of the wallet gateway
  CHECK_LIVE_GATEWAY_TOKEN   access token of a provisioned test identity
  CHECK_LIVE_HUMAN_BASE      base URL of the human service
  CHECK_LIVE_HUMAN_ORIGIN    https origin of the wallet app the service must admit
  CHECK_LIVE_HUMAN_ASSERTION wallet identity assertion of a provisioned, funded
                             test identity, sent as the bearer of the plan
  CHECK_LIVE_HUMAN_ASSET     64-hex asset id of the kernel's native asset
  CHECK_LIVE_HUMAN_DESTINATION
                             kernel account the planned send pays
  CHECK_LIVE_CUTOVER_HOST    public wallet hostname, optionally with :port
                             (CHECK_LIVE_CA, when set, authenticates it)
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
endpoint | attestors | gateway | cutover | human | human-session) ;;
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

human() {
	local base="${CHECK_LIVE_HUMAN_BASE:-}" origin="${CHECK_LIVE_HUMAN_ORIGIN:-}"
	local failures=0 check raw status verdict golden
	if [ -z "$base" ]; then
		echo "check-live: CHECK_LIVE_HUMAN_BASE is required" >&2
		usage >&2
		exit 2
	fi
	if [ -z "$origin" ]; then
		echo "check-live: CHECK_LIVE_HUMAN_ORIGIN is required" >&2
		usage >&2
		exit 2
	fi
	base="${base%/}"
	golden="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/human/schema/human-api/golden/intent.plan.request.json"
	for check in live preflight plan; do
		status=0
		case "$check" in
		live)
			raw="$(curl -sS --max-time "$timeout" -i -H "origin: $origin" "$base/livez" 2>&1)" || status=$?
			;;
		preflight)
			raw="$(curl -sS --max-time "$timeout" -i -X OPTIONS -H "origin: $origin" -H 'access-control-request-method: POST' -H 'access-control-request-headers: authorization,content-type,idempotency-key' "$base/v1/intents/plan" 2>&1)" || status=$?
			;;
		plan)
			raw="$(python3 -c 'import json, sys; print(json.dumps(json.load(open(sys.argv[1]))["body"]))' "$golden" |
				curl -sS --max-time "$timeout" -i -X POST -H "origin: $origin" -H 'content-type: application/json' --data-binary @- "$base/v1/intents/plan" 2>&1)" || status=$?
			;;
		esac
		if [ "$status" -ne 0 ]; then
			verdict="fail transport $(printf '%s' "$raw" | tr '\n' ' ' | cut -c1-200)"
		else
			verdict="$(printf '%s' "$raw" | python3 -c '
import json
import sys

check, origin = sys.argv[1], sys.argv[2]
raw = sys.stdin.read()
head, sep, body = raw.partition("\r\n\r\n")
if not sep:
    head, sep, body = raw.partition("\n\n")
lines = head.split("\n")
parts = lines[0].strip().split(" ")
code = parts[1] if len(parts) > 1 else "?"
headers = {}
for line in lines[1:]:
    name, colon, value = line.partition(":")
    if colon:
        headers[name.strip().lower()] = value.strip()
try:
    doc = json.loads(body) if body.strip() else None
except ValueError:
    doc = None


def show():
    if doc is not None:
        return json.dumps(doc, separators=(",", ":"), sort_keys=True)[:200]
    return " ".join(body.split())[:200] or "empty"


if check == "live":
    result = doc.get("result") if isinstance(doc, dict) else None
    if code == "200" and isinstance(result, dict) and result.get("live") is True and result.get("service") == "layerx-human-service":
        print("pass http=200 live=true service=layerx-human-service")
    else:
        print("fail http=" + code + " " + show())
elif check == "preflight":
    allow = headers.get("access-control-allow-origin")
    if code.startswith("2") and allow == origin:
        print("pass http=" + code + " allow-origin=" + allow)
    else:
        print("fail http=" + code + " allow-origin=" + str(allow))
else:
    error = doc.get("error") if isinstance(doc, dict) else None
    if (
        code == "401"
        and isinstance(doc, dict)
        and doc.get("ok") is False
        and isinstance(error, dict)
        and error.get("code") == "unauthenticated"
        and isinstance(doc.get("trace"), str)
    ):
        print("pass http=401 code=unauthenticated trace=present")
    else:
        print("fail http=" + code + " " + show())
' "$check" "$origin")"
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

human_session() {
	local base="${CHECK_LIVE_HUMAN_BASE:-}" origin="${CHECK_LIVE_HUMAN_ORIGIN:-}"
	local assertion="${CHECK_LIVE_HUMAN_ASSERTION:-}" asset="${CHECK_LIVE_HUMAN_ASSET:-}"
	local destination="${CHECK_LIVE_HUMAN_DESTINATION:-}" name
	local failures=0 check raw status verdict golden body
	for name in CHECK_LIVE_HUMAN_BASE CHECK_LIVE_HUMAN_ORIGIN CHECK_LIVE_HUMAN_ASSERTION CHECK_LIVE_HUMAN_ASSET CHECK_LIVE_HUMAN_DESTINATION; do
		if [ -z "${!name:-}" ]; then
			echo "check-live: $name is required" >&2
			usage >&2
			exit 2
		fi
	done
	base="${base%/}"
	golden="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/human/schema/human-api/golden/intent.plan.request.json"
	body="$(python3 -c '
import datetime, json, sys
body = json.load(open(sys.argv[1]))["body"]
body["asset_id"] = sys.argv[2]
body["destination"]["account"] = sys.argv[3]
deadline = datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(minutes=10)
body["constraints"]["deadline"] = deadline.strftime("%Y-%m-%dT%H:%M:%SZ")
print(json.dumps(body))
' "$golden" "$asset" "$destination")"
	for check in preflight plan; do
		status=0
		case "$check" in
		preflight)
			raw="$(curl -sS --max-time "$timeout" -i -X OPTIONS -H "origin: $origin" -H 'access-control-request-method: POST' -H 'access-control-request-headers: authorization,content-type' "$base/v1/intents/plan" 2>&1)" || status=$?
			;;
		plan)
			raw="$(printf '%s' "$body" | curl -sS --max-time "$timeout" -i -X POST -H "origin: $origin" -H 'content-type: application/json' -H @<(printf 'authorization: Bearer %s\n' "$assertion") --data-binary @- "$base/v1/intents/plan" 2>&1)" || status=$?
			;;
		esac
		if [ "$status" -ne 0 ]; then
			verdict="fail transport $(printf '%s' "$raw" | tr '\n' ' ' | cut -c1-200)"
		else
			verdict="$(printf '%s' "$raw" | python3 -c '
import json
import re
import sys

check, origin = sys.argv[1], sys.argv[2]
raw = sys.stdin.read()
head, sep, body = raw.partition("\r\n\r\n")
if not sep:
    head, sep, body = raw.partition("\n\n")
lines = head.split("\n")
parts = lines[0].strip().split(" ")
code = parts[1] if len(parts) > 1 else "?"
headers = {}
for line in lines[1:]:
    name, colon, value = line.partition(":")
    if colon:
        headers[name.strip().lower()] = value.strip()
try:
    doc = json.loads(body) if body.strip() else None
except ValueError:
    doc = None


def show():
    if doc is not None:
        return json.dumps(doc, separators=(",", ":"), sort_keys=True)[:200]
    return " ".join(body.split())[:200] or "empty"


def money(value):
    return isinstance(value, dict) and isinstance(value.get("amount"), str) and value["amount"].isdigit() and isinstance(value.get("currency"), str) and value["currency"] != ""


def endpoint(value):
    return isinstance(value, dict) and isinstance(value.get("kind"), str) and value["kind"] != ""


def plan_fault(result):
    if not isinstance(result, dict):
        return "result"
    if not isinstance(result.get("plan_digest"), str) or not re.fullmatch("[0-9a-f]{64}", result["plan_digest"]):
        return "plan_digest"
    if not isinstance(result.get("journey_kind"), str) or result["journey_kind"] == "":
        return "journey_kind"
    if not money(result.get("total_fee")):
        return "total_fee"
    legs = result.get("legs")
    if not isinstance(legs, list) or not legs:
        return "legs"
    for position, leg in enumerate(legs):
        if (
            not isinstance(leg, dict)
            or leg.get("index") != position
            or not isinstance(leg.get("mechanism"), str)
            or leg["mechanism"] == ""
            or not isinstance(leg.get("domain"), str)
            or leg["domain"] == ""
            or not endpoint(leg.get("source"))
            or not endpoint(leg.get("destination"))
            or not money(leg.get("money"))
            or not money(leg.get("fee"))
        ):
            return "legs[" + str(position) + "]"
    requirements = result.get("signing_requirements")
    if not isinstance(requirements, list):
        return "signing_requirements"
    for position, requirement in enumerate(requirements):
        if (
            not isinstance(requirement, dict)
            or not isinstance(requirement.get("leg_index"), int)
            or not 0 <= requirement["leg_index"] < len(legs)
            or not all(isinstance(requirement.get(key), str) and requirement[key] != "" for key in ("action_key", "signing_context", "authority"))
        ):
            return "signing_requirements[" + str(position) + "]"
    return None


if check == "preflight":
    allow = headers.get("access-control-allow-origin")
    allowed = [value.strip().lower() for value in headers.get("access-control-allow-headers", "").split(",")]
    if code.startswith("2") and allow == origin and "authorization" in allowed:
        print("pass http=" + code + " allow-origin=" + allow + " allow-headers=authorization")
    else:
        print("fail http=" + code + " allow-origin=" + str(allow) + " allow-headers=" + (",".join(value for value in allowed if value) or "none"))
else:
    result = doc.get("result") if isinstance(doc, dict) else None
    fault = plan_fault(result) if code == "200" and isinstance(doc, dict) and doc.get("ok") is True else "envelope"
    if fault is None and isinstance(doc.get("trace"), str):
        print("pass http=200 journey_kind=" + result["journey_kind"] + " legs=" + str(len(result["legs"])) + " signing_requirements=" + str(len(result["signing_requirements"])) + " plan_digest=" + result["plan_digest"][:16])
    else:
        print("fail http=" + code + " shape=" + (fault or "trace") + " " + show())
' "$check" "$origin")"
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

# served_by_request <url>: GET the URL and print the status code, the value of
# the last x-served-by response header (or "none") and the body on separate
# lines; exits with curl's status on a transport error.
served_by_request() {
	local url="$1" headers body code status=0 tls=()
	if [ -n "${CHECK_LIVE_CA:-}" ]; then
		tls=(--cacert "$CHECK_LIVE_CA")
	fi
	headers="$(mktemp)"
	body="$(mktemp)"
	code="$(curl -sS --max-time "$timeout" ${tls[@]+"${tls[@]}"} -D "$headers" -o "$body" -w '%{http_code}' "$url" 2>&1)" || status=$?
	if [ "$status" -ne 0 ]; then
		printf '%s\n' "$code"
		rm -f "$headers" "$body"
		return "$status"
	fi
	printf '%s\n' "$code"
	python3 -c '
import sys

value = "none"
for line in open(sys.argv[1], encoding="latin-1"):
    name, sep, rest = line.partition(":")
    if sep and name.strip().lower() == "x-served-by":
        value = rest.strip() or "empty"
print(value)
' "$headers"
	cat "$body"
	rm -f "$headers" "$body"
}

cutover() {
	local host="${CHECK_LIVE_CUTOVER_HOST:-}" failures=0 check path answer status verdict
	if [ -z "$host" ]; then
		echo "check-live: CHECK_LIVE_CUTOVER_HOST is required; it names the public wallet hostname and is set only once the endpoint has been cut over" >&2
		usage >&2
		exit 2
	fi
	if ! printf '%s' "$host" | grep -Eq '^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*(:[0-9]{1,5})?$'; then
		echo "check-live: CHECK_LIVE_CUTOVER_HOST must be a bare hostname with an optional :port, without a scheme or path" >&2
		exit 2
	fi
	if [ -n "${CHECK_LIVE_CA:-}" ] && [ ! -r "$CHECK_LIVE_CA" ]; then
		echo "check-live: CHECK_LIVE_CA does not name a readable file" >&2
		exit 2
	fi
	for check in served_by readiness; do
		if [ "$check" = served_by ]; then
			path=/healthz
		else
			path=/readyz
		fi
		status=0
		answer="$(served_by_request "https://$host$path")" || status=$?
		if [ "$status" -ne 0 ]; then
			verdict="fail transport $(printf '%s' "$answer" | tr '\n' ' ' | cut -c1-200)"
		else
			verdict="$(printf '%s' "$answer" | python3 -c '
import json
import sys

check = sys.argv[1]
code = sys.stdin.readline().strip()
served = sys.stdin.readline().strip()
text = sys.stdin.read()
named = served == "paxeer-wallet-gateway"
line = "http=" + code + " x-served-by=" + served
if check == "served_by":
    print(("pass " if code == "200" and named else "fail ") + line)
    sys.exit(0)
try:
    doc = json.loads(text)
except ValueError:
    print("fail " + line + " non-json " + " ".join(text.split())[:200])
    sys.exit(0)
ready = isinstance(doc, dict) and doc.get("ready") is True
line += " ready=" + str(ready).lower()
print(("pass " if code == "200" and named and ready else "fail ") + line)
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

if [ "$mode" = human ]; then
	human
fi

if [ "$mode" = human-session ]; then
	human_session
fi

if [ "$mode" = cutover ]; then
	cutover
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
