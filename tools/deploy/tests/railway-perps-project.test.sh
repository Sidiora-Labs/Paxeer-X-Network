#!/usr/bin/env bash
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../../.." && pwd)"
tool="$root/tools/deploy/railway-perps-project.py"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fail() {
	echo "FAIL: $*" >&2
	exit 1
}

d() { printf 'sha256:%064d' "$1"; }
cat >"$tmp/digests.json" <<JSON
{"perps-data-nats": "$(d 1)", "perps-data-indexer": "$(d 2)", "perps-gateway": "$(d 3)",
 "perps-explorer": "$(d 4)", "perps-portal": "$(d 5)", "perps-web": "$(d 6)"}
JSON
python3 -c 'import json, sys; m = json.load(open(sys.argv[1])); del m["perps-web"]; print(json.dumps(m))' \
	"$tmp/digests.json" >"$tmp/short.json"

export RAILWAY_API_TOKEN=sentinel-railway-token-7f3a
export GHCR_USERNAME=sentinel-ghcr-user-1c9e
export GHCR_TOKEN=sentinel-ghcr-token-5b2d
export PERPS_POSTGRES_PASSWORD=sentinel-pg-password-88aa
export PERPS_NATS_CLUSTER_PASSWORD=sentinel-cluster-pw-41
export PERPS_NATS_INDEXER_PASSWORD=sentinel-indexer-pw-42
export PERPS_NATS_GATEWAY_PASSWORD=sentinel-gateway-pw-43
export PERPS_AUTH_SECRET=sentinel-auth-secret-44
export PERPS_RPC_URLS=sentinel-rpc-urls-45
export PERPS_COMET_URLS=sentinel-comet-urls-46

# Recorded GraphQL responses: the project and environment exist, Postgres runs
# with its full variable set and volume, nats runs on an older image; the other
# seven services are missing.
cat >"$tmp/replay.json" <<JSON
[
 {"op": "Projects", "variables": {}, "data": {"projects": {"edges": [
  {"node": {"id": "p-other", "name": "unrelated", "environments": {"edges": []}, "services": {"edges": []}, "volumes": {"edges": []}}},
  {"node": {"id": "p1", "name": "paxeer-x-perps",
   "environments": {"edges": [{"node": {"id": "e1", "name": "production"}}]},
   "services": {"edges": [{"node": {"id": "s-pg", "name": "Postgres"}}, {"node": {"id": "s-nats", "name": "nats"}}]},
   "volumes": {"edges": [{"node": {"id": "v1", "volumeInstances": {"edges": [
     {"node": {"serviceId": "s-pg", "environmentId": "e1", "mountPath": "/var/lib/postgresql/data"}}]}}}]}}}]}}},
 {"op": "Instance", "variables": {"s": "s-pg", "e": "e1"}, "data": {"serviceInstance": {
   "source": {"image": "ghcr.io/railwayapp-templates/postgres-ssl:18"}}}},
 {"op": "Variables", "variables": {"p": "p1", "e": "e1", "s": "s-pg"}, "data": {"variables": {
   "POSTGRES_USER": "postgres", "POSTGRES_DB": "railway", "POSTGRES_PASSWORD": "sentinel-pg-password-88aa",
   "PGDATA": "/var/lib/postgresql/data/pgdata", "PGHOST": "\${{RAILWAY_PRIVATE_DOMAIN}}", "PGPORT": "5432",
   "PGUSER": "\${{POSTGRES_USER}}", "PGPASSWORD": "\${{POSTGRES_PASSWORD}}", "PGDATABASE": "\${{POSTGRES_DB}}",
   "DATABASE_URL": "postgresql://\${{PGUSER}}:\${{POSTGRES_PASSWORD}}@\${{RAILWAY_PRIVATE_DOMAIN}}:5432/\${{PGDATABASE}}"}}},
 {"op": "Instance", "variables": {"s": "s-nats", "e": "e1"}, "data": {"serviceInstance": {
   "source": {"image": "ghcr.io/sidiora-labs/perps-data-nats@$(d 9)"}, "healthcheckPath": "/healthz",
   "healthcheckTimeout": 300, "restartPolicyType": "ALWAYS", "numReplicas": 1}}},
 {"op": "Variables", "variables": {"p": "p1", "e": "e1", "s": "s-nats"}, "data": {"variables": {
   "PORT": "8222", "NATS_CLUSTER_PASSWORD": "sentinel-old-cluster-pw-99"}}}
]
JSON

run() {
	set +e
	python3 "$tool" "$@" >"$tmp/out" 2>"$tmp/err"
	code=$?
	set -e
	cat "$tmp/out" "$tmp/err" >>"$tmp/all"
}

run --plan --digests "$tmp/digests.json" --replay "$tmp/replay.json" --start-height 34800001
[ "$code" -eq 0 ] || { cat "$tmp/err" >&2; fail "plan exited $code"; }
cp "$tmp/out" "$tmp/plan"
n="$(grep -c '^service ' "$tmp/plan")"
[ "$n" -eq 9 ] || fail "plan lists $n services, want 9"
grep -q '^services=9$' "$tmp/plan" || fail "plan misses services=9"
grep -q '^project paxeer-x-perps exists$' "$tmp/plan" || fail "project not seen as existing"
grep -q '^environment production exists$' "$tmp/plan" || fail "environment not seen as existing"
grep -q '^service 1 Postgres database unchanged$' "$tmp/plan" || fail "Postgres is not unchanged"
grep -q '^service 2 nats image update$' "$tmp/plan" || fail "nats is not an update"
grep -q "^  image ghcr.io/sidiora-labs/perps-data-nats@$(d 9) -> ghcr.io/sidiora-labs/perps-data-nats@$(d 1)$" "$tmp/plan" ||
	fail "nats image change missing"
grep -q '^  volume /data$' "$tmp/plan" || fail "nats volume missing"
grep -q '^  var NATS_CLUSTER_PASSWORD change <env PERPS_NATS_CLUSTER_PASSWORD>$' "$tmp/plan" || fail "nats secret change not planned"
for s in nats-2 nats-3 indexer gateway explorer portal web; do
	grep -qE "^service [0-9]+ $s image create$" "$tmp/plan" || fail "$s is not a create"
done
grep -q "^  create image=ghcr.io/sidiora-labs/perps-data-indexer@$(d 2)$" "$tmp/plan" || fail "indexer image ref missing"
grep -q "^  create image=ghcr.io/sidiora-labs/perps-web@$(d 6)$" "$tmp/plan" || fail "web image ref missing"
grep -q '^  var PERPS_START_HEIGHT add 34800001$' "$tmp/plan" || fail "start height not planned"
grep -q '^  setting healthcheckPath=/healthz/live$' "$tmp/plan" || fail "indexer health not from its railway json"
grep -q '^  setting numReplicas=2$' "$tmp/plan" || fail "gateway replicas not from its railway json"
grep -q '^  var DATABASE_URL add \${{Postgres.DATABASE_URL}}$' "$tmp/plan" || fail "Postgres reference missing"
grep -q '^  registry credentials user=\$GHCR_USERNAME token=\$GHCR_TOKEN$' "$tmp/plan" || fail "registry credentials not planned"

run --apply --digests "$tmp/digests.json" --replay "$tmp/replay.json"
[ "$code" -eq 2 ] || fail "--apply without --start-height exited $code, want 2"
grep -q 'requires --start-height' "$tmp/err" || fail "--apply refusal does not name --start-height"

run --plan --digests "$tmp/short.json" --replay "$tmp/replay.json"
[ "$code" -eq 2 ] || fail "missing digest exited $code, want 2"
grep -q 'no digest for: perps-web' "$tmp/err" || fail "missing digest not named"

(unset RAILWAY_API_TOKEN; python3 "$tool" --plan --digests "$tmp/digests.json" --replay "$tmp/replay.json" >"$tmp/out" 2>"$tmp/err") && code=0 || code=$?
[ "$code" -eq 2 ] || fail "missing token exited $code, want 2"

for v in "$RAILWAY_API_TOKEN" "$GHCR_USERNAME" "$GHCR_TOKEN" "$PERPS_POSTGRES_PASSWORD" "$PERPS_NATS_CLUSTER_PASSWORD" \
	"$PERPS_NATS_INDEXER_PASSWORD" "$PERPS_NATS_GATEWAY_PASSWORD" "$PERPS_AUTH_SECRET" "$PERPS_RPC_URLS" \
	"$PERPS_COMET_URLS" sentinel-old-cluster-pw-99; do
	if grep -qF "$v" "$tmp/all"; then fail "secret value $v appears in the output"; fi
done

echo "railway-perps-project.test.sh: ok"
