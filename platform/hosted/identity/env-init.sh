#!/bin/sh
# Init of the identity service. Runs as root in the container: generates
# the store key and the nine service tokens on
# the volume on first boot, waits until tools/bringup/ca.sh issue identity has
# put the TLS identity on the volume, hands every file to uid 4020 and starts
# the service under that uid.
set -eu
umask 077
tls_dir="$(dirname "$LAYERX_IDENTITY_TLS_CERT_DER")"
store_dir="$(dirname "$LAYERX_IDENTITY_STORE_KEY_FILE")"
tokens_dir="$LAYERX_IDENTITY_SERVICE_TOKENS_DIR"
mkdir -p "$LAYERX_IDENTITY_STATE_DIR" "$store_dir" "$tokens_dir"

# fresh <file>: writes 32 random bytes as hex to the file unless it holds one.
fresh() {
	[ -s "$1" ] || { openssl rand -hex 32 >"$1.new" && mv "$1.new" "$1"; }
}
fresh "$LAYERX_IDENTITY_STORE_KEY_FILE"
for service in gateway registry webhooks dashboard faucet testnet ramp provisioning registrar; do
	fresh "$tokens_dir/$service"
done

if [ ! -s "$LAYERX_IDENTITY_TLS_CERT_DER" ] || [ ! -s "$LAYERX_IDENTITY_TLS_KEY_DER" ]; then
	echo "identity-env-init: waiting for $tls_dir from tools/bringup/ca.sh issue identity"
	until [ -s "$LAYERX_IDENTITY_TLS_CERT_DER" ] && [ -s "$LAYERX_IDENTITY_TLS_KEY_DER" ]; do
		sleep 5
	done
fi

chown -R 4020:4020 "$LAYERX_IDENTITY_STATE_DIR" "$store_dir" "$tokens_dir" "$tls_dir"
# ca.sh makes the certificate root under umask 077; uid 4020 only traverses it.
chmod 0711 "$(dirname "$tls_dir")"
exec setpriv --reuid=4020 --regid=4020 --clear-groups --no-new-privs /usr/local/bin/layerx-identity
