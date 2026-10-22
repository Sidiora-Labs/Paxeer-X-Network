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

# Stream into a .part file; every stage's status is checked before the archive is
# published under its final name, and nothing is pruned unless that succeeded.
set +e
tar -czf - "${tar_args[@]}" | age -R "$BACKUP_RECIPIENTS_FILE" | store "mkdir -p $dir && umask 077 && cat > $dir/$n.part"
rc=("${PIPESTATUS[@]}")
set -e
stages=(tar age upload) failed=
# GNU tar exits 1 when a live file changed while read; the archive is still complete
# enough to keep and is verified below. Exit 2 or higher is a real failure.
if [ "${rc[0]}" -eq 1 ]; then
  echo "backup: warning: tar stage exit 1 (file changed as we read it); keeping archive" >&2
  rc[0]=0
fi
for i in 0 1 2; do
  [ "${rc[$i]}" -eq 0 ] || failed+=" ${stages[$i]} (exit ${rc[$i]})"
done
if [ -n "$failed" ]; then
  store "rm -f -- $dir/$n.part" || true
  die "failed stage:$failed; no archive stored, nothing pruned"
fi
store "cd $dir && head -c 21 -- $n.part | grep -qx 'age-encryption.org/v1' && sha256sum -- $n.part | sed 's/\.part\$//' > $n.sha256.part && mv -- $n.part $n && mv -- $n.sha256.part $n.sha256 && sha256sum --quiet -c -- $n.sha256" ||
  { store "rm -f -- $dir/$n.part $dir/$n.sha256.part $dir/$n $dir/$n.sha256" || true; die "verify failed; no archive stored, nothing pruned"; }

p=$(q "$BACKUP_PREFIX")
store "cd $dir && ls -1 -- $p-*.tar.gz.age | sort | head -n -$BACKUP_RETENTION | while read -r f; do rm -f -- \"\$f\" \"\$f.sha256\"; done"
echo "$name"
