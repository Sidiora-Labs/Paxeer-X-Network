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
usage: tools/bringup/search-front.sh names | render | deploy

The search front of search.paxeer.network: the nginx Fly app of
interop/deploy/search-front/fly.toml that keeps each client address on one
serving RPC name. Runs on the edge host, the operator host that holds the Fly
login, and reads the host map from BRINGUP_HOSTS_FILE without printing it.

names     asks every RPC_HOSTS destination over ssh which apiN site its nginx
          serves (rpc_unit of tools/bringup/check-live.sh) and prints one line
          per destination, ordered by N: "serve <apiN name>" for a serving
          RPC name, "validator <apiN name>" for a destination that
          VALIDATOR_HOSTS also names. Exits 1 naming RPC_HOSTS[k] when a
          destination does not answer or serves no apiN site.

render    prints the upstream list that nginx includes at http level: a
          comment line and the split_clients block on $xweb_client (the edge's
          X-Real-IP, else Fly-Client-IP) that sets $xweb_node, one "<percent>% <name>;" line per serving RPC name with
          "*" on the last, every name an equal share. All configured serving
          names remain assigned even when readiness fails, preserving the
          receiver between a payment challenge and retry. A separate map
          admits only names whose bounded typed /readyz contract is valid,
          including starting or unavailable roles; nginx checks current
          readiness on the same backend before every paid request. With no
          serving name, readiness and search paths answer 503.

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
  SEARCH_FRONT_READINESS_TIMEOUT seconds per HTTPS readiness probe, default 5

Exits 1 when a destination, a docker or a flyctl step fails; 2 on a usage
error or a host map lacking a role.
EOF
}

search_front_toml="interop/deploy/search-front/fly.toml"
search_front_host="search.paxeer.network"
search_front_secret="SEARCH_FRONT_UPSTREAMS"
search_front_regions="${SEARCH_FRONT_REGIONS:-ams,fra}"

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
	local listing share i
	local -a names
	listing="$(search_front_names)" || return 1
	mapfile -t names < <(sed -n 's/^serve //p' <<<"$listing")
	echo "# The serving RPC names of $search_front_host, rendered by tools/bringup/search-front.sh."
	if [ "${#names[@]}" -eq 0 ]; then
		# shellcheck disable=SC2016
		echo 'map $xweb_client $xweb_node { default ""; }'
		# shellcheck disable=SC2016
		echo 'map $xweb_node $xweb_eligible { default 0; }'
		return 0
	fi
	# shellcheck disable=SC2016
	echo 'split_clients "${xweb_client}" $xweb_node {'
	if [ "${#names[@]}" -gt 0 ]; then
		share=$((10000 / ${#names[@]}))
		for i in "${!names[@]}"; do
			if [ "$i" -eq $((${#names[@]} - 1)) ]; then
				echo "    * ${names[$i]};"
			else
				printf '    %d.%02d%% %s;\n' $((share / 100)) $((share % 100)) "${names[$i]}"
			fi
		done
	fi
	echo '}'
	# shellcheck disable=SC2016
	echo 'map $xweb_node $xweb_eligible {'
	echo '    default 0;'
	for i in "${!names[@]}"; do
		if search_front_candidate "${names[$i]}"; then
			printf '    %s 1;\n' "${names[$i]}"
		else
			echo "search-front: serving slot $i has no valid readiness contract" >&2
		fi
	done
	echo '}'
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
names | render | deploy) ;;
*)
	usage >&2
	exit 2
	;;
esac
if [ "$#" -ne 1 ]; then
	usage >&2
	exit 2
fi

tools=(ssh timeout sort)
[ "$mode" = names ] || tools+=(python3)
[ "$mode" != deploy ] || tools+=(flyctl docker git base64)
for tool in "${tools[@]}"; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "search-front: $tool is required" >&2
		exit 2
	fi
done

load_hosts
case "$mode" in
names) search_front_names ;;
render) search_front_render ;;
deploy) search_front ;;
esac
