#!/usr/bin/env bash
# Nightly encrypted backup: tar the configured paths (validator keys, node keys, kernel
# volume state) plus an optional database dump, encrypt to the age recipients, stream to
# the archive store and keep the newest BACKUP_RETENTION archives.
# Usage: backup.sh [env-file]
set -euo pipefail
. "$(dirname "$0")/lib.sh"
load_env "${1:-}"
: "${BACKUP_RECIPIENTS_FILE:?BACKUP_RECIPIENTS_FILE must be set}"
: "${BACKUP_SOURCES:?BACKUP_SOURCES must be set}"
[ -s "$BACKUP_RECIPIENTS_FILE" ] || die "recipients file empty or missing: $BACKUP_RECIPIENTS_FILE"

umask 077
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

tar_args=(-C /)
for src in $BACKUP_SOURCES; do
  [ -e "$src" ] || die "source missing: $src"
  case $src in /*) ;; *) die "source must be absolute: $src" ;; esac
  tar_args+=("${src#/}")
done

if [ -n "${BACKUP_DB_DUMP_CMD:-}" ]; then
  mkdir "$stage/db"
  sh -c "$BACKUP_DB_DUMP_CMD" >"$stage/db/wallet-db.dump" || die "database dump failed"
  [ -s "$stage/db/wallet-db.dump" ] || die "database dump is empty"
  tar_args+=(-C "$stage" db)
fi

name="$BACKUP_PREFIX-$(date -u +%Y%m%dT%H%M%S.%NZ).tar.gz.age"
dir=$(q "$BACKUP_DIR")
n=$(q "$name")

store "mkdir -p $dir && umask 077 && cat > $dir/$n.part && mv $dir/$n.part $dir/$n && cd $dir && sha256sum $n > $n.sha256" < <(
  tar -czf - "${tar_args[@]}" | age -R "$BACKUP_RECIPIENTS_FILE"
)

p=$(q "$BACKUP_PREFIX")
store "cd $dir && ls -1 -- $p-*.tar.gz.age | sort | head -n -$BACKUP_RETENTION | while read -r f; do rm -f -- \"\$f\" \"\$f.sha256\"; done"
echo "$name"
