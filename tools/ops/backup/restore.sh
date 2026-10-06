#!/usr/bin/env bash
# Verify and restore one archive written by backup.sh.
# Usage: restore.sh <archive-name|latest> <dest-dir> [env-file]
# Needs BACKUP_IDENTITY (age identity file) in the env file or environment.
set -euo pipefail
. "$(dirname "$0")/lib.sh"
[ $# -ge 2 ] || die "usage: restore.sh <archive-name|latest> <dest-dir> [env-file]"
want=$1 dest=$2
load_env "${3:-}"
: "${BACKUP_IDENTITY:?BACKUP_IDENTITY must be set}"

dir=$(q "$BACKUP_DIR")
if [ "$want" = latest ]; then
  want=$(store "cd $dir && ls -1 -- $(q "$BACKUP_PREFIX")-*.tar.gz.age | sort | tail -n 1")
  [ -n "$want" ] || die "no archives in store"
fi
case $want in */* | '' | .*) die "bad archive name: $want" ;; esac

umask 077
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
store "cat $dir/$(q "$want")" >"$work/$want"
store "cat $dir/$(q "$want").sha256" >"$work/$want.sha256"
(cd "$work" && sha256sum --quiet -c "$want.sha256") || die "checksum mismatch: $want"
age -d -i "$BACKUP_IDENTITY" "$work/$want" | tar -tzf - >/dev/null || die "archive does not decrypt or list: $want"

mkdir -p "$dest"
age -d -i "$BACKUP_IDENTITY" "$work/$want" | tar -xzpf - -C "$dest"
echo "$want"
