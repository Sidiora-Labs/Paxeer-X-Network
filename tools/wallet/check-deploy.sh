#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'EOF'
usage: tools/wallet/check-deploy.sh [dir]

Validates the wallet deployment definitions offline, without contacting the
platform. dir defaults to human/wallet/deploy and must hold the attestor-*.toml,
gateway.toml and endpoint.toml definitions and the env documentation file.
Every build dockerfile is resolved against the repository root. TOML is parsed
with the python3 standard library (tomllib, Python 3.11 or newer).

Rules, each printed as "pass <rule> <file>" or "fail <rule> <file>: <reason>":
  attestor-count      exactly five attestor definitions
  distinct-regions    five distinct attestor primary regions
  continents          attestor regions span at least two continent groups
  mount               each attestor has exactly one mount
  min-machines        each attestor keeps min_machines_running = 1
  auto-start          each attestor sets auto_start_machines = false
  auto-stop           each attestor sets auto_stop_machines = false
  no-public-ports     no attestor service declares ports
  health-check        each attestor checks /health; gateway and endpoint check a route
  dockerfile          the build dockerfile exists in the repository
  env-no-secrets      no [env] value looks like a secret
  env-documented      every [env] name appears in the env documentation file
  public-http-service gateway and endpoint expose a public http service
  force-https         gateway and endpoint force https

Exits 1 when any rule fails, 2 on a usage error.
EOF
}

case "${1:-}" in
-h | --help)
	usage
	exit 0
	;;
esac

if [ "$#" -gt 1 ]; then
	usage >&2
	exit 2
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
dir="${1:-$root/human/wallet/deploy}"

if [ ! -d "$dir" ]; then
	echo "check-deploy: $dir is not a directory" >&2
	exit 2
fi

if ! python3 -c 'import tomllib' 2>/dev/null; then
	echo "check-deploy: python3 with tomllib (3.11 or newer) is required" >&2
	exit 2
fi

exec python3 - "$dir" "$root" <<'PY'
import glob
import os
import re
import sys
import tomllib

deploy_dir, repo_root = sys.argv[1], sys.argv[2]
failures = 0

CONTINENTS = {
    "north-america": {"iad", "ord", "sjc", "sea", "yyz", "gru"},
    "europe": {"ams", "fra", "lhr", "cdg", "arn", "mad"},
    "asia-pacific": {"sin", "nrt", "hkg", "syd", "bom", "jnb"},
}

SECRET_SHAPES = [
    ("64 hex characters", re.compile(r"(?<![0-9A-Fa-f])[0-9A-Fa-f]{64}(?![0-9A-Fa-f])")),
    ("PEM block", re.compile(r"-----BEGIN [A-Z0-9 ]+-----")),
    ("JWT", re.compile(r"eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*")),
    ("URL with credentials", re.compile(r"[A-Za-z][A-Za-z0-9+.-]*://[^/\s:@]+:[^/\s@]+@")),
]


def report(ok, rule, name, reason=""):
    global failures
    if ok:
        print(f"pass {rule} {name}")
    else:
        failures += 1
        print(f"fail {rule} {name}: {reason}")


def as_list(value):
    if value is None:
        return []
    if isinstance(value, list):
        return value
    return [value]


def load(path):
    with open(path, "rb") as handle:
        return tomllib.load(handle)


env_path = os.path.join(deploy_dir, "env")
documented = set()
if os.path.isfile(env_path):
    with open(env_path, encoding="utf-8") as handle:
        for line in handle:
            match = re.match(r"^([A-Z][A-Z0-9_]*) - \S", line)
            if match:
                documented.add(match.group(1))
report(os.path.isfile(env_path) and bool(documented), "env-documented", "env",
       "env documentation file is missing or names no variable")

definitions = {}
for path in sorted(glob.glob(os.path.join(deploy_dir, "*.toml"))):
    name = os.path.basename(path)
    try:
        definitions[name] = load(path)
    except (OSError, tomllib.TOMLDecodeError) as error:
        report(False, "parse", name, str(error))

attestors = {n: d for n, d in definitions.items() if re.fullmatch(r"attestor-\d+\.toml", n)}
report(len(attestors) == 5, "attestor-count", "attestor-*.toml",
       f"found {len(attestors)} attestor definitions, want 5")

regions = {n: d.get("primary_region", "") for n, d in attestors.items()}
region_values = list(regions.values())
duplicates = sorted({r for r in region_values if region_values.count(r) > 1})
report(len(set(region_values)) == 5 and not duplicates and all(region_values),
       "distinct-regions", "attestor-*.toml",
       f"regions {sorted(region_values)} are not five distinct values"
       + (f", duplicated {duplicates}" if duplicates else ""))

groups = set()
unknown = []
for region in region_values:
    group = next((g for g, members in CONTINENTS.items() if region in members), None)
    if group is None:
        unknown.append(region)
    else:
        groups.add(group)
report(len(groups) >= 2 and not unknown, "continents", "attestor-*.toml",
       f"regions span {sorted(groups)}" + (f", unknown regions {unknown}" if unknown else "")
       + ", want at least two continent groups")


def services(definition):
    return as_list(definition.get("services"))


def http_check_paths(definition):
    paths = []
    for service in services(definition):
        for check in as_list(service.get("http_checks")):
            paths.append(check.get("path"))
    for check in (definition.get("checks") or {}).values():
        if isinstance(check, dict) and check.get("type", "http") == "http":
            paths.append(check.get("path"))
    http_service = definition.get("http_service") or {}
    for check in as_list(http_service.get("checks")):
        paths.append(check.get("path"))
    return [p for p in paths if p]


def scaling_values(definition, key):
    holders = services(definition)
    if definition.get("http_service"):
        holders = holders + [definition["http_service"]]
    return [holder.get(key) for holder in holders]


for name, definition in sorted(attestors.items()):
    mounts = as_list(definition.get("mounts"))
    report(len(mounts) == 1 and bool(mounts[0].get("source")) and bool(mounts[0].get("destination")),
           "mount", name, f"found {len(mounts)} mounts, want exactly one with source and destination")

    holders = services(definition)
    minimums = scaling_values(definition, "min_machines_running")
    report(bool(holders) and all(v == 1 for v in minimums), "min-machines", name,
           f"min_machines_running values {minimums}, want 1 on every service")

    starts = scaling_values(definition, "auto_start_machines")
    report(bool(holders) and all(v is False for v in starts), "auto-start", name,
           f"auto_start_machines values {starts}, want false on every service")

    stops = scaling_values(definition, "auto_stop_machines")
    report(bool(holders) and all(v is False or v == "off" for v in stops), "auto-stop", name,
           f"auto_stop_machines values {stops}, want false on every service")

    public = [s.get("internal_port") for s in holders if as_list(s.get("ports"))]
    report(not public and not definition.get("http_service"), "no-public-ports", name,
           f"public ports declared on internal ports {public}" if public
           else "an http_service exposes the attestor publicly")

    paths = http_check_paths(definition)
    report("/health" in paths, "health-check", name, f"health check paths {paths}, want /health")

for name in ("gateway.toml", "endpoint.toml"):
    definition = definitions.get(name)
    if definition is None:
        report(False, "public-http-service", name, "definition is missing")
        continue
    http_service = definition.get("http_service") or {}
    report(bool(http_service.get("internal_port")), "public-http-service", name,
           "no http_service with an internal_port")
    report(http_service.get("force_https") is True, "force-https", name,
           "http_service.force_https is not true")
    paths = http_check_paths(definition)
    report(any(p.startswith("/") for p in paths), "health-check", name,
           "no http health check with a path")

for name, definition in sorted(definitions.items()):
    dockerfile = (definition.get("build") or {}).get("dockerfile")
    exists = bool(dockerfile) and not os.path.isabs(dockerfile) \
        and os.path.isfile(os.path.join(repo_root, dockerfile))
    report(exists, "dockerfile", name, f"build dockerfile {dockerfile!r} is not a file in the repository")

    env = definition.get("env") or {}
    shaped = []
    for key, value in env.items():
        for label, pattern in SECRET_SHAPES:
            if pattern.search(str(value)):
                shaped.append(f"{key} ({label})")
    report(not shaped, "env-no-secrets", name, f"secret-shaped values in {shaped}")

    missing = sorted(k for k in env if k not in documented)
    report(not missing, "env-documented", name, f"names missing from env: {missing}")

print(f"check-deploy: {failures} rule(s) failed" if failures else "check-deploy: all rules passed")
sys.exit(1 if failures else 0)
PY
