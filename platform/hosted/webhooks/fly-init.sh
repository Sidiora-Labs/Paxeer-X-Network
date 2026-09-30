#!/bin/sh
# Init of one process group of the webhooks app on Fly
# (platform/hosted/webhooks/fly.toml). Runs as root on the group's machine:
#   webhooks-fly-init public|ingress
# public serves plain HTTP on [::]:9444 behind the Fly edge; ingress serves TLS
# on [::]:443 with the certificate of the developer row of tools/bringup/ca.sh
# for the event producers on the private network. Each machine is its own
# delivery instance, named by its Fly machine id, and the service runs as uid
# 65532.
set -eu
[ "$#" -eq 1 ] || {
	echo "usage: webhooks-fly-init public|ingress" >&2
	exit 2
}
: "${FLY_MACHINE_ID:?webhooks-fly-init runs on a Fly machine}"
export LAYERX_WEBHOOKS_INSTANCE_ID="$FLY_MACHINE_ID"
case "$1" in
public)
	export LAYERX_WEBHOOKS_LISTENER=plain LAYERX_WEBHOOKS_LISTEN="[::]:9444"
	exec setpriv --reuid=65532 --regid=65532 --clear-groups --no-new-privs /usr/local/bin/layerx-webhooks
	;;
ingress)
	export LAYERX_WEBHOOKS_LISTENER=tls LAYERX_WEBHOOKS_LISTEN="[::]:443" \
		LAYERX_WEBHOOKS_TLS_CERT_DER=/run/layerx/tls/server.der \
		LAYERX_WEBHOOKS_TLS_KEY_DER=/run/layerx/tls/server-key.der
	exec setpriv --reuid=65532 --regid=65532 --clear-groups \
		--inh-caps=-all,+net_bind_service --ambient-caps=-all,+net_bind_service \
		/usr/local/bin/layerx-webhooks
	;;
*)
	echo "usage: webhooks-fly-init public|ingress" >&2
	exit 2
	;;
esac
