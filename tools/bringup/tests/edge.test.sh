#!/usr/bin/env bash
set -euo pipefail

# Renders a fixture EDGE_APP map holding every upstream mode, first with
# render --dry-run and then with set --render-only, checks each mode's
# directives and that no Fly name is left, and runs nginx -t over the rendered
# set when nginx is installed.
here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "${work:?}"' EXIT

failures=0
check() {
	if eval "$2"; then
		echo "ok   $1"
	else
		echo "FAIL $1"
		failures=$((failures + 1))
	fi
}

mkdir -p "$work/live"
openssl req -x509 -newkey rsa:2048 -nodes -days 2 -subj /CN=edge.test \
	-keyout "$work/key.pem" -out "$work/cert.pem" >/dev/null 2>&1
for n in api-mainnet-beta.paxeer.network api-hull.paxeer.network hooks.paxeer.network rpc.paxeer.network; do
	mkdir -p "$work/live/$n"
	cp "$work/cert.pem" "$work/live/$n/fullchain.pem"
	cp "$work/key.pem" "$work/live/$n/privkey.pem"
done
{
	printf '# fixture map\n\n'
	printf 'api-mainnet-beta.paxeer.network\trailway\trouter-beta.up.railway.app:443\t-\n'
	printf 'api-hull.paxeer.network\tbox\t10.20.0.5:8443\thull.box.test\n'
	printf 'hooks.paxeer.network\tbox\t10.20.0.6:8080\t-\n'
	printf 'machine.paxeer.network\tstream\t10.20.0.7:9454\t-\n'
	printf 'index.paxeer.network:443\tstream\t10.20.0.7:9420\t-\n'
	for p in 9443 9445 9446 9449 9457; do
		printf 'kernel.paxeer.network\tstream\t10.20.0.7:%s\t-\n' "$p"
	done
} >"$work/edge-apps"
cat >"$work/edge.env" <<'EOF'
EDGE_APP=edge-apps
EDGE_RPC_POOL="rpc-a.example.org rpc-b.example.org rpc-c.example.org"
EOF
run=(env -u BRINGUP_HOSTS_FILE -u EDGE_RESOLVER -u EDGE_APP EDGE_LOCAL=1 EDGE_CERT_DIR="$work/live"
	EDGE_WEBROOT="$work/www" EDGE_CRON_DIR="$work/cron")

status=0
"${run[@]}" EDGE_NGINX_DIR="$work/dry/nginx" "$here/edge.sh" render --dry-run "$work/edge.env" >"$work/dry.out" 2>"$work/dry.err" || status=$?
check edge_dry_run_exit '[ "$status" -eq 0 ] || cat "$work/dry.err"'
check edge_dry_run_writes_nothing '[ ! -e "$work/dry" ]'
check edge_dry_run_paths 'grep -qxF "# $work/dry/nginx/sites-available/api-mainnet-beta.paxeer.network.conf" "$work/dry.out" && grep -qxF "	include $work/dry/nginx/edge/stream/*.conf;" "$work/dry.out"'

status=0
"${run[@]}" EDGE_NGINX_DIR="$work/nginx" "$here/edge.sh" set "$work/edge.env" --render-only >"$work/set.out" 2>&1 || status=$?
check edge_set_exit '[ "$status" -eq 0 ] || cat "$work/set.out"'
check edge_set_manifest '[ "$(wc -l <"$work/nginx/edge/manifest")" -eq 11 ]'
check edge_dry_run_matches_set 'diff <(grep -v "^# /" "$work/dry.out" | sed "s#$work/dry/nginx#$work/nginx#g") <(for f in $(find "$work/nginx/sites-available" "$work/nginx/modules-enabled" "$work/nginx/edge/stream" -type f -name "*.conf" | sort); do cat "$f"; done)'

site="$work/nginx/sites-enabled"
railway="$site/api-mainnet-beta.paxeer.network.conf"
check edge_railway 'grep -qxF "		set \$edge_upstream router-beta.up.railway.app:443;" "$railway" &&
	grep -qxF "		proxy_pass https://\$edge_upstream;" "$railway" &&
	grep -qxF "		proxy_ssl_server_name on;" "$railway" &&
	grep -qxF "		proxy_ssl_name router-beta.up.railway.app;" "$railway" &&
	grep -qxF "		proxy_set_header Host router-beta.up.railway.app;" "$railway" &&
	grep -qxF "		proxy_set_header X-Forwarded-Host api-mainnet-beta.paxeer.network;" "$railway" &&
	grep -qxF "		proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;" "$railway" &&
	grep -qxF "		proxy_set_header X-Forwarded-Proto \$scheme;" "$railway" &&
	grep -qxF "		proxy_set_header Upgrade \$http_upgrade;" "$railway" &&
	grep -qxF "		proxy_set_header Connection \$http_connection;" "$railway" &&
	grep -qxF "		proxy_ssl_trusted_certificate /etc/ssl/certs/ca-certificates.crt;" "$railway"'
check edge_listen_and_certs 'grep -qxF "	listen 127.0.0.1:10443 ssl http2 proxy_protocol;" "$railway" &&
	grep -qxF "	listen 80;" "$railway" &&
	grep -qxF "	ssl_certificate $work/live/api-mainnet-beta.paxeer.network/fullchain.pem;" "$railway" &&
	grep -qxF "	ssl_certificate_key $work/live/api-mainnet-beta.paxeer.network/privkey.pem;" "$railway"'
box_tls="$site/api-hull.paxeer.network.conf"
check edge_box_https 'grep -qxF "		set \$edge_upstream 10.20.0.5:8443;" "$box_tls" &&
	grep -qxF "		proxy_pass https://\$edge_upstream;" "$box_tls" &&
	grep -qxF "		proxy_ssl_name hull.box.test;" "$box_tls" &&
	grep -qxF "		proxy_ssl_trusted_certificate /etc/layerx/ca.crt;" "$box_tls" &&
	grep -qxF "		proxy_set_header Host api-hull.paxeer.network;" "$box_tls" &&
	grep -qxF "		proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;" "$box_tls"'
box_plain="$site/hooks.paxeer.network.conf"
check edge_box_http 'grep -qxF "		proxy_pass http://\$edge_upstream;" "$box_plain" &&
	grep -qxF "		set \$edge_upstream 10.20.0.6:8080;" "$box_plain" &&
	! grep -q "proxy_ssl" "$box_plain"'
stream="$work/nginx/edge/stream"
check edge_stream_kernel 'for p in 9443 9445 9446 9449 9457; do
		grep -qxF "	kernel.paxeer.network 10.20.0.7:$p;" "$stream/$p.conf" && grep -qxF "	listen $p;" "$stream/$p.conf" && grep -qxF "	ssl_preread on;" "$stream/$p.conf" || exit 1
	done'
check edge_stream_machine 'grep -qxF "	machine.paxeer.network 10.20.0.7:9454;" "$stream/9454.conf"'
check edge_stream_index 'grep -qxF "	index.paxeer.network 10.20.0.7:9420;" "$stream/443.conf" && grep -qxF "	listen 443;" "$stream/443.conf"'
check edge_stream_root 'grep -qxF "stream {" "$work/nginx/modules-enabled/99-edge-stream.conf"'
check edge_no_stream_site '[ ! -e "$site/machine.paxeer.network.conf" ] && [ ! -e "$site/kernel.paxeer.network.conf" ] && [ ! -e "$site/index.paxeer.network.conf" ]'
check edge_rpc_pool '[ "$(grep -c "^	server 127.0.0.1:187[0-9]* max_fails=2 fail_timeout=10s; # rpc-[abc].example.org$" "$site/rpc.paxeer.network.conf")" -eq 3 ]'
check edge_no_fly '! grep -rqi fly "$work/nginx" "$work/dry.out" "$here/edge.sh" "$here/edge-apps.example"'

status=0
"${run[@]}" EDGE_NGINX_DIR="$work/nginx" "$here/edge.sh" render kernel.paxeer.network >"$work/render.out" 2>&1 || status=$?
check edge_render_kernel '[ "$status" -eq 0 ] && [ "$(grep -c "^	kernel.paxeer.network 10.20.0.7:" "$work/render.out")" -eq 5 ]'
echo rpc-b.example.org >"$work/nginx/edge/rpc-down"
"${run[@]}" EDGE_NGINX_DIR="$work/nginx" "$here/edge.sh" render rpc.paxeer.network >"$work/rpc.out" 2>&1 || true
check edge_render_rpc_down 'grep -qxF "	server 127.0.0.1:18702 max_fails=2 fail_timeout=10s down; # rpc-b.example.org" "$work/rpc.out"'
rm -f "$work/nginx/edge/rpc-down"

printf 'bad.paxeer.network\ttunnel\thost:443\t-\n' >"$work/bad-apps"
printf 'EDGE_APP=bad-apps\n' >"$work/bad.env"
status=0
"${run[@]}" EDGE_NGINX_DIR="$work/bad" "$here/edge.sh" render --dry-run "$work/bad.env" >/dev/null 2>&1 || status=$?
check edge_unknown_mode '[ "$status" -eq 1 ]'
status=0
EDGE_LOCAL=1 "$here/edge.sh" add only-two-args >/dev/null 2>&1 || status=$?
check edge_usage '[ "$status" -eq 2 ]'

if command -v nginx >/dev/null 2>&1; then
	stream_mod="$(find /usr/lib/nginx/modules /usr/share/nginx/modules -name ngx_stream_module.so 2>/dev/null | head -n 1 || true)"
	sed -i "s#/etc/layerx/ca.crt#$work/cert.pem#" "$work/nginx/sites-available/"*.conf
	{
		echo "pid $work/nginx.pid;"
		echo "error_log $work/error.log;"
		[ -z "$stream_mod" ] || echo "load_module $stream_mod;"
		echo "events {}"
		echo "http {"
		echo "	access_log off;"
		echo "	server_names_hash_bucket_size 128;"
		echo "	include $work/nginx/sites-enabled/*.conf;"
		echo "}"
		[ -z "$stream_mod" ] || echo "include $work/nginx/modules-enabled/99-edge-stream.conf;"
	} >"$work/nginx.conf"
	[ -n "$stream_mod" ] || echo "note edge_nginx_t: ngx_stream_module.so is absent on this host; the stream blocks are checked by text"
	check edge_nginx_t 'nginx -t -c "$work/nginx.conf" >"$work/nginx-t.out" 2>&1 || { cat "$work/nginx-t.out"; false; }'
else
	echo "note edge_nginx_t: nginx is not installed; skipped"
fi

status=0
"$here/edge-sync.sh" >/dev/null 2>&1 || status=$?
check edge_sync_usage '[ "$status" -eq 2 ]'

if [ "$failures" -ne 0 ]; then
	echo "edge.test: $failures case(s) failed"
	exit 1
fi
echo "edge.test: all cases passed"
