#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: scan-secrets.sh <path>..." >&2
  exit 2
}

[ "$#" -ge 1 ] || usage

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
allow_file="${SCAN_SECRETS_ALLOW:-$script_dir/scan-secrets.allow}"

for p in "$@"; do
  if [ ! -e "$p" ]; then
    echo "scan-secrets: no such path: $p" >&2
    exit 2
  fi
done

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

find "$@" \
  \( -type d \( -name node_modules -o -name dist -o -name .next -o -name .git \) -prune \) -o \
  \( -type f \
     ! -name pnpm-lock.yaml ! -name package-lock.json ! -name npm-shrinkwrap.json \
     ! -name yarn.lock ! -name bun.lockb ! -name Cargo.lock ! -name go.sum \
     ! -name Gemfile.lock ! -name poetry.lock ! -name composer.lock \
     ! -size +2048k -print0 \) > "$work/files"

tr '\0' '\n' < "$work/files" | awk -F/ '$NF == ".env" || $NF ~ /^\.env\./ || $NF ~ /\.env$/' | tr '\n' '\0' > "$work/envfiles"

scanned="$(tr -cd '\0' < "$work/files" | wc -c | tr -d ' ')"

hex64='(0x)?[0-9a-fA-F]{64}([^0-9a-fA-F]|$)'

rule_names=(
  "pem-private-key"
  "assigned-hex-key"
  "jwt"
  "credentialed-connection-string"
  "stripe-key"
  "aws-access-key"
  "github-token"
  "gitlab-token"
  "slack-token"
  "sendgrid-key"
  "supabase-service-key"
)
rule_res=(
  '-----BEGIN ([A-Z0-9]+ )*PRIVATE KEY( BLOCK)?-----'
  "[A-Za-z0-9_.-]*(key|secret|private|mnemonic|seed)[A-Za-z0-9_.-]*[\"'\`]?[[:space:]]*(:=|=|:)[[:space:]]*[\"'\`]?${hex64}"
  'eyJ[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}'
  '(postgres|postgresql|mysql|redis|rediss|amqp|amqps|mongodb|mongodb\+srv)://[^:/@[:space:]]+:[^@/[:space:]]+@'
  'sk_(live|test)_[0-9A-Za-z]{10,}'
  'AKIA[0-9A-Z]{16}'
  'ghp_[A-Za-z0-9]{20,}'
  'glpat-[A-Za-z0-9_-]{20,}'
  'xox[a-z]-[A-Za-z0-9-]{10,}'
  'SG\.[A-Za-z0-9_+/=-]{20,}'
  'eyJhbGciOi'
)
rule_flags=("" "-i" "" "" "" "" "" "" "" "" "")

allow_paths=()
allow_res=()
if [ -f "$allow_file" ]; then
  while IFS= read -r entry || [ -n "$entry" ]; do
    case "$entry" in
      ''|'#'*) continue ;;
    esac
    case "$entry" in
      *:*) ;;
      *) echo "scan-secrets: malformed allowlist entry: $entry" >&2; exit 2 ;;
    esac
    allow_paths+=("${entry%%:*}")
    allow_res+=("${entry#*:}")
  done < "$allow_file"
fi

allowed() {
  local file="$1" content="$2" i
  for i in "${!allow_paths[@]}"; do
    case "$file" in
      "${allow_paths[$i]}"|*/"${allow_paths[$i]}") ;;
      *) continue ;;
    esac
    if printf '%s\n' "$content" | grep -Eq -e "${allow_res[$i]}"; then
      return 0
    fi
  done
  return 1
}

report_matches() {
  local rule="$1" list="$2" re="$3" flags="$4" file rest line content
  local -a opts=(-HnIZE)
  [ -s "$list" ] || return 0
  if [ -n "$flags" ]; then
    opts+=("$flags")
  fi
  xargs -0 grep "${opts[@]}" -e "$re" -- < "$list" > "$work/matches" 2>/dev/null || true
  while IFS= read -r -d '' file && IFS= read -r rest; do
    line="${rest%%:*}"
    content="${rest#*:}"
    if allowed "$file" "$content"; then
      continue
    fi
    printf '%s:%s: %s\n' "$file" "$line" "$rule" >> "$work/findings"
  done < "$work/matches"
}

: > "$work/findings"
for i in "${!rule_names[@]}"; do
  report_matches "${rule_names[$i]}" "$work/files" "${rule_res[$i]}" "${rule_flags[$i]}"
done
report_matches "env-file-hex-key" "$work/envfiles" "(^|[^0-9a-fA-F])${hex64}" ""

found="$(sort -u "$work/findings" | tee "$work/sorted" | wc -l | tr -d ' ')"
cat "$work/sorted"
echo "scan-secrets: scanned $scanned files, $found findings"
[ "$found" -eq 0 ]
