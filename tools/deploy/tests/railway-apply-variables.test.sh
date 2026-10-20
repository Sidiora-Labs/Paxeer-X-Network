#!/usr/bin/env bash
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../../.." && pwd)"
tool="$root/tools/deploy/railway-apply-variables.py"
replay="$here/fixtures/railway-apply-variables-replay.json"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fail() {
	echo "FAIL: $*" >&2
	exit 1
}

# The charts service from the real manifest; every name it needs gets a sentinel
# value from --values, except RPC_URL, which comes from the environment under --from-env.
python3 - "$root/launchpad/deploy/railway/variables.json" "$tmp" "$replay" <<'PY'
import json, sys
m = json.load(open(sys.argv[1]))
charts = m["charts"]
json.dump({"charts": charts}, open(f"{sys.argv[2]}/manifest.json", "w"))
# charts first, then a service the project does not have: apply must refuse before writing charts.
json.dump({"charts": charts, "ghost": {"fixed": {"NODE_ENV": "production"}}}, open(f"{sys.argv[2]}/ghost.json", "w"))
needed = [n for k, v in charts.items() if isinstance(v, list) and k not in ("secret", "secrets")
          for n in v if n not in charts.get("fixed", {})]
values = {n: f"sentinel-value-{i:02d}" for i, n in enumerate(needed) if n != "RPC_URL"}
values["WEBHOOK_HMAC_SECRET"] = "sentinel-webhook-secret-9d4e"
values["DATABASE_URL"] = "postgresql://charts:sentinel-db-password-77@postgres.railway.internal:5432/railway"
json.dump({"charts": values}, open(f"{sys.argv[2]}/values.json", "w"))
bad = dict(values, REDIS_URL="redis://line-one\nline-two")
json.dump({"charts": bad}, open(f"{sys.argv[2]}/newline.json", "w"))

# Apply replay: the recorded reads plus the two writes the apply must send for charts.
replay = json.load(open(sys.argv[3]))
live = next(e for e in replay if e["op"] == "Variables")["data"]["variables"]
want = dict(charts["fixed"], **values, RPC_URL="https://rpc.sentinel.invalid")
upsert = {k: v for k, v in want.items() if live.get(k) != v}
ids = {"projectId": "proj-test", "environmentId": "env-prod", "serviceId": "svc-charts"}
replay += [
    {"op": "VariablesUpsert", "variables": {"i": dict(ids, variables=upsert, skipDeploys=True)},
     "data": {"variableCollectionUpsert": True}},
    {"op": "InstanceUpdate", "variables": {"s": "svc-charts", "e": "env-prod", "i": {"registryCredentials": {
        "username": "sentinel-ghcr-user-4a1f", "password": "sentinel-ghcr-token-8c3e"}}},
     "data": {"serviceInstanceUpdate": True}},
]
json.dump(replay, open(f"{sys.argv[2]}/apply-replay.json", "w"))
print(len(upsert), file=open(f"{sys.argv[2]}/upsert-count", "w"))
PY

export RAILWAY_API_TOKEN=sentinel-railway-token-2b61
export RPC_URL=https://rpc.sentinel.invalid
run() { python3 "$tool" proj-test production --manifest "$tmp/manifest.json" --replay "$replay" "$@"; }

run --plan --values "$tmp/values.json" --from-env --registry-credentials >"$tmp/plan.out" 2>&1 || fail "plan exited $?: $(cat "$tmp/plan.out")"
cat "$tmp/plan.out"
for name in EVENT_EMITTER_ADDRESS POOL_BEACON_ADDRESS REDIS_BULL_URL WEBHOOK_HMAC_SECRET; do
	grep -q "^  missing $name = " "$tmp/plan.out" || fail "plan does not list missing $name"
done
grep -q '^  missing WEBHOOK_HMAC_SECRET = \*\*\*\*$' "$tmp/plan.out" || fail "secret value not masked"
grep -q '^  differs RUNTIME_PROFILE platform -> container$' "$tmp/plan.out" || fail "differing fixed value not shown"
grep -q '^  differs DATABASE_URL \*\*\*\* -> \*\*\*\*$' "$tmp/plan.out" || fail "password URL not masked"
grep -q '^  differs RPC_URL https://rpc.example.invalid -> https://rpc.sentinel.invalid$' "$tmp/plan.out" ||
	fail "environment value not used"
grep -q '^  extra CHART_FEATURES_CONFIG_FILE (kept)$' "$tmp/plan.out" || fail "extra name not reported"
grep -q '^  registry credentials user=\$GHCR_USERNAME' "$tmp/plan.out" || fail "registry credentials not planned"
grep -q '^  missing NODE_ENV' "$tmp/plan.out" && fail "unchanged name listed as missing"
if grep -Eq 'sentinel-(webhook-secret|db-password)|live-(db|redis)-password|sentinel-railway-token' "$tmp/plan.out"; then
	fail "plan printed a secret"
fi

set +e
env -u RAILWAY_API_TOKEN python3 "$tool" proj-test production --manifest "$tmp/manifest.json" --replay "$replay" \
	--apply --values "$tmp/values.json" >"$tmp/notoken.out" 2>&1
code=$?
run --apply --values "$tmp/newline.json" >"$tmp/newline.out" 2>&1
nl=$?
env -u RPC_URL python3 "$tool" proj-test production --manifest "$tmp/manifest.json" --replay "$replay" \
	--plan --values "$tmp/values.json" --from-env >"$tmp/unset.out" 2>&1
unset_code=$?
env -u RPC_URL python3 "$tool" proj-test production --manifest "$tmp/manifest.json" --replay "$replay" \
	--apply --values "$tmp/values.json" --from-env >"$tmp/unset-apply.out" 2>&1
unset_apply=$?
run --plan --values "$tmp/values.json" >"$tmp/noenv.out" 2>&1
noenv=$?
run --apply --values "$tmp/values.json" >"$tmp/noenv-apply.out" 2>&1
noenv_apply=$?
GHCR_USERNAME=sentinel-ghcr-user-4a1f GHCR_TOKEN=sentinel-ghcr-token-8c3e python3 "$tool" proj-test production \
	--manifest "$tmp/ghost.json" --replay "$tmp/apply-replay.json" --apply --values "$tmp/values.json" --from-env \
	--registry-credentials >"$tmp/ghost.out" 2>&1
ghost=$?
GHCR_USERNAME=sentinel-ghcr-user-4a1f GHCR_TOKEN=sentinel-ghcr-token-8c3e python3 "$tool" proj-test production \
	--manifest "$tmp/manifest.json" --replay "$tmp/apply-replay.json" --apply --values "$tmp/values.json" --from-env \
	--registry-credentials >"$tmp/apply.out" 2>&1
apply_code=$?
set -e
[ "$code" -eq 2 ] || fail "--apply without a token exited $code, want 2"
grep -q 'RAILWAY_API_TOKEN is not set' "$tmp/notoken.out" || fail "no token message"
[ "$nl" -eq 2 ] || fail "newline value exited $nl, want 2"
grep -q 'charts: REDIS_URL contains a newline' "$tmp/newline.out" || fail "no newline message"
[ "$unset_code" -eq 3 ] || fail "plan with an unresolved name exited $unset_code, want 3"
grep -q '^  unresolved RPC_URL has no value' "$tmp/unset.out" || fail "plan does not list the unresolved name"
grep -q '^  missing WEBHOOK_HMAC_SECRET = \*\*\*\*$' "$tmp/unset.out" || fail "plan with an unresolved name is incomplete"
grep -q 'extra RPC_URL' "$tmp/unset.out" && fail "unresolved name reported as extra"
[ "$unset_apply" -eq 2 ] || fail "apply with an unresolved name exited $unset_apply, want 2"
grep -q 'refused charts: RPC_URL has no value' "$tmp/unset-apply.out" || fail "no apply refusal message"
# RPC_URL is exported, but without --from-env the shell environment is never a value source.
[ "$noenv" -eq 3 ] || fail "plan without --from-env exited $noenv, want 3"
grep -q '^  unresolved RPC_URL has no value (manifest, --values; --from-env not given)$' "$tmp/noenv.out" ||
	fail "plan without --from-env took RPC_URL from the environment"
grep -q 'rpc.sentinel.invalid' "$tmp/noenv.out" && fail "plan without --from-env printed the environment value"
[ "$noenv_apply" -eq 2 ] || fail "apply without --from-env exited $noenv_apply, want 2"
grep -q 'refused charts: RPC_URL has no value' "$tmp/noenv-apply.out" || fail "apply without --from-env not refused"
[ "$ghost" -eq 1 ] || fail "apply with a service missing from the project exited $ghost, want 1"
grep -q '^service ghost not in project$' "$tmp/ghost.out" || fail "missing service not named"
grep -q 'refused: 1 manifest services are not in project proj-test; nothing written' "$tmp/ghost.out" ||
	fail "no refusal for the missing service"
grep -q 'applied' "$tmp/ghost.out" && fail "apply wrote to charts before refusing the missing service"
[ "$apply_code" -eq 0 ] || fail "apply exited $apply_code: $(cat "$tmp/apply.out")"
grep -q '^  applied registry credentials$' "$tmp/apply.out" || fail "registry credentials not applied to a service without an image read"
grep -q "^  applied $(cat "$tmp/upsert-count") variables$" "$tmp/apply.out" || fail "apply did not upsert the changed variables"
grep -q 'sentinel-ghcr-token' "$tmp/apply.out" && fail "apply printed the registry token"
# The user service from the real manifest carries a signing seed: plan and apply never print it.
python3 - "$root/launchpad/deploy/railway/variables.json" "$tmp" "$replay" <<'PY'
import json, sys
user = json.load(open(sys.argv[1]))["user"]
assert "ACCOUNT_SERVICE_SIGNING_SEED" in user["secrets"], "the manifest does not list the signing seed as a secret"
json.dump({"user": user}, open(f"{sys.argv[2]}/user.json", "w"))
needed = [n for k, v in user.items() if isinstance(v, list) and k not in ("secret", "secrets")
          for n in v if n not in user["fixed"]]
values = {n: f"sentinel-user-{i:02d}" for i, n in enumerate(needed)}
values["ACCOUNT_SERVICE_SIGNING_SEED"] = "sentinel-signing-seed-6f0a"
json.dump({"user": values}, open(f"{sys.argv[2]}/user-values.json", "w"))
json.dump([v for n, v in values.items() if n in user["secrets"]] + ["sentinel-live-seed-1c9b"],
          open(f"{sys.argv[2]}/user-secret-values.json", "w"))
project = next(e for e in json.load(open(sys.argv[3])) if e["op"] == "Project")
project["data"]["project"]["services"]["edges"] = [{"node": {"id": "svc-user", "name": "user"}}]
want = dict(user["fixed"], **values)
json.dump([project,
           {"op": "Variables", "variables": {"p": "proj-test", "e": "env-prod", "s": "svc-user"},
            "data": {"variables": {"ACCOUNT_SERVICE_SIGNING_SEED": "sentinel-live-seed-1c9b"}}},
           {"op": "VariablesUpsert", "variables": {"i": {"projectId": "proj-test", "environmentId": "env-prod",
            "serviceId": "svc-user", "variables": want, "skipDeploys": True}},
            "data": {"variableCollectionUpsert": True}}], open(f"{sys.argv[2]}/user-replay.json", "w"))
# The same service with the seed left out of its secrets list, and with no secrets list at all.
json.dump({"user": dict(user, secrets=[n for n in user["secrets"] if n != "ACCOUNT_SERVICE_SIGNING_SEED"])},
          open(f"{sys.argv[2]}/unlisted.json", "w"))
json.dump({"user": {k: v for k, v in user.items() if k != "secrets"}}, open(f"{sys.argv[2]}/nolist.json", "w"))
PY
user() { python3 "$tool" proj-test production --replay "$tmp/user-replay.json" --values "$tmp/user-values.json" "$@"; }
set +e
user --manifest "$tmp/user.json" --plan >"$tmp/user-plan.out" 2>&1
user_plan=$?
user --manifest "$tmp/user.json" --apply >"$tmp/user-apply.out" 2>&1
user_apply=$?
user --manifest "$tmp/unlisted.json" --plan >"$tmp/unlisted.out" 2>&1
unlisted=$?
user --manifest "$tmp/nolist.json" --apply >"$tmp/nolist.out" 2>&1
nolist=$?
set -e
[ "$user_plan" -eq 0 ] || fail "user plan exited $user_plan: $(cat "$tmp/user-plan.out")"
[ "$user_apply" -eq 0 ] || fail "user apply exited $user_apply: $(cat "$tmp/user-apply.out")"
grep -q '^  differs ACCOUNT_SERVICE_SIGNING_SEED \*\*\*\* -> \*\*\*\*$' "$tmp/user-plan.out" || fail "signing seed not masked in plan"
grep -q '^  applied ' "$tmp/user-apply.out" || fail "user apply did not upsert"
python3 - "$tmp/user-secret-values.json" "$tmp/user-plan.out" "$tmp/user-apply.out" <<'PY' || fail "user plan or apply printed a secret value"
import json, sys
secrets = json.load(open(sys.argv[1]))
for path in sys.argv[2:]:
    out = open(path).read()
    leaked = [v for v in secrets if v in out]
    assert not leaked, f"{path} printed {leaked}"
PY
[ "$unlisted" -eq 2 ] || fail "seed missing from the secrets list exited $unlisted, want 2"
grep -q "bad input: user: ACCOUNT_SERVICE_SIGNING_SEED match the secret name pattern but are not in the service's secrets list" \
	"$tmp/unlisted.out" || fail "no refusal naming the unlisted seed: $(cat "$tmp/unlisted.out")"
grep -q 'sentinel-' "$tmp/unlisted.out" && fail "refused plan printed a value"
[ "$nolist" -eq 2 ] || fail "service without a secrets list exited $nolist, want 2"
grep -q 'ACCOUNT_SERVICE_SIGNING_SEED' "$tmp/nolist.out" || fail "refusal without a secrets list does not name the seed"
grep -q 'applied' "$tmp/nolist.out" && fail "apply wrote before refusing the missing secrets list"
# A GraphQL invalid-input error that echoes the submitted input, and a transcript with no
# response for the write: neither error path prints a submitted or live secret value.
python3 - "$tmp" <<'PY'
import json, sys
replay = json.load(open(f"{sys.argv[1]}/apply-replay.json"))
upsert = next(e for e in replay if e["op"] == "VariablesUpsert")
echo = json.dumps(upsert["variables"]["i"])
upsert.pop("data")
upsert["errors"] = [{"message": f'Variable "$i" got invalid value {echo}; Bearer sentinel-railway-token-2b61',
                     "extensions": {"code": "GRAPHQL_VALIDATION_FAILED"}}]
json.dump(replay, open(f"{sys.argv[1]}/echo-replay.json", "w"))
PY
set +e
GHCR_USERNAME=sentinel-ghcr-user-4a1f GHCR_TOKEN=sentinel-ghcr-token-8c3e python3 "$tool" proj-test production \
	--manifest "$tmp/manifest.json" --replay "$tmp/echo-replay.json" --apply --values "$tmp/values.json" --from-env \
	--registry-credentials >"$tmp/echo.out" 2>"$tmp/echo.err"
echo_code=$?
GHCR_USERNAME=sentinel-ghcr-user-4a1f GHCR_TOKEN=sentinel-ghcr-token-8c3e python3 "$tool" proj-test production \
	--manifest "$tmp/manifest.json" --replay "$replay" --apply --values "$tmp/values.json" --from-env \
	--registry-credentials >"$tmp/miss.out" 2>"$tmp/miss.err"
miss_code=$?
set -e
[ "$echo_code" -eq 1 ] || fail "apply against an invalid-input error exited $echo_code, want 1"
grep -q '^railway call failed: Railway GraphQL error: Variable "\$i" got invalid value .*\.\.\. ([0-9]* more characters)$' "$tmp/echo.err" ||
	fail "invalid-input error not reported bounded: $(cat "$tmp/echo.err")"
[ "$(wc -c <"$tmp/echo.err")" -lt 700 ] || fail "invalid-input error is not bounded"
[ "$miss_code" -eq 1 ] || fail "apply against a transcript without the write exited $miss_code, want 1"
grep -q '^railway call failed: replay has no response for VariablesUpsert with variables i.environmentId, i.projectId, i.serviceId, i.skipDeploys, i.variables.' \
	"$tmp/miss.err" || fail "replay miss does not name the variables: $(cat "$tmp/miss.err")"
for f in echo.out echo.err miss.out miss.err; do
	if grep -Eq 'sentinel-(webhook-secret|db-password|ghcr-token|ghcr-user|railway-token)' "$tmp/$f"; then
		fail "$f printed a secret value: $(cat "$tmp/$f")"
	fi
done
echo PASS railway-apply-variables
