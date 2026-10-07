#!/bin/sh
# real-ip <out> <command...>: writes the nginx realip directives that take the
# client address from X-Forwarded-For, the rightmost hop outside the proxies
# of LAYERX_TRUSTED_PROXIES (comma or space separated addresses or CIDRs), to
# <out>, then execs the command. Refuses an empty list or a malformed entry.
set -eu
out=$1
shift
list=$(printf '%s' "${LAYERX_TRUSTED_PROXIES:-}" | tr ',' ' ')
lines=
for cidr in $list; do
	if ! printf '%s' "$cidr" | grep -Eqx '(([0-9]{1,3}\.){3}[0-9]{1,3}(/[0-9]{1,2})?|[0-9A-Fa-f.]*:[0-9A-Fa-f:.]*(/[0-9]{1,3})?)'; then
		echo "real-ip: malformed LAYERX_TRUSTED_PROXIES entry $cidr" >&2
		exit 2
	fi
	lines="${lines}set_real_ip_from $cidr;
"
done
if [ -z "$lines" ]; then
	echo "real-ip: LAYERX_TRUSTED_PROXIES is unset or empty" >&2
	exit 2
fi
mkdir -p "$(dirname "$out")"
printf '%sreal_ip_header X-Forwarded-For;\nreal_ip_recursive on;\n' "$lines" >"$out.new"
mv "$out.new" "$out"
exec "$@"
