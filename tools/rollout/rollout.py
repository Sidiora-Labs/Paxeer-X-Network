#!/usr/bin/env python3
import datetime
import json
import os
import re
import shlex
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
    "railway-image": {"target", "image", "railway_service", "health", "order"},
    "box": {"target", "image", "box_role", "box_unit", "health", "order"},
}
NAME = r"[a-z0-9][a-z0-9-]*"
STRING = r'"((?:[^"\\]|\\.)*)"'
LINE = re.compile(r"^([a-z_][a-z0-9_]*)\s*=\s*(" + STRING + r"|-?\d+)\s*(?:#.*)?$")
DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")
HEALTH = re.compile(r"^(/[A-Za-z0-9._/-]*|tcp:\d{1,5})$")
IN_PROGRESS = {"BUILDING", "DEPLOYING", "INITIALIZING", "QUEUED", "WAITING"}
TERMINAL = {"SUCCESS": "pass", "SLEEPING": "pass", "FAILED": "fail", "CRASHED": "fail",
            "REMOVED": "fail", "REMOVING": "fail", "SKIPPED": "fail", "NEEDS_APPROVAL": "blocked"}
RAILWAY = ("railway", "railway-image")
SECRET = re.compile(r"SECRET|TOKEN|PASSWORD|KEY|DSN|URL_WITH_PASSWORD|SEED|MNEMONIC|PRIVATE|SIGNING|CREDENTIAL|JWT|WEBHOOK")
URL_WITH_PASSWORD = re.compile(r"://[^/@\s]*:[^/@\s]*@")
URL_PASSWORD = re.compile(r"(://[^/@\s:]*:)[^/@\s]*@")
BEARER = re.compile(r"(?i)\bbearer\s+[^\s\"']+")
MASK = "[redacted]"
ERROR_LIMIT = 512
MASKED = set()
SECRET_ENV = ("RAILWAY_API_TOKEN", "GHCR_TOKEN")


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
            raise Malformed(f"[{section}]: target must be railway, railway-image or box, got {target!r}")
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


def manifest_meta(path):
    meta = parse(path).get("meta", {})
    for key in ("project", "environment"):
        if key in meta and (not isinstance(meta[key], str) or not re.fullmatch(NAME, meta[key])):
            raise Malformed(f"{path}: [meta] {key} must be a lowercase name")
    return meta


def host_key(role):
    return role.upper().replace("-", "_") + "_HOST"


def image_ref(svc, digests):
    return f"{GHCR}/{svc['image']}@{digests[svc['image']]}"


def load_digests(path):
    digests = json.loads(Path(path).read_text())
    if not isinstance(digests, dict):
        raise ValueError("digest map must be a JSON object")
    for image, digest in digests.items():
        if not isinstance(digest, str) or not DIGEST.match(digest):
            raise ValueError(f"digest for {image} must be sha256:<64hex>")
    return digests


def describe(svc, sha, digests=None):
    head = f"{svc['order']:>3} {svc['name']:<20} {svc['target']:<7}"
    if svc["target"] == "railway-image":
        return f"{head} {svc['railway_service']} image={image_ref(svc, digests)} health={svc['health']}"
    if svc["target"] == "railway":
        if svc["image"] in STOCK:
            return f"{head} {svc['railway_service']} skip stock-image={svc['image']}"
        return f"{head} {svc['railway_service']} commit={sha} health={svc['health']}"
    return (f"{head} {svc['box_role']} {GHCR}/{svc['image']}:{sha[:12]} unit={svc['box_unit']} "
            f"host=${host_key(svc['box_role'])} health={svc['health']}")


def mask(*values):
    MASKED.update(v for v in values if isinstance(v, str) and v)


def secret_values(node, key=""):
    if isinstance(node, dict):
        for k, v in node.items():
            yield from secret_values(v, str(k))
    elif isinstance(node, list):
        for v in node:
            yield from secret_values(v, key)
    elif isinstance(node, str) and SECRET.search(key.upper()):
        yield node


def redact(text):
    text = str(text)
    values = MASKED | {os.environ[k] for k in SECRET_ENV if os.environ.get(k)}
    for value in sorted(values, key=len, reverse=True):
        for form in {value, json.dumps(value)[1:-1], json.dumps(json.dumps(value)[1:-1])[1:-1]}:
            text = text.replace(form, MASK)
    return BEARER.sub("Bearer " + MASK, URL_PASSWORD.sub(r"\1" + MASK + "@", text))


def graphql_error(errors, variables):
    mask(*secret_values(variables))
    if isinstance(errors, list) and errors and all(isinstance(e, dict) and isinstance(e.get("message"), str)
                                                   for e in errors):
        text = "; ".join(e["message"] for e in errors)
    else:
        text = "unrecognised error payload " + json.dumps(errors, default=str)
    text = redact(text)
    if len(text) > ERROR_LIMIT:
        text = f"{text[:ERROR_LIMIT]}... ({len(text) - ERROR_LIMIT} more characters)"
    return f"Railway GraphQL error: {text}"


def gql(query, variables):
    token = os.environ.get("RAILWAY_API_TOKEN")
    if not token:
        raise SystemExit("RAILWAY_API_TOKEN is not set")
    req = urllib.request.Request(
        API, data=json.dumps({"query": query, "variables": variables}).encode(),
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {token}"})
    with urllib.request.urlopen(req, timeout=30) as resp:
        try:
            body = json.load(resp)
        except ValueError:
            raise RuntimeError("Railway returned a response that is not JSON") from None
    if not isinstance(body, dict):
        raise RuntimeError(graphql_error(body, variables))
    if body.get("errors") or not isinstance(body.get("data"), dict):
        raise RuntimeError(graphql_error(body.get("errors", body), variables))
    return body["data"]


def railway_ids(env, project_name):
    project = os.environ.get("RAILWAY_PROJECT_ID")
    if not project:
        raise SystemExit("RAILWAY_PROJECT_ID is not set")
    data = gql("query Project($id: String!) { project(id: $id) { name "
               "services { edges { node { id name } } } "
               "environments { edges { node { id name } } } } }", {"id": project})["project"]
    if not data or data.get("name") != project_name:
        raise SystemExit(f"RAILWAY_PROJECT_ID names project {(data or {}).get('name')!r}, "
                         f"the manifest is for {project_name!r}")
    names = {e["node"]["name"]: e["node"]["id"] for e in data["services"]["edges"]}
    envs = {e["node"]["name"]: e["node"]["id"] for e in data["environments"]["edges"]}
    if env not in envs:
        raise SystemExit(f"environment {env} not in the Railway project")
    return names, envs[env]


def service_instance(ids):
    return gql("query Instance($s: String!, $e: String!) { "
               "serviceInstance(serviceId: $s, environmentId: $e) { source { image } healthcheckPath "
               "latestDeployment { id status meta } } }", ids)["serviceInstance"] or {}


def update_instance(ids, change):
    if change:
        gql("mutation InstanceUpdate($s: String!, $e: String!, $i: ServiceInstanceUpdateInput!) { "
            "serviceInstanceUpdate(serviceId: $s, environmentId: $e, input: $i) }", dict(ids, i=change))


def health_change(svc, instance, log):
    if instance.get("healthcheckPath") == svc["health"]:
        return {}
    log.write(f"healthcheckPath {instance.get('healthcheckPath')} -> {svc['health']}\n")
    return {"healthcheckPath": svc["health"]}


def apply_railway(svc, sha, names, env_id, log):
    service_id = names.get(svc["railway_service"])
    if not service_id:
        log.write(f"railway service {svc['railway_service']} not in the project\n")
        return "blocked"
    ids = {"s": service_id, "e": env_id}
    update_instance(ids, health_change(svc, service_instance(ids), log))
    deployment = gql("mutation DeployCommit($s: String!, $e: String!, $c: String!) { "
                     "serviceInstanceDeployV2(serviceId: $s, environmentId: $e, commitSha: $c) }",
                     dict(ids, c=sha))["serviceInstanceDeployV2"]
    log.write(f"deployment {deployment} commit {sha}\n")
    return wait_deployment(deployment, log)


def runs(deployment, ref, health):
    meta = deployment.get("meta") or {}
    deploy = (meta.get("serviceManifest") or {}).get("deploy") or {}
    status = deployment.get("status")
    return (bool(deployment.get("id")) and (status in IN_PROGRESS or status in TERMINAL)
            and TERMINAL.get(status) != "fail"
            and meta.get("image") == ref and deploy.get("healthcheckPath") == health)


def apply_railway_image(svc, ref, names, env_id, log):
    service_id = names.get(svc["railway_service"])
    if not service_id:
        log.write(f"railway service {svc['railway_service']} not in the project\n")
        return "blocked"
    ids = {"s": service_id, "e": env_id}
    instance = service_instance(ids)
    current = (instance.get("source") or {}).get("image")
    change = health_change(svc, instance, log)
    if current != ref:
        log.write(f"image {current} -> {ref}\n")
        change["source"] = {"image": ref}
    latest = instance.get("latestDeployment") or {}
    if not change and runs(latest, ref, svc["health"]):
        log.write(f"unchanged image {ref} health {svc['health']}, "
                  f"latest deployment {latest['id']} {latest['status']}\n")
        return wait_deployment(latest["id"], log)
    if not change:
        log.write(f"latest deployment {latest.get('id')} {latest.get('status')} does not run "
                  f"image {ref} health {svc['health']}, redeploying\n")
    update_instance(ids, change)
    deployment = gql("mutation Deploy($s: String!, $e: String!) { "
                     "serviceInstanceDeployV2(serviceId: $s, environmentId: $e) }",
                     ids)["serviceInstanceDeployV2"]
    log.write(f"deployment {deployment} image {ref}\n")
    return wait_deployment(deployment, log)


def wait_deployment(deployment, log):
    deadline = time.monotonic() + int(os.environ.get("ROLLOUT_TIMEOUT", "1800"))
    last = None
    while time.monotonic() < deadline:
        status = gql("query Deployment($id: String!) { deployment(id: $id) { status } }",
                     {"id": deployment})["deployment"]["status"]
        if status != last:
            log.write(f"{datetime.datetime.now(datetime.UTC):%FT%TZ} {status}\n")
            log.flush()
            last = status
        if status not in IN_PROGRESS:
            if status in TERMINAL:
                return TERMINAL[status]
            log.write(f"deployment {deployment} has unknown status {status!r}\n")
            return "fail"
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
    token_file = os.environ.get("REGISTRY_TOKEN_FILE")
    if not token_file or not Path(token_file).is_absolute():
        log.write("REGISTRY_TOKEN_FILE must reference an absolute box-local file\n")
        return "blocked"
    registry_user = os.environ.get("GHCR_ACTOR", "")
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9-]*", registry_user):
        log.write("GHCR_ACTOR must be a registry username\n")
        return "blocked"
    script = (HERE / "box-apply.sh").read_bytes()
    command = shlex.join(
        [
            "bash",
            "-s",
            "--",
            svc["box_role"],
            f"{svc['image']}:{sha[:12]}",
            svc["box_unit"],
            svc["health"],
            token_file,
            registry_user,
        ]
    )
    proc = subprocess.run(
        ["ssh", "-o", "BatchMode=yes", hosts[key], command],
        input=script,
        stdout=log,
        stderr=subprocess.STDOUT,
        timeout=int(os.environ.get("ROLLOUT_TIMEOUT", "1800")),
    )
    log.write(f"box-apply exit {proc.returncode}\n")
    return "pass" if proc.returncode == 0 else "fail"


def main(argv):
    usage = ("usage: rollout.sh plan|apply --sha <40hex> [--only <svc>,..] [--env <manifest environment>] "
             "[--digests <image-digest-map.json>]")
    if not argv or argv[0] not in ("plan", "apply"):
        print(usage, file=sys.stderr)
        return 2
    mode, args = argv[0], argv[1:]
    opts = {"--sha": None, "--only": None, "--env": None, "--digests": None}
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
    manifest = os.environ.get("ROLLOUT_MANIFEST", ROOT / "deploy/rollout.kvx")
    try:
        plan = services(manifest)
        meta = manifest_meta(manifest)
    except (Malformed, OSError) as e:
        print(f"malformed manifest: {e}", file=sys.stderr)
        return 2
    env = meta.get("environment")
    if opts["--env"] and env and opts["--env"] != env:
        print(f"--env {opts['--env']} does not match the manifest environment {env}", file=sys.stderr)
        return 2
    env = opts["--env"] or env
    if mode == "apply" and not env:
        print("the manifest [meta] names no environment and --env is not given", file=sys.stderr)
        return 2
    if opts["--only"]:
        only = opts["--only"].split(",")
        unknown = sorted(set(only) - {s["name"] for s in plan})
        if unknown:
            print(f"unknown services: {','.join(unknown)}", file=sys.stderr)
            return 2
        plan = [s for s in plan if s["name"] in only]
    digests = None
    images = sorted({s["image"] for s in plan if s["target"] == "railway-image"})
    if images:
        if not opts["--digests"]:
            print("--digests is required for railway-image services", file=sys.stderr)
            print(usage, file=sys.stderr)
            return 2
        try:
            digests = load_digests(opts["--digests"])
        except (ValueError, OSError) as e:
            print(f"bad digest map: {e}", file=sys.stderr)
            return 2
        missing = [i for i in images if i not in digests]
        if missing:
            print(f"digest map has no digest for: {','.join(missing)}", file=sys.stderr)
            return 2
    print(f"rollout {mode} env={env or '-'} sha={sha}")
    for svc in plan:
        print(describe(svc, sha, digests))
    if mode == "plan" or os.environ.get("ROLLOUT_DRY_RUN") == "1":
        return 0

    work = [s for s in plan if not (s["target"] == "railway" and s["image"] in STOCK)]
    names = env_id = hosts = None
    hosted = [s for s in work if s["target"] in RAILWAY]
    if hosted:
        if not meta.get("project"):
            print("the manifest [meta] names no Railway project", file=sys.stderr)
            return 2
        tcp = [s["name"] for s in hosted if not s["health"].startswith("/")]
        if tcp:
            print(f"Railway runs only HTTP health checks, give an HTTP path for: {','.join(tcp)}",
                  file=sys.stderr)
            return 2
        names, env_id = railway_ids(env, meta["project"])
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
                elif svc["target"] == "railway-image":
                    outcome = apply_railway_image(svc, image_ref(svc, digests), names, env_id, log)
                else:
                    outcome = apply_box(svc, sha, hosts, log)
            except (RuntimeError, OSError, subprocess.TimeoutExpired) as e:
                log.write(f"error: {redact(e)}\n")
                outcome = "fail"
        target = "railway" if svc["target"] == "railway-image" else svc["target"]
        subprocess.run([str(HERE / "ledger.sh"), sha, svc["name"], target, started,
                        outcome, str(path)], check=True)
        print(f"{outcome} {svc['name']} evidence={path}")
        if outcome != "pass":
            return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
