#!/bin/sh
# Root init of the hpx registry Fly app: prepares the volume at /srv/hpx,
# starts nginx on [::]:8080 and the registry on loopback under the hpx user,
# and exits when either exits, so Fly Machines restart the machine.
set -eu
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
