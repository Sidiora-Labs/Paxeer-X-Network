#!/bin/bash
# Root init of the kernel app (human/wallet/deploy/human.toml), the node pod of
# platform/hosted/node/deployment.yaml on one Fly machine. It replays the pod's
# init containers on the volume, mounts the pod's memory volumes, and runs each
# container command under its uid through setpriv and the runtime clock,
# restarting one that exits. A service starts once every file it waits on
# exists; /run/layerx/init/<service> reads "<uid> running <pid>" or
# "<uid> waiting <genesis|path>".
#
# Volume layout (/data):
#   /data/layerx/node         layerxd --data-dir; never holds a key, the genesis
#                             metadata or the run directory (bootstrap.sh refuses them)
#   /data/layerx/keys         every key: sequencer.key and treasury.key, tokens/,
#                             checkpoint-authority/key.pem, checkpoint-submitter/key,
#                             publication/, human-authority/
#   /data/layerx/genesis      metadata.lxgb of tools/bringup/kernel-genesis.sh
#   /data/layerx/guarantor-*  the pod's guarantor storage
#   /data/layerx/settlement   settlement.env and checkpoint-settlement.json
#   /data/layerx/mirror       the mirror publisher's state directory
#   /data/human-state         the pod's human-state volume
#   /data/tls/<service>       identities of tools/bringup/ca.sh issue <service>
set -euo pipefail
umask 077

layerx=/data/layerx
node_data=$layerx/node
keys=$layerx/keys
genesis=$layerx/genesis
human_state=/data/human-state
tls=${LAYERX_FLY_TLS_DIR:-/data/tls}
run=/run/layerx
status=$run/init
genesis_files="$genesis/metadata.lxgb $keys/sequencer.key $genesis/asset-id $genesis/replica-id"

# The layerx-node-config ConfigMap of the pod, and the precompile addresses of
# its layerxd container. The network id is kernel_network_id of the spec's
# [design.fly], set in the app env; the asset id (the PAX record of the custody
# asset map) and the replica id are the ones tools/bringup/kernel-genesis.sh
# wrote beside the genesis metadata.
: "${LAYERX_NODE_NETWORK_ID:?the kernel network id is set in the app env}"
export LAYERX_NODE_NETWORK_ID
export LAYERX_NODE_PAXEER_RELAY_PORT=18545
export LAYERX_NODE_PAXEER_CHAIN_ID=125
export LAYERX_NODE_PAXEER_RPC_URL=http://127.0.0.1:$LAYERX_NODE_PAXEER_RELAY_PORT
export LAYERX_NODE_REGISTRY_PRECOMPILE=0x0000000000000000000000000000000000001004
export LAYERX_NODE_CUSTODY_PRECOMPILE=0x0000000000000000000000000000000000001013
export LAYERX_NODE_ANCHOR_PRECOMPILE=0x0000000000000000000000000000000000001014

log() { printf 'kernel-init: %s\n' "$*" >&2; }

# fresh <file> <owner> <mode> <command...>: writes the command's output to the
# file unless it holds something already.
fresh() {
	local file=$1 owner=$2 mode=$3
	shift 3
	[ -s "$file" ] || { "$@" >"$file.new" && mv "$file.new" "$file"; }
	chown "$owner" "$file"
	chmod "$mode" "$file"
}

evm_key() {
	local key
	while :; do
		key="0x$(openssl rand -hex 32)"
		python3 /opt/layerx/paxeer/evm.py address /dev/stdin <<<"$key" >/dev/null 2>&1 && break
	done
	printf '%s' "$key"
}

memory() {
	mkdir -p "$1"
	mountpoint -q "$1" || mount -t tmpfs -o "nosuid,nodev,mode=$2" tmpfs "$1"
}

# The mirror-signer and mirror-publisher containers' secrets, imported as Fly
# secrets of the app, each base64: the Ethereum secp256k1 publisher key, the
# Solana ed25519 publisher keypair, the config interop/deploy/mirror/
# render-config.py rendered, and a tar of the <backend>.ca.der and
# <backend>.token files its RPC endpoints name under $mirror_run/rpc.
# mirror_inputs writes each present one to memory for uid 4021 and drops it
# from the environment every service inherits.
mirror_material=/run/mirror-material
mirror_run=/run/mirror-publisher

mirror_input() {
	local name=$1 file=$2
	[ -n "${!name:-}" ] || return 0
	base64 -d <<<"${!name}" >"$file" || {
		log "$name is not base64"
		exit 1
	}
	chown 4021:4020 "$file"
	chmod 0400 "$file"
}

mirror_inputs() {
	mirror_input LAYERX_KERNEL_MIRROR_ETHEREUM_KEY "$mirror_material/ethereum.key"
	mirror_input LAYERX_KERNEL_MIRROR_SOLANA_KEYPAIR "$mirror_material/solana.json"
	mirror_input LAYERX_KERNEL_MIRROR_CONFIG "$mirror_run/config.json"
	if [ -n "${LAYERX_KERNEL_MIRROR_RPC_CREDENTIALS:-}" ]; then
		install -d -o 4021 -g 4020 -m 0700 "$mirror_run/rpc"
		base64 -d <<<"$LAYERX_KERNEL_MIRROR_RPC_CREDENTIALS" | tar -x -C "$mirror_run/rpc" --no-same-owner --no-same-permissions || {
			log "LAYERX_KERNEL_MIRROR_RPC_CREDENTIALS is not a base64 tar"
			exit 1
		}
		chown -R 4021:4020 "$mirror_run/rpc"
		find "$mirror_run/rpc" -type f -exec chmod 0400 {} +
	fi
	unset LAYERX_KERNEL_MIRROR_ETHEREUM_KEY LAYERX_KERNEL_MIRROR_SOLANA_KEYPAIR LAYERX_KERNEL_MIRROR_CONFIG \
		LAYERX_KERNEL_MIRROR_RPC_CREDENTIALS
}

memory "$run" 0755
memory /run/authority-private 0700
memory /run/human-private 0755
memory /run/mirror-signer 0700
memory "$mirror_material" 0700
memory "$mirror_run" 0700
memory /tmp 1777
chown 4020:4020 "$run"
chmod 2775 "$run"
chown 4021:4020 /run/authority-private /run/mirror-signer "$mirror_material" "$mirror_run"
mirror_inputs
mkdir -p "$status" "$run/clock"
install -d -o 4020 -g 4020 -m 0750 "$run/node"
chmod 0755 "$status"
echo "$$" >"$status/pid"

install -d -o 0 -g 4020 -m 2775 "$layerx" "$genesis" "$layerx/settlement"
install -d -o 0 -g 4020 -m 0750 "$keys" "$keys/tokens"
install -d -o 0 -g 0 -m 0700 "$keys/checkpoint-authority" "$keys/publication" "$keys/human-authority"
install -d -o 4021 -g 4020 -m 0750 "$keys/checkpoint-submitter"
install -d -o 4021 -g 4020 -m 0700 "$layerx/mirror"
install -d -o 0 -g 4020 -m 0711 "$tls"

# guarantor-storage
install -d -o 4020 -g 4020 -m 2770 "$layerx/guarantor-submitter"
for identity in 1 2; do
	install -d -o 4020 -g 4020 -m 0750 "$layerx/guarantor-$identity" "$layerx/guarantor-$identity/identity"
	install -d -o 4020 -g 4020 -m 2770 "$layerx/guarantor-$identity/state"
done

# human-directories
install -d -o 4020 -g 4020 -m 0750 "$run/human"
install -d -o 4021 -g 4020 -m 0750 "$run/human/owner"
install -d -o 4021 -g 4020 -m 0700 "$run/human/authority-clock"
install -d -o 0 -g 4020 -m 0750 "$human_state"
install -d -o 4020 -g 4020 -m 0700 "$human_state/components" "$human_state/identity" "$human_state/security" \
	"$human_state/movement" "$human_state/movement/evidence"
install -d -o 4021 -g 4020 -m 0700 "$human_state/agent" "$human_state/authority"
install -d -o 4026 -g 4020 -m 0700 "$human_state/kms"

# The trust root of the [[files]] entry, where the pod mounted it.
install -d -m 0755 "$run/trust"
install -m 0444 /etc/layerx/trust/ca.crt "$run/trust/ca.crt"

# Material the pod read from secrets and the machine now makes on the volume.
fresh "$keys/treasury.key" 4020:4020 0400 openssl rand -hex 32
fresh "$keys/tokens/program-token" 4020:4020 0440 openssl rand -hex 32
fresh "$keys/tokens/replica-token" 4020:4020 0440 openssl rand -hex 32
fresh "$keys/checkpoint-submitter/key" 4021:4020 0400 evm_key

# The registry's two bearers, Fly secrets of this app and of the registry app
# (platform/hosted/registry/fly.toml): the agent boundary reads the node bearer
# from registry-component/token and the receipt authority the authority bearer
# from registry-authority/token of LAYERX_AUTHORITY_TOKEN_FILES, where the pod
# mounted the layerx-program-registry-node-client and
# layerx-program-registry-authority-client secrets. Neither reaches a service's
# environment.
registry_bearer() {
	local variable=$1 directory=$run/$2
	if [ -z "${!variable:-}" ]; then
		log "$variable is unset; the program registry cannot authenticate until its deploy step imports it"
	else
		install -d -o 4021 -g 4020 -m 0750 "$directory"
		printf '%s' "${!variable}" >"$directory/token"
		chown 4021:4020 "$directory/token"
		chmod 0440 "$directory/token"
	fi
	unset "$variable"
}
registry_bearer LAYERX_REGISTRY_NODE_AUTHORIZATION registry-component
registry_bearer LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION registry-authority

# checkpoint_authority: the guarantor-checkpoint-authority init container.
checkpoint_authority() {
	local source=$keys/checkpoint-authority/key.pem target=$layerx/guarantor-submitter/checkpoint-authority.pem
	[ -e "$target" ] || install -o 4021 -g 4020 -m 0600 "$source" "$target"
	cmp -s "$source" "$target" || {
		log "the volume holds a different checkpoint authority key than $source"
		return 1
	}
}

# publication_policy: the publication-policy init container.
publication_policy() {
	install -d -o 4020 -g 4020 -m 0700 "$run/publication"
	install -o 4020 -g 4020 -m 0600 "$keys/publication/binding-policy.json" "$run/publication/binding-policy.json"
}

# human_authority_material: the human-authority-material init container.
human_authority_material() {
	local input=$keys/human-authority material=/run/authority-private/material
	install -d -o 4021 -g 4020 -m 0700 "$material"
	install -o 4021 -g 4020 -m 0600 "$input/authority-token" "$material/human-agent.token"
	install -o 4021 -g 4020 -m 0600 "$input/principal-policy.json" "$material/principal-policy.json"
	install -o 4021 -g 4020 -m 0600 "$input/registry.json" "$material/registry.json"
	if [ -e "$input/genesis-handover-trust.lxt" ] || [ -e "$input/handover-finality.conf" ]; then
		install -o 4021 -g 4020 -m 0600 "$input/genesis-handover-trust.lxt" "$material/genesis-handover-trust.lxt"
		install -o 4021 -g 4020 -m 0600 "$input/handover-finality.conf" "$material/handover-finality.conf"
	fi
}

# tls_for <service> <uid>: hands the identity ca.sh wrote as root to the uid.
tls_for() {
	chown -R "$2:4020" "$tls/$1"
	chmod 0711 "$tls"
}

# missing <paths...>: prints the first path that does not exist.
missing() {
	local path
	for path in "$@"; do
		[ -e "$path" ] || {
			printf '%s' "$path"
			return 0
		}
	done
	return 1
}

# service <name> <uid> <waits> <prepare> <clock> -- <command...>: runs the
# command under the uid once every path of the space-separated waits exists,
# after the root prepare step ("-" for none), inside the runtime clock unless
# clock is "-" (a command that enters the clock itself), and restarts it when
# it exits.
service() {
	local name=$1 uid=$2 waits=$3 prepare=$4 clock=$5 absent wrap=() pid rc
	shift 6
	[ "$clock" = - ] || {
		install -d -o "$uid" -g 4020 -m 0700 "$run/clock/$name"
		wrap=(/usr/local/bin/layerx-runtime-clock --runtime-dir "$run/clock/$name" --)
	}
	(
		trap - TERM INT
		while :; do
			# shellcheck disable=SC2086
			if absent="$(missing $waits)"; then
				case " $genesis_files " in
				*" $absent "*) echo "$uid waiting genesis" ;;
				*) echo "$uid waiting $absent" ;;
				esac >"$status/$name"
				sleep 5
				continue
			fi
			if [ "$prepare" != - ] && ! "$prepare"; then
				echo "$uid waiting $prepare" >"$status/$name"
				sleep 5
				continue
			fi
			setpriv --reuid="$uid" --regid=4020 --clear-groups --no-new-privs --pdeathsig TERM \
				${wrap[@]+"${wrap[@]}"} "$@" &
			pid=$!
			echo "$uid running $pid" >"$status/$name"
			rc=0
			wait "$pid" || rc=$?
			log "$name exited with status $rc; restarting"
			sleep 2
		done
	) &
}

guarantor() {
	local identity=$1 port=$2 peer=$3
	service "guarantor-$identity" 4021 \
		"$genesis_files $tls/guarantor/cert.pem $keys/checkpoint-authority/key.pem $keys/publication/authorization.json" \
		guarantor_prepare clock -- \
		env \
		LAYERX_GUARANTOR_IDENTITY_DIR="$layerx/guarantor-$identity/identity" \
		LAYERX_GUARANTOR_STATE_DIR="$layerx/guarantor-$identity/state" \
		LAYERX_GUARANTOR_LNI_SOCKET="$run/node/layerxd.lni.sock" \
		LAYERX_GUARANTOR_SETTLEMENT_ENV="$layerx/settlement/settlement.env" \
		LAYERX_GUARANTOR_SETTLEMENT_FILE="$layerx/settlement/checkpoint-settlement.json" \
		LAYERX_GUARANTOR_SETTLEMENT_DOMAIN=beta \
		LAYERX_GUARANTOR_LISTEN_PORT="$port" \
		LAYERX_GUARANTOR_PEER_URL="https://127.0.0.1:$peer" \
		LAYERX_GUARANTOR_TLS_CA_FILE="$tls/guarantor/ca.pem" \
		LAYERX_GUARANTOR_TLS_CERT_FILE="$tls/guarantor/cert.pem" \
		LAYERX_GUARANTOR_TLS_KEY_FILE="$tls/guarantor/key.pem" \
		LAYERX_GUARANTOR_SUBMITTER_KEY_FILE="$keys/checkpoint-submitter/key" \
		LAYERX_GUARANTOR_SUBMITTER_LOCK_FILE="$layerx/guarantor-submitter/submitter.lock" \
		LAYERX_GUARANTOR_CHECKPOINT_AUTHORITY_KEY_FILE="$layerx/guarantor-submitter/checkpoint-authority.pem" \
		LAYERX_GUARANTOR_PUBLICATION_AUTHORIZATION_SOURCE="$layerx/guarantor-submitter/publication-authorization.json" \
		LAYERX_GUARANTOR_PYTHON=/opt/layerx/guarantor/venv/bin/python3 \
		/opt/layerx/guarantor.sh
}

# The publication authorization of kernel-genesis.sh, handed from the root-only
# publication directory to the guarantor uid that installs it.
guarantor_prepare() {
	checkpoint_authority && tls_for guarantor 4021 &&
		install -o 4021 -g 4020 -m 0600 "$keys/publication/authorization.json" \
			"$layerx/guarantor-submitter/publication-authorization.json"
}

human_authority_ready() {
	while missing "$keys/human-authority/authority-token" "$keys/human-authority/principal-policy.json" \
		"$keys/human-authority/registry.json" >/dev/null; do
		sleep 5
	done
	human_authority_material
	log "human authority material installed"
}

# The Paxeer side of the pod: two layerx-paxeer-boundary processes on chain
# 125, each fronting its own serving RPC name through its own loopback socat
# hop to port 443 of that name, verified against the system CA with the name
# as SNI, and the pod's paxeer relay on the relay port dialing the first.
# LAYERX_KERNEL_PAXEER_RPC_NAMES holds the two serving RPC names, first one
# first.
paxeer_boundaries=(paxeer-boundary-loopback paxeer-boundary-public)
paxeer_boundary_ports=(9447 9448)
paxeer_hop_ports=(18546 18547)
paxeer_prepares=(paxeer_boundary_loopback_prepare paxeer_boundary_public_prepare)
system_ca=/etc/ssl/certs/ca-certificates.crt

paxeer_boundary_loopback_prepare() {
	tls_for paxeer-boundary-loopback 4020
}

paxeer_boundary_public_prepare() {
	tls_for paxeer-boundary-public 4020
}

start_paxeer() {
	local -a names
	local k name boundary
	read -r -a names <<<"${LAYERX_KERNEL_PAXEER_RPC_NAMES:-}"
	if [ "${#names[@]}" -ne 2 ] || [ "${names[0]}" = "${names[1]}" ]; then
		log "LAYERX_KERNEL_PAXEER_RPC_NAMES must hold two different serving RPC names"
		exit 1
	fi
	for k in 0 1; do
		name="${names[$k]}"
		case "$name" in
		api[1-9].mainnet-beta.paxeer.network | api1[0-6].mainnet-beta.paxeer.network) ;;
		*)
			log "LAYERX_KERNEL_PAXEER_RPC_NAMES entry $((k + 1)) is not a public RPC name"
			exit 1
			;;
		esac
		boundary="${paxeer_boundaries[$k]}"
		service "paxeer-hop-$((k + 1))" 4020 "" - - -- \
			socat -T 120 "TCP4-LISTEN:${paxeer_hop_ports[$k]},bind=127.0.0.1,reuseaddr,fork" \
			"OPENSSL:$name:443,cafile=$system_ca,verify=1,snihost=$name,commonname=$name"
		service "$boundary" 4020 "$tls/$boundary/cert.der $tls/$boundary/key.der $tls/$boundary/ca.pem" \
			"${paxeer_prepares[$k]}" - -- \
			env \
			"LAYERX_PAXEER_BOUNDARY_LISTEN=[::]:${paxeer_boundary_ports[$k]}" \
			"LAYERX_PAXEER_NODE_URL=http://127.0.0.1:${paxeer_hop_ports[$k]}" \
			"LAYERX_PAXEER_CHAIN_ID=$LAYERX_NODE_PAXEER_CHAIN_ID" \
			"LAYERX_PAXEER_BOUNDARY_TLS_CERT_DER=$tls/$boundary/cert.der" \
			"LAYERX_PAXEER_BOUNDARY_TLS_KEY_DER=$tls/$boundary/key.der" \
			/usr/local/bin/layerx-paxeer-boundary
	done
	service paxeer-relay 4020 "$tls/${paxeer_boundaries[0]}/ca.pem" "${paxeer_prepares[0]}" - -- \
		socat -T 120 "TCP4-LISTEN:$LAYERX_NODE_PAXEER_RELAY_PORT,bind=127.0.0.1,reuseaddr,fork" \
		"OPENSSL:127.0.0.1:${paxeer_boundary_ports[0]},cafile=$tls/${paxeer_boundaries[0]}/ca.pem,verify=1,commonname=localhost"
}

trap 'kill 0' TERM INT

service treasury-signer 4020 "$keys/treasury.key $keys/publication/binding-policy.json" publication_policy clock -- \
	python3 /opt/layerx/signer/signer.py \
	--socket "$run/node/treasury-signer.sock" \
	--provider file \
	--key-file "$keys/treasury.key" \
	--allowed-uid 4020,4021 \
	--socket-group 4020 \
	--public-key-file "$run/node/treasury-public-key" \
	--binding-policy "$run/publication/binding-policy.json"

# kernel_ids: the generated asset and replica ids, refused when either is not
# 64 hex characters or is the old beta constant.
kernel_ids() {
	local id
	for id in "$(cat "$genesis/asset-id")" "$(cat "$genesis/replica-id")"; do
		case "$id" in
		b5a32b12029f8ddfb905f90f280f664b46390de0fc62770fc197dd87b18cd898 | 6c61796572782d626574612d726563656970742d617574686f726974792d3031)
			log "the genesis ids hold the old beta constant $id; run kernel-genesis.sh rotate"
			return 1
			;;
		esac
		[[ $id =~ ^[0-9a-f]{64}$ ]] || return 1
	done
}

# shellcheck disable=SC2016 # the ids expand when the service starts
service layerxd 4020 "$genesis_files" kernel_ids clock -- \
	/bin/sh -c 'exec /opt/layerx/supervisor.sh "$@" --asset "$(cat '"$genesis"'/asset-id)" --replica-id "$(cat '"$genesis"'/replica-id)"' layerxd \
	--role sequencer --data-dir "$node_data" --run-dir "$run/node" -- \
	--network-id "$LAYERX_NODE_NETWORK_ID" \
	--genesis-metadata "$genesis/metadata.lxgb" \
	--custody-profile "$genesis/custody.profile" \
	--withdrawal-fee 0 \
	--module-fees /opt/layerx/genesis-module-fees.json \
	--sequencer-key "$keys/sequencer.key" \
	--treasury-signer-socket "$run/node/treasury-signer.sock" \
	--program-token-file "$keys/tokens/program-token" \
	--replica-token-file "$keys/tokens/replica-token" \
	--program-port 9401 \
	--replica-port 9402 \
	--lni-uid 4021 \
	--lni-gid 4020

service layerxd-authority 4020 "$genesis_files" - clock -- \
	/opt/layerx/supervisor.sh --role replica --data-dir "$node_data" --run-dir "$run/node"

guarantor 1 9451 9452
guarantor 2 9452 9451

start_paxeer

service human 4020 "" - - -- /usr/local/bin/human-entrypoint service

# The mirror-signer and mirror-publisher containers: the signer serves both
# publisher keys on its socket, and the publisher reads the LNI socket and
# answers /readyz and /status on 127.0.0.1:9456, the status_listen the
# rendered config names.
service mirror-signer 4021 "$genesis_files $mirror_material/ethereum.key" - - -- \
	env \
	LAYERX_MIRROR_SIGNER_SOCKET=/run/mirror-signer/signer.sock \
	LAYERX_MIRROR_SIGNER_ETHEREUM_KEY_FILE="$mirror_material/ethereum.key" \
	LAYERX_MIRROR_SIGNER_SOLANA_KEY_FILE="$mirror_material/solana.json" \
	/usr/local/bin/layerx-mirror-signer

service mirror-publisher 4021 \
	"$genesis_files $run/node/layerxd.lni.sock $mirror_run/config.json /run/mirror-signer/signer.sock" - - -- \
	/usr/local/bin/layerx-mirror-publisher "$mirror_run/config.json"

human_authority_ready &

wait
