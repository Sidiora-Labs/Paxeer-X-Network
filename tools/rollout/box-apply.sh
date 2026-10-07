#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 4 ]; then
	echo "usage: box-apply.sh <role> <image>:<sha12> <unit> <health>" >&2
	exit 2
fi
role="$1" ref="$2" unit="$3" health="$4"
[[ "$role" =~ ^[a-z0-9][a-z0-9-]*$ ]] || { echo "bad role $role" >&2; exit 2; }
[[ "$ref" =~ ^[a-z0-9][a-z0-9-]*:[0-9a-f]{12}$ ]] || { echo "bad image $ref" >&2; exit 2; }
[[ "$unit" =~ ^[a-z0-9][a-z0-9-]*\.service$ ]] || { echo "bad unit $unit" >&2; exit 2; }
[[ "$health" =~ ^(/[A-Za-z0-9._/-]*|tcp:[0-9]{1,5})$ ]] || { echo "bad health $health" >&2; exit 2; }

image="ghcr.io/sidiora-labs/$ref"
file="/etc/layerx/$role.image"
timeout="${ROLLOUT_HEALTH_TIMEOUT:-300}"

healthy() {
	case "$health" in
	tcp:*) timeout 5 bash -c "exec 3<>/dev/tcp/127.0.0.1/${health#tcp:}" 2>/dev/null ;;
	*) curl -fsS -m 5 -o /dev/null "http://127.0.0.1:${LAYERX_HEALTH_PORT:-8080}$health" ;;
	esac
}

settle() {
	local end=$((SECONDS + timeout))
	while [ "$SECONDS" -lt "$end" ]; do
		if systemctl is-active --quiet "$unit" && healthy; then
			return 0
		fi
		sleep 5
	done
	return 1
}

docker pull "$image"
mkdir -p /etc/layerx
if [ -f "$file" ]; then
	cp -p "$file" "$file.prev"
fi
printf '%s\n' "$image" >"$file.tmp"
mv "$file.tmp" "$file"
systemctl restart "$unit"
if settle; then
	echo "pass $role $image $unit $health"
	exit 0
fi
echo "fail $role $image $unit $health" >&2
if [ -f "$file.prev" ]; then
	mv "$file.prev" "$file"
	systemctl restart "$unit"
	if settle; then
		echo "restored $role $(cat "$file")" >&2
	else
		echo "restore of $role $(cat "$file") is unhealthy too" >&2
	fi
fi
exit 1
