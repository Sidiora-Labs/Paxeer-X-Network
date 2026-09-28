#!/usr/bin/env bash
# Stop and disable the old wallet service once the proxied path has served
# through the new gateway for the soak period. Run on the current host as the
# operator after apply.sh.
#
#   CUTOVER_PUBLIC_HOST      public wallet endpoint hostname
#   CUTOVER_COMPOSE_DIR      directory of the old service's compose project
#   CUTOVER_RETIRE_SERVICES  comma-separated compose services to retire; every
#                            other service of the project keeps running
#   CUTOVER_PROXY_SERVICE    compose service name of the proxy, never retired
#   CUTOVER_STATE_DIR        the state directory apply.sh wrote
#   CUTOVER_SOAK_SECONDS     seconds the proxied path must have served since
#                            apply.sh changed the proxy
#
# Refuses unless apply.sh recorded its change at least CUTOVER_SOAK_SECONDS
# ago and the public hostname answers 200 with the gateway's x-served-by
# header on /healthz and /readyz. For each retired service it clears the
# restart policy of its containers (docker update --restart=no) so neither a
# daemon restart nor a reboot brings it back, stops it through docker compose,
# and checks it is stopped with policy no. It then checks every other service
# that was running still runs and the public hostname is still served by the
# gateway. Exits 0 on success, 1 on a refusal or failed check, 2 on a usage
# error.
set -euo pipefail

SCRIPT=retire
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
. "$here/lib.sh"

require_tools curl python3 docker
require_host CUTOVER_PUBLIC_HOST
require_var CUTOVER_COMPOSE_DIR CUTOVER_RETIRE_SERVICES CUTOVER_PROXY_SERVICE \
	CUTOVER_STATE_DIR CUTOVER_SOAK_SECONDS
[ -d "$CUTOVER_COMPOSE_DIR" ] || die "CUTOVER_COMPOSE_DIR does not name a directory"
case "$CUTOVER_SOAK_SECONDS" in
'' | *[!0-9]*) die "CUTOVER_SOAK_SECONDS must be a positive whole number of seconds" ;;
esac
[ "$CUTOVER_SOAK_SECONDS" -gt 0 ] || die "CUTOVER_SOAK_SECONDS must be a positive whole number of seconds"

IFS=',' read -r -a services <<<"$CUTOVER_RETIRE_SERVICES"
[ "${#services[@]}" -gt 0 ] || die "CUTOVER_RETIRE_SERVICES names no service"
known="$(compose config --services)"
for service in "${services[@]}"; do
	[ -n "$service" ] || die "CUTOVER_RETIRE_SERVICES holds an empty entry"
	[ "$service" != "$CUTOVER_PROXY_SERVICE" ] || die "the proxy service $service serves the cut-over path and is never retired"
	grep -qxF -- "$service" <<<"$known" || die "service $service is not part of the compose project"
done

record="$CUTOVER_STATE_DIR/applied-at"
[ -s "$record" ] || {
	echo "retire: $record is missing; run apply.sh first" >&2
	exit 1
}
read -r applied_at _ <"$record"
case "$applied_at" in
'' | *[!0-9]*)
	echo "retire: $record is malformed" >&2
	exit 1
	;;
esac
served_for=$(($(date -u +%s) - applied_at))
if [ "$served_for" -lt "$CUTOVER_SOAK_SECONDS" ]; then
	echo "retire: the proxied path has served for ${served_for}s of the ${CUTOVER_SOAK_SECONDS}s soak; $((CUTOVER_SOAK_SECONDS - served_for))s remain" >&2
	exit 1
fi
echo "pass soak served_for=${served_for}s soak=${CUTOVER_SOAK_SECONDS}s"

public_served() {
	local path line ok=0
	for path in /healthz /readyz; do
		if line="$(served_by_gateway "https://$CUTOVER_PUBLIC_HOST$path")"; then
			echo "pass $1 $path $line"
		else
			echo "fail $1 $path $line"
			ok=1
		fi
	done
	return "$ok"
}

public_served served_before || {
	echo "retire: the public hostname is not served by the gateway; nothing stopped" >&2
	exit 1
}

running_before="$(compose ps --services --status running)"

failures=0
for service in "${services[@]}"; do
	ids="$(compose ps -a -q "$service")"
	if [ -n "$ids" ]; then
		# shellcheck disable=SC2086
		docker update --restart=no $ids >/dev/null
	fi
	compose stop "$service"
	for id in $ids; do
		state="$(docker inspect -f '{{.State.Running}} {{.HostConfig.RestartPolicy.Name}}' "$id")"
		if [ "$state" = "false no" ]; then
			echo "pass retired $service container=${id:0:12} running=false restart=no"
		else
			echo "fail retired $service container=${id:0:12} state=$state"
			failures=$((failures + 1))
		fi
	done
	[ -n "$ids" ] || echo "pass retired $service no-container"
done

running_after="$(compose ps --services --status running)"
while read -r service; do
	[ -n "$service" ] || continue
	case ",$CUTOVER_RETIRE_SERVICES," in
	*",$service,"*) continue ;;
	esac
	if grep -qxF -- "$service" <<<"$running_after"; then
		echo "pass kept $service running"
	else
		echo "fail kept $service stopped"
		failures=$((failures + 1))
	fi
done <<<"$running_before"

public_served served_after || failures=$((failures + 1))

if [ "$failures" -ne 0 ]; then
	echo "retire: $failures check(s) failed"
	exit 1
fi
echo "retire: retired ${CUTOVER_RETIRE_SERVICES}; restore with docker update --restart=unless-stopped <container> and docker compose start <service>"
