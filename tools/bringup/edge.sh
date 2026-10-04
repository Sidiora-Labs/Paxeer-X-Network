#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'EOF'
usage: tools/bringup/edge.sh add <name> <app> [<port> stream] | register <name> <app> | remove <name> | render <name> | list

Serves a public name on the edge host, where the *.paxeer.network wildcard
lands, by proxying it to its Fly app at <app>.fly.dev. Acts on this host when
the host map's EDGE_HOST resolves to one of its addresses (or EDGE_LOCAL=1),
over ssh to EDGE_HOST otherwise. Only names this script registered are ever
rendered, changed or removed; the manifest it owns, EDGE_NGINX_DIR/edge/manifest,
holds one "<name> <mode> <app> <port>" line per name and no address.

add       http mode (no port): renders EDGE_NGINX_DIR/sites-available/<name>.conf
          with the plain listener and the ACME location first, obtains the
          certbot certificate for the name over HTTP-01 when absent (renewed by
          certbot's timer, which reloads nginx), then renders the TLS server
          that proxies to https://<app>.fly.dev with Host <app>.fly.dev,
          X-Forwarded-Host, X-Forwarded-For, X-Real-IP and X-Forwarded-Proto,
          Upgrade and Connection passed, the upstream verified against the
          system roots and re-resolved every 30 seconds.
          stream mode (<port> stream): passes the port through unchanged by SNI
          to <app>.fly.dev on the same port (the app's dedicated IPv4) from an
          nginx stream block, so client certificates reach the app; on a port
          the HTTP names share (443) those names move behind the stream block
          to a loopback listener that keeps the client address.
          Every step runs nginx -t and reloads; a failing nginx -t restores the
          previous files. Prints "added <name> mode=<mode> app=<app> port=<port>".
register  the same as add; a trailing .fly.dev on <app> is dropped.
remove    deletes the name's site or stream entry and reloads; prints
          "removed <name>".
render    prints the file add renders for a registered name: the TLS site of
          an http name, the stream block of a stream name's port.
list      prints the manifest.

Environment:
  BRINGUP_HOSTS_FILE  the private host map naming EDGE_HOST; never printed
  EDGE_LOCAL          1 acts on this host without reading the host map
  EDGE_NGINX_DIR      nginx configuration root, default /etc/nginx
  EDGE_WEBROOT        ACME webroot, default /var/www/certbot
  EDGE_CERT_DIR       certbot live directory, default /etc/letsencrypt/live
  EDGE_RESOLVER       resolver nginx uses for the upstreams, default 127.0.0.53

Browser sponsorship uses this same public name for /gas-station/quote and
/v1/wallet/sponsored/{submit,status}. The station process must set
GAS_STATION_BROWSER_ORIGINS to comma-separated exact wallet origins; HTTPS
origins or loopback HTTP are accepted. Origin and preflight headers pass to
the serving upstream, which owns the refusal policy. No wildcard CORS is added.

Exits 1 when a step fails or a name is refused, 2 on a usage error.
EOF
}

nginx_dir="${EDGE_NGINX_DIR:-/etc/nginx}"
webroot="${EDGE_WEBROOT:-/var/www/certbot}"
cert_dir="${EDGE_CERT_DIR:-/etc/letsencrypt/live}"
resolver="${EDGE_RESOLVER:-127.0.0.53}"
state="$nginx_dir/edge"
manifest="$state/manifest"
stream_root="$nginx_dir/modules-enabled/99-edge-stream.conf"
marker="# rendered by tools/bringup/edge.sh; edit the manifest through it, not this file"
# The HTTP names listen here, behind the stream block, once a stream name
# shares their port.
loopback_port=10443
inner_port=10444

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

render_http() {
	local name="$1" app="$2" tls="$3" listen
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
		set \$edge_upstream $app.fly.dev;
		proxy_pass https://\$edge_upstream;
		proxy_http_version 1.1;
		proxy_set_header Host $app.fly.dev;
		proxy_set_header X-Forwarded-Host $name;
		proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
		proxy_set_header X-Real-IP \$remote_addr;
		proxy_set_header X-Forwarded-Proto \$scheme;
		proxy_set_header Origin \$http_origin;
		proxy_set_header Access-Control-Request-Method \$http_access_control_request_method;
		proxy_set_header Access-Control-Request-Headers \$http_access_control_request_headers;
		proxy_set_header Upgrade \$http_upgrade;
		proxy_set_header Connection \$http_connection;
		proxy_ssl_server_name on;
		proxy_ssl_name $app.fly.dev;
		proxy_ssl_verify on;
		proxy_ssl_verify_depth 4;
		proxy_ssl_trusted_certificate /etc/ssl/certs/ca-certificates.crt;
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
	names="$(awk -v p="$port" '$2 == "stream" && $4 == p {printf "\t%s %s.fly.dev:%s;\n", $1, $3, p}' "$manifest")"
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

# write_all <tls-for-name|-> <name>: renders every registered name's files
# from the manifest; the named name gets only its plain listener unless its
# certificate is present.
write_all() {
	local name mode app port tls port_file
	mkdir -p "$state/stream"
	rm -f -- "$state"/stream/*.conf
	while read -r name mode app port; do
		[ -n "$name" ] || continue
		if [ "$mode" = http ]; then
			tls=tls
			[ -r "$cert_dir/$name/fullchain.pem" ] || tls=plain
			render_http "$name" "$app" "$tls" >"$nginx_dir/sites-available/$name.conf"
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
	local name="$1" app="${2%.fly.dev}" port="${3:-443}" mode=http line users
	[[ "$name" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$ ]] || die "not a DNS name: $name"
	[[ "$app" =~ ^[a-z0-9][a-z0-9-]*$ ]] || die "not a Fly app name: $app"
	if [ "$#" -eq 4 ]; then
		if [ "$4" != stream ] || ! [[ "$port" =~ ^[0-9]+$ ]] || [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
			usage >&2
			exit 2
		fi
		mode=stream
	fi
	case "$name" in
	api[0-9]*.mainnet-beta.paxeer.network | paxscan.io) die "refuse $name: served on its own host" ;;
	esac
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
	line="$name $mode $app $port"
	[ "$mode" = stream ] || line="$name http $app 443"
	snapshot
	awk -v n="$name" '$1 != n' "$manifest" >"$manifest.new"
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
	echo "added $name mode=$mode app=$app port=$port"
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
	local name mode app port
	read -r name mode app port <<<"$(entry "$1")" || die "not registered: $1"
	if [ "$mode" = stream ]; then
		render_stream "$port"
	else
		render_http "$name" "$app" tls
	fi
}

sub="${1:-}"
case "$sub:$#" in
add:3 | add:5 | register:3) ;;
remove:2 | render:2) ;;
list:1) ;;
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
add | register) edge_add "$@" ;;
remove) edge_remove "$1" ;;
render) cmd_render "$1" ;;
list) [ ! -r "$manifest" ] || cat "$manifest" ;;
esac
