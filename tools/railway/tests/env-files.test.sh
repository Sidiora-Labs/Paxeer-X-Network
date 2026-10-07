#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/../../.." && pwd)
shim="$root/docker/common/env-files.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

fail() {
	echo "FAIL: $*" >&2
	exit 1
}

table="$work/files.tsv"
printf '%s\t%s\t%s\t%s\n' \
	TEST_TLS_KEY "$work/out/tls/key.pem" 0600 public,ingress \
	TEST_CONFIG "$work/out/config.json" 0644 '*' \
	TEST_DEFAULT_MODE "$work/out/deep/nested/token" '' '*' \
	TEST_BINARY "$work/out/blob.der" 0400 '*' \
	TEST_OTHER "$work/out/other/secret" 0600 kms >"$table"

key_value='-----BEGIN KEY-----
abc
-----END KEY-----'
config_value='{"a":1}'
printf '\000\001\377binary' >"$work/blob.src"

base_env=(
	LAYERX_FILES_TABLE="$table"
	TEST_TLS_KEY="$(printf '%s' "$key_value" | base64 -w0)"
	TEST_CONFIG="$(printf '%s' "$config_value" | base64 -w0)"
	TEST_DEFAULT_MODE="$(printf 'tok' | base64 -w0)"
	TEST_BINARY="$(base64 -w0 <"$work/blob.src")"
	TEST_OTHER="$(printf 'never' | base64 -w0)"
)

env -i PATH="$PATH" "${base_env[@]}" LAYERX_ROLE=ingress sh "$shim" env >"$work/child.env" ||
	fail "shim exited $? for role ingress"

[ "$(cat "$work/out/tls/key.pem")" = "$key_value" ] || fail "key.pem content"
[ "$(cat "$work/out/config.json")" = "$config_value" ] || fail "config.json content"
[ "$(cat "$work/out/deep/nested/token")" = tok ] || fail "default-mode token content"
cmp -s "$work/blob.src" "$work/out/blob.der" || fail "binary content"
[ "$(stat -c %a "$work/out/tls/key.pem")" = 600 ] || fail "key.pem mode $(stat -c %a "$work/out/tls/key.pem")"
[ "$(stat -c %a "$work/out/config.json")" = 644 ] || fail "config.json mode"
[ "$(stat -c %a "$work/out/deep/nested/token")" = 600 ] || fail "default mode is not 0600"
[ "$(stat -c %a "$work/out/blob.der")" = 400 ] || fail "blob.der mode"
[ ! -e "$work/out/other/secret" ] || fail "row for role kms was written for role ingress"
[ ! -e "$work/out/other" ] || fail "directory for role kms was created for role ingress"
if ls -A "$work/out/tls" "$work/out" | grep -q '^\.layerx-env-files'; then fail "temp file left behind"; fi
for n in TEST_TLS_KEY TEST_CONFIG TEST_DEFAULT_MODE TEST_BINARY TEST_OTHER; do
	if grep -q "^$n=" "$work/child.env"; then fail "$n still set in the exec'd child"; fi
done
grep -q '^LAYERX_ROLE=ingress$' "$work/child.env" || fail "unrelated env not passed through"

rm -rf "$work/out"
env -i PATH="$PATH" "${base_env[@]}" LAYERX_ROLE=kms sh "$shim" true || fail "shim failed for role kms"
[ "$(cat "$work/out/other/secret")" = never ] || fail "kms row content"
[ ! -e "$work/out/tls/key.pem" ] || fail "public,ingress row written for role kms"

rm -rf "$work/out"
set +e
env -i PATH="$PATH" "${base_env[@]}" TEST_CONFIG= LAYERX_ROLE=ingress sh "$shim" true 2>"$work/empty.err"
rc_empty=$?
env -i PATH="$PATH" LAYERX_FILES_TABLE="$table" TEST_TLS_KEY="$(printf x | base64 -w0)" \
	TEST_DEFAULT_MODE=dG9r TEST_BINARY=dG9r LAYERX_ROLE=ingress sh "$shim" true 2>"$work/missing.err"
rc_missing=$?
rm -rf "$work/out"
env -i PATH="$PATH" "${base_env[@]}" TEST_CONFIG='not*base64!' LAYERX_ROLE=ingress sh "$shim" true 2>"$work/invalid.err"
rc_invalid=$?
set -e
[ "$rc_missing" = 1 ] || fail "missing required variable exited $rc_missing, want 1"
grep -q 'TEST_CONFIG' "$work/missing.err" || fail "missing-variable message does not name TEST_CONFIG"
[ "$rc_invalid" = 1 ] || fail "invalid base64 exited $rc_invalid, want 1"
grep -q 'TEST_CONFIG' "$work/invalid.err" || fail "invalid-base64 message does not name TEST_CONFIG"
[ ! -e "$work/out/config.json" ] || fail "invalid value produced config.json"
[ "$rc_empty" = 0 ] || fail "empty value (valid empty base64) exited $rc_empty"

if [ "$(id -u)" != 0 ]; then
	mkdir -p "$work/ro"
	chmod 0555 "$work/ro"
	printf 'TEST_RO\t%s\t\t*\n' "$work/ro/secret" >"$work/ro.tsv"
	set +e
	env -i PATH="$PATH" LAYERX_FILES_TABLE="$work/ro.tsv" TEST_RO=dG9r sh "$shim" true 2>"$work/ro.err"
	rc_ro=$?
	set -e
	[ "$rc_ro" = 1 ] || fail "unwritable directory exited $rc_ro, want 1"
	grep -q 'TEST_RO' "$work/ro.err" || fail "unwritable-dir message does not name TEST_RO"
else
	ro_dir=/proc/layerx-env-files-test
	printf 'TEST_RO\t%s/secret\t\t*\n' "$ro_dir" >"$work/ro.tsv"
	set +e
	env -i PATH="$PATH" LAYERX_FILES_TABLE="$work/ro.tsv" TEST_RO=dG9r sh "$shim" true 2>"$work/ro.err"
	rc_ro=$?
	set -e
	[ "$rc_ro" = 1 ] || fail "unwritable directory exited $rc_ro, want 1"
	grep -q 'TEST_RO' "$work/ro.err" || fail "unwritable-dir message does not name TEST_RO"
fi

out=$(env -i PATH="$PATH" LAYERX_FILES_TABLE="$work/absent.tsv" sh "$shim" printf ran)
[ "$out" = ran ] || fail "absent table did not exec the command"

if [ "$(id -u)" = 0 ]; then
	printf 'TEST_OWNED\t%s\t0640\t*\n' "$work/owned/file" >"$work/owned.tsv"
	env -i PATH="$PATH" LAYERX_FILES_TABLE="$work/owned.tsv" LAYERX_FILES_OWNER=65534:65534 TEST_OWNED=dG9r sh "$shim" true ||
		fail "chown row failed"
	[ "$(stat -c %u:%g:%a "$work/owned/file")" = 65534:65534:640 ] || fail "owner/mode of chowned file"
fi

for t in "$root"/docker/*/files.tsv; do
	awk -F'\t' 'NF != 4 || $1 !~ /^[A-Z_][A-Z0-9_]*$/ || $2 !~ /^\// || $3 !~ /^(0[0-7][0-7][0-7])?$/ || $4 == "" { print FILENAME ":" NR ": " $0; bad = 1 } END { exit bad }' "$t" ||
		fail "$t has malformed rows"
done

echo "env-files: ok"
