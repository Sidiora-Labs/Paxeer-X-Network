#!/bin/sh
# Root init of the bridge relayer Fly app: hands the mounted secrets to their
# owners, splits the RPC bearer tokens into owner-only files, starts the
# bridge signer on its Unix socket, the relayer beside it and, when
# LAYERX_BRIDGE_COSIGN_TRANSPORT_CONFIG is set, the cosign share transport,
# and exits when any of them exits, so Fly Machines restart the machine.
set -eu
secrets=/run/secrets/bridge
signer_socket=/run/bridge-signer/signer.sock

for key in "$secrets"/signer/*; do
	chown 4101:4100 "$key"
	chmod 0400 "$key"
done
chown 4101:4100 "$secrets/signer"
chmod 0500 "$secrets/signer"
chown 4102:4102 "$secrets/relayer.json"
chmod 0400 "$secrets/relayer.json"
chmod 0711 "$secrets"

install -d -o 4102 -g 4102 -m 0700 /run/bridge-relayer/rpc
while IFS='=' read -r name token || [ -n "$name" ]; do
	[ -n "$name" ] || continue
	case "$name" in
	[a-z0-9]*[!a-z0-9_-]* | [!a-z0-9]*)
		echo "bridge-relayer-entrypoint: rpc token name $name is not [a-z0-9][a-z0-9_-]*" >&2
		exit 1
		;;
	esac
	if [ -z "$token" ]; then
		echo "bridge-relayer-entrypoint: rpc token $name is empty" >&2
		exit 1
	fi
	(umask 077 && printf '%s' "$token" >"/run/bridge-relayer/rpc/$name.token")
	chown 4102:4102 "/run/bridge-relayer/rpc/$name.token"
done <"$secrets/rpc-tokens"
rm -f "$secrets/rpc-tokens"

install -d -o 4102 -g 4102 -m 0700 /data/relayer /data/cosign
transport_pid=
if [ -n "${LAYERX_BRIDGE_COSIGN_TRANSPORT_CONFIG+set}" ]; then
	if [ ! -f "$LAYERX_BRIDGE_COSIGN_TRANSPORT_CONFIG" ] || [ ! -r "$LAYERX_BRIDGE_COSIGN_TRANSPORT_CONFIG" ]; then
		echo "bridge-relayer-entrypoint: LAYERX_BRIDGE_COSIGN_TRANSPORT_CONFIG is set but '$LAYERX_BRIDGE_COSIGN_TRANSPORT_CONFIG' is missing or unreadable" >&2
		exit 1
	fi
	install -d -o 4102 -g 4102 -m 0700 /data/cosign-delivery
fi
install -d -o 4101 -g 4100 -m 0750 /run/bridge-signer

setpriv --reuid=4101 --regid=4100 --clear-groups \
	/usr/local/bin/layerx-mirror-signer bridge --socket "$signer_socket" &
signer_pid=$!
waited=0
while [ ! -S "$signer_socket" ] && kill -0 "$signer_pid" 2>/dev/null && [ "$waited" -lt 30 ]; do
	sleep 1
	waited=$((waited + 1))
done
if [ ! -S "$signer_socket" ]; then
	echo "bridge-relayer-entrypoint: the bridge signer did not publish $signer_socket" >&2
	kill -TERM "$signer_pid" 2>/dev/null || true
	exit 1
fi

setpriv --reuid=4102 --regid=4102 --groups=4100 \
	/usr/local/bin/layerx-bridge-relayer --config "$secrets/relayer.json" &
relayer_pid=$!

if [ -n "${LAYERX_BRIDGE_COSIGN_TRANSPORT_CONFIG+set}" ]; then
	setpriv --reuid=4102 --regid=4102 --clear-groups \
		/usr/local/bin/layerx-bridge-cosign &
	transport_pid=$!
fi

stop() {
	kill -TERM "$signer_pid" "$relayer_pid" $transport_pid 2>/dev/null || true
	wait
}
trap 'stop; exit 0' TERM INT

while kill -0 "$signer_pid" 2>/dev/null && kill -0 "$relayer_pid" 2>/dev/null &&
	{ [ -z "$transport_pid" ] || kill -0 "$transport_pid" 2>/dev/null; }; do
	sleep 1
done
echo "bridge-relayer-entrypoint: the bridge signer, the relayer or the cosign transport exited" >&2
stop
exit 1
