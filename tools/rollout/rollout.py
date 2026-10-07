#!/usr/bin/env python3
import datetime
import json
import os
import re
import subprocess
import sys
import time
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
HERE = Path(__file__).resolve().parent
GHCR = "ghcr.io/sidiora-labs"
API = "https://backboard.railway.com/graphql/v2"
STOCK = {"redis", "postgres"}
KEYS = {
    "railway": {"target", "image", "railway_service", "health", "order"},
    "box": {"target", "image", "box_role", "box_unit", "health", "order"},
}
NAME = r"[a-z0-9][a-z0-9-]*"
STRING = r'"((?:[^"\\]|\\.)*)"'
LINE = re.compile(r"^([a-z_][a-z0-9_]*)\s*=\s*(" + STRING + r"|-?\d+)\s*(?:#.*)?$")
HEALTH = re.compile(r"^(/[A-Za-z0-9._/-]*|tcp:\d{1,5})$")
TERMINAL = {"SUCCESS": "pass", "FAILED": "fail", "CRASHED": "fail", "REMOVED": "fail",
            "SKIPPED": "fail", "NEEDS_APPROVAL": "blocked"}


class Malformed(Exception):
    pass


def parse(path):
    sections = {}
    current = None
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
        if not m:
            raise Malformed(f"{path}:{no}: not a key = value line: {line}")
        if current is None:
            raise Malformed(f"{path}:{no}: key outside a section")
        key, value = m.group(1), (m.group(3) if m.group(3) is not None else m.group(2))
        if m.group(3) is None:
            value = int(value)
        if key in sections[current]:
            raise Malformed(f"{path}:{no}: duplicate key {key} in [{current}]")
        sections[current][key] = value
    return sections


def services(path):
    sections = parse(path)
    out = []
    for section, kv in sections.items():
        if section == "meta":
            continue
        m = re.fullmatch(r"service\.(" + NAME + ")", section)
        if not m:
            raise Malformed(f"{path}: unexpected section [{section}]")
        name = m.group(1)
        target = kv.get("target")
        if target not in KEYS:
            raise Malformed(f"[{section}]: target must be railway or box, got {target!r}")
        if set(kv) != KEYS[target]:
            missing = sorted(KEYS[target] - set(kv))
            extra = sorted(set(kv) - KEYS[target])
            raise Malformed(f"[{section}]: missing {missing} unexpected {extra}")
        for key in KEYS[target] - {"order"}:
            if not isinstance(kv[key], str) or not kv[key]:
                raise Malformed(f"[{section}]: {key} must be a non-empty string")
        if not isinstance(kv["order"], int) or kv["order"] < 1:
            raise Malformed(f"[{section}]: order must be a positive integer")
        if not re.fullmatch(NAME, kv["image"]):
            raise Malformed(f"[{section}]: bad image {kv['image']!r}")
        if not HEALTH.match(kv["health"]):
            raise Malformed(f"[{section}]: health must be /<path> or tcp:<port>")
        if target == "box" and not re.fullmatch(NAME + r"\.service", kv["box_unit"]):
            raise Malformed(f"[{section}]: box_unit must be <name>.service")
        out.append(dict(kv, name=name))
    if not out:
        raise Malformed(f"{path}: no [service.*] sections")
    return sorted(out, key=lambda s: s["order"])


def host_key(role):
    return role.upper().replace("-", "_") + "_HOST"


def describe(svc, sha):
    head = f"{svc['order']:>3} {svc['name']:<20} {svc['target']:<7}"
    if svc["target"] == "railway":
        if svc["image"] in STOCK:
            return f"{head} {svc['railway_service']} skip stock-image={svc['image']}"
        return f"{head} {svc['railway_service']} commit={sha} health={svc['health']}"
    return (f"{head} {svc['box_role']} {GHCR}/{svc['image']}:{sha[:12]} unit={svc['box_unit']} "
            f"host=${host_key(svc['box_role'])} health={svc['health']}")


def gql(query, variables):
    token = os.environ.get("RAILWAY_API_TOKEN")
    if not token:
        raise SystemExit("RAILWAY_API_TOKEN is not set")
    req = urllib.request.Request(
        API, data=json.dumps({"query": query, "variables": variables}).encode(),
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {token}"})
    with urllib.request.urlopen(req, timeout=30) as resp:
        body = json.load(resp)
    if body.get("errors"):
        raise RuntimeError(json.dumps(body["errors"]))
    return body["data"]


def railway_ids(env):
    project = os.environ.get("RAILWAY_PROJECT_ID")
    if not project:
        raise SystemExit("RAILWAY_PROJECT_ID is not set")
    data = gql("query($id: String!) { project(id: $id) { "
               "services { edges { node { id name } } } "
               "environments { edges { node { id name } } } } }", {"id": project})["project"]
    names = {e["node"]["name"]: e["node"]["id"] for e in data["services"]["edges"]}
    envs = {e["node"]["name"]: e["node"]["id"] for e in data["environments"]["edges"]}
    if env not in envs:
        raise SystemExit(f"environment {env} not in the Railway project")
    return names, envs[env]


def apply_railway(svc, sha, names, env_id, log):
    service_id = names.get(svc["railway_service"])
    if not service_id:
        log.write(f"railway service {svc['railway_service']} not in the project\n")
        return "blocked"
    deployment = gql("mutation($s: String!, $e: String!, $c: String!) { "
                     "serviceInstanceDeployV2(serviceId: $s, environmentId: $e, commitSha: $c) }",
                     {"s": service_id, "e": env_id, "c": sha})["serviceInstanceDeployV2"]
    log.write(f"deployment {deployment} commit {sha}\n")
    deadline = time.monotonic() + int(os.environ.get("ROLLOUT_TIMEOUT", "1800"))
    last = None
    while time.monotonic() < deadline:
        status = gql("query($id: String!) { deployment(id: $id) { status } }",
                     {"id": deployment})["deployment"]["status"]
        if status != last:
            log.write(f"{datetime.datetime.now(datetime.UTC):%FT%TZ} {status}\n")
            log.flush()
            last = status
        if status in TERMINAL:
            return TERMINAL[status]
        time.sleep(int(os.environ.get("ROLLOUT_POLL", "10")))
    log.write(f"timeout waiting for deployment {deployment}, last status {last}\n")
    return "fail"


def load_hosts():
    path = Path(os.environ.get("ROLLOUT_HOSTS_FILE", ROOT / "deploy/hosts.env"))
    if not path.is_file():
        raise SystemExit(f"host map {path} is missing")
    hosts = {}
    for line in path.read_text().splitlines():
        line = line.strip()
        if line and not line.startswith("#") and "=" in line:
            k, v = line.split("=", 1)
            hosts[k.strip()] = v.strip().strip("'\"")
    return hosts


def apply_box(svc, sha, hosts, log):
    key = host_key(svc["box_role"])
    if not hosts.get(key):
        log.write(f"{key} is not set in the host map\n")
        return "blocked"
    script = (HERE / "box-apply.sh").read_bytes()
    proc = subprocess.run(
        ["ssh", "-o", "BatchMode=yes", hosts[key], "bash", "-s", "--",
         svc["box_role"], f"{svc['image']}:{sha[:12]}", svc["box_unit"], svc["health"]],
        input=script, stdout=log, stderr=subprocess.STDOUT,
        timeout=int(os.environ.get("ROLLOUT_TIMEOUT", "1800")))
    log.write(f"box-apply exit {proc.returncode}\n")
    return "pass" if proc.returncode == 0 else "fail"


def main(argv):
    usage = "usage: rollout.sh plan|apply --sha <40hex> [--only <svc>,..] [--env beta]"
    if not argv or argv[0] not in ("plan", "apply"):
        print(usage, file=sys.stderr)
        return 2
    mode, args = argv[0], argv[1:]
    opts = {"--sha": None, "--only": None, "--env": "beta"}
    while args:
        if args[0] not in opts or len(args) < 2:
            print(usage, file=sys.stderr)
            return 2
        opts[args[0]] = args[1]
        args = args[2:]
    sha = opts["--sha"]
    if not sha or not re.fullmatch(r"[0-9a-f]{40}", sha):
        print("--sha must be a 40-hex commit", file=sys.stderr)
        return 2
    try:
        plan = services(os.environ.get("ROLLOUT_MANIFEST", ROOT / "deploy/rollout.kvx"))
    except (Malformed, OSError) as e:
        print(f"malformed manifest: {e}", file=sys.stderr)
        return 2
    if opts["--only"]:
        only = opts["--only"].split(",")
        unknown = sorted(set(only) - {s["name"] for s in plan})
        if unknown:
            print(f"unknown services: {','.join(unknown)}", file=sys.stderr)
            return 2
        plan = [s for s in plan if s["name"] in only]
    print(f"rollout {mode} env={opts['--env']} sha={sha}")
    for svc in plan:
        print(describe(svc, sha))
    if mode == "plan" or os.environ.get("ROLLOUT_DRY_RUN") == "1":
        return 0

    work = [s for s in plan if not (s["target"] == "railway" and s["image"] in STOCK)]
    names = env_id = hosts = None
    if any(s["target"] == "railway" for s in work):
        names, env_id = railway_ids(opts["--env"])
    if any(s["target"] == "box" for s in work):
        hosts = load_hosts()
    day = datetime.datetime.now(datetime.UTC).strftime("%F")
    logs = Path(os.environ.get("ROLLOUT_LOG_DIR", "/root/lx-ops/rollout")) / day
    logs.mkdir(parents=True, exist_ok=True)
    for svc in work:
        started = datetime.datetime.now(datetime.UTC).strftime("%FT%TZ")
        path = logs / f"{sha[:12]}-{svc['name']}.log"
        with open(path, "a") as log:
            log.write(f"{started} apply {svc['name']} {svc['target']} {sha}\n")
            log.flush()
            try:
                if svc["target"] == "railway":
                    outcome = apply_railway(svc, sha, names, env_id, log)
                else:
                    outcome = apply_box(svc, sha, hosts, log)
            except (RuntimeError, OSError, subprocess.TimeoutExpired) as e:
                log.write(f"error: {e}\n")
                outcome = "fail"
        subprocess.run([str(HERE / "ledger.sh"), sha, svc["name"], svc["target"], started,
                        outcome, str(path)], check=True)
        print(f"{outcome} {svc['name']} evidence={path}")
        if outcome != "pass":
            return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
