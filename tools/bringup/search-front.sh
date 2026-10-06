#!/usr/bin/env bash
set -euo pipefail

# The host map, the Fly helpers and rpc_unit are the probe's.
# shellcheck source=tools/bringup/check-live.sh
check_live_path="$(dirname "${BASH_SOURCE[0]}")/check-live.sh"
if [ "$(grep -Fxc 'mode="${1:-}"' "$check_live_path")" != 1 ]; then
	echo "search-front: check-live helper boundary is unavailable" >&2
	exit 2
fi
# shellcheck disable=SC1090
. <(sed '/^mode="\${1:-}"$/,$d' "$check_live_path")
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

usage() {
	cat <<'EOF'
usage: tools/bringup/search-front.sh names | render | config | deploy

The search front of search.paxeer.network: the nginx Fly app of
interop/deploy/search-front/fly.toml that keeps each client address on one
serving RPC name. Runs on the edge host, the operator host that holds the Fly
login; names reads the host map from BRINGUP_HOSTS_FILE without printing it.

names     asks every RPC_HOSTS destination over ssh which apiN site its nginx
          serves (rpc_unit of tools/bringup/check-live.sh) and prints one line
          per destination, ordered by N: "serve <apiN name>" for a serving
          RPC name, "validator <apiN name>" for a destination that
          VALIDATOR_HOSTS also names. Exits 1 naming RPC_HOSTS[k] when a
          destination does not answer or serves no apiN site.

render    prints the upstream list that nginx includes at http level: a
          comment line, the split_clients block on $xweb_client (the edge's
          X-Real-IP, else Fly-Client-IP) that pins $xweb_node to one serving
          name for every client, and the map that admits it. The pin is
          SEARCH_FRONT_PRIMARY when its bounded typed /readyz contract is
          valid, else SEARCH_FRONT_BACKUP when its contract is valid, else
          SEARCH_FRONT_PRIMARY unadmitted, so readiness and search paths
          answer 503. One pinned name keeps the receiver between a payment
          challenge and its retry; nginx checks current readiness on that
          backend before every paid request.

config    prints the serving sidecar configuration of
          interop/crates/x-websearch/config.example.json with every "${NAME}"
          string replaced by the environment variable NAME and the note
          dropped. Exits 1 naming every unset or empty variable, never a
          value.

deploy    creates the app in FLY_ORG when it does not exist, imports the
          rendered list base64-encoded as the [[files]] secret
          SEARCH_FRONT_UPSTREAMS through flyctl secrets import on standard
          input, builds docker/search-front/Dockerfile from the repository
          root, deploys it and scales it to one machine in each of
          SEARCH_FRONT_REGIONS. search.paxeer.network reaches the app through
          the edge host's nginx, which the *.paxeer.network wildcard lands on:
          when tools/bringup/edge.sh exists it registers the name with the
          app's fly.dev name as upstream, otherwise one line says the name
          awaits edge registration and names that upstream.

Environment:
  BRINGUP_HOSTS_FILE   the private host map, as tools/bringup/check-live.sh
  CHECK_LIVE_TIMEOUT   seconds per ssh call, default 30
  FLY_ORG              the Fly organisation, default paxlabs-inc
  SEARCH_FRONT_REGIONS comma-separated regions, default ams,fra
  SEARCH_FRONT_PRIMARY the pinned serving name, default api15.paxeer.network
  SEARCH_FRONT_BACKUP  the serving name pinned while the primary is not
                       ready, default api1.paxeer.network
  SEARCH_FRONT_READINESS_TIMEOUT seconds per HTTPS readiness probe, default 5

Exits 1 when a destination, a docker or a flyctl step fails; 2 on a usage
error or a host map lacking a role.
EOF
}

search_front_toml="interop/deploy/search-front/fly.toml"
search_front_host="search.paxeer.network"
search_front_secret="SEARCH_FRONT_UPSTREAMS"
search_front_regions="${SEARCH_FRONT_REGIONS:-ams,fra}"
search_front_primary="${SEARCH_FRONT_PRIMARY:-api15.paxeer.network}"
search_front_backup="${SEARCH_FRONT_BACKUP:-api1.paxeer.network}"
search_front_config_template="interop/crates/x-websearch/config.example.json"

search_front_names() {
	local -a dests validators lines=()
	local k v reply name role status
	read -r -a dests <<<"$RPC_HOSTS"
	read -r -a validators <<<"$VALIDATOR_HOSTS"
	for k in "${!dests[@]}"; do
		status=0
		reply="$(rpc_unit "${dests[$k]}")" || status=$?
		if [ "$status" -ne 0 ]; then
			echo "search-front: RPC_HOSTS[$k] ssh=$status" >&2
			return 1
		fi
		read -r name _ <<<"$reply"
		case "$name" in
		api[1-9] | api1[0-6]) ;;
		*)
			echo "search-front: RPC_HOSTS[$k] site=none" >&2
			return 1
			;;
		esac
		role=serve
		for v in "${validators[@]}"; do
			[ "$v" != "${dests[$k]}" ] || role=validator
		done
		lines+=("$role $name.$rpc_domain")
	done
	printf '%s\n' "${lines[@]}" | sort -k2,2V
}

search_front_candidate() {
	local name="$1" seconds="${SEARCH_FRONT_READINESS_TIMEOUT:-5}"
	if [[ ! "$seconds" =~ ^[1-9][0-9]*$ ]] || [ "$seconds" -gt 30 ]; then
		echo "search-front: readiness timeout must be 1..30 seconds" >&2
		return 1
	fi
	timeout "$seconds" python3 -I - "$name" "$seconds" <<'PY'
import json
import sys
import time
import urllib.error
import urllib.request

def unique_object(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise ValueError("duplicate readiness field")
        value[key] = item
    return value

def integer(value):
    return type(value) is int and 0 <= value <= 18446744073709551615

try:
    name, seconds = sys.argv[1:]
    origin = "https://" + name + "/readyz"
    try:
        response = urllib.request.urlopen(origin, timeout=int(seconds))
    except urllib.error.HTTPError as error:
        if error.code != 503:
            raise
        response = error
    with response:
        status = response.status
        if status not in (200, 503):
            raise ValueError("readiness status")
        if response.geturl() != origin:
            raise ValueError("readiness redirect")
        raw = response.read(65537)
    if len(raw) > 65536:
        raise ValueError("readiness response bound")
    value = json.loads(raw, object_pairs_hook=unique_object)
    if not isinstance(value, dict) or set(value) != {"version", "ready", "roles", "checked_at_unix_ms", "network_id", "protocol_version"}:
        raise ValueError("readiness shape")
    if type(value["version"]) is not int or value["version"] != 1 or type(value["ready"]) is not bool or (status == 200) != value["ready"]:
        raise ValueError("readiness version or state")
    if not integer(value["network_id"]) or value["network_id"] == 0 or type(value["protocol_version"]) is not int or value["protocol_version"] != 3:
        raise ValueError("readiness protocol identity")
    checked = value["checked_at_unix_ms"]
    now = time.time_ns() // 1000000
    if not integer(checked) or checked > now + 5000:
        raise ValueError("readiness clock")
    roles = value["roles"]
    if not isinstance(roles, list) or not 1 <= len(roles) <= 3:
        raise ValueError("readiness roles")
    expected = {
        "paid_delivery": {"index", "content_storage", "payment_journal", "settlement_authority"},
        "evm_attestor": {"evm_chain", "registered_peer_quorum", "attestor_progress", "attestation_journal"},
        "kernel_relay": {"evm_chain", "registered_peer_quorum", "kernel_authority", "relay_progress", "kernel_journal"},
    }
    seen_roles = set()
    states = {"starting", "ready", "unavailable", "stale"}
    for role in roles:
        if not isinstance(role, dict) or set(role) != {"role", "state", "dependencies", "freshness_budget_ms", "first_use_deadline_unix_ms"}:
            raise ValueError("readiness role shape")
        identity = role["role"]
        if identity not in expected or identity in seen_roles or role["state"] not in states:
            raise ValueError("readiness role identity or state")
        seen_roles.add(identity)
        budget = role["freshness_budget_ms"]
        if not integer(budget) or not 1 <= budget <= 300000 or now - checked > budget or not integer(role["first_use_deadline_unix_ms"]):
            raise ValueError("readiness freshness")
        dependencies = role["dependencies"]
        if not isinstance(dependencies, list) or len(dependencies) != len(expected[identity]):
            raise ValueError("readiness dependencies")
        seen_dependencies = set()
        for dependency in dependencies:
            if not isinstance(dependency, dict) or set(dependency) != {"dependency", "state", "critical", "last_success_unix_ms"}:
                raise ValueError("readiness dependency shape")
            key = dependency["dependency"]
            if key not in expected[identity] or key in seen_dependencies or dependency["state"] not in states or dependency["critical"] is not True:
                raise ValueError("readiness critical dependency")
            seen_dependencies.add(key)
            last_success = dependency["last_success_unix_ms"]
            if last_success is not None and (not integer(last_success) or last_success > checked):
                raise ValueError("readiness dependency freshness")
            if dependency["state"] == "ready" and (last_success is None or checked - last_success > budget):
                raise ValueError("ready dependency has no fresh success")
        if (role["state"] == "ready") != all(item["state"] == "ready" for item in dependencies):
            raise ValueError("readiness role aggregation")
    if "paid_delivery" not in seen_roles:
        raise ValueError("missing serving role")
    if value["ready"] != all(role["state"] == "ready" for role in roles):
        raise ValueError("readiness aggregate")
except Exception:
    sys.exit(1)
PY
}

search_front_render() {
	local name pinned admitted=0
	for name in "$search_front_primary" "$search_front_backup"; do
		if [[ ! "$name" =~ ^api([1-9]|1[0-6])\.([a-z0-9-]+\.)?paxeer\.network$ ]]; then
			echo "search-front: $name is not a serving apiN name" >&2
			return 1
		fi
	done
	if [ "$search_front_primary" = "$search_front_backup" ]; then
		echo "search-front: the backup repeats the primary" >&2
		return 1
	fi
	pinned="$search_front_primary"
	if search_front_candidate "$search_front_primary"; then
		admitted=1
	elif search_front_candidate "$search_front_backup"; then
		pinned="$search_front_backup"
		admitted=1
		echo "search-front: $search_front_primary has no valid readiness contract, $pinned is pinned" >&2
	else
		echo "search-front: neither $search_front_primary nor $search_front_backup has a valid readiness contract" >&2
	fi
	echo "# The serving name of $search_front_host, rendered by tools/bringup/search-front.sh."
	# shellcheck disable=SC2016
	echo 'split_clients "${xweb_client}" $xweb_node {'
	echo "    * $pinned;"
	echo '}'
	# shellcheck disable=SC2016
	echo 'map $xweb_node $xweb_eligible {'
	echo '    default 0;'
	echo "    $pinned $admitted;"
	echo '}'
}

search_front_config() {
	python3 -I - "$repo_root/$search_front_config_template" <<'PY'
import json
import os
import re
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    template = json.load(handle)
template.pop("note")
missing = []

def fill(value):
    if isinstance(value, dict):
        return {key: fill(item) for key, item in value.items()}
    if isinstance(value, list):
        return [fill(item) for item in value]
    if isinstance(value, str) and "${" in value:
        match = re.fullmatch(r"\$\{([A-Z][A-Z0-9_]*)\}", value)
        if not match:
            sys.exit("search-front: malformed reference in " + sys.argv[1])
        item = os.environ.get(match[1], "")
        if not item and match[1] not in missing:
            missing.append(match[1])
        return item
    return value

config = fill(template)
if missing:
    sys.exit("search-front: unset " + " ".join(missing))
print(json.dumps(config, indent=2))
PY
}

search_front() {
	local app list image regions
	app="$(fly_app "$search_front_toml")" || {
		echo "search-front: $search_front_toml names no app" >&2
		return 1
	}
	list="$(search_front_render)" || return 1
	if ! flyctl status --app "$app" >/dev/null 2>&1; then
		flyctl apps create "$app" --org "${FLY_ORG:-paxlabs-inc}"
	fi
	printf '%s=%s\n' "$search_front_secret" "$(printf '%s\n' "$list" | base64 -w 0)" |
		flyctl secrets import --app "$app" --stage
	image="$app:$(git -C "$repo_root" rev-parse --short=12 HEAD)"
	docker build -f "$repo_root/docker/search-front/Dockerfile" -t "$image" "$repo_root"
	flyctl deploy --config "$repo_root/$search_front_toml" --app "$app" --image "$image" --local-only --ha=false --yes
	regions="$(tr ',' '\n' <<<"$search_front_regions" | grep -c .)"
	flyctl scale count "$regions" --app "$app" --region "$search_front_regions" --max-per-region 1 --yes
	if [ -x "$repo_root/tools/bringup/edge.sh" ]; then
		"$repo_root/tools/bringup/edge.sh" register "$search_front_host" "$app.fly.dev"
	else
		echo "$search_front_host awaits edge registration: upstream $app.fly.dev"
	fi
}

mode="${1:-}"
case "$mode" in
-h | --help)
	usage
	exit 0
	;;
names | render | config | deploy) ;;
*)
	usage >&2
	exit 2
	;;
esac
if [ "$#" -ne 1 ]; then
	usage >&2
	exit 2
fi

tools=(timeout)
[ "$mode" != names ] || tools+=(ssh sort)
[ "$mode" = names ] || tools+=(python3)
[ "$mode" != deploy ] || tools+=(flyctl docker git base64)
for tool in "${tools[@]}"; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "search-front: $tool is required" >&2
		exit 2
	fi
done

case "$mode" in
names)
	load_hosts
	search_front_names
	;;
render) search_front_render ;;
config) search_front_config ;;
deploy) search_front ;;
esac
