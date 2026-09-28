#!/bin/sh
set -u

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
scanner="$script_dir/scan-secrets.sh"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM

failures=0

fail() {
  echo "FAIL: $1" >&2
  failures=$((failures + 1))
}

repeat() {
  text=$1
  count=$2
  out=""
  i=0
  while [ "$i" -lt "$count" ]; do
    out="$out$text"
    i=$((i + 1))
  done
  printf '%s' "$out"
}

dirty="$work/dirty"
clean="$work/clean"
mkdir -p "$dirty/src" "$dirty/.vscode" "$dirty/android/app/build" "$dirty/ios/Pods" "$clean/src"

printf 'export const answer = 42;\n' > "$clean/src/clean.ts"
cp "$clean/src/clean.ts" "$dirty/src/clean.ts"

hex=$(repeat 'ab' 32)
words="$(repeat 'word ' 11)word"

printf '%s\n' "-----BEGIN ""PRIVATE KEY-----" "MIIB" "-----END ""PRIVATE KEY-----" > "$dirty/src/pem.txt"
printf 'const privateKey = "0x%s";\n' "$hex" > "$dirty/src/hexkey.ts"
printf 'const phrase = "%s";\n' "$words" > "$dirty/src/mnemonic.ts"
printf 'const token = "%s.%s.%s";\n' "eyJ""hbGciOiJIUzI1NiJ9" "eyJ""zdWIiOiIxMjM0NTY3ODkwIn0" "c2lnbmF0dXJlc2lnbmF0dXJl" > "$dirty/src/jwt.ts"
printf 'const aws = "%s%s";\n' "AKIA" "ABCDEFGHIJKLMNOP" > "$dirty/src/apikey.ts"
printf 'const supabase = "%s%s";\n' "sb_secret_" "abcdefghijklmnopqrstuv" > "$dirty/src/supabase.ts"
printf 'Authorization: %s %s\n' "Bearer" "abcdefghijklmnopqrstuvwxyz012345" > "$dirty/src/bearer.txt"
printf '{"crypto":{"%s":"%s"}}\n' "ciphertext" "$hex" > "$dirty/src/wallet.json"
printf 'NAME=value\n' > "$dirty/.env"
printf 'NAME=value\n' > "$dirty/.env.production"
printf 'x' > "$dirty/release.keystore"
printf 'x' > "$dirty/upload.jks"
printf 'x' > "$dirty/signing.p12"
printf 'x' > "$dirty/app.mobileprovision"
printf 'x' > "$dirty/server.pem"
printf 'x' > "$dirty/.DS_Store"
printf '{}\n' > "$dirty/.vscode/settings.json"
printf 'x' > "$dirty/android/app/build/output.txt"
printf 'x' > "$dirty/ios/Pods/manifest.txt"

output=$(sh "$scanner" "$dirty" 2>&1)
status=$?
[ "$status" -eq 1 ] || fail "dirty tree exited $status, expected 1"

for expected in \
  "src/pem.txt:1: private key block" \
  "src/hexkey.ts:1: hex private key" \
  "src/mnemonic.ts:1: mnemonic phrase" \
  "src/jwt.ts:1: json web token" \
  "src/apikey.ts:1: api key" \
  "src/supabase.ts:1: supabase key" \
  "src/bearer.txt:1: bearer token" \
  "src/wallet.json:1: keystore" \
  ".env: environment file" \
  ".env.production: environment file" \
  "release.keystore: signing file" \
  "upload.jks: signing file" \
  "signing.p12: signing file" \
  "app.mobileprovision: signing file" \
  "server.pem: certificate or key file" \
  ".DS_Store: local artefact" \
  ".vscode: local artefact" \
  "android/app/build: native build intermediate" \
  "ios/Pods: native build intermediate"; do
  printf '%s\n' "$output" | grep -qxF -e "$expected" || fail "missing hit: $expected"
done

if printf '%s\n' "$output" | grep -q 'src/clean.ts'; then
  fail "clean file reported"
fi
if printf '%s\n' "$output" | grep -qF -e "$hex"; then
  fail "scanner printed a matched value"
fi

output=$(sh "$scanner" "$clean" 2>&1)
status=$?
[ "$status" -eq 0 ] || fail "clean tree exited $status, expected 0: $output"
[ -z "$output" ] || fail "clean tree produced output: $output"

if [ "$failures" -ne 0 ]; then
  echo "scan-secrets.test: $failures failure(s)" >&2
  exit 1
fi
echo "scan-secrets.test: ok"
