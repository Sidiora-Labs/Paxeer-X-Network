#!/bin/sh
set -eu

node_pid=""
nginx_pid=""

shutdown() {
  trap - TERM INT EXIT
  [ -z "$nginx_pid" ] || kill -TERM "$nginx_pid" 2>/dev/null || true
  [ -z "$node_pid" ] || kill -TERM "$node_pid" 2>/dev/null || true
  wait 2>/dev/null || true
}

trap shutdown TERM INT EXIT

secret="${TRUSTED_PROXY_SECRET:-}"
case "$secret" in
  *[!A-Za-z0-9._~+/=-]*)
    echo "TRUSTED_PROXY_SECRET may only contain A-Z a-z 0-9 . _ ~ + / = -" >&2
    exit 1
    ;;
esac
printf 'proxy_set_header x-paxport-proxy-secret "%s";\n' "$secret" > /tmp/nginx/proxy-secret.conf

PORT=3000 HOSTNAME=127.0.0.1 node /app/human/apps/wallet/server.js &
node_pid="$!"

nginx -e /dev/stderr -c /etc/nginx/nginx.conf -g 'daemon off;' &
nginx_pid="$!"

while kill -0 "$node_pid" 2>/dev/null && kill -0 "$nginx_pid" 2>/dev/null; do
  sleep 1
done

exit 1
