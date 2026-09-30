#!/usr/bin/env bash
set -euo pipefail

# The host map, the Fly helpers and rpc_unit are the probe's.
# shellcheck source=tools/bringup/check-live.sh
. "$(dirname "${BASH_SOURCE[0]}")/check-live.sh"

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
          comment line and the split_clients block on Fly-Client-IP that sets
          $xweb_node, one "<percent>% <name>;" line per serving RPC name with
          "*" on the last, every name an equal share. With no serving name
          the block is empty and the search paths answer 502.

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

search_front_render() {
	local listing share i
	local -a names
	listing="$(search_front_names)" || return 1
	mapfile -t names < <(sed -n 's/^serve //p' <<<"$listing")
	echo "# The serving RPC names of $search_front_host, rendered by tools/bringup/search-front.sh."
	# shellcheck disable=SC2016
	echo 'split_clients "${http_fly_client_ip}" $xweb_node {'
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
