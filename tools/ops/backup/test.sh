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

last=$(ls "$tmp/store"/*.age | sort | tail -n 1)
b=$(od -An -tu1 -j200 -N1 "$last" | tr -d ' ')
printf "\\$(printf %o $((b ^ 1)))" | dd of="$last" bs=1 seek=200 conv=notrunc status=none
"$here/restore.sh" latest "$tmp/bad" "$tmp/backup.env" 2>/dev/null && fail "tampered archive restored"
[ -e "$tmp/bad" ] && fail "tampered archive wrote output"

echo "backup round-trip ok"
