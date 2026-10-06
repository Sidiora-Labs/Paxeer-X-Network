#!/usr/bin/env bash
set -euo pipefail

# Renders the site of one HTTP name and the stream block of one stream name
# from a fixture manifest and compares each with the expected text beside
# this file, then renders the whole served set from a fixture env file and
# runs nginx -t on it.
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "${work:?}"' EXIT
mkdir -p "$work/nginx/edge"
printf '%s\n' \
	"api-mainnet-beta.paxeer.network http paxeer-shared-endpoint 443" \
	"machine.paxeer.network stream paxeer-human-service 9454" >"$work/nginx/edge/manifest"

failures=0
case_render() {
	local name="$1" want="$2" got status=0
	got="$(env -u BRINGUP_HOSTS_FILE -u EDGE_WEBROOT -u EDGE_CERT_DIR -u EDGE_RESOLVER \
		EDGE_LOCAL=1 EDGE_NGINX_DIR="$work/nginx" "$here/edge.sh" render "$name" 2>&1)" || status=$?
	if [ "$status" -eq 0 ] && [ "$got" = "$(cat "$here/$want")" ]; then
		echo "ok   edge_render_$name"
	else
		echo "FAIL edge_render_$name: exit $status, diff against $want:"
		diff <(printf '%s\n' "$got") "$here/$want" || true
		failures=$((failures + 1))
	fi
}
case_render api-mainnet-beta.paxeer.network edge.test.http.conf
case_render machine.paxeer.network edge.test.stream.conf

status=0
EDGE_LOCAL=1 "$here/edge.sh" add only-two-args >/dev/null 2>&1 || status=$?
if [ "$status" -eq 2 ]; then
	echo "ok   edge_usage"
else
	echo "FAIL edge_usage: want exit 2, got $status"
	failures=$((failures + 1))
fi

# The served set: every name of the table and the balanced RPC name render
# from a fixture env file with fixture certificates, and nginx -t accepts the
# result.
set_dir="$work/set"
mkdir -p "$set_dir/live"
openssl req -x509 -newkey rsa:2048 -nodes -days 2 -subj /CN=edge.test \
	-keyout "$set_dir/key.pem" -out "$set_dir/cert.pem" >/dev/null 2>&1
names="api-mainnet-beta.paxeer.network api-hull.paxeer.network dev.paxeer.network api-dev.paxeer.network
hooks.paxeer.network interchain.paxeer.network chain.paxeer.network archive.paxeer.network
search.paxeer.network node.hyperpaxeer.com wallet-api.paxeer.network human.paxeer.network rpc.paxeer.network"
for n in $names; do
	mkdir -p "$set_dir/live/$n"
	cp "$set_dir/cert.pem" "$set_dir/live/$n/fullchain.pem"
	cp "$set_dir/key.pem" "$set_dir/live/$n/privkey.pem"
done
cat >"$set_dir/edge.env" <<'EOF'
EDGE_APP_API_MAINNET_BETA=fixture-router
EDGE_APP_API_HULL=fixture-human
EDGE_APP_MACHINE=fixture-human
EDGE_APP_INDEX=fixture-indexer
EDGE_APP_DEV=fixture-dev
EDGE_APP_API_DEV=fixture-dev-api
EDGE_APP_HOOKS=fixture-webhooks
EDGE_APP_INTERCHAIN=fixture-interop
EDGE_APP_CHAIN=fixture-gas-station.fly.dev
EDGE_APP_ARCHIVE=fixture-relay-archive
EDGE_APP_SEARCH=fixture-search-front
EDGE_APP_NODE_HYPERPAXEER=fixture-hpx-registry
EDGE_APP_WALLET_API=fixture-wallet-gateway
EDGE_APP_HUMAN=fixture-human-web
EDGE_RPC_POOL="rpc-a.example.org rpc-b.example.org rpc-c.example.org"
EOF
set_env=(env -u BRINGUP_HOSTS_FILE -u EDGE_RESOLVER EDGE_LOCAL=1 EDGE_NGINX_DIR="$set_dir/nginx"
	EDGE_CERT_DIR="$set_dir/live" EDGE_WEBROOT="$set_dir/www" EDGE_CRON_DIR="$set_dir/cron")
check() {
	if eval "$2"; then
		echo "ok   $1"
	else
		echo "FAIL $1"
		failures=$((failures + 1))
	fi
}
status=0
"${set_env[@]}" "$here/edge.sh" set "$set_dir/edge.env" --render-only >"$set_dir/set.out" 2>&1 || status=$?
check edge_set_exit '[ "$status" -eq 0 ]'
check edge_set_manifest '[ "$(wc -l <"$set_dir/nginx/edge/manifest")" -eq 15 ]'
for n in $names; do
	check "edge_set_site_$n" 'grep -q "server_name $n;" "$set_dir/nginx/sites-enabled/$n.conf" && grep -q "ssl_certificate $set_dir/live/$n/fullchain.pem;" "$set_dir/nginx/sites-enabled/$n.conf"'
done
check edge_set_chain_app 'grep -q "proxy_ssl_name fixture-gas-station.fly.dev;" "$set_dir/nginx/sites-enabled/chain.paxeer.network.conf"'
check edge_set_stream_machine 'grep -q "machine.paxeer.network fixture-human.fly.dev:9454;" "$set_dir/nginx/edge/stream/9454.conf"'
check edge_set_stream_index 'grep -q "index.paxeer.network fixture-indexer.fly.dev:443;" "$set_dir/nginx/edge/stream/443.conf"'
check edge_set_http_behind_stream 'grep -q "listen 127.0.0.1:10443 ssl http2 proxy_protocol;" "$set_dir/nginx/sites-enabled/rpc.paxeer.network.conf"'
check edge_set_no_stream_site '[ ! -e "$set_dir/nginx/sites-enabled/machine.paxeer.network.conf" ] && [ ! -e "$set_dir/nginx/sites-enabled/index.paxeer.network.conf" ]'
check edge_set_rpc_pool '[ "$(grep -c "^	server 127.0.0.1:187[0-9]* max_fails=2 fail_timeout=10s; # rpc-[abc].example.org$" "$set_dir/nginx/sites-enabled/rpc.paxeer.network.conf")" -eq 3 ]'
check edge_set_rpc_member_sni 'grep -q "proxy_ssl_name rpc-b.example.org;" "$set_dir/nginx/sites-enabled/rpc.paxeer.network.conf"'
echo rpc-b.example.org >"$set_dir/nginx/edge/rpc-down"
"${set_env[@]}" "$here/edge.sh" render rpc.paxeer.network >"$set_dir/rpc-render.out" 2>&1 || true
check edge_render_rpc_down 'grep -qx "	server 127.0.0.1:18702 max_fails=2 fail_timeout=10s down; # rpc-b.example.org" "$set_dir/rpc-render.out"'
rm -f "$set_dir/nginx/edge/rpc-down"

# nginx -t over the rendered set, with the stream block when this nginx can
# load its stream module.
stream_mod="$(find /usr/lib/nginx/modules /usr/share/nginx/modules -name ngx_stream_module.so 2>/dev/null | head -n 1 || true)"
{
	echo "pid $set_dir/nginx.pid;"
	echo "error_log $set_dir/error.log;"
	[ -z "$stream_mod" ] || echo "load_module $stream_mod;"
	echo "events {}"
	echo "http {"
	echo "	access_log off;"
	echo "	server_names_hash_bucket_size 128;"
	echo "	include $set_dir/nginx/sites-enabled/*.conf;"
	echo "}"
	[ -z "$stream_mod" ] || echo "include $set_dir/nginx/modules-enabled/99-edge-stream.conf;"
} >"$set_dir/nginx.conf"
if [ -z "$stream_mod" ]; then
	echo "note edge_nginx_t: ngx_stream_module.so is absent on this host; nginx -t covers the http sites, the stream blocks are checked by text"
fi
check edge_nginx_t 'nginx -t -c "$set_dir/nginx.conf" >"$set_dir/nginx-t.out" 2>&1 || { cat "$set_dir/nginx-t.out"; false; }'

status=0
"$here/edge-sync.sh" >/dev/null 2>&1 || status=$?
check edge_sync_usage '[ "$status" -eq 2 ]'

if [ "$failures" -ne 0 ]; then
	echo "edge.test: $failures case(s) failed"
	exit 1
fi
echo "edge.test: all cases passed"
