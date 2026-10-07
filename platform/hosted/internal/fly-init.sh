#!/bin/sh
# Init of one role of the internal service image; each role is its own
# service with its own volume at /data, selected by LAYERX_ROLE:
#   LAYERX_ROLE=kms
#   LAYERX_ROLE=journeys|payments|approvals|programs with
#     LAYERX_INTERNAL_UPSTREAM_URL (upstream origin) and
#     LAYERX_INTERNAL_UPSTREAM_ROOT (internal|ISRG_Root_X1|ISRG_Root_X2)
# writes the role's tokens and the kms seal secret from the INTERNAL_<ROLE>_*
# variables of tools/bringup/mint-secrets.sh (base64) when they are set and
# generates them on the volume only when unset, writes the event source's
# producer file, enrollment key and signed empty enrollment snapshot on first
# boot, waits until tools/bringup/ca.sh issue internal-<role> has put the TLS
# identity on the volume, writes the upstream root (the internal CA, or the
# named ISRG root of the image's trust store) as DER, hands every file to uid
# 4020 and starts the service under that uid on [::]:9443 with a plain
# GET /healthz listener on LAYERX_HEALTH_ADDR (default [::]:8080).
set -eu
umask 077
usage() {
	echo "usage: LAYERX_ROLE=kms | LAYERX_ROLE=journeys|payments|approvals|programs LAYERX_INTERNAL_UPSTREAM_URL=<upstream origin> LAYERX_INTERNAL_UPSTREAM_ROOT=internal|ISRG_Root_X1|ISRG_Root_X2 internal-env-init" >&2
	exit 2
}
[ "$#" -eq 0 ] || usage
group="${LAYERX_ROLE:-}"
[ -n "$group" ] || usage
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
			echo "internal-env-init: invalid retained material $1" >&2
			exit 1
		}
		[ "$(stat -c %h "$1")" = 1 ] || exit 1
		case "$(stat -c %u "$1")" in 0 | 4020) ;; *) exit 1 ;; esac
		[ "$(stat -c %a "$1")" = 600 ] || exit 1
		return
	fi
	if [ "$retained" = true ]; then
		echo "internal-env-init: retained material missing $1" >&2
		exit 1
	fi
	temporary=$(mktemp "$run_dir/.secret.XXXXXX")
	openssl rand -hex 32 > "$temporary"
	chmod 0600 "$temporary"
	ln "$temporary" "$1"
	rm "$temporary"
}

# provide <path> <variable>: the variable's base64 value when set, otherwise
# the retained or freshly generated value. A set seal secret never replaces a
# different retained one, which would orphan the sealed keys.
provide() {
	eval "encoded=\${$2:-}"
	if [ -z "$encoded" ]; then
		fresh "$1"
		return
	fi
	temporary=$(mktemp "$run_dir/.secret.XXXXXX")
	printf '%s' "$encoded" | base64 -d >"$temporary" 2>/dev/null || {
		rm -f "$temporary"
		echo "internal-env-init: $2 is not base64" >&2
		exit 1
	}
	[ -s "$temporary" ] || {
		rm -f "$temporary"
		echo "internal-env-init: $2 is empty" >&2
		exit 1
	}
	if [ -e "$1" ] || [ -L "$1" ]; then
		[ -f "$1" ] && [ ! -L "$1" ] || {
			rm -f "$temporary"
			echo "internal-env-init: invalid retained material $1" >&2
			exit 1
		}
		if [ "$2" = INTERNAL_KMS_SEAL_SECRET ] && ! cmp -s "$temporary" "$1"; then
			rm -f "$temporary"
			echo "internal-env-init: $2 differs from the retained seal secret" >&2
			exit 1
		fi
	elif [ "$2" = INTERNAL_KMS_SEAL_SECRET ] && [ "$retained" = true ]; then
		rm -f "$temporary"
		echo "internal-env-init: retained material missing $1" >&2
		exit 1
	fi
	chmod 0600 "$temporary"
	mv -f "$temporary" "$1"
	unset "$2"
}

case "$group" in
kms)
	provide "$run_dir/token" INTERNAL_KMS_TOKEN
	provide "$run_dir/seal-secret" INTERNAL_KMS_SEAL_SECRET
	;;
journeys | approvals | payments | programs)
	upstream_url="${LAYERX_INTERNAL_UPSTREAM_URL:-}"
	upstream_root="${LAYERX_INTERNAL_UPSTREAM_ROOT:-}"
	[ -n "$upstream_url" ] || usage
	case "$upstream_root" in
	internal | ISRG_Root_X1 | ISRG_Root_X2) ;;
	*) usage ;;
	esac
	variable=INTERNAL_$(printf '%s' "$group" | tr a-z A-Z)
	provide "$run_dir/token" "${variable}_TOKEN"
	provide "$run_dir/producer-token" "${variable}_PRODUCER_TOKEN"
	fresh "$run_dir/enrollment-key"
	if [ -L "$run_dir/credentials.json" ]; then
		echo "internal-env-init: enrollment snapshot is a symbolic link" >&2
		exit 1
	fi
	if [ ! -e "$run_dir/credentials.json" ]; then
		[ "$retained" = false ] || {
			echo "internal-env-init: retained enrollment snapshot is missing" >&2
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
	echo "internal-env-init: waiting for $tls_dir from tools/bringup/ca.sh issue internal-$group"
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
	if [ "$upstream_root" = internal ]; then
		cp "$tls_dir/ca.der" "$run_dir/upstream-ca.der.new"
	else
		openssl x509 -in "/usr/share/ca-certificates/mozilla/$upstream_root.crt" -outform DER -out "$run_dir/upstream-ca.der.new"
	fi
	mv "$run_dir/upstream-ca.der.new" "$run_dir/upstream-ca.der"
	export LAYERX_EVENTS_LISTEN="[::]:9443"
	export LAYERX_EVENTS_KIND="$group"
	export LAYERX_EVENTS_STATE_DIR="$state_dir"
	export LAYERX_EVENTS_TOKEN_FILE="$run_dir/token"
	export LAYERX_EVENTS_TLS_CERT_DER="$tls_dir/cert.der"
	export LAYERX_EVENTS_TLS_KEY_DER="$tls_dir/key.der"
	export LAYERX_EVENTS_CLIENT_CA_DER="$tls_dir/ca.der"
	export LAYERX_EVENTS_UPSTREAM_URL="$upstream_url"
	export LAYERX_EVENTS_UPSTREAM_CA_DER="$run_dir/upstream-ca.der"
	export LAYERX_EVENTS_PRODUCERS_FILE="$run_dir/producers.json"
	export LAYERX_EVENTS_CREDENTIALS_FILE="$run_dir/credentials.json"
	export LAYERX_EVENTS_ENROLLMENT_KEY_FILE="$run_dir/enrollment-key"
	binary=/usr/local/bin/layerx-event-source
fi
export LAYERX_HEALTH_ADDR="${LAYERX_HEALTH_ADDR:-[::]:8080}"

chown -R 4020:4020 "$state_dir" "$run_dir" "$tls_dir"
# ca.sh makes the certificate root under umask 077; uid 4020 only traverses it.
chmod 0711 "$(dirname "$tls_dir")"
exec setpriv --reuid=4020 --regid=4020 --clear-groups --no-new-privs "$binary"
