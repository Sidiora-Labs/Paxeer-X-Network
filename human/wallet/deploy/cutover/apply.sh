#!/usr/bin/env bash
# Point the wallet endpoint at the new gateway through the current host's
# proxy. Run on the current host as the operator. Idempotent: a second run
# with the same values changes nothing and only verifies.
#
#   CUTOVER_GATEWAY_HOST            platform hostname of the new gateway
#   CUTOVER_PUBLIC_HOST             public wallet endpoint hostname
#   CUTOVER_COMPOSE_DIR             directory of the old service's compose project
#   CUTOVER_PROXY_SERVICE           compose service name of the proxy
#   CUTOVER_CADDYFILE               the proxy's Caddyfile on the host, absolute
#                                   or relative to CUTOVER_COMPOSE_DIR
#   CUTOVER_CADDYFILE_IN_CONTAINER  the same file's path inside the proxy container
#   CUTOVER_SITE_LABEL              the wallet site block's address line exactly
#                                   as written in the Caddyfile
#   CUTOVER_STATE_DIR               directory for backups and the applied-at record
#
# Steps: run preflight.sh; render wallet-endpoint.caddy for the gateway;
# replace only the reverse_proxy directive of the wallet site block (every
# other site and directive is copied unchanged); back the previous Caddyfile
# up into the state directory; validate the result inside the proxy
# container; write it in place (the file is bind-mounted, so its inode must
# not change); reload the proxy; verify through this host's proxy that the
# public hostname answers 200 with the gateway's x-served-by header. A failed
# validation leaves the file untouched; a failed reload or verification
# restores the backup and reloads. Exits 0 on success, 1 on a failed step,
# 2 on a usage error.
set -euo pipefail

SCRIPT=apply
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$here/lib.sh"

require_tools curl python3 docker
require_host CUTOVER_GATEWAY_HOST CUTOVER_PUBLIC_HOST
require_var CUTOVER_COMPOSE_DIR CUTOVER_PROXY_SERVICE CUTOVER_CADDYFILE \
	CUTOVER_CADDYFILE_IN_CONTAINER CUTOVER_SITE_LABEL CUTOVER_STATE_DIR
[ -d "$CUTOVER_COMPOSE_DIR" ] || die "CUTOVER_COMPOSE_DIR does not name a directory"
case "$CUTOVER_CADDYFILE" in
/*) caddyfile="$CUTOVER_CADDYFILE" ;;
*) caddyfile="$CUTOVER_COMPOSE_DIR/$CUTOVER_CADDYFILE" ;;
esac
if [ ! -f "$caddyfile" ] || [ ! -w "$caddyfile" ]; then
	die "CUTOVER_CADDYFILE does not name a writable file"
fi
mkdir -p "$CUTOVER_STATE_DIR"
chmod 700 "$CUTOVER_STATE_DIR"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

echo "apply: preflight"
"$here/preflight.sh" || {
	echo "apply: preflight failed; nothing changed" >&2
	exit 1
}

sed "s/__CUTOVER_GATEWAY_HOST__/$CUTOVER_GATEWAY_HOST/g" "$here/wallet-endpoint.caddy" >"$work/directive"
status=0
python3 "$here/rewrite-caddyfile.py" "$caddyfile" "$CUTOVER_SITE_LABEL" "$work/directive" "$work/Caddyfile" || status=$?
case "$status" in
0) changed=1 ;;
3) changed=0 ;;
*)
	echo "apply: the Caddyfile could not be rewritten; nothing changed" >&2
	exit 1
	;;
esac

reload() {
	compose exec -T "$CUTOVER_PROXY_SERVICE" caddy reload --adapter caddyfile --config "$CUTOVER_CADDYFILE_IN_CONTAINER"
}

verify() {
	local attempt line
	for attempt in 1 2 3 4 5 6; do
		if line="$(served_by_gateway "https://$CUTOVER_PUBLIC_HOST/readyz" --resolve "$CUTOVER_PUBLIC_HOST:443:127.0.0.1")"; then
			echo "pass proxied_path $line"
			return 0
		fi
		echo "wait proxied_path attempt=$attempt $line"
		sleep 5
	done
	echo "fail proxied_path $line"
	return 1
}

backup=""
if [ "$changed" -eq 1 ]; then
	stamp="$(date -u +%Y%m%dT%H%M%SZ)"
	backup="$CUTOVER_STATE_DIR/Caddyfile.$stamp"
	cp -p "$caddyfile" "$backup"
	if [ ! -e "$CUTOVER_STATE_DIR/Caddyfile.pre-cutover" ]; then
		cp -p "$caddyfile" "$CUTOVER_STATE_DIR/Caddyfile.pre-cutover"
	fi
	echo "apply: backed up the previous Caddyfile to $backup"

	candidate=/tmp/Caddyfile.cutover-candidate
	compose exec -T "$CUTOVER_PROXY_SERVICE" sh -c "cat > $candidate" <"$work/Caddyfile"
	if ! compose exec -T "$CUTOVER_PROXY_SERVICE" caddy validate --adapter caddyfile --config "$candidate"; then
		compose exec -T "$CUTOVER_PROXY_SERVICE" rm -f "$candidate" || true
		echo "apply: the rewritten Caddyfile failed validation; nothing changed" >&2
		exit 1
	fi
	compose exec -T "$CUTOVER_PROXY_SERVICE" rm -f "$candidate" || true

	cat "$work/Caddyfile" >"$caddyfile"
	if ! reload || ! verify; then
		echo "apply: restoring the previous Caddyfile from $backup" >&2
		cat "$backup" >"$caddyfile"
		reload || echo "apply: the reload after the restore failed; reload the proxy by hand" >&2
		exit 1
	fi
	printf '%s %s\n' "$(date -u +%s)" "$CUTOVER_GATEWAY_HOST" >"$CUTOVER_STATE_DIR/applied-at"
else
	echo "apply: the wallet site block already points at the gateway; no change"
	verify || exit 1
	if [ ! -s "$CUTOVER_STATE_DIR/applied-at" ]; then
		printf '%s %s\n' "$(date -u +%s)" "$CUTOVER_GATEWAY_HOST" >"$CUTOVER_STATE_DIR/applied-at"
	fi
fi

echo "apply: the public hostname is served by the gateway through the proxy"
echo "apply: rollback: cat $CUTOVER_STATE_DIR/Caddyfile.pre-cutover > $caddyfile, then reload the proxy"
