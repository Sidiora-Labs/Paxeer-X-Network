#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/../../.." && pwd)
dir="$root/docker/redis"
base=6c0be0aa5
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

fail() {
	echo "FAIL: $*" >&2
	exit 1
}

for toml in human/wallet/deploy/redis.toml platform/hosted/internal/redis.toml; do
	git -C "$root" show "$base:$toml" >"$work/fly.toml" || fail "cannot read $toml at $base"
	python3 - "$work/fly.toml" "$dir/files.tsv" "$toml" <<'PY' || fail "files.tsv does not cover $toml"
import sys, tomllib
cfg = tomllib.load(open(sys.argv[1], "rb"))
rows = {tuple(l.rstrip("\n").split("\t")[:2]) for l in open(sys.argv[2]) if l.strip()}
entries = cfg["files"]
assert entries, "no [[files]]"
missing = [(f["secret_name"], f["guest_path"]) for f in entries if (f["secret_name"], f["guest_path"]) not in rows]
assert not missing, f"{sys.argv[3]} missing {missing}"
PY
done

awk -F'\t' 'NF != 4 || $4 != "*" { exit 1 }' "$dir/files.tsv" || fail "files.tsv rows are not four columns for every role"

sh -n "$dir/entrypoint.sh" || fail "entrypoint syntax"
sh -n "$root/docker/common/env-files.sh" || fail "shim syntax"

docker build --check -f "$dir/Dockerfile" "$root" >"$work/check.log" 2>&1 || {
	cat "$work/check.log" >&2
	fail "docker build --check"
}
grep -q '^ENTRYPOINT \["/usr/local/bin/layerx-env-files", "/usr/local/bin/layerx-redis"\]$' "$dir/Dockerfile" || fail "entrypoint line"
grep -qE '^FROM redis:7\.4-alpine@sha256:[0-9a-f]{64}$' "$dir/Dockerfile" || fail "base image not pinned by digest"

for role in redis-router redis-internal; do
	env -i PATH="$PATH" LAYERX_ROLE=$role REDIS_MAXMEMORY=3gb sh "$dir/entrypoint.sh" render >"$work/$role.conf" ||
		fail "render for $role"
	for directive in \
		'include /run/secrets/redis.conf' \
		'bind \* -::\*' \
		'port 0' \
		'tls-port 6379' \
		'tls-cert-file /run/secrets/redis.crt' \
		'tls-key-file /run/secrets/redis.key' \
		'tls-ca-cert-file /run/secrets/ca.crt' \
		'aclfile /run/secrets/users.acl' \
		'appendonly yes' \
		'dir /data' \
		'maxmemory 3gb' \
		'maxmemory-policy noeviction'; do
		grep -qx "$directive" "$work/$role.conf" || fail "$role conf lacks '$directive'"
	done
	[ "$(head -n1 "$work/$role.conf")" = 'include /run/secrets/redis.conf' ] || fail "operator include must come first so rendered directives win"
	cut -f2 "$dir/files.tsv" | while read -r path; do
		[ "$path" = /run/secrets/redis.conf ] || grep -q " $path\$" "$work/$role.conf" || fail "$role conf does not use $path"
	done
done

! env -i PATH="$PATH" LAYERX_ROLE=kms sh "$dir/entrypoint.sh" render >/dev/null 2>&1 || fail "unknown role accepted"
! env -i PATH="$PATH" LAYERX_ROLE=redis-router REDIS_MAXMEMORY='1gb; rm' sh "$dir/entrypoint.sh" render >/dev/null 2>&1 || fail "bad maxmemory accepted"

echo "PASS redis image"
