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
# Every Go service names the runtime documents and pool identity production refuses to start without.
runtime="DB_BUDGET_DOCUMENT DB_BUDGET_SHA256 DB_POOL_ID EXPECTED_SCHEMA_DOCUMENT EXPECTED_SCHEMA_SHA256 INSTANCE_ID ZONE"
db="DATABASE_URL REDIS_URL"
chain="DATABASE_URL REDIS_BULL_URL REDIS_URL NATS_CREDENTIALS NATS_URL DELIVERY_CONFIG_DOCUMENT DELIVERY_CONFIG_SHA256"
s3="S3_ACCESS_KEY_ID S3_ENDPOINT S3_SECRET_ACCESS_KEY"
identity="IDENTITY_ASSERTION_ISSUER IDENTITY_CA_FILE IDENTITY_CLIENT_CERT_FILE IDENTITY_CLIENT_KEY_FILE
	IDENTITY_PEER_CERT_SHA256 IDENTITY_SERVICE_URL IDENTITY_SIGNING_KEY STREAM_SIGNING_KEY"
want="$tmp/want"
{
	for s in api api-sockets indexer charts stats pnl ranking gateway user social metadata livestream; do
		for n in $runtime; do echo "$s $n"; done
	done
	for s in api api-sockets social livestream metadata user; do for n in $db; do echo "$s $n"; done; done
	for n in $identity ACCOUNTING_READ_CONFIG_DOCUMENT ACCOUNTING_READ_CONFIG_SHA256 STATS_SERVICE_URL; do echo "api $n"; done
	for n in $identity NATS_CREDENTIALS NATS_URL STREAM_CONSUMER_CONFIG_DOCUMENT STREAM_CONSUMER_CONFIG_SHA256; do
		echo "api-sockets $n"
	done
	for s in indexer charts stats pnl; do for n in $chain; do echo "$s $n"; done; done
	for n in INDEXER_LIFECYCLE_CONFIG INDEXER_RECEIPT_CONFIG; do echo "indexer ${n}_DOCUMENT"; echo "indexer ${n}_SHA256"; done
	for n in CHART_FEATURES_CONFIG_DOCUMENT CHART_FEATURES_CONFIG_SHA256; do echo "charts $n"; done
	for s in charts stats pnl; do echo "$s WEBHOOK_HMAC_SECRET"; done
	for n in IDENTITY_ASSERTION_SIGNING_KEY IDENTITY_MTLS_CA_FILE IDENTITY_MTLS_CLIENT_CERT_FILE IDENTITY_MTLS_CLIENT_KEY_PEM \
		IDENTITY_MTLS_PEER_CERT_SHA256 IDENTITY_SERVICE_URL STATS_AUDIT_KEY STATS_CURSOR_KEY; do echo "stats $n"; done
	echo "pnl RPC_URL_FALLBACK"
	for n in DATABASE_URL REDIS_BULL_URL REDIS_URL RANKING_RUNTIME_DOCUMENT RANKING_RUNTIME_SHA256; do echo "ranking $n"; done
	for n in CORS_ALLOWED_ORIGINS DATABASE_URL GATEWAY_JWT_SECRET GATEWAY_RUNTIME_DOCUMENT GATEWAY_RUNTIME_SHA256 \
		IDENTITY_CONFIG_DOCUMENT IDENTITY_CONFIG_SHA256 IDENTITY_LIMITER_KEY IDENTITY_SERVICE_TOKEN IDENTITY_SIGNING_SECRET \
		METADATA_UPLOAD_URL PUBLIC_URL REDIS_URL $s3 SOCIAL_HTTP_URL SOCIAL_WS_URL; do echo "gateway $n"; done
	for n in $s3 S3_BUCKET ACCOUNT_AUDIT_KEY ACCOUNT_CHAIN_RPC_URL ACCOUNT_CURSOR_KEY ACCOUNT_DOWNLOAD_BASE_URL \
		ACCOUNT_ENDPOINT_FINGERPRINT_KEY ACCOUNT_ENDPOINT_KEY ACCOUNT_ENDPOINT_PROOF_PUBLIC_KEY ACCOUNT_EXPORT_BUCKET \
		ACCOUNT_EXPORT_KEY ACCOUNT_GATEWAY_CERT_SHA256 ACCOUNT_GATEWAY_PUBLIC_KEY ACCOUNT_NATS_CA_FILE ACCOUNT_NATS_CERT_FILE \
		ACCOUNT_NATS_CREDENTIALS_FILE ACCOUNT_NATS_KEY_FILE ACCOUNT_NATS_STREAM ACCOUNT_NATS_URL ACCOUNT_SERVICE_SIGNING_SEED \
		ACCOUNT_SUBJECTS_DOCUMENT ACCOUNT_SUBJECTS_SHA256 ACCOUNT_TLS_CA_FILE ACCOUNT_TLS_CERT_FILE ACCOUNT_TLS_KEY_FILE \
		IDENTITY_CERT_SHA256 IDENTITY_SERVICE_URL METADATA_CERT_SHA256 METADATA_SERVICE_URL NOTIFICATION_CERT_SHA256 \
		NOTIFICATION_D11_TOKEN NOTIFICATION_SERVICE_TOKEN NOTIFICATION_SERVICE_URL; do echo "user $n"; done
	for n in SOCIAL_RUNTIME_DOCUMENT SOCIAL_RUNTIME_SHA256; do echo "social $n"; done
	for n in $s3 S3_BUCKET S3_APPROVED_BUCKET S3_QUARANTINE_BUCKET CLAMAV_HOST IDENTITY_AUDIENCE IDENTITY_SERVICE_TOKEN \
		IDENTITY_SERVICE_URL METADATA_PUBLICATION_AUDIT_KEY METADATA_PUBLICATION_CONFIG_DOCUMENT \
		METADATA_PUBLICATION_CONFIG_SHA256 PUBLIC_URL; do echo "metadata $n"; done
	for n in IDENTITY_SERVICE_TOKEN LIVEPEER_API_KEY LIVEPEER_WEBHOOK_SECRET; do echo "livestream $n"; done
	for n in DATA_API_URL GATEWAY_URL LIVESTREAM_API_URL METADATA_API_URL PNL_API_URL STATS_API_URL \
		SUPABASE_PUBLISHABLE_KEY SUPABASE_URL WALLETCONNECT_PROJECT_ID WALLET_IFRAME_ORIGIN; do echo "web NEXT_PUBLIC_$n"; done
} | sort >"$want"
awk '/^service /{svc=$2} /^  unresolved /{print svc, $2}' "$tmp/plan.out" | sort >"$tmp/got"
diff -u "$want" "$tmp/got" || fail "unresolved names differ from the reported operator-secret and decision lists"

grep -q 'not in project' "$tmp/plan.out" && fail "a manifest service is missing from the replay"
grep -q '^  missing RPC_URL = https://api5.mainnet-beta.paxeer.network$' "$tmp/plan.out" || fail "RPC_URL not taken from the values file"
grep -q '^  missing LIVEPEER_API_URL = https://livepeer.studio/api$' "$tmp/plan.out" || fail "LIVEPEER_API_URL not taken"
grep -q '^  missing METADATA_INGRESS = edge$' "$tmp/plan.out" || fail "metadata ingress is not edge"
grep -q '^  missing MODERATION_SOURCE = database$' "$tmp/plan.out" || fail "edge metadata does not read moderation from the database"
[ "$(grep -c '^  missing PORT = 8080$' "$tmp/plan.out")" -eq 12 ] || fail "not every Go service binds the container port 8080"
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
