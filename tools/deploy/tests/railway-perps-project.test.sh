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
export RAILWAY_WORKSPACE_ID=ws-perps
export ROLLOUT_POLL=0
export GHCR_USERNAME=sentinel-ghcr-user-1c9e
export GHCR_TOKEN=sentinel-ghcr-token-5b2d
export PERPS_POSTGRES_PASSWORD=sentinel-pg-password-88aa
export PERPS_NATS_CLUSTER_PASSWORD=sentinel-cluster-pw-41
export PERPS_NATS_INDEXER_PASSWORD=sentinel-indexer-pw-42
export PERPS_NATS_GATEWAY_PASSWORD=sentinel-gateway-pw-43
export PERPS_AUTH_SECRET=sentinel-auth-secret-44
export PERPS_RPC_URLS=sentinel-rpc-urls-45
export PERPS_COMET_URLS=sentinel-comet-urls-46

# Recorded GraphQL responses: the workspace lists the project on its second
# page, the environment exists, Postgres runs with its full variable set and
# volume, nats runs on an older image; the other seven services are missing.
cat >"$tmp/replay.json" <<JSON
[
 {"op": "Projects", "variables": {"w": "ws-perps", "a": null}, "data": {"projects": {
  "pageInfo": {"hasNextPage": true, "endCursor": "c1"}, "edges": [
  {"node": {"id": "p-other", "name": "unrelated", "environments": {"edges": []}, "services": {"edges": []}, "volumes": {"edges": []}}}]}}},
 {"op": "Projects", "variables": {"w": "ws-perps", "a": "c1"}, "data": {"projects": {
  "pageInfo": {"hasNextPage": false, "endCursor": "c2"}, "edges": [
  {"node": {"id": "p1", "name": "paxeer-x-perps",
   "environments": {"edges": [{"node": {"id": "e1", "name": "production"}}]},
   "services": {"edges": [{"node": {"id": "s-pg", "name": "Postgres"}}, {"node": {"id": "s-nats", "name": "nats"}}]},
   "volumes": {"edges": [{"node": {"id": "v1", "volumeInstances": {"edges": [
     {"node": {"serviceId": "s-pg", "environmentId": "e1", "mountPath": "/var/lib/postgresql/data"}}]}}}]}}}]}}},
 {"op": "Instance", "variables": {"s": "s-pg", "e": "e1"}, "data": {"serviceInstance": {
   "source": {"image": "ghcr.io/railwayapp-templates/postgres-ssl:18"},
   "latestDeployment": {"id": "dep-pg-0", "status": "SUCCESS"}}}},
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

(unset PERPS_AUTH_SECRET; run --plan --digests "$tmp/digests.json" --replay "$tmp/replay.json"; echo "$code" >"$tmp/code")
[ "$(cat "$tmp/code")" -eq 3 ] || fail "plan with unresolved values exited $(cat "$tmp/code"), want 3"
grep -q '^  var PERPS_AUTH_SECRET <env PERPS_AUTH_SECRET> missing$' "$tmp/out" || fail "unset env source not listed"
grep -q '^  var PERPS_START_HEIGHT <--start-height not given> missing$' "$tmp/out" || fail "missing start height not listed"
grep -q '^services=9$' "$tmp/out" || fail "plan with unresolved values is incomplete"
grep -q 'plan has 2 unresolved variables' "$tmp/err" || fail "unresolved count not reported"

(unset RAILWAY_WORKSPACE_ID; run --plan --digests "$tmp/digests.json" --replay "$tmp/replay.json" --start-height 34800001
	echo "$code" >"$tmp/code")
[ "$(cat "$tmp/code")" -eq 2 ] || fail "missing workspace exited $(cat "$tmp/code"), want 2"
grep -q 'RAILWAY_WORKSPACE_ID is not set' "$tmp/err" || fail "missing workspace not named"

# Apply replay: the same project, but nats already has its volume mounted at
# /nats. Every write the apply makes must match a recorded request body
# exactly; there is no ProjectCreate, and no VolumeCreate for nats.
python3 - "$tmp" "$root/deploy/railway-perps-project.kvx" <<'PY2'
import copy, json, os, re, sys
tmp, manifest = sys.argv[1], sys.argv[2]
base = json.load(open(f"{tmp}/replay.json"))
page2 = base[1]["data"]["projects"]["edges"][0]["node"]
page2["volumes"]["edges"].append({"node": {"id": "v2", "volumeInstances": {"edges": [
    {"node": {"serviceId": "s-nats", "environmentId": "e1", "mountPath": "/nats"}}]}}})
vars_, cur = {}, None
for line in open(manifest):
    m = re.fullmatch(r"\[vars\.([a-z0-9-]+)\]\s*", line)
    if m:
        cur = vars_.setdefault(m.group(1), {})
    elif line.startswith("["):
        cur = None
    elif cur is not None and (m := re.fullmatch(r'([A-Z_0-9]+) = "(.*)"\s*', line)):
        v = m.group(2)
        cur[m.group(1)] = os.environ[v[4:]] if v.startswith("env:") else "34800001" if v == "arg:start-height" else v
creds = {"username": os.environ["GHCR_USERNAME"], "password": os.environ["GHCR_TOKEN"]}
d = lambda n: "sha256:%064d" % n
ref = lambda img, n: {"image": f"ghcr.io/sidiora-labs/{img}@{d(n)}"}
def inst(img, n, health, replicas):
    return {"source": ref(img, n), "healthcheckPath": health, "healthcheckTimeout": 300,
            "restartPolicyType": "ALWAYS", "numReplicas": replicas, "registryCredentials": creds}
nats_upsert = {k: v for k, v in vars_["nats"].items() if k != "PORT"}
assert sorted(nats_upsert) == ["NATS_CLUSTER_PASSWORD", "NATS_GATEWAY_PASSWORD", "NATS_INDEXER_PASSWORD",
                               "NATS_MAX_FILE_STORE", "NATS_ROUTES"], sorted(nats_upsert)
writes = [
    ("InstanceUpdate", {"s": "s-nats", "e": "e1", "i": {"source": ref("perps-data-nats", 1), "registryCredentials": creds}},
     {"serviceInstanceUpdate": True}),
    ("VolumeMount", {"v": "v2", "e": "e1", "i": {"mountPath": "/data"}}, {"volumeInstanceUpdate": True}),
    ("VariablesUpsert", {"i": {"projectId": "p1", "environmentId": "e1", "serviceId": "s-nats",
                               "variables": nats_upsert, "skipDeploys": True}}, {"variableCollectionUpsert": True}),
]
created = {"nats-2": ("perps-data-nats", 1, "/healthz", 1), "nats-3": ("perps-data-nats", 1, "/healthz", 1),
           "indexer": ("perps-data-indexer", 2, "/healthz/live", 1), "gateway": ("perps-gateway", 3, "/healthz/ready", 2),
           "explorer": ("perps-explorer", 4, "/healthz/ready", 1), "portal": ("perps-portal", 5, "/healthz/ready", 1),
           "web": ("perps-web", 6, "/", 1)}
for name, (img, n, health, replicas) in created.items():
    sid = f"s-{name}"
    writes += [
        ("ServiceCreate", {"i": {"projectId": "p1", "name": name, "source": ref(img, n)}}, {"serviceCreate": {"id": sid}}),
        ("InstanceUpdate", {"s": sid, "e": "e1", "i": inst(img, n, health, replicas)}, {"serviceInstanceUpdate": True}),
        ("VariablesUpsert", {"i": {"projectId": "p1", "environmentId": "e1", "serviceId": sid,
                                   "variables": vars_[name], "skipDeploys": True}}, {"variableCollectionUpsert": True}),
    ]
    if name.startswith("nats"):
        writes.append(("VolumeCreate", {"i": {"projectId": "p1", "environmentId": "e1", "serviceId": sid, "mountPath": "/data"}},
                       {"volumeCreate": {"id": f"v-{name}"}}))
    if name in ("gateway", "explorer", "portal", "web"):
        writes.append(("DomainCreate", {"i": {"serviceId": sid, "environmentId": "e1"}},
                       {"serviceDomainCreate": {"domain": f"{name}.up.railway.app"}}))
for name in ["nats", *created]:
    writes.append(("Deploy", {"s": f"s-{name}", "e": "e1"}, {"serviceInstanceDeployV2": f"dep-{name}"}))
entries = base + [{"op": op, "variables": v, "data": data} for op, v, data in writes]
for name, status in (("crash", "CRASHED"), ("apply", "SUCCESS")):
    out = copy.deepcopy(entries) + [{"op": "DeploymentStatus", "variables": {"id": f"dep-{s}"},
                                     "data": {"deployment": {"status": status if s == "nats" else "SUCCESS"}}}
                                    for s in ["nats", *created]]
    out.append({"op": "DeploymentStatus", "variables": {"id": "dep-pg-0"}, "data": {"deployment": {"status": "SUCCESS"}}})
    json.dump(out, open(f"{tmp}/{name}-replay.json", "w"))

# Rerun replay: every service already runs the manifest with nothing to
# change, but the indexer's latest deployment CRASHED and the explorer sleeps.
node = copy.deepcopy(page2)
node["services"]["edges"] = [{"node": {"id": "s-pg", "name": "Postgres"}}] + [
    {"node": {"id": f"s-{n}", "name": n}} for n in ["nats", *created]]
node["volumes"]["edges"][1]["node"]["volumeInstances"]["edges"][0]["node"]["mountPath"] = "/data"
node["volumes"]["edges"] += [{"node": {"id": f"v-{n}", "volumeInstances": {"edges": [
    {"node": {"serviceId": f"s-{n}", "environmentId": "e1", "mountPath": "/data"}}]}}} for n in ("nats-2", "nats-3")]
rerun = [base[0], copy.deepcopy(base[1]), base[2], base[3]]
rerun[1]["data"]["projects"]["edges"][0]["node"] = node
latest = {"nats": "SUCCESS", "nats-2": "SUCCESS", "nats-3": "SUCCESS", "indexer": "CRASHED", "gateway": "SUCCESS",
          "explorer": "SLEEPING", "portal": "SUCCESS", "web": "SUCCESS"}
for name, (img, n, health, replicas) in {"nats": ("perps-data-nats", 1, "/healthz", 1), **created}.items():
    sid, pv = f"s-{name}", {"p": "p1", "e": "e1", "s": f"s-{name}"}
    instance = {k: v for k, v in inst(img, n, health, replicas).items() if k != "registryCredentials"}
    instance["latestDeployment"] = {"id": f"dep-{name}-0", "status": latest[name]}
    rerun += [{"op": "Instance", "variables": {"s": sid, "e": "e1"}, "data": {"serviceInstance": instance}},
              {"op": "Variables", "variables": pv, "data": {"variables": vars_[name]}}]
    if name in ("gateway", "explorer", "portal", "web"):
        rerun.append({"op": "Domains", "variables": pv,
                      "data": {"domains": {"serviceDomains": [{"domain": f"{name}.up.railway.app"}]}}})
rerun.append({"op": "Deploy", "variables": {"s": "s-indexer", "e": "e1"}, "data": {"serviceInstanceDeployV2": "dep-indexer-1"}})
rerun += [{"op": "DeploymentStatus", "variables": {"id": f"dep-{n}-0"}, "data": {"deployment": {"status": latest[n]}}}
          for n in latest]
rerun.append({"op": "DeploymentStatus", "variables": {"id": "dep-pg-0"}, "data": {"deployment": {"status": "SUCCESS"}}})
for name, status in (("rerun-crash", "CRASHED"), ("rerun", "SUCCESS")):
    json.dump(rerun + [{"op": "DeploymentStatus", "variables": {"id": "dep-indexer-1"},
                        "data": {"deployment": {"status": status}}}], open(f"{tmp}/{name}-replay.json", "w"))
PY2

run --plan --digests "$tmp/digests.json" --replay "$tmp/apply-replay.json" --start-height 34800001
[ "$code" -eq 0 ] || { cat "$tmp/err" >&2; fail "plan over the moved volume exited $code"; }
sed -n '/^service 2 nats image/,/^service /p' "$tmp/out" >"$tmp/nats-plan"
grep -q '^  mount /nats -> /data$' "$tmp/nats-plan" || fail "changed nats mount path not planned as a mount update"
grep -q '^  volume ' "$tmp/nats-plan" && fail "changed nats mount path planned as a new volume"

run --apply --digests "$tmp/digests.json" --replay "$tmp/apply-replay.json" --start-height 34800001
[ "$code" -eq 0 ] || { cat "$tmp/out" "$tmp/err" >&2; fail "apply exited $code"; }
grep -E '^(applied|deployed|wave) ' "$tmp/out" >"$tmp/applied"
{
	echo "wave 1 Postgres SUCCESS"
	printf 'applied nats %s\n' InstanceUpdate VolumeMount VariablesUpsert Deploy
	echo "deployed nats deployment=dep-nats"
	for s in nats-2 nats-3; do
		printf "applied $s %s\n" ServiceCreate InstanceUpdate VolumeCreate VariablesUpsert Deploy
		echo "deployed $s deployment=dep-$s"
	done
	printf 'wave 2 %s SUCCESS\n' nats nats-2 nats-3
	printf 'applied indexer %s\n' ServiceCreate InstanceUpdate VariablesUpsert Deploy
	echo "deployed indexer deployment=dep-indexer"
	echo "wave 3 indexer SUCCESS"
	for w in "4 gateway" "5 explorer portal" "6 web"; do
		set -- $w
		n="$1"
		shift
		for s in "$@"; do
			printf "applied $s %s\n" ServiceCreate InstanceUpdate VariablesUpsert DomainCreate Deploy
			echo "deployed $s deployment=dep-$s"
		done
		for s in "$@"; do echo "wave $n $s SUCCESS"; done
	done
} >"$tmp/want-applied"
diff -u "$tmp/want-applied" "$tmp/applied" || fail "apply writes or wave order differ"

run --apply --digests "$tmp/digests.json" --replay "$tmp/crash-replay.json" --start-height 34800001
[ "$code" -eq 1 ] || fail "apply with a crashed nats exited $code, want 1"
grep -q '^wave 2 nats CRASHED$' "$tmp/out" || fail "crashed nats deployment not reported"
grep -qE '^applied (indexer|gateway|explorer|portal|web) ' "$tmp/out" && fail "a later wave was applied after nats crashed"
grep -q 'wave 2 failed: nats deployment dep-nats is CRASHED; later waves were not applied' "$tmp/err" ||
	fail "wave failure not explained"

run --plan --digests "$tmp/digests.json" --replay "$tmp/rerun-replay.json" --start-height 34800001
[ "$code" -eq 0 ] || { cat "$tmp/err" >&2; fail "rerun plan exited $code"; }
grep -q '^service 3 indexer image update$' "$tmp/out" || fail "crashed unchanged indexer is not planned"
grep -q '^  redeploy latest deployment dep-indexer-0 is CRASHED$' "$tmp/out" || fail "indexer redeploy not planned"
[ "$(grep -c ' unchanged$' "$tmp/out")" -eq 8 ] || fail "rerun plan does not leave the other eight services unchanged"

run --apply --digests "$tmp/digests.json" --replay "$tmp/rerun-replay.json" --start-height 34800001
[ "$code" -eq 0 ] || { cat "$tmp/out" "$tmp/err" >&2; fail "rerun apply exited $code"; }
grep -E '^(applied|deployed|wave) ' "$tmp/out" >"$tmp/applied"
{
	echo "wave 1 Postgres SUCCESS"
	printf 'wave 2 %s SUCCESS\n' nats nats-2 nats-3
	echo "applied indexer Deploy"
	echo "deployed indexer deployment=dep-indexer-1"
	echo "wave 3 indexer SUCCESS"
	echo "wave 4 gateway SUCCESS"
	echo "wave 5 explorer SLEEPING"
	echo "wave 5 portal SUCCESS"
	echo "wave 6 web SUCCESS"
} >"$tmp/want-applied"
diff -u "$tmp/want-applied" "$tmp/applied" || fail "rerun did not redeploy only the crashed indexer and gate every wave"

run --apply --digests "$tmp/digests.json" --replay "$tmp/rerun-crash-replay.json" --start-height 34800001
[ "$code" -eq 1 ] || fail "rerun with an indexer that crashes again exited $code, want 1"
grep -q '^deployed indexer deployment=dep-indexer-1$' "$tmp/out" || fail "crashed unchanged indexer was not redeployed"
grep -q '^wave 3 indexer CRASHED$' "$tmp/out" || fail "indexer crashing again not reported"
grep -qE '^wave [456] ' "$tmp/out" && fail "a later wave passed after the redeployed indexer crashed"
grep -q 'wave 3 failed: indexer deployment dep-indexer-1 is CRASHED; later waves were not applied' "$tmp/err" ||
	fail "rerun wave failure not explained"

# Status replays built from the passing rerun: an unchanged indexer whose
# latest deployment is SKIPPED or REMOVING is redeployed like a crashed one,
# and a deployment status outside the known set fails the wave at once.
python3 - "$tmp" <<'PY2'
import json, sys
tmp = sys.argv[1]
base = json.load(open(f"{tmp}/rerun-replay.json"))
def variant(name, latest, new):
    out = json.loads(json.dumps(base))
    for e in out:
        if e["op"] == "Instance" and e["variables"]["s"] == "s-indexer":
            e["data"]["serviceInstance"]["latestDeployment"]["status"] = latest
        if e["op"] == "DeploymentStatus" and e["variables"]["id"] == "dep-indexer-1":
            e["data"]["deployment"]["status"] = new
    json.dump(out, open(f"{tmp}/{name}-replay.json", "w"))
variant("rerun-skipped", "SKIPPED", "SUCCESS")
variant("rerun-removing", "REMOVING", "SUCCESS")
variant("rerun-unknown", "CRASHED", "SOMETHING_NEW")
PY2

for st in skipped removing; do
	ST="$(echo "$st" | tr a-z A-Z)"
	run --plan --digests "$tmp/digests.json" --replay "$tmp/rerun-$st-replay.json" --start-height 34800001
	[ "$code" -eq 0 ] || { cat "$tmp/err" >&2; fail "rerun plan with a $ST indexer exited $code"; }
	grep -q "^  redeploy latest deployment dep-indexer-0 is $ST$" "$tmp/out" || fail "$ST indexer redeploy not planned"
	run --apply --digests "$tmp/digests.json" --replay "$tmp/rerun-$st-replay.json" --start-height 34800001
	[ "$code" -eq 0 ] || { cat "$tmp/out" "$tmp/err" >&2; fail "rerun apply with a $ST indexer exited $code"; }
	grep -q '^deployed indexer deployment=dep-indexer-1$' "$tmp/out" || fail "$ST unchanged indexer was not redeployed"
	grep -q '^wave 3 indexer SUCCESS$' "$tmp/out" || fail "redeployed $ST indexer did not pass wave 3"
	grep -q '^wave 5 explorer SLEEPING$' "$tmp/out" || fail "sleeping explorer did not pass wave 5"
	grep -q '^wave 6 web SUCCESS$' "$tmp/out" || fail "rerun with a $ST indexer did not reach wave 6"
done

set +e
ROLLOUT_TIMEOUT=1800 timeout 60 python3 "$tool" --apply --digests "$tmp/digests.json" --replay "$tmp/rerun-unknown-replay.json" \
	--start-height 34800001 >"$tmp/out" 2>"$tmp/err"
code=$?
set -e
cat "$tmp/out" "$tmp/err" >>"$tmp/all"
[ "$code" -eq 1 ] || fail "unknown deployment status exited $code, want 1 at once"
grep -q "^wave 3 indexer unknown status 'SOMETHING_NEW'$" "$tmp/out" || fail "unknown status not reported"
grep -qE '^wave [456] ' "$tmp/out" && fail "a later wave passed after an unknown status"
grep -q "wave 3 failed: indexer deployment dep-indexer-1 is unknown status 'SOMETHING_NEW'; later waves were not applied" "$tmp/err" ||
	fail "unknown status failure does not name the service and status"

python3 - "$tmp" <<'PY2'
import json, sys
r = json.load(open(f"{sys.argv[1]}/replay.json"))
dup = json.loads(json.dumps(r[1]["data"]["projects"]["edges"][0]))
dup["node"]["id"] = "p2"
r[1]["data"]["projects"]["edges"].append(dup)
json.dump(r, open(f"{sys.argv[1]}/dup-replay.json", "w"))
PY2
run --apply --digests "$tmp/digests.json" --replay "$tmp/dup-replay.json" --start-height 34800001
[ "$code" -eq 1 ] || fail "two same-named projects exited $code, want 1"
grep -q 'has 2 projects named paxeer-x-perps' "$tmp/err" || fail "duplicate project not refused"
grep -q '^applied ' "$tmp/out" && fail "apply wrote with an ambiguous project"

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
