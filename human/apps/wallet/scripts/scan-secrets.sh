#!/bin/sh
set -u

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root=${1:-$(dirname -- "$script_dir")}

if [ ! -d "$root" ]; then
  echo "scan-secrets: not a directory: $root" >&2
  exit 2
fi

cd "$root" || exit 2

hits=0

report_content() {
  class=$1
  flags=$2
  pattern=$3
  found=$(find . \
    \( -name node_modules -o -name .git -o -name .next -o -name coverage \
       -o -path ./dist -o -path ./build \) -prune \
    -o -type f -exec grep -nIH $flags -e "$pattern" -- {} + 2>/dev/null \
    | sed -e 's|^\./||' -e 's|^\([^:]*:[0-9][0-9]*\):.*$|\1|')
  if [ -n "$found" ]; then
    printf '%s\n' "$found" | while IFS= read -r location; do
      printf '%s: %s\n' "$location" "$class"
    done
    hits=1
  fi
}

report_paths() {
  class=$1
  shift
  found=$(find . \( -name node_modules -o -name .git \) -prune -o \( "$@" \) -print | sed 's|^\./||')
  if [ -n "$found" ]; then
    printf '%s\n' "$found" | while IFS= read -r path; do
      printf '%s: %s\n' "$path" "$class"
    done
    hits=1
  fi
}

report_content "private key block" -E \
  '-----BEGIN ([A-Z0-9]+ )*PRIVATE KEY( BLOCK)?-----'

report_content "hex private key" -Ei \
  '(priv(ate)?[_-]?key|secret|seed|signer|mnemonic|pk|key)[a-z0-9_]*["'"'"'`]?[[:space:]]*[:=(,][[:space:]]*["'"'"'`]?(0x)?[0-9a-f]{64}([^0-9a-f]|$)'

report_content "hex private key" -Ei \
  'wallet\([[:space:]]*["'"'"'`](0x)?[0-9a-f]{64}["'"'"'`]'

report_content "mnemonic phrase" -E \
  '["'"'"'`]([a-z]{3,8} ){11}([a-z]{3,8} ){0,12}[a-z]{3,8}["'"'"'`]'

report_content "json web token" -E \
  'eyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}'

report_content "api key" -E \
  '(^|[^A-Za-z0-9_-])(sk_live_|sk_test_|rk_live_)[A-Za-z0-9]{16,}|(^|[^A-Za-z0-9])AKIA[0-9A-Z]{16}|gh[pousr]_[A-Za-z0-9]{36,}|xox[abprs]-[A-Za-z0-9-]{10,}|AIza[0-9A-Za-z_-]{35}|glpat-[A-Za-z0-9_-]{20,}|(^|[^A-Za-z0-9_-])sk-(proj-)?[A-Za-z0-9_-]{32,}'

report_content "supabase key" -E \
  'sb_(secret|publishable)_[A-Za-z0-9_-]{16,}|SUPABASE_SERVICE_ROLE_KEY[[:space:]]*[:=][[:space:]]*["'"'"'`]?[A-Za-z0-9._-]{20,}'

report_content "bearer token" -E \
  '[Bb]earer[[:space:]]+[A-Za-z0-9._~+/=-]{24,}'

report_content "keystore" -E \
  '"ciphertext"[[:space:]]*:[[:space:]]*"[0-9a-f]{32,}"'

report_paths "environment file" -type f \( -name '.env' -o -name '.env.*' \)

report_paths "signing file" -type f \( -name '*.keystore' -o -name '*.jks' -o -name '*.p12' \
  -o -name '*.pfx' -o -name '*.mobileprovision' -o -name '*.p8' -o -name 'UTC--*' \)

report_paths "certificate or key file" -type f \( -name '*.pem' -o -name '*.key' -o -name '*.crt' \
  -o -name '*.cer' -o -name '*.der' -o -name 'id_rsa*' -o -name 'id_ed25519*' \)

report_paths "local artefact" \( -name '.DS_Store' -o -name 'Thumbs.db' -o -name 'desktop.ini' \
  -o -name '*.swp' -o -name '*.iml' -o -name '*.tsbuildinfo' -o -name '.idea' -o -name '.vscode' \)

report_paths "native build intermediate" -type d \( -path './android/build' -o -path './android/app/build' \
  -o -path './ios/Pods' -o -path './ios/build' -o -name 'DerivedData' -o -name '.gradle' \)

exit "$hits"
