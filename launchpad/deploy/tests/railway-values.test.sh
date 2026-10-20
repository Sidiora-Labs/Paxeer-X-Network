#!/usr/bin/env bash
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../../.." && pwd)"
tool="$root/tools/deploy/railway-apply-variables.py"
values="$root/launchpad/deploy/railway-values.json"
manifest="$root/launchpad/deploy/railway/variables.json"
fixture="$root/tools/deploy/tests/fixtures/railway-apply-variables-replay.json"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fail() {
	echo "FAIL: $*" >&2
	exit 1
}

# The recorded replay, widened to every manifest service with no live variables,
# so the plan resolves every name from the values file alone.
python3 - "$manifest" "$fixture" "$tmp/replay.json" <<'PY'
import json, sys
services = list(json.load(open(sys.argv[1])))
replay = json.load(open(sys.argv[2]))
project = next(e for e in replay if e["op"] == "Project")
env = next(n["node"]["id"] for n in project["data"]["project"]["environments"]["edges"] if n["node"]["name"] == "production")
project["data"]["project"]["services"]["edges"] = [{"node": {"id": f"svc-{s}", "name": s}} for s in services]
out = [project] + [{"op": "Variables", "variables": {"p": project["variables"]["id"], "e": env, "s": f"svc-{s}"},
                    "data": {"variables": {}}} for s in services]
json.dump(out, open(sys.argv[3], "w"))
PY

set +e
env -i PATH="$PATH" RAILWAY_API_TOKEN=sentinel-railway-token-5c07 python3 "$tool" proj-test production \
	--plan --values "$values" --replay "$tmp/replay.json" >"$tmp/plan.out" 2>&1
code=$?
set -e
cat "$tmp/plan.out"
[ "$code" -eq 3 ] || fail "plan exited $code, want 3 (unresolved names remain)"

# Operator secrets and values that need a decision: nothing else may stay unresolved.
db="DATABASE_URL REDIS_URL"
chain="DATABASE_URL REDIS_BULL_URL REDIS_URL"
s3="S3_ACCESS_KEY_ID S3_ENDPOINT S3_SECRET_ACCESS_KEY"
want="$tmp/want"
{
	for s in api api-sockets social livestream; do for n in $db; do echo "$s $n"; done; done
	for s in indexer ranking; do for n in $chain; do echo "$s $n"; done; done
	for s in charts stats pnl; do for n in $chain WEBHOOK_HMAC_SECRET; do echo "$s $n"; done; done
	for n in GATEWAY_JWT_SECRET METADATA_UPLOAD_URL REDIS_URL $s3 SOCIAL_HTTP_URL SOCIAL_WS_URL; do echo "gateway $n"; done
	for s in user metadata; do for n in $db $s3 S3_BUCKET; do echo "$s $n"; done; done
	for n in DATA_API_URL GATEWAY_URL LIVESTREAM_API_URL METADATA_API_URL PNL_API_URL STATS_API_URL \
		SUPABASE_PUBLISHABLE_KEY SUPABASE_URL WALLETCONNECT_PROJECT_ID WALLET_IFRAME_ORIGIN; do echo "web NEXT_PUBLIC_$n"; done
} | sort >"$want"
awk '/^service /{svc=$2} /^  unresolved /{print svc, $2}' "$tmp/plan.out" | sort >"$tmp/got"
diff -u "$want" "$tmp/got" || fail "unresolved names differ from the reported operator-secret and decision lists"

grep -q 'not in project' "$tmp/plan.out" && fail "a manifest service is missing from the replay"
grep -q '^  missing RPC_URL = https://api5.mainnet-beta.paxeer.network$' "$tmp/plan.out" || fail "RPC_URL not taken from the values file"
grep -q '^  missing LIVEPEER_API_URL = https://livepeer.studio/api$' "$tmp/plan.out" || fail "LIVEPEER_API_URL not taken"
grep -q '^  missing METADATA_INGRESS = edge$' "$tmp/plan.out" || fail "metadata ingress is not edge"
grep -q '^  missing NEXT_PUBLIC_EXPLORER_API = https://api.paxscan.io/api/v2$' "$tmp/plan.out" || fail "explorer API not taken"
grep -q 'sentinel-railway-token' "$tmp/plan.out" && fail "plan printed the token"

# The web build args carry the same values the web service receives at runtime, and every
# Dockerfile.web build arg has a value except the ones that need a decision.
python3 - "$manifest" "$values" "$root/launchpad/deploy/Dockerfile.web" <<'PY'
import json, re, sys
args = json.load(open(sys.argv[1]))["web"]["buildArgs"]
web = json.load(open(sys.argv[2]))["web"]
assert all(args.get(k) == v for k, v in web.items()), f"web.buildArgs differs from the web values: {set(web.items()) - set(args.items())}"
declared = set(re.findall(r"^ARG ((?:NEXT_PUBLIC|WEB)_[A-Z_]+)", open(sys.argv[3]).read(), re.M))
undecided = {f"NEXT_PUBLIC_{n}" for n in """ANALYTICS_KEY DATA_API_URL GATEWAY_URL LIVESTREAM_API_URL MARKET_SOCIAL_ORIGINS
    METADATA_API_URL PNL_API_URL PRIVATE_QUALIFICATION_AUTHORITY_PROFILE
    SENTRY_DSN SENTRY_RELEASE SOCIAL_API_URL STATS_API_URL SUPABASE_PUBLISHABLE_KEY SUPABASE_URL
    USER_API_URL WALLETCONNECT_PROJECT_ID WALLET_IFRAME_ORIGIN""".split()}
assert set(args) <= declared, f"build args Dockerfile.web does not declare: {set(args) - declared}"
assert declared - set(args) == undecided, f"unfilled build args differ from the decision list: {(declared - set(args)) ^ undecided}"
assert all(v and not any(c.isspace() for c in v) for v in args.values()), "a build arg is empty or has whitespace"
PY
echo PASS railway-values
