#!/usr/bin/env bash
set -euo pipefail

# Renders the search front's upstream list and the serving sidecar
# configuration from a fixture env, and runs nginx -t on
# interop/deploy/search-front/nginx.conf with the rendered list included.
# The readiness probes go through a closed local proxy port, so neither
# pinned name is ready and the render takes its unadmitted-primary path.
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$here/../../.." && pwd)"
front="$repo_root/tools/bringup/search-front.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work:?}"' EXIT

failures=0
check() {
	if (eval "$2"); then
		echo "ok   $1"
	else
		echo "FAIL $1"
		failures=$((failures + 1))
	fi
}

fixture_env=(
	-u BRINGUP_HOSTS_FILE -u SEARCH_FRONT_PRIMARY -u SEARCH_FRONT_BACKUP
	https_proxy=http://127.0.0.1:9 HTTPS_PROXY=http://127.0.0.1:9 no_proxy= NO_PROXY=
	SEARCH_FRONT_READINESS_TIMEOUT=2
	X_WEBSEARCH_SEED_URL=https://paxeer.network/
	X_WEBSEARCH_SID_ASSET_ID=0101010101010101010101010101010101010101010101010101010101010101
	X_WEBSEARCH_SID_PRICE=311400000000000
	X_WEBSEARCH_PAX_ASSET_ID=0202020202020202020202020202020202020202020202020202020202020202
	X_WEBSEARCH_PAX_PRICE=74405000000000
	X_WEBSEARCH_USDC_ASSET_ID=0303030303030303030303030303030303030303030303030303030303030303
	X_WEBSEARCH_USDC_PRICE=1000
	X_WEBSEARCH_USDL_ASSET_ID=0404040404040404040404040404040404040404040404040404040404040404
	X_WEBSEARCH_USDL_PRICE=1000000000000000
	X_WEBSEARCH_GATEWAY_ENDPOINT=https://api-mainnet-beta.paxeer.network/rpc
	X_WEBSEARCH_SEQUENCER_ID=0505050505050505050505050505050505050505050505050505050505050505
	X_WEBSEARCH_SEQUENCER_PUBLIC_KEY=d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a
	X_WEBSEARCH_EVM_ENDPOINT=https://api15.paxeer.network/
)

status=0
env "${fixture_env[@]}" "$front" render >"$work/upstreams.conf" 2>"$work/render.err" || status=$?
check render_exit '[ "$status" -eq 0 ]'
check render_pins_primary_unadmitted 'diff - <(grep -v "^#" "$work/upstreams.conf") <<"WANT"
split_clients "${xweb_client}" $xweb_node {
    * api15.paxeer.network;
}
map $xweb_node $xweb_eligible {
    default 0;
    api15.paxeer.network 0;
}
WANT'
check render_names_both_unready 'grep -Fq "neither api15.paxeer.network nor api1.paxeer.network" "$work/render.err"'

status=0
env "${fixture_env[@]}" SEARCH_FRONT_BACKUP=api15.paxeer.network "$front" render >/dev/null 2>&1 || status=$?
check render_refuses_repeated_backup '[ "$status" -eq 1 ]'
status=0
env "${fixture_env[@]}" SEARCH_FRONT_PRIMARY=api17.paxeer.network "$front" render >/dev/null 2>&1 || status=$?
check render_refuses_non_api_name '[ "$status" -eq 1 ]'

status=0
env "${fixture_env[@]}" "$front" config >"$work/x-websearch.json" 2>"$work/config.err" || status=$?
check config_exit '[ "$status" -eq 0 ]'
check config_matches_template 'env "${fixture_env[@]}" python3 - "$repo_root/interop/crates/x-websearch/config.example.json" "$work/x-websearch.json" <<"PY"
import json, os, re, sys
template = json.load(open(sys.argv[1]))
rendered = json.load(open(sys.argv[2]))
assert "note" in template and "note" not in rendered
assert "REPLACE_WITH" not in open(sys.argv[1]).read()
template.pop("note")
def walk(want, got):
    if isinstance(want, dict):
        assert isinstance(got, dict) and set(want) == set(got)
        for key in want:
            walk(want[key], got[key])
    elif isinstance(want, list):
        assert isinstance(got, list) and len(want) == len(got)
        for a, b in zip(want, got):
            walk(a, b)
    elif isinstance(want, str) and want.startswith("${"):
        name = re.fullmatch(r"\$\{([A-Z0-9_]+)\}", want)[1]
        assert got == os.environ[name], name
    else:
        assert want == got
walk(template, rendered)
assert rendered["assets"]["PAX"]["price"] == "74405000000000"
PY'
status=0
env "${fixture_env[@]}" X_WEBSEARCH_EVM_ENDPOINT= env -u X_WEBSEARCH_PAX_PRICE "$front" config >/dev/null 2>"$work/missing.err" || status=$?
check config_refuses_unset '[ "$status" -eq 1 ] && grep -Fxq "search-front: unset X_WEBSEARCH_PAX_PRICE X_WEBSEARCH_EVM_ENDPOINT" "$work/missing.err"'

mkdir -p "$work/logs"
sed -e "s|/var/log/nginx/error.log|$work/logs/error.log|" \
	-e "s|/var/log/nginx/access.log|$work/logs/access.log|" \
	-e "s|/var/run/nginx.pid|$work/nginx.pid|" \
	-e "s|/etc/nginx/search/upstreams.conf|$work/upstreams.conf|" \
	-e "s|listen \[::\]:8080 ipv6only=off;|listen 127.0.0.1:18080;|" \
	"$repo_root/interop/deploy/search-front/nginx.conf" >"$work/nginx.conf"
check nginx_conf_passes_402 'grep -Fq "proxy_intercept_errors off;" "$work/nginx.conf" && ! grep -Eiq "proxy_hide_header +(PAYMENT|LAYERX)" "$work/nginx.conf"'
if command -v nginx >/dev/null 2>&1; then
	check nginx_t 'nginx -t -c "$work/nginx.conf" >"$work/nginx-t.out" 2>&1 || { cat "$work/nginx-t.out"; false; }'
else
	echo "note nginx_t: nginx is absent on this host; running nginx -t in nginx:1.27-alpine"
	check nginx_t 'docker run --rm --network none -v "$work:$work" nginx:1.27-alpine nginx -t -c "$work/nginx.conf" >"$work/nginx-t.out" 2>&1 || { cat "$work/nginx-t.out"; false; }'
fi

if [ "$failures" -ne 0 ]; then
	echo "search-front.test: $failures case(s) failed"
	exit 1
fi
echo "search-front.test: all cases passed"
