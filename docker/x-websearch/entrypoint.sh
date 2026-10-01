#!/bin/bash
# The Fly attestor machine's init: starts as root, hands the /data volume
# (config, keys, state) to 65532, then runs one socat hop per peer from
# 127.0.0.1:<port> to <peer>.internal:8480 and the x-websearch process, all as
# 65532, and exits when any of them exits so Fly Machines restart the machine.
# XWEB_HOPS lists the hops as space-separated <port>=<peer .internal name>.
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
	echo "x-websearch-fly-init: must start as root" >&2
	exit 1
fi
mkdir -p /data/state
chown -R 65532:65532 /data
chmod 0700 /data /data/state
for key in "${X_WEBSEARCH_ATTESTOR_KEY_FILE:?}" "${X_WEBSEARCH_SUBMITTER_KEY_FILE:?}" "${X_WEBSEARCH_RECEIVER_KEY_FILE:?}"; do
	chown 65532:65532 "$key"
	chmod 0600 "$key"
done

drop=(setpriv --reuid=65532 --regid=65532 --clear-groups --inh-caps=-all --no-new-privs)
pids=()
for hop in ${XWEB_HOPS:?}; do
	"${drop[@]}" socat "TCP-LISTEN:${hop%%=*},bind=127.0.0.1,reuseaddr,fork" "TCP6:${hop#*=}:8480" &
	pids+=("$!")
done
"${drop[@]}" /usr/local/bin/x-websearch "$@" &
pids+=("$!")
trap 'kill -TERM "${pids[@]}" 2>/dev/null' TERM INT
status=0
wait -n || status=$?
kill -TERM "${pids[@]}" 2>/dev/null || true
wait || true
exit "$status"
