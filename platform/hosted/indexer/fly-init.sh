#!/bin/sh
# Init of the indexer app on Fly (platform/hosted/indexer/fly.toml). Runs as
# root on the app's one machine, or on the one-off backfill machine on the
# same volume:
#   indexer-fly-init backfill
#   indexer-fly-init serve
# Both read LAYERX_INDEXER_START_BLOCK, the archive node's first retained
# block, from the CometBFT location of LAYERX_INDEXER_COMET_URL once and keep
# it on the volume. backfill reads the archive node's head from
# LAYERX_INDEXER_EVM_URL once as the cutover, keeps it on the volume and runs
# layerx-indexer backfill up to it; an interrupted backfill resumes to the
# same cutover. serve waits until the backfill has finished and until
# tools/bringup/ca.sh issue indexer has put the TLS identity on the volume,
# then starts the indexer, which resumes live at the cutover plus one. The
# indexer runs under uid 4020.
set -eu
umask 077
usage() {
	echo "usage: indexer-fly-init backfill | serve" >&2
	exit 2
}
[ "$#" -eq 1 ] || usage
case "$1" in
backfill | serve) ;;
*) usage ;;
esac
state_dir="$(dirname "$LAYERX_INDEXER_DB")"
tls_dir="$(dirname "$LAYERX_INDEXER_TLS_CERT_DER")"
mkdir -p "$state_dir"

# post <url> <ca der> <json-rpc body>: the answer of one JSON-RPC POST over
# TLS verified against the one pinned root.
post() {
	openssl x509 -inform DER -in "$2" -out "$state_dir/pin.pem.new"
	curl -fsS -m 30 --cacert "$state_dir/pin.pem.new" -H 'content-type: application/json' --data "$3" "$1"
}

# once <file> <command...>: the decimal the command prints, kept in the file
# the first time and read from it afterwards.
once() {
	file="$1"
	shift
	if [ ! -s "$file" ]; then
		value="$("$@")"
		case "$value" in
		'' | *[!0-9]*)
			echo "indexer-fly-init: $file: the node answered no height" >&2
			exit 1
			;;
		esac
		printf '%s\n' "$value" >"$file.new"
		mv "$file.new" "$file"
	fi
	cat "$file"
}

earliest() {
	post "$LAYERX_INDEXER_COMET_URL" "$LAYERX_INDEXER_COMET_CA_DER" '{"jsonrpc":"2.0","id":1,"method":"status","params":{}}' |
		jq -r '(.result // .) | .sync_info.earliest_block_height'
}

head_height() {
	hex="$(post "$LAYERX_INDEXER_EVM_URL" "$LAYERX_INDEXER_EVM_CA_DER" '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' | jq -r .result)"
	case "$hex" in
	0x[0-9a-fA-F]*) echo "$((hex))" ;;
	esac
}

LAYERX_INDEXER_START_BLOCK="$(once "$state_dir/start-block" earliest)"
export LAYERX_INDEXER_START_BLOCK
rm -f "$state_dir/pin.pem.new"

if [ "$1" = backfill ]; then
	cutover="$(once "$state_dir/cutover-height" head_height)"
	rm -f "$state_dir/pin.pem.new"
	chown -R 4020:4020 "$state_dir"
	setpriv --reuid=4020 --regid=4020 --clear-groups --no-new-privs /usr/local/bin/layerx-indexer backfill --cutover-height "$cutover"
	: >"$state_dir/backfill-done"
	echo "indexer-fly-init: the backfill reached the cutover $cutover"
	exit 0
fi

if [ ! -e "$state_dir/backfill-done" ]; then
	echo "indexer-fly-init: waiting for indexer-fly-init backfill to reach its cutover on this volume"
	until [ -e "$state_dir/backfill-done" ]; do
		sleep 5
	done
fi
if [ ! -s "$LAYERX_INDEXER_TLS_CERT_DER" ] || [ ! -s "$LAYERX_INDEXER_TLS_KEY_DER" ]; then
	echo "indexer-fly-init: waiting for $tls_dir from tools/bringup/ca.sh issue indexer"
	until [ -s "$LAYERX_INDEXER_TLS_CERT_DER" ] && [ -s "$LAYERX_INDEXER_TLS_KEY_DER" ]; do
		sleep 5
	done
fi
chown -R 4020:4020 "$state_dir" "$tls_dir"
# ca.sh makes the certificate root under umask 077; uid 4020 only traverses it.
chmod 0711 "$(dirname "$tls_dir")"
exec setpriv --reuid=4020 --regid=4020 --clear-groups --no-new-privs /usr/local/bin/layerx-indexer
