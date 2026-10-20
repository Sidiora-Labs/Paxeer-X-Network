#!/usr/bin/env python3
import json
import os
import re
import sys
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools/rollout"))
from rollout import API, GHCR, Malformed, load_digests  # noqa: E402

MANIFEST = ROOT / "deploy/railway-perps-project.kvx"
NAME = r"[a-z0-9][a-z0-9-]*"
LINE = re.compile(r'^([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(?:"((?:[^"\\]|\\.)*)"|(-?\d+))\s*(?:#.*)?$')
KEYS = {
    "database": ({"kind", "image", "railway_service", "volume", "order"}, set()),
    "image": ({"kind", "image", "railway_service", "order"},
              {"config", "health", "restart", "replicas", "volume", "public"}),
}
USAGE = ("usage: railway-perps-project.py --plan|--apply --digests <image-digest-map.json> "
         "[--start-height <n>] [--replay <responses.json>] [--manifest <kvx>]")


def parse(path):
    sections, current = {}, None
    for no, raw in enumerate(Path(path).read_text().splitlines(), 1):
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        head = re.fullmatch(r"\[([a-z0-9_.-]+)\]", line)
        if head:
            current = head.group(1)
            if current in sections:
                raise Malformed(f"{path}:{no}: duplicate section [{current}]")
            sections[current] = {}
            continue
        m = LINE.match(line)
        if not m or current is None:
            raise Malformed(f"{path}:{no}: not a key = value line in a section: {line}")
        key = m.group(1)
        if key in sections[current]:
            raise Malformed(f"{path}:{no}: duplicate key {key} in [{current}]")
        sections[current][key] = m.group(2) if m.group(3) is None else int(m.group(3))
    return sections


def load(path):
    sections = parse(path)
    meta = sections.pop("meta", None)
    if not meta or meta.get("kind") != "railway-project" or not meta.get("project") or not meta.get("environment"):
        raise Malformed(f"{path}: [meta] needs kind = \"railway-project\", project and environment")
    out = []
    for section, kv in sections.items():
        m = re.fullmatch(r"(service|vars)\.(" + NAME + ")", section)
        if not m:
            raise Malformed(f"{path}: unexpected section [{section}]")
        if m.group(1) == "vars":
            if f"service.{m.group(2)}" not in sections:
                raise Malformed(f"[{section}] has no [service.{m.group(2)}]")
            continue
        svc = dict(kv, name=m.group(2), vars=sections.get(f"vars.{m.group(2)}", {}))
        if kv.get("kind") not in KEYS:
            raise Malformed(f"[{section}]: kind must be database or image")
        need, opt = KEYS[kv["kind"]]
        if not need <= set(kv) or set(kv) - need - opt:
            raise Malformed(f"[{section}]: missing {sorted(need - set(kv))} unexpected {sorted(set(kv) - need - opt)}")
        if kv["kind"] == "image":
            if not re.fullmatch(NAME, kv["image"]):
                raise Malformed(f"[{section}]: bad image {kv['image']!r}")
            if "config" in kv:
                if {"health", "restart", "replicas"} & set(kv):
                    raise Malformed(f"[{section}]: health, restart and replicas come from {kv['config']}")
                cfg = json.loads((ROOT / kv["config"]).read_text())
                d = cfg["deploy"]
                svc.update(health=d["healthcheckPath"], timeout=d["healthcheckTimeout"],
                           restart=d["restartPolicyType"], replicas=d["numReplicas"])
                if set(cfg["variables"]) != set(svc["vars"]):
                    raise Malformed(f"[vars.{svc['name']}] must set exactly {sorted(cfg['variables'])}")
            elif not {"health", "restart", "replicas"} <= set(kv):
                raise Malformed(f"[{section}]: needs config or health, restart and replicas")
            svc.setdefault("timeout", 300)
            if not re.fullmatch(r"/[A-Za-z0-9._/-]*", svc["health"]):
                raise Malformed(f"[{section}]: health must be /<path>")
            if svc["restart"] not in ("ALWAYS", "ON_FAILURE", "NEVER"):
                raise Malformed(f"[{section}]: bad restart policy {svc['restart']!r}")
        for k, v in svc["vars"].items():
            if not isinstance(v, str):
                raise Malformed(f"[vars.{svc['name']}]: {k} must be a string")
            if v.startswith(("env:", "arg:")) and not re.fullmatch(r"env:[A-Z_][A-Z0-9_]*|arg:start-height", v):
                raise Malformed(f"[vars.{svc['name']}]: bad source {v!r}")
        out.append(svc)
    if not out:
        raise Malformed(f"{path}: no [service.*] sections")
    return meta, sorted(out, key=lambda s: s["order"])


def http_transport(token):
    def call(query, variables):
        req = urllib.request.Request(
            API, data=json.dumps({"query": query, "variables": variables}).encode(),
            headers={"Content-Type": "application/json", "Authorization": f"Bearer {token}"})
        with urllib.request.urlopen(req, timeout=30) as resp:
            body = json.load(resp)
        if body.get("errors"):
            raise RuntimeError(json.dumps(body["errors"]))
        return body["data"]
    return call


def replay_transport(path):
    entries = json.loads(Path(path).read_text())

    def call(query, variables):
        op = re.match(r"\s*(?:query|mutation)\s+(\w+)", query).group(1)
        for e in entries:
            if e["op"] == op and e.get("variables", {}) == variables:
                return e["data"]
        raise RuntimeError(f"replay has no response for {op} {json.dumps(variables, sort_keys=True)}")
    return call


def read_only(transport):
    def call(query, variables):
        if query.lstrip().startswith("mutation"):
            raise RuntimeError("--plan refuses a write")
        return transport(query, variables)
    return call


def edges(conn):
    return [e["node"] for e in (conn or {}).get("edges", [])]


def current_state(gql, meta, plan):
    projects = edges(gql("query Projects { projects { edges { node { id name "
                         "environments { edges { node { id name } } } "
                         "services { edges { node { id name } } } "
                         "volumes { edges { node { id volumeInstances { edges { node { serviceId environmentId mountPath } } } } } } "
                         "} } } }", {})["projects"])
    project = next((p for p in projects if p["name"] == meta["project"]), None)
    state = {"project": project, "env": None, "services": {}}
    if not project:
        return state
    env = next((e for e in edges(project["environments"]) if e["name"] == meta["environment"]), None)
    state["env"] = env
    ids = {s["name"]: s["id"] for s in edges(project["services"])}
    mounts = {}
    for vol in edges(project["volumes"]):
        for vi in edges(vol["volumeInstances"]):
            if env and vi["environmentId"] == env["id"]:
                mounts[vi["serviceId"]] = vi["mountPath"]
    for svc in plan:
        sid = ids.get(svc["railway_service"])
        if not sid or not env:
            continue
        v = {"s": sid, "e": env["id"]}
        pv = {"p": project["id"], "e": env["id"], "s": sid}
        instance = gql("query Instance($s: String!, $e: String!) { serviceInstance(serviceId: $s, environmentId: $e) { "
                       "source { image } healthcheckPath healthcheckTimeout restartPolicyType numReplicas } }",
                       v)["serviceInstance"] or {}
        variables = gql("query Variables($p: String!, $e: String!, $s: String!) { "
                        "variables(projectId: $p, environmentId: $e, serviceId: $s, unrendered: true) }",
                        pv)["variables"] or {}
        domains = []
        if svc.get("public"):
            domains = (gql("query Domains($p: String!, $e: String!, $s: String!) { "
                           "domains(projectId: $p, environmentId: $e, serviceId: $s) { serviceDomains { domain } } }",
                           pv)["domains"] or {}).get("serviceDomains") or []
        state["services"][svc["railway_service"]] = {
            "id": sid, "instance": instance, "variables": variables,
            "volume": mounts.get(sid), "domains": domains}
    return state


def resolve(value, start_height):
    if value.startswith("env:"):
        return os.environ.get(value[4:]), f"<env {value[4:]}>"
    if value == "arg:start-height":
        return start_height, (start_height or "<--start-height not given>")
    return value, value


def desired_instance(svc, digests):
    if svc["kind"] == "database":
        return {"source": {"image": svc["image"]}}
    return {"source": {"image": f"{GHCR}/{svc['image']}@{digests[svc['image']]}"},
            "healthcheckPath": svc["health"], "healthcheckTimeout": svc["timeout"],
            "restartPolicyType": svc["restart"], "numReplicas": svc["replicas"]}


def plan_service(svc, cur, digests, start_height):
    want = desired_instance(svc, digests)
    steps = []
    have = (cur or {}).get("instance") or {}
    diff = {k: v for k, v in want.items() if have.get(k) != v}
    if cur is None:
        steps.append(("create", f"image={want['source']['image']}"))
    if svc["kind"] == "image":
        if cur is None or "source" in diff:
            steps.append(("registry", "credentials user=$GHCR_USERNAME token=$GHCR_TOKEN"))
        settings = {k: v for k, v in diff.items() if k != "source"}
        if "source" in diff and cur is not None:
            steps.append(("image", f"{have.get('source', {}).get('image')} -> {want['source']['image']}"))
        for k in sorted(settings):
            steps.append(("setting", f"{k}={settings[k]}"))
    elif "source" in diff and cur is not None:
        steps.append(("image", f"{have.get('source', {}).get('image')} -> {want['source']['image']}"))
    if svc.get("volume") and (cur is None or cur.get("volume") != svc["volume"]):
        steps.append(("volume", svc["volume"]))
    if svc.get("public") and (cur is None or not cur.get("domains")):
        steps.append(("domain", "railway-provided"))
    have_vars = (cur or {}).get("variables") or {}
    upsert = {}
    for key in sorted(svc["vars"]):
        value, shown = resolve(svc["vars"][key], start_height)
        if value is None:
            steps.append(("var", f"{key} {shown} missing"))
        elif key not in have_vars:
            steps.append(("var", f"{key} add {shown}"))
            upsert[key] = value
        elif have_vars[key] != value:
            steps.append(("var", f"{key} change {shown}"))
            upsert[key] = value
    return want, diff, steps, upsert


def apply_service(gql, svc, state, want, diff, steps, upsert):
    project, env = state["project"]["id"], state["env"]["id"]
    cur = state["services"].get(svc["railway_service"])
    sid = cur["id"] if cur else gql(
        "mutation ServiceCreate($i: ServiceCreateInput!) { serviceCreate(input: $i) { id } }",
        {"i": {"projectId": project, "name": svc["railway_service"], "source": want["source"]}})["serviceCreate"]["id"]
    ids = {"s": sid, "e": env}
    update = dict(want) if cur is None else dict(diff)
    kinds = {k for k, _ in steps}
    if "registry" in kinds:
        update["registryCredentials"] = {"username": os.environ["GHCR_USERNAME"], "password": os.environ["GHCR_TOKEN"]}
    if update:
        gql("mutation InstanceUpdate($s: String!, $e: String!, $i: ServiceInstanceUpdateInput!) { "
            "serviceInstanceUpdate(serviceId: $s, environmentId: $e, input: $i) }", dict(ids, i=update))
    if "volume" in kinds:
        gql("mutation VolumeCreate($i: VolumeCreateInput!) { volumeCreate(input: $i) { id } }",
            {"i": {"projectId": project, "environmentId": env, "serviceId": sid, "mountPath": svc["volume"]}})
    if upsert:
        gql("mutation VariablesUpsert($i: VariableCollectionUpsertInput!) { variableCollectionUpsert(input: $i) }",
            {"i": {"projectId": project, "environmentId": env, "serviceId": sid, "variables": upsert, "skipDeploys": True}})
    if "domain" in kinds:
        gql("mutation DomainCreate($i: ServiceDomainCreateInput!) { serviceDomainCreate(input: $i) { domain } }",
            {"i": {"serviceId": sid, "environmentId": env}})
    if steps:
        deployment = gql("mutation Deploy($s: String!, $e: String!) { serviceInstanceDeployV2(serviceId: $s, environmentId: $e) }",
                         ids)["serviceInstanceDeployV2"]
        print(f"deployed {svc['railway_service']} deployment={deployment}")


def main(argv, transport=None):
    opts = {"--digests": None, "--start-height": None, "--replay": None, "--manifest": str(MANIFEST)}
    mode = None
    while argv:
        if argv[0] in ("--plan", "--apply") and mode is None:
            mode, argv = argv[0][2:], argv[1:]
        elif argv[0] in opts and len(argv) > 1:
            opts[argv[0]], argv = argv[1], argv[2:]
        else:
            print(USAGE, file=sys.stderr)
            return 2
    if mode is None:
        print(USAGE, file=sys.stderr)
        return 2
    token = os.environ.get("RAILWAY_API_TOKEN")
    if not token:
        print("RAILWAY_API_TOKEN is not set", file=sys.stderr)
        return 2
    start_height = opts["--start-height"]
    if start_height is not None and not re.fullmatch(r"[1-9][0-9]*", start_height):
        print("--start-height must be a positive integer (the upgrade height plus one)", file=sys.stderr)
        return 2
    if mode == "apply" and start_height is None:
        print("--apply requires --start-height (the upgrade height plus one)", file=sys.stderr)
        return 2
    try:
        meta, plan = load(opts["--manifest"])
    except (Malformed, OSError, KeyError, ValueError) as e:
        print(f"malformed manifest: {e}", file=sys.stderr)
        return 2
    if not opts["--digests"]:
        print("--digests is required", file=sys.stderr)
        return 2
    try:
        digests = load_digests(opts["--digests"])
    except (ValueError, OSError) as e:
        print(f"bad digest map: {e}", file=sys.stderr)
        return 2
    missing = sorted({s["image"] for s in plan if s["kind"] == "image"} - set(digests))
    if missing:
        print(f"digest map has no digest for: {','.join(missing)}", file=sys.stderr)
        return 2
    if transport is None:
        transport = replay_transport(opts["--replay"]) if opts["--replay"] else http_transport(token)
    gql = read_only(transport) if mode == "plan" else transport
    try:
        state = current_state(gql, meta, plan)
    except (RuntimeError, OSError, KeyError, TypeError) as e:
        print(f"railway query failed: {e}", file=sys.stderr)
        return 1
    print(f"railway-perps-project {mode} project={meta['project']} env={meta['environment']}")
    print(f"project {meta['project']} {'exists' if state['project'] else 'create'}")
    print(f"environment {meta['environment']} {'exists' if state['env'] else 'create'}")
    rows = []
    for svc in plan:
        cur = state["services"].get(svc["railway_service"])
        want, diff, steps, upsert = plan_service(svc, cur, digests, start_height)
        status = "create" if cur is None else ("update" if steps else "unchanged")
        print(f"service {svc['order']} {svc['railway_service']} {svc['kind']} {status}")
        for kind, text in steps:
            print(f"  {kind} {text}")
        rows.append((svc, want, diff, steps, upsert))
    print(f"services={len(plan)}")
    if mode == "plan":
        return 0

    needed = {"GHCR_USERNAME", "GHCR_TOKEN"} | {v[4:] for s in plan for v in s["vars"].values() if v.startswith("env:")}
    unset = sorted(n for n in needed if not os.environ.get(n))
    if unset:
        print(f"--apply needs these environment variables: {','.join(unset)}", file=sys.stderr)
        return 2
    try:
        if not state["project"]:
            created = gql("mutation ProjectCreate($i: ProjectCreateInput!) { projectCreate(input: $i) { "
                          "id environments { edges { node { id name } } } } }",
                          {"i": {"name": meta["project"], "defaultEnvironmentName": meta["environment"]}})["projectCreate"]
            state["project"] = created
            state["env"] = next((e for e in edges(created["environments"]) if e["name"] == meta["environment"]), None)
        if not state["env"]:
            state["env"] = gql("mutation EnvironmentCreate($i: EnvironmentCreateInput!) { environmentCreate(input: $i) { id } }",
                               {"i": {"projectId": state["project"]["id"], "name": meta["environment"]}})["environmentCreate"]
        for svc, want, diff, steps, upsert in rows:
            apply_service(gql, svc, state, want, diff, steps, upsert)
    except (RuntimeError, OSError, KeyError, TypeError) as e:
        print(f"railway write failed: {e}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
