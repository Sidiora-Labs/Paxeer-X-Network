#!/usr/bin/env python3
import hashlib
import json
import os
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools/rollout"))
from rollout import gql as http_transport  # noqa: E402

MANIFEST = ROOT / "launchpad/deploy/railway/variables.json"
SECRET = re.compile(r"SECRET|TOKEN|PASSWORD|KEY|DSN|URL_WITH_PASSWORD")
URL_WITH_PASSWORD = re.compile(r"://[^/@\s]*:[^/@\s]*@")
USAGE = ("usage: railway-apply-variables.py <project-id> <environment> --plan|--apply "
         "[--values <values.json>] [--from-env] [--registry-credentials] [--replay <responses.json>] [--manifest <json>]")


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


def load_manifest(path):
    data = json.loads(Path(path).read_text())
    if not isinstance(data, dict) or not data:
        raise ValueError("manifest must be a non-empty object of services")
    out = {}
    for name, svc in data.items():
        fixed = svc.get("fixed", {})
        if not isinstance(fixed, dict) or not all(isinstance(v, str) for v in fixed.values()):
            raise ValueError(f"{name}: fixed must map names to strings")
        # Every list of names other than the secret marker is a set of names that need a value.
        needed = []
        for key, value in svc.items():
            if key in ("fixed", "secret", "secrets") or not isinstance(value, list):
                continue
            if not all(isinstance(n, str) for n in value):
                raise ValueError(f"{name}: {key} must list names")
            needed += [n for n in value if n not in fixed and n not in needed]
        marked = svc.get("secret", svc.get("secrets"))
        out[name] = {"fixed": fixed, "needed": needed, "secrets": None if marked is None else set(marked)}
    return out


def is_secret(svc, name, value):
    if svc["secrets"] is not None:
        return name in svc["secrets"]
    return bool(SECRET.search(name) or URL_WITH_PASSWORD.search(value or ""))


def show(svc, name, value):
    if is_secret(svc, name, value):
        return "****"
    if name.endswith("_DOCUMENT"):
        return f"<document {len(value)} bytes sha256:{hashlib.sha256(value.encode()).hexdigest()[:16]}>"
    return value


def desired(name, svc, values, from_env):
    scoped = values.get(name) if isinstance(values.get(name), dict) else {}
    want, problems = dict(svc["fixed"]), []
    for key in svc["needed"]:
        value = scoped.get(key, values.get(key) if isinstance(values.get(key), str) else None)
        if value is None and from_env:
            value = os.environ.get(key)
        if value is None:
            problems.append(f"{key} has no value (manifest, --values{' or environment' if from_env else '; --from-env not given'})")
            continue
        want[key] = value
    for key, value in want.items():
        if "\n" in value and not key.endswith("_DOCUMENT"):
            problems.append(f"{key} contains a newline")
    return want, problems


def edges(conn):
    return [e["node"] for e in (conn or {}).get("edges", [])]


def main(argv, transport=None):
    pos, opts, flags = [], {"--values": None, "--replay": None, "--manifest": str(MANIFEST)}, set()
    while argv:
        if argv[0] in ("--plan", "--apply", "--registry-credentials", "--from-env"):
            flags.add(argv[0])
            argv = argv[1:]
        elif argv[0] in opts and len(argv) > 1:
            opts[argv[0]], argv = argv[1], argv[2:]
        elif not argv[0].startswith("--"):
            pos.append(argv[0])
            argv = argv[1:]
        else:
            print(USAGE, file=sys.stderr)
            return 2
    if len(pos) != 2 or len(flags & {"--plan", "--apply"}) != 1:
        print(USAGE, file=sys.stderr)
        return 2
    project, env_name = pos
    apply = "--apply" in flags
    if not os.environ.get("RAILWAY_API_TOKEN"):
        print("RAILWAY_API_TOKEN is not set", file=sys.stderr)
        return 2
    if apply and "--registry-credentials" in flags and not (os.environ.get("GHCR_USERNAME") and os.environ.get("GHCR_TOKEN")):
        print("--registry-credentials needs GHCR_USERNAME and GHCR_TOKEN", file=sys.stderr)
        return 2
    try:
        manifest = load_manifest(opts["--manifest"])
        values = json.loads(Path(opts["--values"]).read_text()) if opts["--values"] else {}
        if not isinstance(values, dict):
            raise ValueError("--values must be a JSON object")
    except (OSError, ValueError, AttributeError) as e:
        print(f"bad input: {e}", file=sys.stderr)
        return 2
    plan, problems = {}, {}
    for name, svc in manifest.items():
        plan[name], problems[name] = desired(name, svc, values, "--from-env" in flags)
    if apply and any(problems.values()):
        for name, bad in problems.items():
            for p in bad:
                print(f"refused {name}: {p}", file=sys.stderr)
        return 2

    if transport is None:
        transport = replay_transport(opts["--replay"]) if opts["--replay"] else http_transport
    gql = transport if apply else read_only(transport)
    print(f"railway-apply-variables {'apply' if apply else 'plan'} project={project} env={env_name}")
    try:
        data = gql("query Project($id: String!) { project(id: $id) { "
                   "services { edges { node { id name } } } "
                   "environments { edges { node { id name } } } } }", {"id": project})["project"]
        env = next((e["id"] for e in edges(data["environments"]) if e["name"] == env_name), None)
        if env is None:
            print(f"environment {env_name} not in project {project}", file=sys.stderr)
            return 1
        ids = {s["name"]: s["id"] for s in edges(data["services"])}
        absent = [name for name in plan if not ids.get(name)]
        for name in absent:
            print(f"service {name} not in project")
        if apply and absent:
            print(f"refused: {len(absent)} manifest services are not in project {project}; nothing written",
                  file=sys.stderr)
            return 1
        writes = []
        for name, want in plan.items():
            if name in absent:
                continue
            svc, sid = manifest[name], ids[name]
            pv = {"p": project, "e": env, "s": sid}
            have = gql("query Variables($p: String!, $e: String!, $s: String!) { "
                       "variables(projectId: $p, environmentId: $e, serviceId: $s, unrendered: true) }",
                       pv)["variables"] or {}
            upsert = {}
            lines = []
            for key in sorted(want):
                if key not in have:
                    lines.append(f"  missing {key} = {show(svc, key, want[key])}")
                    upsert[key] = want[key]
                elif have[key] != want[key]:
                    if is_secret(svc, key, have[key]) or is_secret(svc, key, want[key]):
                        lines.append(f"  differs {key} **** -> ****")
                    else:
                        lines.append(f"  differs {key} {show(svc, key, have[key])} -> {show(svc, key, want[key])}")
                    upsert[key] = want[key]
            extra = set(have) - set(want) - set(svc["needed"])
            lines += [f"  extra {key} (kept)" for key in sorted(extra)]
            print(f"service {name} {'update' if upsert else 'unchanged'} missing={sum(k not in have for k in upsert)} "
                  f"differs={sum(k in have for k in upsert)} extra={len(extra)}")
            lines += [f"  unresolved {p}" for p in problems[name]]
            for line in lines:
                print(line)
            if "--registry-credentials" in flags:
                print("  registry credentials user=$GHCR_USERNAME token=$GHCR_TOKEN")
            writes.append((name, sid, upsert))
        if not apply:
            return 1 if absent else 3 if any(problems.values()) else 0
        for name, sid, upsert in writes:
            print(f"service {name} apply")
            if upsert:
                gql("mutation VariablesUpsert($i: VariableCollectionUpsertInput!) { variableCollectionUpsert(input: $i) }",
                    {"i": {"projectId": project, "environmentId": env, "serviceId": sid,
                           "variables": upsert, "skipDeploys": True}})
                print(f"  applied {len(upsert)} variables")
            if "--registry-credentials" in flags:
                gql("mutation InstanceUpdate($s: String!, $e: String!, $i: ServiceInstanceUpdateInput!) { "
                    "serviceInstanceUpdate(serviceId: $s, environmentId: $e, input: $i) }",
                    {"s": sid, "e": env, "i": {"registryCredentials": {
                        "username": os.environ["GHCR_USERNAME"], "password": os.environ["GHCR_TOKEN"]}}})
                print("  applied registry credentials")
    except (RuntimeError, OSError, KeyError, TypeError) as e:
        print(f"railway call failed: {e}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
