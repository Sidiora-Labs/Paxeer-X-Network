#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'EOF'
usage: tools/bringup/edge.sh add <name>[:<port>] <railway|box|stream> <host:port> <sni|-> | register <name> <host>[:<port>]
       tools/bringup/edge.sh remove <name> | render <name> | render --dry-run <env-file> | list
       tools/bringup/edge.sh set <env-file> [--render-only] | rpc-health | rerender

Serves a public name on the edge host, where the *.paxeer.network wildcard
lands, by proxying it to the upstream its map line names. Acts on this host
when the host map's EDGE_HOST resolves to one of its addresses (or
EDGE_LOCAL=1), over ssh to EDGE_HOST otherwise. Only names this script
registered are ever rendered, changed or removed; the manifest it owns,
EDGE_NGINX_DIR/edge/manifest, holds one
"<name> <http|stream|rpc> <upstream> <port> <railway|box|stream|rpc> <sni|->"
line per name and port.

Upstream modes, one per map line (tools/bringup/edge-apps.example):
  railway  the TLS site proxies to https://<host:port>, the service's Railway
           public domain, with Host and the TLS name (proxy_ssl_server_name on,
           proxy_ssl_name) set to that domain, or to <sni> when given, verified
           against the system roots and re-resolved every 30 seconds.
  box      the TLS site proxies to a box at <host:port>: plain http when <sni>
           is -, https otherwise with the TLS name <sni> verified against the
           internal CA at /etc/layerx/ca.crt. Host stays the public name.
  stream   an nginx stream block passes TLS through unchanged by SNI to the box
           at <host:port>, so client certificates reach it. <name>:<port> sets
           the public port, which defaults to the upstream's; on a port the
           HTTP names share (443) those names move behind the stream block to a
           loopback listener that keeps the client address. <sni> is -.
Both HTTP modes pass X-Forwarded-Host, X-Forwarded-For, X-Real-IP,
X-Forwarded-Proto, Upgrade and Connection.

add       renders the name: an HTTP name gets
          EDGE_NGINX_DIR/sites-available/<name>.conf with the plain listener
          and the ACME location first, the certbot certificate for the name
          over HTTP-01 when absent (renewed by certbot's timer, which reloads
          nginx), then its TLS server; a stream name gets its port's stream
          block. Every step runs nginx -t and reloads; a failing nginx -t
          restores the previous files. Prints
          "added <name> mode=<mode> upstream=<host:port> port=<port>".
register  add <name> railway <host>:<port, default 443> -.
remove    deletes every site or stream entry of the name and reloads; prints
          "removed <name>".
render    prints the files add renders for a registered name: the TLS site of
          an HTTP name, the stream block of each port of a stream name.
          --dry-run renders the whole set of <env-file> as set would and
          prints every file, each after a "# <path>" line naming where set
          writes it, without writing anything; it runs on this host.
list      prints the manifest.
set       registers the whole served set from <env-file> on this host: every
          line of the map file EDGE_APP names (relative to the env file), and
          the balanced RPC name EDGE_RPC_NAME (default rpc.paxeer.network) over
          the full nodes of EDGE_RPC_POOL (space-separated public RPC names).
          Names registered otherwise stay. Renders, runs nginx -t, reloads,
          keeps a copy of this script in EDGE_NGINX_DIR/edge and installs
          EDGE_CRON_DIR/edge-rpc-health running rpc-health every minute.
          --render-only writes the files without the checks, the reload and
          the cron entry. No certbot runs: edge-sync.sh obtains the
          certificates on the primary edge; a name without one keeps its plain
          listener.
rpc-health
          asks every pool member for eth_blockNumber, marks a member down when
          it does not answer or lags the highest answer by more than
          EDGE_RPC_MAX_LAG blocks (default 20), and re-renders and reloads when
          the down set changed. Exits 1 and leaves the pool as it is when no
          member answers.
rerender  renders every registered name from the state on this host, runs
          nginx -t and reloads (edge-sync.sh runs it on the secondary edges).

Environment:
  BRINGUP_HOSTS_FILE  the private host map naming EDGE_HOST; never printed
  EDGE_LOCAL          1 acts on this host without reading the host map
  EDGE_NGINX_DIR      nginx configuration root, default /etc/nginx
  EDGE_WEBROOT        ACME webroot, default /var/www/certbot
  EDGE_CERT_DIR       certbot live directory, default /etc/letsencrypt/live
  EDGE_RESOLVER       resolver nginx uses for the upstreams, default 127.0.0.53
  EDGE_CRON_DIR       where set installs the rpc-health entry, default /etc/cron.d
  EDGE_RPC_MAX_LAG    blocks a pool member may lag the highest, default 20

Browser sponsorship uses this same public name for /gas-station/quote and
/v1/wallet/sponsored/{submit,status}. The station process must set
GAS_STATION_BROWSER_ORIGINS to comma-separated exact wallet origins; HTTPS
origins or loopback HTTP are accepted. Origin and preflight headers pass to
the serving upstream, which owns the refusal policy. No wildcard CORS is added.

Exits 1 when a step fails or a name is refused, 2 on a usage error.
EOF
}

dns_re='^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$'
upstream_re='^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?:[0-9]{1,5}$'
box_ca=/etc/layerx/ca.crt

nginx_dir="${EDGE_NGINX_DIR:-/etc/nginx}"
webroot="${EDGE_WEBROOT:-/var/www/certbot}"
cert_dir="${EDGE_CERT_DIR:-/etc/letsencrypt/live}"
resolver="${EDGE_RESOLVER:-127.0.0.53}"
cron_dir="${EDGE_CRON_DIR:-/etc/cron.d}"
state="$nginx_dir/edge"
manifest="$state/manifest"
rpc_pool="$state/rpc-pool"
rpc_down="$state/rpc-down"
stream_root="$nginx_dir/modules-enabled/99-edge-stream.conf"
marker="# rendered by tools/bringup/edge.sh; edit the manifest through it, not this file"
# The HTTP names listen here, behind the stream block, once a stream name
# shares their port.
loopback_port=10443
inner_port=10444
# Pool member i of the balanced RPC name is a loopback server on this port + i
# that carries the member's own SNI and Host.
rpc_member_base=18700

die() {
	echo "edge: $*" >&2
	exit 1
}

# on_edge: true when EDGE_LOCAL=1 or EDGE_HOST (user@ stripped) resolves to an
# address of this host.
on_edge() {
	local addr
	[ "${EDGE_LOCAL:-}" != 1 ] || return 0
	if [ -z "${BRINGUP_HOSTS_FILE:-}" ] || [ ! -r "$BRINGUP_HOSTS_FILE" ]; then
		echo "edge: BRINGUP_HOSTS_FILE is unset or unreadable" >&2
		exit 2
	fi
	# shellcheck disable=SC1090
	. "$BRINGUP_HOSTS_FILE"
	[ -n "${EDGE_HOST:-}" ] || {
		echo "edge: BRINGUP_HOSTS_FILE lacks EDGE_HOST" >&2
		exit 2
	}
	for addr in $(getent ahosts "${EDGE_HOST#*@}" | awk '{print $1}' | sort -u); do
		ip -o addr show | awk '{print $4}' | cut -d/ -f1 | grep -qxF -- "$addr" && return 0
	done
	return 1
}

# entry <name>: prints the manifest line of the name, status 1 when none.
entry() {
	[ -r "$manifest" ] && awk -v n="$1" '$1 == n {print; found=1} END {exit !found}' "$manifest"
}

# stream_ports / http_shared <port>: the ports stream names use, and whether a
# stream name uses the HTTP port.
stream_ports() {
	[ -r "$manifest" ] && awk '$2 == "stream" {print $4}' "$manifest" | sort -un
}
http_shared() {
	stream_ports | grep -qx 443
}

# upstream_tls <tls name> <trusted roots>: the verified upstream TLS lines.
upstream_tls() {
	printf '\t\tproxy_ssl_server_name on;\n\t\tproxy_ssl_name %s;\n\t\tproxy_ssl_verify on;\n\t\tproxy_ssl_verify_depth 4;\n\t\tproxy_ssl_trusted_certificate %s;\n' "$1" "$2"
}

# render_http <name> <railway|box|-> <host:port> <sni|-> <tls|plain>: the site
# of an HTTP name; a plain site carries only the port 80 listener.
render_http() {
	local name="$1" kind="$2" up="$3" sni="$4" tls="$5" listen scheme host_header upstream_tls=""
	case "$kind" in
	railway)
		scheme=https
		host_header="${up%:*}"
		[ "$sni" = - ] || host_header="$sni"
		upstream_tls="$(upstream_tls "$host_header" /etc/ssl/certs/ca-certificates.crt)"$'\n'
		;;
	box)
		scheme=http
		host_header="$name"
		if [ "$sni" != - ]; then
			scheme=https
			upstream_tls="$(upstream_tls "$sni" "$box_ca")"$'\n'
		fi
		;;
	esac
	echo "$marker"
	cat <<EOF
server {
	listen 80;
	listen [::]:80;
	server_name $name;

	location ^~ /.well-known/acme-challenge/ {
		root $webroot;
		default_type text/plain;
		try_files \$uri =404;
	}

	location / {
		return 301 https://\$host\$request_uri;
	}
}
EOF
	[ "$tls" = tls ] || return 0
	if http_shared; then
		listen="	listen 127.0.0.1:$loopback_port ssl http2 proxy_protocol;
	set_real_ip_from 127.0.0.1;
	real_ip_header proxy_protocol;"
	else
		listen="	listen 443 ssl http2;
	listen [::]:443 ssl http2;"
	fi
	cat <<EOF

server {
$listen
	server_name $name;

	ssl_certificate $cert_dir/$name/fullchain.pem;
	ssl_certificate_key $cert_dir/$name/privkey.pem;
	ssl_protocols TLSv1.2 TLSv1.3;

	client_max_body_size 0;
	resolver $resolver valid=30s ipv6=off;

	location / {
		set \$edge_upstream $up;
		proxy_pass $scheme://\$edge_upstream;
		proxy_http_version 1.1;
		proxy_set_header Host $host_header;
		proxy_set_header X-Forwarded-Host $name;
		proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
		proxy_set_header X-Real-IP \$remote_addr;
		proxy_set_header X-Forwarded-Proto \$scheme;
		proxy_set_header Origin \$http_origin;
		proxy_set_header Access-Control-Request-Method \$http_access_control_request_method;
		proxy_set_header Access-Control-Request-Headers \$http_access_control_request_headers;
		proxy_set_header Upgrade \$http_upgrade;
		proxy_set_header Connection \$http_connection;
$upstream_tls		proxy_read_timeout 3600s;
		proxy_send_timeout 3600s;
	}
}
EOF
}

# render_rpc <name> <tls|plain>: the balanced RPC site. Each pool member gets a
# loopback server that proxies to it under its own name; the public server
# balances over those loopback servers, skipping the members rpc-health marked
# down.
render_rpc() {
	local name="$1" tls="$2" host i=0 down listen
	render_http "$name" - - - plain
	[ "$tls" = tls ] || return 0
	echo
	echo "upstream edge_rpc_pool {"
	echo "	least_conn;"
	while read -r host; do
		[ -n "$host" ] || continue
		i=$((i + 1))
		down=""
		! grep -qxF -- "$host" "$rpc_down" 2>/dev/null || down=" down"
		echo "	server 127.0.0.1:$((rpc_member_base + i)) max_fails=2 fail_timeout=10s$down; # $host"
	done <"$rpc_pool"
	echo "	keepalive 32;"
	echo "}"
	cat <<EOF

map \$http_upgrade \$edge_rpc_connection {
	default upgrade;
	'' '';
}
EOF
	i=0
	while read -r host; do
		[ -n "$host" ] || continue
		i=$((i + 1))
		cat <<EOF

server {
	listen 127.0.0.1:$((rpc_member_base + i));
	resolver $resolver valid=30s ipv6=off;

	location / {
		set \$edge_rpc_member $host;
		proxy_pass https://\$edge_rpc_member;
		proxy_http_version 1.1;
		proxy_set_header Host $host;
		proxy_set_header X-Forwarded-For \$http_x_forwarded_for;
		proxy_set_header X-Real-IP \$http_x_real_ip;
		proxy_set_header Upgrade \$http_upgrade;
		proxy_set_header Connection \$http_connection;
		proxy_ssl_server_name on;
		proxy_ssl_name $host;
		proxy_ssl_verify on;
		proxy_ssl_verify_depth 4;
		proxy_ssl_trusted_certificate /etc/ssl/certs/ca-certificates.crt;
		proxy_read_timeout 3600s;
		proxy_send_timeout 3600s;
	}
}
EOF
	done <"$rpc_pool"
	if http_shared; then
		listen="	listen 127.0.0.1:$loopback_port ssl http2 proxy_protocol;
	set_real_ip_from 127.0.0.1;
	real_ip_header proxy_protocol;"
	else
		listen="	listen 443 ssl http2;
	listen [::]:443 ssl http2;"
	fi
	cat <<EOF

server {
$listen
	server_name $name;

	ssl_certificate $cert_dir/$name/fullchain.pem;
	ssl_certificate_key $cert_dir/$name/privkey.pem;
	ssl_protocols TLSv1.2 TLSv1.3;

	client_max_body_size 16m;

	location / {
		proxy_pass http://edge_rpc_pool;
		proxy_http_version 1.1;
		proxy_next_upstream error timeout http_502 http_503 http_504;
		proxy_next_upstream_tries 3;
		proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
		proxy_set_header X-Real-IP \$remote_addr;
		proxy_set_header Upgrade \$http_upgrade;
		proxy_set_header Connection \$edge_rpc_connection;
		proxy_read_timeout 3600s;
		proxy_send_timeout 3600s;
	}
}
EOF
}

# render_stream <port>: the stream servers of one port from the manifest. On
# the HTTP port the outer server carries the client address in a PROXY header
# to the loopback HTTP listener, and an inner server strips it again before the
# bytes reach the app, so the app sees the client's TLS untouched.
render_stream() {
	local port="$1" names
	names="$(awk -v p="$port" '$2 == "stream" && $4 == p {printf "\t%s %s;\n", $1, $3}' "$manifest")"
	echo "$marker"
	if [ "$port" = 443 ]; then
		cat <<EOF
map \$ssl_preread_server_name \$edge_stream_outer_$port {
$(awk -v p="$port" -v i="$inner_port" '$2 == "stream" && $4 == p {printf "\t%s 127.0.0.1:%s;\n", $1, i}' "$manifest")
	default 127.0.0.1:$loopback_port;
}

map \$ssl_preread_server_name \$edge_stream_$port {
$names
}

server {
	listen $port;
	listen [::]:$port;
	ssl_preread on;
	proxy_protocol on;
	proxy_pass \$edge_stream_outer_$port;
}

server {
	listen 127.0.0.1:$inner_port proxy_protocol;
	ssl_preread on;
	resolver $resolver valid=30s ipv6=off;
	proxy_pass \$edge_stream_$port;
}
EOF
	else
		cat <<EOF
map \$ssl_preread_server_name \$edge_stream_$port {
$names
}

server {
	listen $port;
	listen [::]:$port;
	ssl_preread on;
	resolver $resolver valid=30s ipv6=off;
	proxy_pass \$edge_stream_$port;
}
EOF
	fi
}

# unrendered_listeners <port>: enabled sites this script did not render that
# listen on the port beyond loopback.
unrendered_listeners() {
	local f
	for f in "$nginx_dir"/sites-enabled/*; do
		[ -e "$f" ] || continue
		head -n 1 "$f" | grep -qxF "$marker" && continue
		grep -qE "^[[:space:]]*listen[[:space:]]+(\[::\]:|0\.0\.0\.0:)?$1([[:space:]]|;)" "$f" && basename "$f"
	done
	return 0
}

# claimed_elsewhere <name>: an enabled site this script did not render serves
# the name.
claimed_elsewhere() {
	local f
	for f in "$nginx_dir"/sites-enabled/*; do
		[ -e "$f" ] || continue
		head -n 1 "$f" | grep -qxF "$marker" && continue
		grep -qE "^[[:space:]]*server_name[^;]*[[:space:]]${1//./\\.}[[:space:];]" "$f" && return 0
	done
	return 1
}

# The files every change may touch, saved before it and restored when nginx -t
# refuses the result.
snapshot() {
	backup="$(mktemp -d)"
	cp -a "$state" "$backup/edge"
	mkdir -p "$backup/sa" "$backup/se"
	find "$nginx_dir/sites-available" "$nginx_dir/sites-enabled" -maxdepth 1 \( -type f -o -type l \) -print0 |
		while IFS= read -r -d '' f; do
			head -n 1 "$f" 2>/dev/null | grep -qxF "$marker" || continue
			case "$f" in
			*/sites-available/*) cp -a "$f" "$backup/sa/" ;;
			*) cp -a "$f" "$backup/se/" ;;
			esac
		done
	[ ! -e "$stream_root" ] || cp -a "$stream_root" "$backup/stream-root"
}
restore() {
	local f
	for f in "$nginx_dir"/sites-available/* "$nginx_dir"/sites-enabled/*; do
		[ -e "$f" ] || [ -L "$f" ] || continue
		head -n 1 "$f" 2>/dev/null | grep -qxF "$marker" && rm -f -- "$f"
	done
	rm -rf -- "${state:?}"
	cp -a "$backup/edge" "$state"
	cp -a "$backup/sa/." "$nginx_dir/sites-available/"
	cp -a "$backup/se/." "$nginx_dir/sites-enabled/"
	rm -f -- "$stream_root"
	[ ! -e "$backup/stream-root" ] || cp -a "$backup/stream-root" "$stream_root"
}

# apply: nginx -t over the written files, then a reload; on a refusal the
# snapshot comes back and nginx keeps its previous configuration.
apply() {
	local out
	if ! out="$(nginx -t 2>&1)"; then
		restore
		die "nginx -t refused the change, previous files restored: $(tail -n 2 <<<"$out" | tr '\n' ' ')"
	fi
	nginx -s reload
}

# map_line <name>[:<port>] <railway|box|stream> <host:port> <sni|->: checks one
# map line and prints its manifest line.
map_line() {
	local name="${1%%:*}" kind="$2" up="$3" sni="$4" port=443 mode=http
	[ "$name" = "$1" ] || port="${1#*:}"
	[[ "$name" =~ $dns_re ]] || die "not a DNS name: $1"
	case "$name" in
	api[0-9]*.mainnet-beta.paxeer.network | paxscan.io) die "refuse $name: served on its own host" ;;
	esac
	[[ "$up" =~ $upstream_re ]] || die "not an upstream host:port for $name: $up"
	[ "$sni" = - ] || [[ "$sni" =~ $dns_re ]] || die "not a TLS name for $name: $sni"
	case "$kind" in
	railway | box) [ "$name" = "$1" ] || die "a public port on $1 applies to stream names only" ;;
	stream)
		mode=stream
		[ "$name" != "$1" ] || port="${up##*:}"
		[ "$sni" = - ] || die "a stream name passes TLS through and takes no TLS name: $name $sni"
		;;
	*) die "unknown mode for $name: $kind (railway, box or stream)" ;;
	esac
	[[ "$port" =~ ^[0-9]{1,5}$ ]] && [ "$port" -ge 1 ] && [ "$port" -le 65535 ] || die "not a port for $name: $port"
	[[ "${up##*:}" =~ ^[0-9]{1,5}$ ]] && [ "${up##*:}" -ge 1 ] && [ "${up##*:}" -le 65535 ] || die "not a port for $name: $up"
	echo "$name $mode $up $port $kind $sni"
}

# read_map <file>: the manifest lines of every map line; blank lines and lines
# starting with # are skipped.
read_map() {
	local name kind up sni extra lines="" dup
	while IFS=$'\t' read -r name kind up sni extra || [ -n "$name" ]; do
		case "$name" in '' | '#'*) continue ;; esac
		[ -n "$sni" ] && [ -z "$extra" ] || die "$1: want four tab-separated fields: $name"
		lines+="$(map_line "$name" "$kind" "$up" "$sni")"$'\n' || exit 1
	done <"$1"
	dup="$(awk 'NF {print $1 ":" $4}' <<<"$lines" | sort | uniq -d | paste -sd, -)"
	[ -z "$dup" ] || die "$1: names mapped twice on one port: $dup"
	printf '%s' "$lines"
}

# write_all: renders every registered name's files from the manifest; an HTTP
# name gets only its plain listener unless its certificate is present.
write_all() {
	local name mode up port kind sni tls port_file
	mkdir -p "$state/stream" "$nginx_dir/sites-available" "$nginx_dir/sites-enabled" "$nginx_dir/modules-enabled"
	rm -f -- "$state"/stream/*.conf
	while read -r name mode up port kind sni; do
		[ -n "$name" ] || continue
		case "$mode:$kind" in
		http:railway | http:box | rpc:rpc | stream:stream) ;;
		*) die "manifest line of $name has no upstream mode this script renders; register it again: $name $mode $up $port" ;;
		esac
		if [ "$mode" = http ] || [ "$mode" = rpc ]; then
			tls=tls
			[ -r "$cert_dir/$name/fullchain.pem" ] || tls=plain
			if [ "$mode" = rpc ]; then
				render_rpc "$name" "$tls" >"$nginx_dir/sites-available/$name.conf"
			else
				render_http "$name" "$kind" "$up" "$sni" "$tls" >"$nginx_dir/sites-available/$name.conf"
			fi
			ln -sfn "$nginx_dir/sites-available/$name.conf" "$nginx_dir/sites-enabled/$name.conf"
		fi
	done <"$manifest"
	for port in $(stream_ports); do
		port_file="$state/stream/$port.conf"
		render_stream "$port" >"$port_file"
	done
	if [ -n "$(stream_ports)" ]; then
		printf '%s\nstream {\n\tinclude %s/stream/*.conf;\n}\n' "$marker" "$state" >"$stream_root"
	else
		rm -f -- "$stream_root"
	fi
}

stream_module_loaded() {
	grep -lqs 'ngx_stream_module' "$nginx_dir"/modules-enabled/*.conf
}

edge_add() {
	local line name mode up port users
	line="$(map_line "$@")" || exit 1
	read -r name mode up port _ <<<"$line"
	if claimed_elsewhere "$name"; then
		die "refuse $name: a site this script did not render serves it"
	fi
	if [ "$mode" = stream ]; then
		stream_module_loaded || die "refuse $name: the nginx stream module is not loaded (package libnginx-mod-stream)"
		users="$(unrendered_listeners "$port" | paste -sd, -)"
		[ -z "$users" ] || die "refuse $name port=$port: sites this script did not render listen on it: $users"
	fi
	mkdir -p "$state" "$webroot"
	touch "$manifest"
	snapshot
	awk -v n="$name" -v p="$port" '!($1 == n && $4 == p)' "$manifest" >"$manifest.new"
	echo "$line" >>"$manifest.new"
	sort -o "$manifest.new" "$manifest.new"
	mv "$manifest.new" "$manifest"
	if [ "$mode" = stream ]; then
		rm -f -- "$nginx_dir/sites-enabled/$name.conf" "$nginx_dir/sites-available/$name.conf"
		write_all
		apply
	else
		write_all
		apply
		if [ ! -r "$cert_dir/$name/fullchain.pem" ]; then
			certbot certonly --webroot -w "$webroot" -d "$name" --cert-name "$name" \
				--non-interactive --agree-tos --keep-until-expiring \
				--deploy-hook 'systemctl reload nginx' >/dev/null 2>&1 ||
				die "certbot could not obtain a certificate for $name; its plain listener stays"
			write_all
			apply
		fi
	fi
	rm -rf -- "${backup:?}"
	echo "added $name mode=$mode upstream=$up port=$port"
}

edge_remove() {
	local name="$1"
	entry "$name" >/dev/null || die "not registered: $name"
	snapshot
	awk -v n="$name" '$1 != n' "$manifest" >"$manifest.new"
	mv "$manifest.new" "$manifest"
	rm -f -- "$nginx_dir/sites-enabled/$name.conf" "$nginx_dir/sites-available/$name.conf"
	write_all
	apply
	rm -rf -- "${backup:?}"
	echo "removed $name"
}

cmd_render() {
	local lines name mode up port kind sni
	lines="$(entry "$1")" || die "not registered: $1"
	while read -r name mode up port kind sni; do
		case "$mode" in
		stream) render_stream "$port" ;;
		rpc) render_rpc "$name" tls ;;
		*) render_http "$name" "$kind" "$up" "$sni" tls ;;
		esac
	done <<<"$lines"
}

# cmd_set <env-file> [--render-only]: registers the served set of the env file.
cmd_set() {
	local env="$1" only="${2:-}" key name mode port map lines="" rpc_name users
	[ -z "$only" ] || [ "$only" = --render-only ] || {
		usage >&2
		exit 2
	}
	[ -r "$env" ] || die "env file unreadable: $env"
	# shellcheck disable=SC1090
	. "$env"
	[ -n "${EDGE_APP:-}" ] || die "$env does not set EDGE_APP, the map file"
	case "$EDGE_APP" in
	/*) map="$EDGE_APP" ;;
	*) map="$(dirname -- "$env")/$EDGE_APP" ;;
	esac
	[ -r "$map" ] || die "map file unreadable: $map"
	lines="$(read_map "$map")" || exit 1
	[ -z "$lines" ] || lines+=$'\n'
	while read -r name _; do
		[ -n "$name" ] || continue
		claimed_elsewhere "$name" && die "refuse $name: a site this script did not render serves it"
	done <<<"$lines"
	rpc_name="${EDGE_RPC_NAME:-rpc.paxeer.network}"
	if [ -n "${EDGE_RPC_POOL:-}" ]; then
		[[ "$rpc_name" =~ $dns_re ]] || die "not a DNS name: $rpc_name"
		for key in $EDGE_RPC_POOL; do
			[[ "$key" =~ $dns_re ]] || die "not a DNS name in EDGE_RPC_POOL: $key"
		done
		awk -v n="$rpc_name" '$1 == n {f = 1} END {exit !f}' <<<"$lines" && die "refuse $rpc_name: the map file names it too"
		claimed_elsewhere "$rpc_name" && die "refuse $rpc_name: a site this script did not render serves it"
		lines+="$rpc_name rpc - 443 rpc -"$'\n'
	else
		echo "edge: skipped $rpc_name: EDGE_RPC_POOL is not set" >&2
	fi
	if [ -z "$only" ] && grep -q ' stream ' <<<"$lines"; then
		stream_module_loaded || die "refuse the stream names: the nginx stream module is not loaded (package libnginx-mod-stream)"
		for port in $(awk '$2 == "stream" {print $4}' <<<"$lines" | sort -un); do
			users="$(unrendered_listeners "$port" | paste -sd, -)"
			[ -z "$users" ] || die "refuse port=$port: sites this script did not render listen on it: $users"
		done
	fi
	mkdir -p "$state" "$webroot" "$nginx_dir/sites-available" "$nginx_dir/sites-enabled"
	touch "$manifest"
	snapshot
	if [ -n "${EDGE_RPC_POOL:-}" ]; then
		tr ' ' '\n' <<<"$EDGE_RPC_POOL" | awk 'NF && !seen[$0]++' >"$rpc_pool"
		touch "$rpc_down"
		grep -xF -f "$rpc_pool" "$rpc_down" >"$rpc_down.new" || true
		mv "$rpc_down.new" "$rpc_down"
	fi
	awk 'NR == FNR {if (NF) set[$1] = 1; next} !($1 in set)' <(printf '%s' "$lines") "$manifest" >"$manifest.new"
	printf '%s' "$lines" >>"$manifest.new"
	sort -o "$manifest.new" "$manifest.new"
	mv "$manifest.new" "$manifest"
	while read -r name mode _; do
		[ "$mode" = stream ] || continue
		rm -f -- "$nginx_dir/sites-enabled/$name.conf" "$nginx_dir/sites-available/$name.conf"
	done <<<"$lines"
	write_all
	if [ -z "$only" ]; then
		apply
		cp -- "${BASH_SOURCE[0]}" "$state/edge.sh.new"
		chmod 0755 "$state/edge.sh.new"
		mv "$state/edge.sh.new" "$state/edge.sh"
		if [ -s "$rpc_pool" ]; then
			mkdir -p "$cron_dir"
			printf '%s\n* * * * * root EDGE_LOCAL=1 EDGE_NGINX_DIR=%q EDGE_CERT_DIR=%q EDGE_RESOLVER=%q flock -n %q %q rpc-health >/dev/null 2>&1\n' \
				"$marker" "$nginx_dir" "$cert_dir" "$resolver" "$state/rpc-health.lock" "$state/edge.sh" >"$cron_dir/edge-rpc-health"
		fi
	fi
	rm -rf -- "${backup:?}"
	awk 'NF {print "set " $1 " mode=" $5 " upstream=" $3 " sni=" $6 " port=" $4}' <<<"$lines"
}

# cmd_dry_run <env-file>: renders the set of the env file into a scratch root
# and prints each file under the path set writes it to.
cmd_dry_run() {
	local real="$nginx_dir" f
	scratch="$(mktemp -d)"
	trap 'rm -rf -- "${scratch:?}"' EXIT
	nginx_dir="$scratch"
	state="$nginx_dir/edge"
	manifest="$state/manifest"
	rpc_pool="$state/rpc-pool"
	rpc_down="$state/rpc-down"
	stream_root="$nginx_dir/modules-enabled/99-edge-stream.conf"
	cmd_set "$1" --render-only >/dev/null
	while IFS= read -r f; do
		echo "# $real${f#"$scratch"}"
		sed "s#$scratch#$real#g" "$f"
	done < <(find "$scratch/sites-available" "$scratch/modules-enabled" "$state/stream" -type f -name '*.conf' | sort)
}

# cmd_rpc_health: marks lagging or silent pool members down, re-renders on a
# change.
cmd_rpc_health() {
	local host hex height top=-1 max_lag="${EDGE_RPC_MAX_LAG:-20}" up=0 down=0 want
	declare -A heights=()
	[ -s "$rpc_pool" ] || die "no RPC pool registered ($rpc_pool)"
	[[ "$max_lag" =~ ^[0-9]+$ ]] || die "EDGE_RPC_MAX_LAG is not a block count: $max_lag"
	while read -r host; do
		[ -n "$host" ] || continue
		hex="$(curl -fsS -m 5 -H 'content-type: application/json' \
			--data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
			"https://$host" 2>/dev/null | jq -r '.result // empty' 2>/dev/null)" || hex=""
		[[ "$hex" =~ ^0x[0-9a-fA-F]{1,15}$ ]] || continue
		height=$((hex))
		heights[$host]=$height
		[ "$height" -le "$top" ] || top=$height
	done <"$rpc_pool"
	[ "$top" -ge 0 ] || die "no pool member answered eth_blockNumber; the pool stays as it is"
	want=""
	while read -r host; do
		[ -n "$host" ] || continue
		if [ -z "${heights[$host]:-}" ] || [ $((top - heights[$host])) -gt "$max_lag" ]; then
			want+="$host"$'\n'
			down=$((down + 1))
		else
			up=$((up + 1))
		fi
	done <"$rpc_pool"
	touch "$rpc_down"
	if printf '%s' "$want" | cmp -s - "$rpc_down"; then
		echo "rpc pool unchanged: $up up, $down down, top $top"
		return 0
	fi
	snapshot
	printf '%s' "$want" >"$rpc_down"
	write_all
	apply
	rm -rf -- "${backup:?}"
	echo "rpc pool changed: $up up, $down down, top $top"
}

cmd_rerender() {
	[ -s "$manifest" ] || die "no manifest at $manifest"
	snapshot
	write_all
	apply
	rm -rf -- "${backup:?}"
	echo "rerendered $(wc -l <"$manifest") names"
}

sub="${1:-}"
case "$sub:$#" in
add:5 | register:3) ;;
remove:2 | render:2) ;;
render:3)
	[ "$2" = --dry-run ] || {
		usage >&2
		exit 2
	}
	cmd_dry_run "$3"
	exit 0
	;;
list:1) ;;
set:2 | set:3 | rpc-health:1 | rerender:1)
	shift
	case "$sub" in
	set) cmd_set "$@" ;;
	rpc-health) cmd_rpc_health ;;
	rerender) cmd_rerender ;;
	esac
	exit 0
	;;
-h:* | --help:*)
	usage
	exit 0
	;;
*)
	usage >&2
	exit 2
	;;
esac

if ! on_edge; then
	exec timeout 15m ssh -o BatchMode=yes -- "$EDGE_HOST" "EDGE_LOCAL=1 bash -s -- $(printf '%q ' "$@")" <"${BASH_SOURCE[0]}"
fi

shift
case "$sub" in
add) edge_add "$@" ;;
register)
	up="$2"
	[[ "$up" == *:* ]] || up="$up:443"
	edge_add "$1" railway "$up" -
	;;
remove) edge_remove "$1" ;;
render) cmd_render "$1" ;;
list) [ ! -r "$manifest" ] || cat "$manifest" ;;
esac
