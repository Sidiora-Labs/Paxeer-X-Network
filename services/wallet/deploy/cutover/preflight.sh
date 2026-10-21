#!/usr/bin/env bash
# Preflight for the wallet endpoint cutover, run on the current host before
# apply.sh. Every value comes from the environment; none has a default.
#
#   CUTOVER_GATEWAY_HOST   platform hostname of the new gateway
#   CUTOVER_PUBLIC_HOST    public wallet endpoint hostname the proxy serves
#   CUTOVER_COMPOSE_DIR    directory of the old service's compose project
#   CUTOVER_PROXY_SERVICE  compose service name of the proxy
#
# Checks, each printed as "pass <check> ..." or "fail <check> ...":
#   gateway_ready     GET https://<gateway>/readyz answers 200, ready true and
#                     x-served-by: paxeer-wallet-gateway
#   proxy_to_gateway  from inside the proxy container, the same route answers
#                     200 with the gateway's header, so the proxy can reach
#                     the gateway over TLS
#   public_path       GET https://<public>/healthz answers 200 through the
#                     live proxy; the line names who serves it today
# Exits 0 when every check passes, 1 when one fails, 2 on a usage error.
set -euo pipefail

SCRIPT=preflight
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$here/lib.sh"

require_tools curl python3 docker
require_host CUTOVER_GATEWAY_HOST CUTOVER_PUBLIC_HOST
require_var CUTOVER_COMPOSE_DIR CUTOVER_PROXY_SERVICE
[ -d "$CUTOVER_COMPOSE_DIR" ] || die "CUTOVER_COMPOSE_DIR does not name a directory"

failures=0

line=""
if line="$(probe "https://$CUTOVER_GATEWAY_HOST/readyz")" && [ "$line" = "200 $SERVED_BY true" ]; then
	echo "pass gateway_ready $line"
else
	echo "fail gateway_ready $line"
	failures=$((failures + 1))
fi

status=0
out="$(compose exec -T "$CUTOVER_PROXY_SERVICE" wget -S -q -O /dev/null -T 20 \
	--header "X-Forwarded-Host: $CUTOVER_PUBLIC_HOST" \
	"https://$CUTOVER_GATEWAY_HOST/readyz" 2>&1)" || status=$?
code="$(printf '%s\n' "$out" | sed -n 's/^ *HTTP\/[0-9.]* \([0-9][0-9][0-9]\).*/\1/p' | tail -n 1)"
served="$(printf '%s\n' "$out" | tr -d '\r' | grep -i '^ *x-served-by:' | tail -n 1 | sed 's/^[^:]*: *//')" || served=""
if [ "$status" -eq 0 ] && [ "$code" = 200 ] && [ "$served" = "$SERVED_BY" ]; then
	echo "pass proxy_to_gateway http=$code x-served-by=$served"
else
	echo "fail proxy_to_gateway exit=$status http=${code:-none} x-served-by=${served:-none} $(printf '%s' "$out" | tr '\n' ' ' | cut -c1-200)"
	failures=$((failures + 1))
fi

if line="$(probe "https://$CUTOVER_PUBLIC_HOST/healthz")" && [ "${line%% *}" = 200 ]; then
	echo "pass public_path $line"
else
	echo "fail public_path $line"
	failures=$((failures + 1))
fi

if [ "$failures" -ne 0 ]; then
	echo "preflight: $failures check(s) failed"
	exit 1
fi
echo "preflight: all checks passed"
