#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'EOF'
usage: tools/bringup/edge-sync.sh [--dry-run] <env-file>

Runs on the primary edge. Registers the served set of <env-file> through
edge.sh set, obtains or renews the certbot certificate of every HTTP and RPC
name in the manifest (DNS-01 through the registrar API when EDGE_DNS_API_KEY is
set, HTTP-01 through the ACME webroot otherwise; --keep-until-expiring, so a
certificate that is not due is left alone), renders again so the names with a
new certificate get their TLS server, then copies the certificates, the
rendered sites, the edge state and the rpc-health cron entry to every
secondary edge in EDGE_SECONDARIES and re-renders and reloads nginx there when
anything changed. Each secondary keeps its own rpc-health view of the pool.
Running it again with nothing due changes nothing. --dry-run prints the
commands that change something instead of running them.

Env file (besides the EDGE_APP_* and EDGE_RPC_* inputs of edge.sh set):
  EDGE_SECONDARIES   space-separated ssh destinations of the secondary edges
  EDGE_ACME_EMAIL    ACME account email; unset registers without one
  EDGE_DNS_API_KEY   registrar API token; set selects DNS-01
  EDGE_DNS_API       registrar DNS zone API, default
                     https://developers.hostinger.com/api/dns/v1/zones
  EDGE_DNS_WAIT      seconds to wait for the TXT record, default 60
EDGE_NGINX_DIR, EDGE_WEBROOT, EDGE_CERT_DIR and EDGE_CRON_DIR are read as
edge.sh reads them and must name the same paths on every edge.

Exits 1 when a step fails, 2 on a usage error.
EOF
}

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
nginx_dir="${EDGE_NGINX_DIR:-/etc/nginx}"
webroot="${EDGE_WEBROOT:-/var/www/certbot}"
cert_dir="${EDGE_CERT_DIR:-/etc/letsencrypt/live}"
cron_dir="${EDGE_CRON_DIR:-/etc/cron.d}"
le_dir="$(dirname "$cert_dir")"
state="$nginx_dir/edge"
marker="# rendered by tools/bringup/edge.sh; edit the manifest through it, not this file"
dry=""

die() {
	echo "edge-sync: $*" >&2
	exit 1
}

# run: the command, or its quoted line under --dry-run.
run() {
	if [ -n "$dry" ]; then
		printf 'dry-run:'
		printf ' %q' "$@"
		echo
	else
		"$@"
	fi
}

# dns_hook auth|cleanup <env-file>: certbot's manual hook; writes or deletes the
# _acme-challenge TXT record of CERTBOT_DOMAIN in its registrable zone.
dns_hook() {
	local env="$2" zone rel body
	# shellcheck disable=SC1090
	. "$env"
	[ -n "${EDGE_DNS_API_KEY:-}" ] || die "EDGE_DNS_API_KEY is not set"
	zone="$(awk -F. '{print $(NF-1) "." $NF}' <<<"$CERTBOT_DOMAIN")"
	rel="_acme-challenge"
	[ "$CERTBOT_DOMAIN" = "$zone" ] || rel="_acme-challenge.${CERTBOT_DOMAIN%."$zone"}"
	if [ "$1" = auth ]; then
		body="$(jq -cn --arg n "$rel" --arg v "$CERTBOT_VALIDATION" \
			'{overwrite: false, zone: [{name: $n, type: "TXT", ttl: 60, records: [{content: $v}]}]}')"
		curl -fsS -m 30 -X PUT -H "Authorization: Bearer $EDGE_DNS_API_KEY" -H 'content-type: application/json' \
			--data "$body" "${EDGE_DNS_API:-https://developers.hostinger.com/api/dns/v1/zones}/$zone" >/dev/null
		sleep "${EDGE_DNS_WAIT:-60}"
	else
		body="$(jq -cn --arg n "$rel" '{filters: [{name: $n, type: "TXT"}]}')"
		curl -fsS -m 30 -X DELETE -H "Authorization: Bearer $EDGE_DNS_API_KEY" -H 'content-type: application/json' \
			--data "$body" "${EDGE_DNS_API:-https://developers.hostinger.com/api/dns/v1/zones}/$zone" >/dev/null
	fi
}

case "${1:-}" in
dns-auth | dns-cleanup)
	[ "$#" -eq 2 ] || {
		usage >&2
		exit 2
	}
	dns_hook "${1#dns-}" "$2"
	exit 0
	;;
-h | --help)
	usage
	exit 0
	;;
--dry-run)
	dry=1
	shift
	;;
esac
[ "$#" -eq 1 ] || {
	usage >&2
	exit 2
}
env="$(realpath -- "$1")"
[ -r "$env" ] || die "env file unreadable: $1"
# shellcheck disable=SC1090
. "$env"

edge() {
	EDGE_LOCAL=1 "$here/edge.sh" "$@"
}

if [ -n "$dry" ]; then
	run env EDGE_LOCAL=1 "$here/edge.sh" set "$env"
else
	edge set "$env"
fi
manifest="$state/manifest"
[ -s "$manifest" ] || die "no manifest at $manifest after edge.sh set"

renewed=0
while read -r name mode _; do
	[ "$mode" = http ] || [ "$mode" = rpc ] || continue
	args=(certonly --non-interactive --agree-tos --keep-until-expiring --cert-name "$name" -d "$name"
		--deploy-hook "touch $state/renewed")
	if [ -n "${EDGE_ACME_EMAIL:-}" ]; then
		args+=(-m "$EDGE_ACME_EMAIL")
	else
		args+=(--register-unsafely-without-email)
	fi
	if [ -n "${EDGE_DNS_API_KEY:-}" ]; then
		args+=(--manual --preferred-challenges dns
			--manual-auth-hook "$(printf '%q dns-auth %q' "$here/edge-sync.sh" "$env")"
			--manual-cleanup-hook "$(printf '%q dns-cleanup %q' "$here/edge-sync.sh" "$env")")
	else
		args+=(--webroot -w "$webroot")
	fi
	run certbot "${args[@]}" || die "certbot could not obtain a certificate for $name"
done <"$manifest"
if [ -e "$state/renewed" ]; then
	renewed=1
	run rm -f -- "$state/renewed"
fi
if [ -n "$dry" ]; then
	run env EDGE_LOCAL=1 "$here/edge.sh" rerender
else
	edge rerender
fi

sites=()
for f in "$nginx_dir"/sites-available/* "$nginx_dir"/sites-enabled/*; do
	[ -e "$f" ] || [ -L "$f" ] || continue
	head -n 1 "$f" 2>/dev/null | grep -qxF "$marker" && sites+=("$f")
done
extra=()
[ ! -e "$nginx_dir/modules-enabled/99-edge-stream.conf" ] || extra+=("$nginx_dir/modules-enabled/99-edge-stream.conf")
[ ! -e "$cron_dir/edge-rpc-health" ] || extra+=("$cron_dir/edge-rpc-health")

for dest in ${EDGE_SECONDARIES:-}; do
	changes="$(rsync -aR --delete --itemize-changes ${dry:+--dry-run} -e 'ssh -o BatchMode=yes' \
		--exclude "$state/rpc-down" --exclude "$state/renewed" --exclude "$state/rpc-health.lock" \
		"$le_dir/" "$state/" "${sites[@]}" "${extra[@]}" "$dest:/")" ||
		die "rsync to $dest failed"
	if [ -z "$changes" ] && [ "$renewed" -eq 0 ]; then
		echo "unchanged $dest"
		continue
	fi
	run ssh -o BatchMode=yes -- "$dest" "EDGE_LOCAL=1 EDGE_NGINX_DIR=$(printf '%q' "$nginx_dir") EDGE_CERT_DIR=$(printf '%q' "$cert_dir") $(printf '%q' "$state/edge.sh") rerender" ||
		die "rerender on $dest failed"
	echo "synced $dest"
done
