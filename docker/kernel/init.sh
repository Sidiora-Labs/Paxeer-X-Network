#!/usr/bin/env bash
set -euo pipefail

# Root init of the kernel app's machine. Every process runs under its own uid
# through setpriv and is restarted when it exits; a TLS listener starts only
# once its certificate is on the volume, where tools/bringup/ca.sh writes it.

tls_dir=/data/tls
node_uid=4020
node_gid=4020

# The Paxeer side of the pod: two layerx-paxeer-boundary processes on chain
# 125, each fronting its own serving RPC name through its own loopback socat
# hop to port 443 of that name, verified against the system CA with the name
# as SNI, and the pod's paxeer relay on the relay port dialing the first.
# LAYERX_KERNEL_PAXEER_RPC_NAMES holds the two serving RPC names, first one
# first.
paxeer_chain_id=125
paxeer_relay_port=18545
paxeer_boundaries=(paxeer-boundary-loopback paxeer-boundary-public)
paxeer_boundary_ports=(9447 9448)
paxeer_hop_ports=(18546 18547)
system_ca=/etc/ssl/certs/ca-certificates.crt

# run_as <uid> <gid> <command...>: runs the command under the uid and gid with
# no supplementary groups and no new privileges.
run_as() {
	local uid="$1" gid="$2"
	shift 2
	setpriv --reuid="$uid" --regid="$gid" --clear-groups --no-new-privs -- "$@"
}

# tls_ready <service>: true once the service's certificate, key and CA are on
# the volume; hands its directory to the node uid, as ca.sh writes it as root
# under umask 077, and lets that uid traverse the root.
tls_ready() {
	local dir="$tls_dir/$1"
	[ -r "$dir/cert.der" ] && [ -r "$dir/key.der" ] && [ -r "$dir/ca.pem" ] || return 1
	chmod 0711 "$tls_dir"
	chown -R "$node_uid:$node_gid" "$dir"
	chmod 0700 "$dir"
}

# supervise <name> <tls service or -> <uid> <gid> <command...>: runs the
# command under the uid in the background, after the service's certificate is
# on the volume when one is named, and restarts it whenever it exits.
supervise() {
	local name="$1" tls="$2" uid="$3" gid="$4"
	shift 4
	(
		until [ "$tls" = - ] || tls_ready "$tls"; do
			sleep 5
		done
		while :; do
			status=0
			run_as "$uid" "$gid" "$@" || status=$?
			echo "init: $name exited with status $status; restarting" >&2
			sleep 2
		done
	) &
}

start_paxeer() {
	local -a names
	local k name
	read -r -a names <<<"${LAYERX_KERNEL_PAXEER_RPC_NAMES:-}"
	if [ "${#names[@]}" -ne 2 ] || [ "${names[0]}" = "${names[1]}" ]; then
		echo "init: LAYERX_KERNEL_PAXEER_RPC_NAMES must hold two different serving RPC names" >&2
		exit 1
	fi
	for k in 0 1; do
		name="${names[$k]}"
		case "$name" in
		api[1-9].mainnet-beta.paxeer.network | api1[0-6].mainnet-beta.paxeer.network) ;;
		*)
			echo "init: LAYERX_KERNEL_PAXEER_RPC_NAMES entry $((k + 1)) is not a public RPC name" >&2
			exit 1
			;;
		esac
		supervise "paxeer-hop-$((k + 1))" - "$node_uid" "$node_gid" \
			socat -T 120 "TCP4-LISTEN:${paxeer_hop_ports[$k]},bind=127.0.0.1,reuseaddr,fork" \
			"OPENSSL:$name:443,cafile=$system_ca,verify=1,snihost=$name,commonname=$name"
		supervise "${paxeer_boundaries[$k]}" "${paxeer_boundaries[$k]}" "$node_uid" "$node_gid" \
			env \
			"LAYERX_PAXEER_BOUNDARY_LISTEN=[::]:${paxeer_boundary_ports[$k]}" \
			"LAYERX_PAXEER_NODE_URL=http://127.0.0.1:${paxeer_hop_ports[$k]}" \
			"LAYERX_PAXEER_CHAIN_ID=$paxeer_chain_id" \
			"LAYERX_PAXEER_BOUNDARY_TLS_CERT_DER=$tls_dir/${paxeer_boundaries[$k]}/cert.der" \
			"LAYERX_PAXEER_BOUNDARY_TLS_KEY_DER=$tls_dir/${paxeer_boundaries[$k]}/key.der" \
			/usr/local/bin/layerx-paxeer-boundary
	done
	supervise paxeer-relay "${paxeer_boundaries[0]}" "$node_uid" "$node_gid" \
		socat -T 120 "TCP4-LISTEN:$paxeer_relay_port,bind=127.0.0.1,reuseaddr,fork" \
		"OPENSSL:127.0.0.1:${paxeer_boundary_ports[0]},cafile=$tls_dir/${paxeer_boundaries[0]}/ca.pem,verify=1,commonname=localhost"
}

start_paxeer
wait
