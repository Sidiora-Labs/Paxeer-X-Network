#!/usr/bin/env bash
set -euo pipefail

# Runs real-ip against good and malformed LAYERX_TRUSTED_PROXIES values, then
# serves the rendered directives from a local nginx and checks the client
# address it derives from X-Forwarded-For.
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
real_ip="$here/real-ip.sh"
work="$(mktemp -d)"
trap '[ -f "$work/nginx.pid" ] && kill "$(cat "$work/nginx.pid")" 2>/dev/null; rm -rf "${work:?}"' EXIT

failures=0
check() {
	if (eval "$2"); then
		echo "ok   $1"
	else
		echo "FAIL $1"
		failures=$((failures + 1))
	fi
}

check renders_and_execs 'LAYERX_TRUSTED_PROXIES="10.0.0.0/8, fd00::/8 127.0.0.1" "$real_ip" "$work/a/real-ip.conf" echo ran >"$work/a.out" &&
	[ "$(cat "$work/a.out")" = ran ] &&
	printf "set_real_ip_from 10.0.0.0/8;\nset_real_ip_from fd00::/8;\nset_real_ip_from 127.0.0.1;\nreal_ip_header X-Forwarded-For;\nreal_ip_recursive on;\n" | cmp -s - "$work/a/real-ip.conf"'
check refuses_empty '! LAYERX_TRUSTED_PROXIES=" , " "$real_ip" "$work/b.conf" true 2>/dev/null && [ ! -e "$work/b.conf" ]'
check refuses_unset '! env -u LAYERX_TRUSTED_PROXIES "$real_ip" "$work/c.conf" true 2>/dev/null && [ ! -e "$work/c.conf" ]'
check refuses_injection '! LAYERX_TRUSTED_PROXIES="10.0.0.0/8;return" "$real_ip" "$work/d.conf" true 2>/dev/null && [ ! -e "$work/d.conf" ]'
check refuses_hostname '! LAYERX_TRUSTED_PROXIES="edge.paxeer.network" "$real_ip" "$work/e.conf" true 2>/dev/null'

if command -v nginx >/dev/null 2>&1 && command -v curl >/dev/null 2>&1; then
	serve() {
		LAYERX_TRUSTED_PROXIES="$1" "$real_ip" "$work/srv/real-ip.conf" true
		cat >"$work/nginx.conf" <<CONF
pid $work/nginx.pid;
error_log $work/error.log;
events {}
http {
    access_log off;
    client_body_temp_path $work/tmp;
    proxy_temp_path $work/tmp;
    fastcgi_temp_path $work/tmp;
    uwsgi_temp_path $work/tmp;
    scgi_temp_path $work/tmp;
    include $work/srv/*.conf;
    server {
        listen 127.0.0.1:18781;
        location / { return 200 "\$remote_addr"; }
    }
}
CONF
		mkdir -p "$work/tmp"
		if [ -f "$work/nginx.pid" ]; then
			nginx -c "$work/nginx.conf" -s reload
			sleep 1
		else
			nginx -c "$work/nginx.conf"
		fi
	}
	client() {
		curl -fsS --max-time 5 -H "X-Forwarded-For: $1" http://127.0.0.1:18781/
	}
	serve "127.0.0.1/32,10.0.0.0/8"
	check xff_rightmost_untrusted '[ "$(client "9.9.9.9, 1.2.3.4, 10.1.2.3")" = 1.2.3.4 ]'
	check xff_single_hop '[ "$(client "1.2.3.4")" = 1.2.3.4 ]'
	check xff_all_trusted_takes_leftmost '[ "$(client "10.0.0.9, 10.1.2.3")" = 10.0.0.9 ]'
	serve "10.0.0.0/8"
	check xff_from_untrusted_peer_ignored '[ "$(client "1.2.3.4")" = 127.0.0.1 ]'
else
	echo "note xff: nginx or curl is absent on this host; the served cases did not run"
fi

if [ "$failures" -ne 0 ]; then
	echo "real-ip.test: $failures case(s) failed"
	exit 1
fi
echo "real-ip.test: all cases passed"
