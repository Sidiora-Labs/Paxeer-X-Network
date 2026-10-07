#!/bin/sh
# Root init of the hpx registry container: prepares the volume at /srv/hpx,
# renders the nginx vhost for PORT and HPX_ADDR, starts nginx on [::]:PORT and
# the registry on HPX_ADDR under the hpx user, and exits when either exits, so
# the platform restarts the container.
set -eu
if ! printf '%s' "${PORT:-}" | grep -Eqx '[0-9]{1,5}'; then
	echo "hpx-entrypoint: PORT is unset or malformed" >&2
	exit 2
fi
if ! printf '%s' "${HPX_ADDR:-}" | grep -Eqx '(\[[0-9A-Fa-f:.]+\]|[0-9A-Za-z.-]+):[0-9]{1,5}'; then
	echo "hpx-entrypoint: HPX_ADDR is unset or malformed" >&2
	exit 2
fi
sed -e "s|\${PORT}|$PORT|g" -e "s|\${HPX_ADDR}|$HPX_ADDR|g" \
	/etc/nginx/hpx-registry.conf.template >/etc/nginx/http.d/hpx-registry.conf
install -d -m 0755 /srv/hpx/artifacts
install -d -o hpx -g hpx -m 0750 /srv/hpx/data
chown -R hpx:hpx /srv/hpx/data

nginx -g 'daemon off;' &
nginx_pid=$!
su-exec hpx /usr/local/bin/hpx-registry &
registry_pid=$!

stop() {
	kill -TERM "$nginx_pid" "$registry_pid" 2>/dev/null || true
	wait
}
trap 'stop; exit 0' TERM INT

while kill -0 "$nginx_pid" 2>/dev/null && kill -0 "$registry_pid" 2>/dev/null; do
	sleep 1
done
echo "hpx-entrypoint: nginx or the registry exited" >&2
stop
exit 1
