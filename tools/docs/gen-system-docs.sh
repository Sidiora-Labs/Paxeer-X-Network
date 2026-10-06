#!/usr/bin/env bash
# Generate docs/system/ from repository sources; --check fails when it is stale.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
gen="$root/tools/docs/gen/gen_system_docs.py"
out="$root/docs/system"

case "${1:-}" in
  "")
    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' EXIT
    python3 "$gen" "$tmp"
    rm -rf "$out"
    mkdir -p "$out"
    cp "$tmp"/*.md "$out"/
    echo "wrote $(ls "$out" | wc -l) files to docs/system"
    ;;
  --check)
    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' EXIT
    python3 "$gen" "$tmp"
    if ! diff -ru "$out" "$tmp"; then
      echo "docs/system is stale; run tools/docs/gen-system-docs.sh" >&2
      exit 1
    fi
    python3 "$gen" "$tmp/again"
    rm -rf "$tmp/again.cmp"; mkdir "$tmp/again.cmp"; cp "$tmp"/*.md "$tmp/again.cmp"/
    diff -ru "$tmp/again.cmp" "$tmp/again" >/dev/null || { echo "generator output is not deterministic" >&2; exit 1; }
    if grep -nE '\b([0-9]{1,3}\.){3}[0-9]{1,3}\b|\.fly\.dev|\.internal\b' "$out"/*.md; then
      echo "docs/system carries private host facts" >&2
      exit 1
    fi
    echo "docs/system is current"
    ;;
  *)
    echo "usage: $0 [--check]" >&2
    exit 2
    ;;
esac
