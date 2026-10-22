#!/usr/bin/env bash
# Round-trips a fixture through backup.sh and restore.sh against a local archive store,
# then checks retention and tamper detection.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

if ! command -v age >/dev/null; then
  GOBIN="${XDG_CACHE_HOME:-$HOME/.cache}/paxeer-age/bin"
  [ -x "$GOBIN/age" ] || (cd "$tmp" && GOBIN=$GOBIN go install filippo.io/age/cmd/...@v1.2.1)
  PATH="$GOBIN:$PATH"
fi

fx=$tmp/fixture
mkdir -p "$fx/validator/config" "$fx/kernel/volume/sub"
echo '{"priv_key":{"type":"tendermint/PrivKeyEd25519","value":"fixture"}}' >"$fx/validator/config/priv_validator_key.json"
echo '{"priv_key":{"type":"tendermint/PrivKeyEd25519","value":"node"}}' >"$fx/validator/config/node_key.json"
head -c 65536 /dev/urandom >"$fx/kernel/volume/sub/state.bin"
echo "CREATE TABLE wallets (id int);" >"$tmp/dump.sql"
chmod 600 "$fx/validator/config/priv_validator_key.json"

age-keygen -o "$tmp/id.txt" 2>/dev/null
age-keygen -y "$tmp/id.txt" >"$tmp/recipients.txt"
cat >"$tmp/backup.env" <<ENV
BACKUP_HOST=
BACKUP_DIR=$tmp/store
BACKUP_RETENTION=3
BACKUP_PREFIX=fixture
BACKUP_RECIPIENTS_FILE=$tmp/recipients.txt
BACKUP_SOURCES="$fx/validator $fx/kernel"
BACKUP_DB_DUMP_CMD="cat $tmp/dump.sql"
BACKUP_IDENTITY=$tmp/id.txt
ENV

fail() { echo "FAIL: $*" >&2; exit 1; }

name=$("$here/backup.sh" "$tmp/backup.env")
[ -f "$tmp/store/$name" ] && [ -f "$tmp/store/$name.sha256" ] || fail "archive not stored"
grep -q fixture "$tmp/store/$name" && fail "archive is not encrypted"

"$here/restore.sh" latest "$tmp/out" "$tmp/backup.env" >/dev/null
diff -r "$fx" "$tmp/out${fx}" || fail "restored tree differs"
cmp "$tmp/dump.sql" "$tmp/out/db/wallet-db.dump" || fail "db dump differs"
[ "$(stat -c %a "$tmp/out${fx}/validator/config/priv_validator_key.json")" = 600 ] || fail "mode not preserved"

for _ in 1 2 3 4; do "$here/backup.sh" "$tmp/backup.env" >/dev/null; done
[ "$(ls "$tmp/store"/*.age | wc -l)" -eq 3 ] || fail "retention did not keep 3 archives"
[ "$(ls "$tmp/store"/*.sha256 | wc -l)" -eq 3 ] || fail "retention left stray checksums"
[ -e "$tmp/store/$name" ] && fail "oldest archive not pruned"

fake() { # fake <tool> <exit>: put a <tool> that prints junk and exits <exit> first on PATH
  mkdir -p "$tmp/fake-$1-$2"
  printf '#!/bin/sh\necho partial\nexit %s\n' "$2" >"$tmp/fake-$1-$2/$1"
  chmod +x "$tmp/fake-$1-$2/$1"
  echo "$tmp/fake-$1-$2"
}

# tar exit 2 or a failing age must store nothing and prune nothing.
before=$(ls "$tmp/store")
for case in "tar 2" "age 1"; do
  set -- $case
  PATH="$(fake "$1" "$2"):$PATH" "$here/backup.sh" "$tmp/backup.env" >/dev/null 2>"$tmp/err" && fail "backup succeeded with $1 exit $2"
  grep -q "failed stage:.*$1 (exit $2)" "$tmp/err" || fail "failing stage not named for $1 exit $2"
  [ "$(ls "$tmp/store")" = "$before" ] || fail "store changed after $1 exit $2"
done

last=$(ls "$tmp/store"/*.age | sort | tail -n 1)
b=$(od -An -tu1 -j200 -N1 "$last" | tr -d ' ')
printf "\\$(printf %o $((b ^ 1)))" | dd of="$last" bs=1 seek=200 conv=notrunc status=none
"$here/restore.sh" latest "$tmp/bad" "$tmp/backup.env" 2>/dev/null && fail "tampered archive restored"
[ -e "$tmp/bad" ] && fail "tampered archive wrote output"

# tar exit 1 ("file changed as we read it") is a warning: the archive is stored and pruning runs.
warn=$(PATH="$(fake tar 1):$PATH" "$here/backup.sh" "$tmp/backup.env" 2>"$tmp/err") || fail "backup failed on tar exit 1"
grep -q "warning: tar stage exit 1" "$tmp/err" || fail "tar exit 1 warning not logged"
[ -f "$tmp/store/$warn" ] && [ -f "$tmp/store/$warn.sha256" ] || fail "archive not stored on tar exit 1"
[ "$(ls "$tmp/store"/*.age | wc -l)" -eq 3 ] || fail "retention did not run after tar exit 1"

echo "backup round-trip ok"
