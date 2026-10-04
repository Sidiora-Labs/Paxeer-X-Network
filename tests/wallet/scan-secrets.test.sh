#!/usr/bin/env bash
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
scan="$here/../../scripts/wallet/scan-secrets.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

failures=0
fail() {
  echo "FAIL: $*" >&2
  failures=$((failures + 1))
}

b64url() {
  base64 | tr -d '=\n' | tr '+/' '-_'
}

hex64="$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"
pem_body="$(head -c 48 /dev/urandom | base64 | tr -d '\n')"
pem_kind="PRIVATE"
jwt="$(printf '{"typ":"JWT","alg":"HS256"}' | b64url).$(printf '{"sub":"synthetic-user","role":"tester"}' | b64url).$(head -c 32 /dev/urandom | b64url)"
db_user="scanuser"
db_pass="scanpass$(od -An -N4 -tx1 /dev/urandom | tr -d ' \n')"

clean="$tmp/clean"
dirty="$tmp/dirty"
mkdir -p "$clean/src" "$clean/node_modules/pkg" "$dirty/src" "$dirty/keys"

cat > "$clean/src/config.ts" <<EOF
export const databaseUrl = process.env.DATABASE_URL;
export const addressExample = '0x1111111111111111111111111111111111111111';
export const digest = 'a$(printf '%063d' 0)';
EOF
printf 'postgres://localhost:5432/test\n' > "$clean/src/local.txt"
printf 'WALLET_KEY=%s\n' "$hex64" > "$clean/node_modules/pkg/ignored.js"
printf 'lockfileVersion: 9\nsecret_key: %s\n' "$hex64" > "$clean/pnpm-lock.yaml"

printf -- '-----BEGIN %s KEY-----\n%s\n-----END %s KEY-----\n' "$pem_kind" "$pem_body" "$pem_kind" > "$dirty/keys/node.pem"
printf 'const unrelated = 1;\nexport const TREASURY_PRIVATE_KEY = "0x%s";\n' "$hex64" > "$dirty/src/treasury.ts"
printf 'line one\nAuthorization: Bearer %s\n' "$jwt" > "$dirty/src/token.txt"
printf '# db\n\n\nDATABASE_URL=postgres://%s:%s@dbhost:5432/wallet\n' "$db_user" "$db_pass" > "$dirty/src/db.conf"

set +e
clean_out="$(SCAN_SECRETS_ALLOW="$tmp/none.allow" "$scan" "$clean" 2>&1)"
clean_rc=$?
dirty_out="$(SCAN_SECRETS_ALLOW="$tmp/none.allow" "$scan" "$dirty" 2>&1)"
dirty_rc=$?
set -e

[ "$clean_rc" -eq 0 ] || fail "clean tree exited $clean_rc: $clean_out"
[ "$dirty_rc" -eq 1 ] || fail "dirty tree exited $dirty_rc, want 1: $dirty_out"

printf '%s\n' "$clean_out" | grep -qx 'scan-secrets: scanned 2 files, 0 findings' \
  || fail "clean tree count line wrong: $clean_out"
printf '%s\n' "$dirty_out" | grep -qx 'scan-secrets: scanned 4 files, 4 findings' \
  || fail "dirty tree count line wrong: $dirty_out"

expect() {
  local out="$1" want="$2"
  printf '%s\n' "$out" | grep -qxF "$want" || fail "missing finding '$want' in: $out"
}
expect "$dirty_out" "$dirty/keys/node.pem:1: pem-private-key"
expect "$dirty_out" "$dirty/src/treasury.ts:2: assigned-hex-key"
expect "$dirty_out" "$dirty/src/token.txt:2: jwt"
expect "$dirty_out" "$dirty/src/db.conf:4: credentialed-connection-string"

printf '# synthetic fixtures\n\nsrc/token.txt:Bearer eyJ\n' > "$tmp/scan.allow"
set +e
allow_out="$(SCAN_SECRETS_ALLOW="$tmp/scan.allow" "$scan" "$dirty" 2>&1)"
allow_rc=$?
set -e
[ "$allow_rc" -eq 1 ] || fail "allowlisted dirty tree exited $allow_rc, want 1: $allow_out"
if printf '%s\n' "$allow_out" | grep -q 'token.txt'; then
  fail "allowlist did not suppress the token finding: $allow_out"
fi
expect "$allow_out" "$dirty/keys/node.pem:1: pem-private-key"
expect "$allow_out" "$dirty/src/treasury.ts:2: assigned-hex-key"
expect "$allow_out" "$dirty/src/db.conf:4: credentialed-connection-string"
printf '%s\n' "$allow_out" | grep -qx 'scan-secrets: scanned 4 files, 3 findings' \
  || fail "allowlisted count line wrong: $allow_out"

set +e
"$scan" > /dev/null 2>&1
usage_rc=$?
"$scan" "$tmp/does-not-exist" > /dev/null 2>&1
missing_rc=$?
set -e
[ "$usage_rc" -eq 2 ] || fail "no arguments exited $usage_rc, want 2"
[ "$missing_rc" -eq 2 ] || fail "missing path exited $missing_rc, want 2"

if [ "$failures" -ne 0 ]; then
  echo "scan-secrets.test: $failures failure(s)" >&2
  exit 1
fi
echo "scan-secrets.test: ok"
