#!/bin/sh
# Init of the identity service on Railway (platform/hosted/identity/railway.env.example).
# Runs as root: writes the store key and the nine service tokens on the volume
# (each token from env IDENTITY_<SERVICE>_TOKEN when set, generated only when
# unset), requires the TLS identity on the volume, hands every file to uid 4020
# and starts the service under that uid.
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
	variable="IDENTITY_$(echo "$service" | tr '[:lower:]' '[:upper:]')_TOKEN"
	if eval "[ \"\${$variable+set}\" = set ]"; then
		eval "printf '%s' \"\$$variable\"" >"$tokens_dir/$service.new"
		mv "$tokens_dir/$service.new" "$tokens_dir/$service"
		unset "$variable"
	else
		fresh "$tokens_dir/$service"
	fi
done

for file in "$LAYERX_IDENTITY_TLS_CERT_DER" "$LAYERX_IDENTITY_TLS_KEY_DER"; do
	[ -s "$file" ] || { echo "identity-env-init: $file is missing or empty" >&2; exit 1; }
done

chown -R 4020:4020 "$LAYERX_IDENTITY_STATE_DIR" "$store_dir" "$tokens_dir" "$tls_dir"
# ca.sh makes the certificate root under umask 077; uid 4020 only traverses it.
chmod 0711 "$(dirname "$tls_dir")"
exec setpriv --reuid=4020 --regid=4020 --clear-groups --no-new-privs /usr/local/bin/layerx-identity
