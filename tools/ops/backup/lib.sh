#!/usr/bin/env bash
# Shared helpers for backup.sh and restore.sh. Sourced, not executed.

die() { echo "backup: $*" >&2; exit 1; }

load_env() {
  local env_file=${1:-${BACKUP_ENV:-/etc/paxeer/backup.env}}
  [ -r "$env_file" ] || die "env file not readable: $env_file"
  set -a
  # shellcheck disable=SC1090
  . "$env_file"
  set +a
  : "${BACKUP_DIR:?BACKUP_DIR must be set}"
  BACKUP_RETENTION=${BACKUP_RETENTION:-14}
  BACKUP_PREFIX=${BACKUP_PREFIX:-$(hostname -s)}
  BACKUP_SSH=${BACKUP_SSH:-ssh -o BatchMode=yes}
  case $BACKUP_RETENTION in '' | *[!0-9]* | 0) die "BACKUP_RETENTION must be a positive integer" ;; esac
  case $BACKUP_PREFIX in '' | *[!A-Za-z0-9._-]*) die "BACKUP_PREFIX must match [A-Za-z0-9._-]+" ;; esac
  command -v age >/dev/null || die "age not found on PATH"
}

# Run a shell snippet in the archive store: over ssh on BACKUP_HOST, or locally when
# BACKUP_HOST is empty (the job runs on the backup host itself).
store() {
  if [ -n "${BACKUP_HOST:-}" ]; then
    # shellcheck disable=SC2086
    $BACKUP_SSH "$BACKUP_HOST" "$1"
  else
    sh -c "$1"
  fi
}

q() { printf '%q' "$1"; }
