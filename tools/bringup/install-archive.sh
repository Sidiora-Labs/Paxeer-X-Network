#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'EOF'
usage: tools/bringup/install-archive.sh <public-rpc-name> <acme-email>

Runs as root on the archive host and brings it to the archive state without
touching what is already right: paxd installed through hpx as a full node at
the release the mirror publishes, min-retain-blocks and ss-keep-recent at 0 so
the node keeps every block, receipt and state version from its first synced
block on, and nginx serving the node's JSON-RPC and WebSocket at the public
RPC name over a Let's Encrypt certificate, like the other fifteen public RPC
hosts. Every step is skipped when its result is already in place; only a
changed app.toml restarts paxd, only a new vhost reloads nginx, and only a
paxd that differs from the published release is updated. A fresh node syncs
from the snapshot the mirror's trust anchor admits; the explorer's history
copy covers the blocks before it.

From the operator host, with the host map loaded:
  ssh -- "$ARCHIVE_HOST" bash -s -- <public-rpc-name> <acme-email> \
    <tools/bringup/install-archive.sh

Arguments:
  public-rpc-name   one of api1 to api16 .mainnet-beta.paxeer.network
  acme-email        the Let's Encrypt account address, used only when the
                    name has no certificate yet

Environment:
  HPX_MIRROR   the hpx mirror, default https://node.hyperpaxeer.com
  HPX_HOME     the node home, default /root/.paxeer

Exits 1 when a step fails, 2 on a usage error.
EOF
}

case "${1:-}" in
-h | --help)
	usage
	exit 0
	;;
esac

if [ "$#" -ne 2 ]; then
	usage >&2
	exit 2
fi

name="$1"
email="$2"

if ! [[ "$name" =~ ^api([1-9]|1[0-6])\.mainnet-beta\.paxeer\.network$ ]] || [[ "$email" != *@* ]]; then
	usage >&2
	exit 2
fi

if [ "$(id -u)" -ne 0 ]; then
	echo "install-archive: must run as root" >&2
	exit 1
fi

mirror="${HPX_MIRROR:-https://node.hyperpaxeer.com}"
home="${HPX_HOME:-/root/.paxeer}"
app="$home/config/app.toml"
vhost="/etc/nginx/sites-available/$name.conf"
enabled="/etc/nginx/sites-enabled/$name.conf"
webroot=/var/www/certbot

say() {
	echo "install-archive: $*"
}

# packages: curl and jq for hpx, nginx and certbot for the public name.
missing=()
for tool in curl jq nginx certbot; do
	command -v "$tool" >/dev/null 2>&1 || missing+=("$tool")
done
if [ "${#missing[@]}" -ne 0 ]; then
	DEBIAN_FRONTEND=noninteractive apt-get update -qq
	DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "${missing[@]}"
	say "installed ${missing[*]}"
fi

# hpx: the CLI verified against the mirror's manifest, as get-hpx.sh does.
if ! command -v hpx >/dev/null 2>&1; then
	want="$(curl -fsSL --retry 5 --max-time 60 "$mirror/checksums.txt" | awk '$2 == "hpx" { print $1; exit }')"
	curl -fsSL --retry 5 --max-time 60 "$mirror/hpx" -o /usr/local/bin/hpx.new
	got="$(sha256sum /usr/local/bin/hpx.new | awk '{print $1}')"
	if [ -z "$want" ] || [ "$got" != "$want" ]; then
		rm -f /usr/local/bin/hpx.new
		echo "install-archive: hpx sha256 mismatch against $mirror/checksums.txt" >&2
		exit 1
	fi
	chmod +x /usr/local/bin/hpx.new
	mv -f /usr/local/bin/hpx.new /usr/local/bin/hpx
	say "installed hpx"
fi

# node: a full node through hpx when none is here; its prompts take their
# defaults from an empty stdin.
if [ ! -f "$home/config/config.toml" ] || [ ! -f /etc/systemd/system/paxd.service ]; then
	HPX_TYPE=fullnode HPX_HOME="$home" HPX_MIRROR="$mirror" hpx setup </dev/null
	say "installed paxd full node"
fi

# retention: both keys at 0 keep every block, receipt and state version.
restart=0
for key in min-retain-blocks ss-keep-recent; do
	current="$(sed -n "s/^$key = //p" "$app")"
	case "$current" in
	0) ;;
	"")
		echo "install-archive: $app has no $key line" >&2
		exit 1
		;;
	*)
		sed -i "s/^$key = .*/$key = 0/" "$app"
		say "$key $current -> 0"
		restart=1
		;;
	esac
done
if [ "$restart" -eq 1 ]; then
	systemctl restart paxd
	say "restarted paxd"
fi
systemctl is-enabled --quiet paxd || systemctl enable paxd
systemctl is-active --quiet paxd || systemctl start paxd

# public name: the vhost the other public RPC hosts run, proxying 443 to the
# node's JSON-RPC and WebSocket ports on loopback, written only when absent.
write_vhost() {
	{
		cat <<EOF
server {
    listen 80;
    listen [::]:80;
    server_name $name;
    location /.well-known/acme-challenge/ { root $webroot; }
    location / { return 301 https://\$host\$request_uri; }
}
EOF
		if [ "$1" = tls ]; then
			cat <<EOF

map \$http_upgrade \$api_connection_upgrade { default upgrade; '' close; }
map \$http_upgrade \$api_backend { default 127.0.0.1:8645; websocket 127.0.0.1:8646; }

server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name $name;

    ssl_certificate     /etc/letsencrypt/live/$name/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$name/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_session_cache shared:api_ssl:10m;

    client_max_body_size 10m;
    proxy_http_version 1.1;
    proxy_set_header Host \$host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto https;
    proxy_set_header Upgrade \$http_upgrade;
    proxy_set_header Connection \$api_connection_upgrade;

    location = /health { access_log off; return 200 'ok'; add_header Content-Type text/plain; }
    location /ws {
        proxy_pass http://127.0.0.1:8646/;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }
    location / {
        proxy_pass http://\$api_backend;
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }
}
EOF
		fi
	} >"$vhost"
	ln -sf "$vhost" "$enabled"
	nginx -t
	systemctl enable --now nginx
	systemctl reload nginx
}

if [ ! -e "$enabled" ]; then
	mkdir -p "$webroot"
	if [ ! -f "/etc/letsencrypt/live/$name/fullchain.pem" ]; then
		write_vhost http
		certbot certonly --webroot -w "$webroot" -d "$name" -m "$email" --agree-tos --non-interactive
		say "issued certificate for $name"
	fi
	write_vhost tls
	say "serving $name"
fi

# release: paxd at the sha256 the mirror publishes, updated through hpx.
published="$(curl -fsS --max-time 15 "$mirror/chain-info.json" | jq -r '.paxd_sha256 // empty')"
if [ -z "$published" ]; then
	echo "install-archive: $mirror/chain-info.json names no paxd_sha256" >&2
	exit 1
fi
if [ "$published" != "$(sha256sum /usr/local/bin/paxd | awk '{print $1}')" ]; then
	HPX_HOME="$home" HPX_MIRROR="$mirror" hpx update
	say "updated paxd to the published release"
fi

say "paxd=$(systemctl is-active paxd) nginx=$(systemctl is-active nginx) health=$(curl -fsS --max-time 10 "https://$name/health")"
