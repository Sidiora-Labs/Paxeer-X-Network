# shellcheck shell=bash
# Shared helpers for the endpoint cutover scripts. Sourced, not executed.

SERVED_BY_HEADER=x-served-by
SERVED_BY=paxeer-wallet-gateway
HOST_PATTERN='^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)+$'

die() {
	echo "$SCRIPT: $*" >&2
	exit 2
}

# require_var NAME...: every named variable is set and non-empty.
require_var() {
	local name
	for name in "$@"; do
		if [ -z "${!name:-}" ]; then
			die "$name is required and has no default"
		fi
	done
}

# require_host NAME...: every named variable holds a bare DNS hostname.
require_host() {
	local name
	for name in "$@"; do
		require_var "$name"
		if ! printf '%s' "${!name}" | grep -Eq "$HOST_PATTERN"; then
			die "$name must be a bare hostname without a scheme, port or path"
		fi
	done
}

require_tools() {
	local tool
	for tool in "$@"; do
		command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
	done
}

# compose ARGS...: docker compose inside the old service's project directory,
# so the project name and its .env interpolation match the running project.
compose() {
	(cd "$CUTOVER_COMPOSE_DIR" && docker compose "$@")
}

# probe URL [CURL ARGS...]: GET URL and print "<status> <served-by> <ready>",
# where served-by is the last x-served-by header value or none and ready is
# the body's ready field (true, false or none). Returns curl's status on a
# transport error after printing "transport <message>".
probe() {
	local url="$1" headers body code status=0
	shift
	headers="$(mktemp)"
	body="$(mktemp)"
	code="$(curl -sS --max-time 20 "$@" -D "$headers" -o "$body" -w '%{http_code}' "$url" 2>&1)" || status=$?
	if [ "$status" -ne 0 ]; then
		echo "transport $(printf '%s' "$code" | tr '\n' ' ' | cut -c1-200)"
		rm -f "$headers" "$body"
		return "$status"
	fi
	python3 - "$code" "$headers" "$body" "$SERVED_BY_HEADER" <<'PY'
import json
import sys

code, headers, body, name = sys.argv[1:]
served = "none"
for line in open(headers, encoding="latin-1"):
    key, sep, value = line.partition(":")
    if sep and key.strip().lower() == name:
        served = value.strip() or "empty"
ready = "none"
try:
    doc = json.load(open(body, encoding="utf-8"))
    if isinstance(doc, dict) and isinstance(doc.get("ready"), bool):
        ready = str(doc["ready"]).lower()
except ValueError:
    pass
print(code, served, ready)
PY
	rm -f "$headers" "$body"
}

# served_by_gateway URL [CURL ARGS...]: the URL answers 200 with the gateway's
# served-by header; prints the probe line.
served_by_gateway() {
	local line
	line="$(probe "$@")" || {
		echo "$line"
		return 1
	}
	echo "$line"
	case "$line" in
	"200 $SERVED_BY "*) return 0 ;;
	*) return 1 ;;
	esac
}
