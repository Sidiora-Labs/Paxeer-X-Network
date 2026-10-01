#!/bin/sh
# Init of one process group of the internal app on Fly
# (platform/hosted/internal/fly.toml). Runs as root on the group's machine:
#   internal-fly-init kms
#   internal-fly-init <kind> <upstream origin> internal|ISRG_Root_X1|ISRG_Root_X2
# generates the group's tokens, the kms seal secret and the event source's
# producer file, enrollment key and signed empty enrollment snapshot on the
# volume on first boot, waits until
# tools/bringup/ca.sh issue internal-<group> has put the TLS identity on the
# volume, writes the upstream root (the internal CA, or the named ISRG root of
# the image's trust store) as DER, hands every file to uid 4020 and starts the
# service under that uid on [::]:9443.
set -eu
umask 077
usage() {
	echo "usage: internal-fly-init kms | journeys|payments|approvals|programs <upstream origin> internal|ISRG_Root_X1|ISRG_Root_X2" >&2
	exit 2
}
[ "$#" -ge 1 ] || usage
group="$1"
tls_dir="/data/tls/internal-$group"
state_dir=/data/state
run_dir=/data/run
mkdir -p "$state_dir" "$run_dir"

# fresh <file>: writes 32 random bytes as hex to the file unless it holds one.
fresh() {
	[ -s "$1" ] || { openssl rand -hex 32 >"$1.new" && mv "$1.new" "$1"; }
}

case "$group" in
kms)
	[ "$#" -eq 1 ] || usage
	fresh "$run_dir/token"
	fresh "$run_dir/seal-secret"
	;;
journeys | approvals | payments | programs)
	[ "$#" -eq 3 ] || usage
	case "$3" in
	internal | ISRG_Root_X1 | ISRG_Root_X2) ;;
	*) usage ;;
	esac
	fresh "$run_dir/token"
	fresh "$run_dir/producer-token"
	fresh "$run_dir/enrollment-key"
	# The credential file is the versioned enrollment snapshot the source
	# re-reads while it runs; start from the signed empty generation 0 and
	# replace a pre-versioned empty map.
	if [ ! -s "$run_dir/credentials.json" ] || [ "$(tr -d ' \n' <"$run_dir/credentials.json")" = '{}' ]; then
		mac=$(printf 'layerx-enrollment-v1\n%s\n0\n' "$group" | openssl dgst -sha256 -mac HMAC -macopt "key:$(cat "$run_dir/enrollment-key")" -r | cut -d' ' -f1)
		printf '{"version":1,"generation":0,"principals":[],"mac":"%s"}\n' "$mac" >"$run_dir/credentials.json.new"
		mv "$run_dir/credentials.json.new" "$run_dir/credentials.json"
	fi
	# The human service produces journeys and approvals; the router and the
	# registry produce payments and programs with a principal digest.
	allow_digest=true
	case "$group" in
	journeys | approvals) allow_digest=false ;;
	esac
	printf '[{"token_file":"%s","allow_principal_digest":%s}]\n' "$run_dir/producer-token" "$allow_digest" >"$run_dir/producers.json"
	;;
*) usage ;;
esac

if [ ! -s "$tls_dir/cert.der" ] || [ ! -s "$tls_dir/key.der" ] || [ ! -s "$tls_dir/ca.der" ]; then
	echo "internal-fly-init: waiting for $tls_dir from tools/bringup/ca.sh issue internal-$group"
	until [ -s "$tls_dir/cert.der" ] && [ -s "$tls_dir/key.der" ] && [ -s "$tls_dir/ca.der" ]; do
		sleep 5
	done
fi

if [ "$group" = kms ]; then
	export LAYERX_KMS_LISTEN="[::]:9443"
	export LAYERX_KMS_STATE_DIR="$state_dir"
	export LAYERX_KMS_TOKEN_FILE="$run_dir/token"
	export LAYERX_KMS_SEAL_SECRET_FILE="$run_dir/seal-secret"
	export LAYERX_KMS_TLS_CERT_DER="$tls_dir/cert.der"
	export LAYERX_KMS_TLS_KEY_DER="$tls_dir/key.der"
	export LAYERX_KMS_CLIENT_CA_DER="$tls_dir/ca.der"
	binary=/usr/local/bin/layerx-kms
else
	if [ "$3" = internal ]; then
		cp "$tls_dir/ca.der" "$run_dir/upstream-ca.der.new"
	else
		openssl x509 -in "/usr/share/ca-certificates/mozilla/$3.crt" -outform DER -out "$run_dir/upstream-ca.der.new"
	fi
	mv "$run_dir/upstream-ca.der.new" "$run_dir/upstream-ca.der"
	export LAYERX_EVENTS_LISTEN="[::]:9443"
	export LAYERX_EVENTS_KIND="$group"
	export LAYERX_EVENTS_STATE_DIR="$state_dir"
	export LAYERX_EVENTS_TOKEN_FILE="$run_dir/token"
	export LAYERX_EVENTS_TLS_CERT_DER="$tls_dir/cert.der"
	export LAYERX_EVENTS_TLS_KEY_DER="$tls_dir/key.der"
	export LAYERX_EVENTS_CLIENT_CA_DER="$tls_dir/ca.der"
	export LAYERX_EVENTS_UPSTREAM_URL="$2"
	export LAYERX_EVENTS_UPSTREAM_CA_DER="$run_dir/upstream-ca.der"
	export LAYERX_EVENTS_PRODUCERS_FILE="$run_dir/producers.json"
	export LAYERX_EVENTS_CREDENTIALS_FILE="$run_dir/credentials.json"
	export LAYERX_EVENTS_ENROLLMENT_KEY_FILE="$run_dir/enrollment-key"
	binary=/usr/local/bin/layerx-event-source
fi

chown -R 4020:4020 "$state_dir" "$run_dir" "$tls_dir"
# ca.sh makes the certificate root under umask 077; uid 4020 only traverses it.
chmod 0711 "$(dirname "$tls_dir")"
exec setpriv --reuid=4020 --regid=4020 --clear-groups --no-new-privs "$binary"
