#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 6 ]; then
	echo "usage: box-apply.sh <role> <image>:<sha12> <unit> <health> <registry-token-file> <registry-user>" >&2
	exit 2
fi
role="$1" ref="$2" unit="$3" health="$4"
REGISTRY_TOKEN_FILE="$5" registry_user="$6"
[[ "$role" =~ ^[a-z0-9][a-z0-9-]*$ ]] || { echo "bad role $role" >&2; exit 2; }
[[ "$ref" =~ ^[a-z0-9][a-z0-9-]*:[0-9a-f]{12}$ ]] || { echo "bad image $ref" >&2; exit 2; }
[[ "$unit" =~ ^[a-z0-9][a-z0-9-]*\.service$ ]] || { echo "bad unit $unit" >&2; exit 2; }
[[ "$health" =~ ^(/[A-Za-z0-9._/-]*|tcp:[0-9]{1,5})$ ]] || { echo "bad health $health" >&2; exit 2; }
[[ "$registry_user" =~ ^[A-Za-z0-9][A-Za-z0-9-]*$ ]] || { echo "bad registry user" >&2; exit 2; }

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

registry_login() {
	if [[ "$REGISTRY_TOKEN_FILE" != /* || ! -f "$REGISTRY_TOKEN_FILE" || ! -r "$REGISTRY_TOKEN_FILE" || ! -s "$REGISTRY_TOKEN_FILE" ]]; then
		echo "registry login refused: REGISTRY_TOKEN_FILE must name a readable, non-empty regular file" >&2
		return 2
	fi
	docker login "${image%%/*}" --username "$registry_user" --password-stdin <"$REGISTRY_TOKEN_FILE"
}

registry_login || {
	login_status=$?
	echo "registry login refused (exit $login_status)" >&2
	exit "$login_status"
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
