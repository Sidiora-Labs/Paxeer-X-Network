#!/usr/bin/env bash
set -euo pipefail

# Renders both validator tables, checks each with nft -c, loads them into a
# fresh network namespace and asks the script's own state reader whether the
# loaded rules behave (attestor ports reset, API and postgres ports 5433/5455
# closed, 22 and 26656 open); then migrates a real paxd test keyring into a
# file keyring and checks the test keyring is removed only once a backup
# archive written by tools/ops/backup/backup.sh holds it.
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../../.." && pwd)"
fw="$repo/tools/bringup/validator-firewall.sh"
migrate="$repo/tools/ops/keyring-migrate.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work:?}"' EXIT

failures=0
check() {
	if eval "$2"; then
		echo "ok   $1"
	else
		echo "FAIL $1"
		[ -z "${out:-}" ] || printf '     output: %s\n' "$out"
		failures=$((failures + 1))
	fi
}

# loaded <file> <table>: the nft -j listing of the table after loading the
# file into an empty network namespace.
loaded() {
	unshare -n sh -c "nft -f '$1' && nft -j list table inet $2"
}

env -u BRINGUP_HOSTS_FILE "$fw" render xweb 198.51.100.7 >"$work/xweb.nft"
env -u BRINGUP_HOSTS_FILE "$fw" render guard >"$work/guard.nft"
check fw_render_xweb_nft_c 'nft -c -f "$work/xweb.nft"'
check fw_render_guard_nft_c 'nft -c -f "$work/guard.nft"'
check fw_guard_ports 'grep -q "tcp dport { 1317, 8545, 8546, 9090, 26657, 5433, 5455 } drop" "$work/guard.nft" && ! grep -q 26656 "$work/guard.nft"'
check fw_state_xweb_match '[ "$(loaded "$work/xweb.nft" xweb | "$fw" state xweb 198.51.100.7)" = match ]'
check fw_state_guard_match '[ "$(loaded "$work/guard.nft" paxeer_validator_guard | "$fw" state guard)" = match ]'
check fw_state_xweb_wrong_peer '[ "$(loaded "$work/xweb.nft" xweb | "$fw" state xweb 198.51.100.8)" = peer0-8480=reset ]'

# The guard as it stood before: no postgres ports.
sed 's/, 5433, 5455//' "$work/guard.nft" >"$work/old-guard.nft"
check fw_state_guard_open_postgres '[ "$(loaded "$work/old-guard.nft" paxeer_validator_guard | "$fw" state guard)" = other-5433=accept ]'
# An input chain cannot close a Docker-published port (DNAT runs first).
sed 's/hook prerouting/hook input/' "$work/guard.nft" >"$work/input-guard.nft"
check fw_state_guard_input_hook '[ "$(loaded "$work/input-guard.nft" paxeer_validator_guard | "$fw" state guard)" = absent ]'
# A guard that also closes the p2p port is refused.
sed 's/5455 }/5455, 26656 }/' "$work/guard.nft" >"$work/p2p-guard.nft"
check fw_state_guard_p2p_closed '[ "$(loaded "$work/p2p-guard.nft" paxeer_validator_guard | "$fw" state guard)" = other-26656=drop ]'
check fw_state_absent '[ "$("$fw" state guard </dev/null)" = absent ]'
status=0
"$fw" render xweb >/dev/null 2>&1 || status=$?
check fw_usage '[ "$status" -eq 2 ]'

# Keyring migration against a real paxd keyring and a real backup archive.
if ! command -v age >/dev/null; then
	GOBIN="${XDG_CACHE_HOME:-$HOME/.cache}/paxeer-age/bin"
	[ -x "$GOBIN/age" ] || (cd "$work" && GOBIN=$GOBIN go install filippo.io/age/cmd/...@v1.2.1)
	PATH="$GOBIN:$PATH"
fi
home="$work/home"
mkdir -p "$home"
paxd keys add operator --home "$home" --keyring-backend test >/dev/null 2>&1
paxd keys add attestor --home "$home" --keyring-backend test >/dev/null 2>&1
printf 'fixture-passphrase-1\n' >"$work/pass"
chmod 600 "$work/pass"
test_keys="$(paxd keys list --home "$home" --keyring-backend test --output json | jq -r '.[] | "\(.name) \(.address)"' | sort)"
file_keys() {
	printf 'fixture-passphrase-1\n%.0s' 1 2 | paxd keys list --home "$1" --keyring-backend file --output json 2>/dev/null |
		jq -r '.[] | "\(.name) \(.address)"' | sort
}
age-keygen -o "$work/id.txt" 2>/dev/null
age-keygen -y "$work/id.txt" >"$work/recipients.txt"
cat >"$work/backup.env" <<ENV
BACKUP_HOST=
BACKUP_DIR=$work/store
BACKUP_PREFIX=fixture
BACKUP_RECIPIENTS_FILE=$work/recipients.txt
BACKUP_SOURCES="$home"
BACKUP_IDENTITY=$work/id.txt
ENV
run_migrate() {
	KEYRING_PASSPHRASE_FILE="$work/pass" BACKUP_ENV="$1" "$migrate" "$home" 2>&1
}

status=0
out="$(run_migrate "$work/backup.env")" || status=$?
check km_no_archive_kept '[ "$status" -eq 1 ] && [ "$out" = "home[0] keys=2 test=kept reason=backup:absent" ] && [ -d "$home/keyring-test" ]'
check km_file_keyring_holds_keys '[ "$(file_keys "$home")" = "$test_keys" ]'

"$repo/tools/ops/backup/backup.sh" "$work/backup.env" >/dev/null
sleep 1
paxd keys add late --home "$home" --keyring-backend test >/dev/null 2>&1
status=0
out="$(run_migrate "$work/backup.env")" || status=$?
check km_stale_archive_kept '[ "$status" -eq 1 ] && [ "$out" = "home[0] keys=3 test=kept reason=backup:stale" ] && [ -d "$home/keyring-test" ]'

test_keys="$(paxd keys list --home "$home" --keyring-backend test --output json | jq -r '.[] | "\(.name) \(.address)"' | sort)"
sleep 1
archive="$("$repo/tools/ops/backup/backup.sh" "$work/backup.env")"
status=0
out="$(run_migrate "$work/backup.env")" || status=$?
check km_verified_archive_removes '[ "$status" -eq 0 ] && [ "$out" = "home[0] keys=3 file=match backup=$archive test=removed" ] && [ ! -e "$home/keyring-test" ]'
check km_file_keyring_after '[ "$(file_keys "$home")" = "$test_keys" ]'
status=0
out="$(run_migrate "$work/backup.env")" || status=$?
check km_rerun_absent '[ "$status" -eq 0 ] && [ "$out" = "home[0] test=absent" ]'
status=0
"$migrate" "$home" >/dev/null 2>&1 || status=$?
check km_usage '[ "$status" -eq 2 ]'

if [ "$failures" -ne 0 ]; then
	echo "validator-firewall.test: $failures check(s) failed"
	exit 1
fi
echo "validator-firewall.test: all checks passed"
