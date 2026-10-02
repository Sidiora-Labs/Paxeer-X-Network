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
chmod 0700 "$state_dir" "$run_dir"

retained=false
if [ -n "$(ls -A "$state_dir")" ] || [ -e "$run_dir/credentials.json" ]; then
	retained=true
fi
fresh() {
	if [ -e "$1" ] || [ -L "$1" ]; then
		[ -f "$1" ] && [ ! -L "$1" ] && [ -s "$1" ] || {
			echo "internal-fly-init: invalid retained material $1" >&2
			exit 1
		}
		[ "$(stat -c %h "$1")" = 1 ] || exit 1
		case "$(stat -c %u "$1")" in 0 | 4020) ;; *) exit 1 ;; esac
		[ "$(stat -c %a "$1")" = 600 ] || exit 1
		return
	fi
	if [ "$retained" = true ]; then
		echo "internal-fly-init: retained material missing $1" >&2
		exit 1
	fi
	temporary=$(mktemp "$run_dir/.secret.XXXXXX")
	openssl rand -hex 32 > "$temporary"
	chmod 0600 "$temporary"
	ln "$temporary" "$1"
	rm "$temporary"
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
	if [ -L "$run_dir/credentials.json" ]; then
		echo "internal-fly-init: enrollment snapshot is a symbolic link" >&2
		exit 1
	fi
	if [ ! -e "$run_dir/credentials.json" ]; then
		[ "$retained" = false ] || {
			echo "internal-fly-init: retained enrollment snapshot is missing" >&2
			exit 1
		}
		chown 4020:4020 "$run_dir" "$run_dir/enrollment-key"
		mac=$(LAYERX_EVENTS_KIND="$group" LAYERX_EVENTS_ENROLLMENT_KEY_FILE="$run_dir/enrollment-key" setpriv --reuid=4020 --regid=4020 --clear-groups --no-new-privs /usr/local/bin/layerx-event-source --empty-enrollment-mac)
		temporary=$(mktemp "$run_dir/.enrollment.XXXXXX")
		printf '{"version":1,"generation":0,"principals":[],"mac":"%s"}\n' "$mac" > "$temporary"
		chmod 0600 "$temporary"
		ln "$temporary" "$run_dir/credentials.json"
		rm "$temporary"
	else
		[ -f "$run_dir/credentials.json" ] && [ -s "$run_dir/credentials.json" ] || exit 1
		[ "$(stat -c %h "$run_dir/credentials.json")" = 1 ] || exit 1
		[ "$(stat -c %a "$run_dir/credentials.json")" = 600 ] || exit 1
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
