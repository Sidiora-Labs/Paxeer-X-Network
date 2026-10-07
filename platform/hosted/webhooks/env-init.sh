#!/bin/sh
# Init of one process group of the webhooks service. Runs as root in the
# group's container:
#   webhooks-env-init public|ingress
# public serves only the developer API, private TLS on [::]:9444 behind the
# unified endpoint, and reads only the developer identity token of the role credentials.
# ingress serves only the internal producer and operator routes, TLS on
# [::]:443 with the certificate of the developer row of tools/bringup/ca.sh,
# requiring an internal-CA client leaf with a webhook role URI SAN, and reads
# the trigger, operator, source, receipt and sequencer credentials. Each
# replica is its own delivery instance, named by its Railway replica id, and the
# service runs as uid 65532.
set -eu
[ "$#" -eq 1 ] || {
	echo "usage: webhooks-env-init public|ingress" >&2
	exit 2
}
: "${RAILWAY_REPLICA_ID:?webhooks-env-init needs RAILWAY_REPLICA_ID}"
export LAYERX_WEBHOOKS_INSTANCE_ID="$RAILWAY_REPLICA_ID"
case "$1" in
public)
	export LAYERX_WEBHOOKS_ROLE=public LAYERX_WEBHOOKS_LISTENER=tls LAYERX_WEBHOOKS_LISTEN="[::]:9444" \
		LAYERX_WEBHOOKS_HEALTH_LISTEN="[::]:9442" \
		LAYERX_WEBHOOKS_TLS_CERT_DER=/run/layerx/tls/server.der \
		LAYERX_WEBHOOKS_TLS_KEY_DER=/run/layerx/tls/server-key.der \
		LAYERX_WEBHOOKS_IDENTITY_TOKEN_FILE=/run/layerx/tokens/identity
	exec setpriv --reuid=65532 --regid=65532 --clear-groups --no-new-privs /usr/local/bin/layerx-webhooks
	;;
ingress)
	export LAYERX_WEBHOOKS_ROLE=ingress LAYERX_WEBHOOKS_LISTENER=tls LAYERX_WEBHOOKS_LISTEN="[::]:443" \
		LAYERX_WEBHOOKS_TLS_CERT_DER=/run/layerx/tls/server.der \
		LAYERX_WEBHOOKS_TLS_KEY_DER=/run/layerx/tls/server-key.der \
		LAYERX_WEBHOOKS_INGRESS_CLIENT_CA_DER=/run/layerx/ca/internal.der \
		LAYERX_WEBHOOKS_COMPONENT_TOKEN_FILE=/run/layerx/tokens/component \
		LAYERX_WEBHOOKS_AUTHORITY_TOKEN_FILE=/run/layerx/tokens/authority \
		LAYERX_WEBHOOKS_JOURNEY_SOURCE_TOKEN_FILE=/run/layerx/tokens/journey \
		LAYERX_WEBHOOKS_PAYMENT_SOURCE_TOKEN_FILE=/run/layerx/tokens/payment \
		LAYERX_WEBHOOKS_APPROVAL_SOURCE_TOKEN_FILE=/run/layerx/tokens/approval \
		LAYERX_WEBHOOKS_PROGRAM_SOURCE_TOKEN_FILE=/run/layerx/tokens/program \
		LAYERX_WEBHOOKS_SOURCE_TRIGGER_TOKEN_FILE=/run/layerx/tokens/source-trigger \
		LAYERX_WEBHOOKS_OPERATOR_TOKEN_FILE=/run/layerx/tokens/operator \
		LAYERX_WEBHOOKS_SEQUENCER_PUBLIC_KEY_FILE=/run/layerx/keys/sequencer \
		LAYERX_WEBHOOKS_SEQUENCER_ID_FILE=/run/layerx/keys/sequencer-id \
		LAYERX_WEBHOOKS_SEQUENCER_FIRST_BATCH_FILE=/run/layerx/keys/sequencer-first-batch \
		LAYERX_WEBHOOKS_SEQUENCER_LAST_BATCH_FILE=/run/layerx/keys/sequencer-last-batch
	exec setpriv --reuid=65532 --regid=65532 --clear-groups \
		--inh-caps=-all,+net_bind_service --ambient-caps=-all,+net_bind_service \
		/usr/local/bin/layerx-webhooks
	;;
*)
	echo "usage: webhooks-env-init public|ingress" >&2
	exit 2
	;;
esac
