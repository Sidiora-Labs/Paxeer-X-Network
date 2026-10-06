#!/usr/bin/env bash
# Debian installer for the fleet monitoring stack.
#   install.sh server [hosts.env]   monitoring host: Docker, rendered config, compose stack
#   install.sh node                 every fleet host: node_exporter from Debian packages
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
[ "$(id -u)" -eq 0 ] || { echo "install.sh: run as root" >&2; exit 1; }

case ${1:-} in
node)
	apt-get update
	apt-get install -y prometheus-node-exporter
	systemctl enable --now prometheus-node-exporter
	echo "node_exporter listening on :9100; allow it only from the monitoring host in the firewall"
	;;
server)
	env_file=${2:-/etc/paxeer-monitoring/hosts.env}
	[ -r "$env_file" ] || { echo "install.sh: hosts env $env_file not readable (start from hosts.env.example)" >&2; exit 1; }
	dest=/opt/paxeer-monitoring

	apt-get update
	apt-get install -y docker.io docker-compose-plugin || apt-get install -y docker.io docker-compose
	systemctl enable --now docker

	install -d -m 0755 "$dest"
	install -m 0644 "$here/docker-compose.yml" "$here/prometheus.yml" "$here/rules.yml" "$here/blackbox.yml" "$dest/"
	install -m 0755 "$here/render.sh" "$dest/"
	rm -rf "$dest/rendered"
	"$dest/render.sh" "$env_file" "$dest/rendered"
	chown -R 65534:65534 "$dest/rendered/secrets"

	if docker compose version >/dev/null 2>&1; then
		docker compose -f "$dest/docker-compose.yml" -p paxeer-monitoring up -d
	else
		docker-compose -f "$dest/docker-compose.yml" -p paxeer-monitoring up -d
	fi
	echo "Prometheus on 127.0.0.1:9090, Alertmanager on 127.0.0.1:9093"
	;;
*)
	echo "usage: install.sh server [hosts.env] | install.sh node" >&2
	exit 2
	;;
esac
