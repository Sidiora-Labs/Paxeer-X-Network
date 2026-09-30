#!/usr/bin/env bash
set -euo pipefail

# Renders the site of one HTTP name and the stream block of one stream name
# from a fixture manifest and compares each with the expected text beside
# this file.
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "${work:?}"' EXIT
mkdir -p "$work/nginx/edge"
printf '%s\n' \
	"api-mainnet-beta.paxeer.network http paxeer-shared-endpoint 443" \
	"machine.paxeer.network stream paxeer-human-service 9454" >"$work/nginx/edge/manifest"

failures=0
case_render() {
	local name="$1" want="$2" got status=0
	got="$(env -u BRINGUP_HOSTS_FILE -u EDGE_WEBROOT -u EDGE_CERT_DIR -u EDGE_RESOLVER \
		EDGE_LOCAL=1 EDGE_NGINX_DIR="$work/nginx" "$here/edge.sh" render "$name" 2>&1)" || status=$?
	if [ "$status" -eq 0 ] && [ "$got" = "$(cat "$here/$want")" ]; then
		echo "ok   edge_render_$name"
	else
		echo "FAIL edge_render_$name: exit $status, diff against $want:"
		diff <(printf '%s\n' "$got") "$here/$want" || true
		failures=$((failures + 1))
	fi
}
case_render api-mainnet-beta.paxeer.network edge.test.http.conf
case_render machine.paxeer.network edge.test.stream.conf

status=0
EDGE_LOCAL=1 "$here/edge.sh" add only-two-args >/dev/null 2>&1 || status=$?
if [ "$status" -eq 2 ]; then
	echo "ok   edge_usage"
else
	echo "FAIL edge_usage: want exit 2, got $status"
	failures=$((failures + 1))
fi

if [ "$failures" -ne 0 ]; then
	echo "edge.test: $failures case(s) failed"
	exit 1
fi
echo "edge.test: all cases passed"
