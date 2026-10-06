#!/usr/bin/env bash
# Every NEXT_PUBLIC_* variable the wallet PWA reads must be a declared build ARG
# passed into the build env, the env schema must parse, and the image must wire
# the proxy headers and the /wallet healthcheck.
set -euo pipefail

root="$(cd "$(dirname "$0")/../../.." && pwd)"
app="$root/human/apps/wallet"
dockerfile="$root/docker/wallet-pwa/Dockerfile"
fail=0
err() { echo "FAIL: $*" >&2; fail=1; }

read_vars="$(cd "$app" && grep -rhoE --include=*.ts --include=*.tsx --include=*.mjs --include=*.js \
  --exclude=*.test.ts --exclude=*.test.tsx --exclude=*.spec.ts \
  'process\.env\.NEXT_PUBLIC_[A-Z0-9_]+' src *.ts *.mjs | sed 's/^process\.env\.//' | sort -u)"
[ -n "$read_vars" ] || err "found no NEXT_PUBLIC_* reads under $app/src"

required="NEXT_PUBLIC_PAXEER_GAS_SPONSOR NEXT_PUBLIC_PAXEER_GAS_PAYMASTER NEXT_PUBLIC_PAXEER_USID_PER_PAX NEXT_PUBLIC_SID NEXT_PUBLIC_CHAIN_ID NEXT_PUBLIC_VAPID_PUBLIC_KEY"
for v in $read_vars $required; do
  grep -qE "^ARG $v(=|\$)" "$dockerfile" || err "$v is read by the app but not declared as ARG"
  grep -qE "(ENV |^ +)$v=\\\$$v( |\$)" "$dockerfile" || err "$v is not passed into the build env"
done

grep -qE '^ARG NEXT_PUBLIC_PAXEER_EXPLORER_URL=https://paxscan\.io$' "$dockerfile" || err "explorer URL ARG default is not https://paxscan.io"
grep -qE '^ARG NEXT_PUBLIC_EXPLORER_BASE=https://paxscan\.io$' "$dockerfile" || err "explorer base ARG default is not https://paxscan.io"
grep -qE 'BLOCKSCOUT_UPSTREAM_BASE=https://api\.paxscan\.io' "$dockerfile" || err "BLOCKSCOUT_UPSTREAM_BASE default is not https://api.paxscan.io"
if grep -q 'NEXT_PUBLIC_RPC_URL' "$dockerfile" "$app/config/env/schema.ts"; then err "stale NEXT_PUBLIC_RPC_URL still present"; fi

node --experimental-strip-types --no-warnings --input-type=module -e "
import assert from 'node:assert/strict';
const { validateEnv } = await import('$app/config/env/schema.ts');
const d = validateEnv({});
assert.equal(d.NEXT_PUBLIC_PAXEER_EXPLORER_URL, 'https://paxscan.io');
assert.equal(d.NEXT_PUBLIC_EXPLORER_BASE, 'https://paxscan.io');
assert.equal(d.BLOCKSCOUT_UPSTREAM_BASE, 'https://api.paxscan.io');
assert.ok(!('NEXT_PUBLIC_RPC_URL' in d));
const s = validateEnv({ NEXT_PUBLIC_PAXEER_GAS_SPONSOR: '0x01', NEXT_PUBLIC_PAXEER_GAS_PAYMASTER: '0x02', NEXT_PUBLIC_PAXEER_USID_PER_PAX: '3.114', NEXT_PUBLIC_SID: '0x03', NEXT_PUBLIC_CHAIN_ID: '125', NEXT_PUBLIC_VAPID_PUBLIC_KEY: 'k', BLOCKSCOUT_UPSTREAM_BASE: 'https://x.example' });
assert.equal(s.NEXT_PUBLIC_PAXEER_GAS_SPONSOR, '0x01');
assert.equal(s.NEXT_PUBLIC_PAXEER_GAS_PAYMASTER, '0x02');
assert.equal(s.NEXT_PUBLIC_PAXEER_USID_PER_PAX, '3.114');
assert.equal(s.NEXT_PUBLIC_SID, '0x03');
assert.equal(s.NEXT_PUBLIC_CHAIN_ID, '125');
assert.equal(s.BLOCKSCOUT_UPSTREAM_BASE, 'https://x.example');
" || err "env schema does not parse or validate"

nginx_conf="$app/deployment/nginx.conf"
grep -qE 'proxy_set_header x-paxport-client-id ip:\$remote_addr;' "$nginx_conf" || err "nginx does not set x-paxport-client-id"
grep -q 'include /tmp/nginx/proxy-secret.conf;' "$nginx_conf" || err "nginx does not include the proxy secret header"
grep -q 'x-paxport-proxy-secret' "$app/deployment/start-wallet.sh" || err "start script does not render x-paxport-proxy-secret"
node -e "const r=require('$app/railway.json');if(!r.deploy.healthcheckPath.startsWith('/wallet/'))process.exit(1)" || err "railway healthcheck is not under /wallet"

tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
secret_line="$(sed -n '/^secret=/,/^printf/p' "$app/deployment/start-wallet.sh" | sed "s#/tmp/nginx/proxy-secret.conf#$tmp/out.conf#")"
TRUSTED_PROXY_SECRET='abcdefghijklmnopqrstuvwxyz0123456789' sh -c "$secret_line" && grep -qx 'proxy_set_header x-paxport-proxy-secret "abcdefghijklmnopqrstuvwxyz0123456789";' "$tmp/out.conf" || err "secret header not rendered"
if TRUSTED_PROXY_SECRET='bad"; evil' sh -c "$secret_line" 2>/dev/null; then err "unsafe secret accepted"; fi
env -u TRUSTED_PROXY_SECRET sh -c "$secret_line" && grep -qx 'proxy_set_header x-paxport-proxy-secret "";' "$tmp/out.conf" || err "unset secret does not blank the header"

[ "$fail" -eq 0 ] && echo "pwa-args: ok ($(echo "$read_vars" | wc -l) NEXT_PUBLIC vars checked)"
exit "$fail"
