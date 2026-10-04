#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
checker="$root/scripts/wallet/check-deploy.sh"
source_dir="$root/human/wallet/deploy"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

failures=0

fresh() {
	local target="$work/$1"
	rm -rf "$target"
	cp -R "$source_dir" "$target"
	printf '%s\n' "$target"
}

expect_pass() {
	local name="$1" dir="$2" output status=0
	output="$("$checker" "$dir" 2>&1)" || status=$?
	if [ "$status" -eq 0 ] && ! grep -q '^fail ' <<<"$output"; then
		echo "ok   $name"
	else
		echo "FAIL $name: checker exited $status on valid definitions"
		printf '%s\n' "$output"
		failures=$((failures + 1))
	fi
}

expect_fail() {
	local name="$1" dir="$2" rule="$3" file="$4" output status=0
	output="$("$checker" "$dir" 2>&1)" || status=$?
	if [ "$status" -eq 1 ] && grep -q "^fail $rule $file" <<<"$output"; then
		echo "ok   $name"
	else
		echo "FAIL $name: want exit 1 with 'fail $rule $file', got exit $status"
		printf '%s\n' "$output"
		failures=$((failures + 1))
	fi
}

set_region() {
	sed -i "s/^primary_region = \".*\"/primary_region = \"$2\"/" "$1"
}

expect_pass "valid definitions pass" "$(fresh valid)"

dir="$(fresh duplicate-region)"
set_region "$dir/attestor-2.toml" iad
expect_fail "duplicate region" "$dir" distinct-regions "attestor-\*.toml"

dir="$(fresh missing-mount)"
sed -i '/^\[\[mounts\]\]/,/^$/d' "$dir/attestor-1.toml"
expect_fail "missing mount" "$dir" mount attestor-1.toml

dir="$(fresh public-port)"
printf '\n[[services.ports]]\n  port = 443\n  handlers = ["tls"]\n' >>"$dir/attestor-2.toml"
expect_fail "public port" "$dir" no-public-ports attestor-2.toml

dir="$(fresh secret-value)"
hex="$(printf 'ab%.0s' $(seq 32))"
sed -i "s/^  ATTESTOR_JWT_AUDIENCE = \".*\"/  ATTESTOR_JWT_AUDIENCE = \"$hex\"/" "$dir/attestor-3.toml"
expect_fail "secret-shaped env value" "$dir" env-no-secrets attestor-3.toml

dir="$(fresh undocumented-name)"
sed -i 's/^\[env\]$/[env]\n  WALLET_UNDOCUMENTED_SETTING = "on"/' "$dir/gateway.toml"
expect_fail "env name missing from env" "$dir" env-documented gateway.toml

dir="$(fresh no-api-tcp-check)"
python3 - "$dir/attestor-4.toml" <<'PY'
import re
import sys

path = sys.argv[1]
text = open(path).read()
text, count = re.subn(r"(internal_port = 8443\n(?:.*\n)*?)\n  \[\[services\.tcp_checks\]\]\n(?:    .*\n)+", r"\1", text, count=1)
assert count == 1
open(path, "w").write(text)
PY
expect_fail "attestor without a tcp check on its API port" "$dir" health-check attestor-4.toml

dir="$(fresh tcp-check-without-mutual-tls)"
sed -i '/^  ATTESTOR_TLS_CA_FILE = /d' "$dir/attestor-5.toml"
expect_fail "tcp check accepted only for a mutual TLS API" "$dir" health-check attestor-5.toml

dir="$(fresh four-attestors)"
rm "$dir/attestor-5.toml"
expect_fail "fourth attestor only" "$dir" attestor-count "attestor-\*.toml"

dir="$(fresh same-continent)"
set_region "$dir/attestor-1.toml" iad
set_region "$dir/attestor-2.toml" ord
set_region "$dir/attestor-3.toml" sjc
set_region "$dir/attestor-4.toml" sea
set_region "$dir/attestor-5.toml" yyz
expect_fail "same-continent set" "$dir" continents "attestor-\*.toml"

dir="$(fresh image-build)"
image_output="$("$checker" "$dir" 2>&1)" || true
if grep -q '^pass dockerfile redis.toml$' <<<"$image_output"; then
	sed -i '/^  image = /d' "$dir/redis.toml"
	expect_fail "image build accepted, empty build refused" "$dir" dockerfile redis.toml
else
	echo "FAIL image build accepted, empty build refused: redis.toml with a build image did not pass the dockerfile rule"
	printf '%s\n' "$image_output"
	failures=$((failures + 1))
fi

if [ "$failures" -ne 0 ]; then
	echo "check-deploy.test: $failures case(s) failed"
	exit 1
fi
echo "check-deploy.test: all cases passed"
