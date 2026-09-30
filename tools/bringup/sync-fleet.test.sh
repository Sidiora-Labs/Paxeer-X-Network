#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
syncer="$root/tools/bringup/sync-fleet.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

for tool in timeout curl rsync tar sha256sum; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "sync-fleet.test: $tool is required" >&2
		exit 2
	fi
done
release_sha256="$(sed -n 's/^release_sha256=//p' "$syncer")"

# Every fixture host keeps its node home and binary under a directory of its
# own below this root; the ssh stand-in rewrites the shared paths into it.
hosts="$work/hosts"
export SYNC_FLEET_TEST_ROOT="$hosts"
export HPX_HOME="$hosts/.paxeer"
export PAXD="$hosts/paxd"
export SYNC_FLEET_TEST_CALLS="$work/calls"
export SYNC_FLEET_TEST_LAG_FILE="$work/lag"
SYNC_FLEET_TEST_REAL_CURL="$(command -v curl)"
SYNC_FLEET_TEST_REAL_RSYNC="$(command -v rsync)"
SYNC_FLEET_TEST_REAL_SHA256SUM="$(command -v sha256sum)"
SYNC_FLEET_TEST_REAL_DF="$(command -v df)"
export SYNC_FLEET_TEST_REAL_CURL SYNC_FLEET_TEST_REAL_RSYNC SYNC_FLEET_TEST_REAL_SHA256SUM SYNC_FLEET_TEST_REAL_DF

mkdir -p "$work/bin"

# ssh stand-in: answers for the fixture destinations only, refuses to run
# without BatchMode, honours -n, records every call, answers the probe's unit
# question from the destination's name, and runs any other command on this
# box with the shared paths moved under the destination's own directory. An
# inner ssh (one run by a remote command) to a destination listed in
# SYNC_FLEET_TEST_ISOLATED fails, as a source that cannot reach a target.
cat >"$work/bin/ssh" <<'SH'
#!/usr/bin/env bash
set -eu
batch=0
dest=""
command=""
while [ "$#" -gt 0 ]; do
	case "$1" in
	-n)
		exec </dev/null
		shift
		;;
	-o)
		[ "${2:-}" = BatchMode=yes ] && batch=1
		shift 2
		;;
	--)
		dest="${2:-}"
		command="${3:-}"
		break
		;;
	*) shift ;;
	esac
done
printf '%s %s\n' "$dest" "$command" >>"$SYNC_FLEET_TEST_CALLS"
[ "$batch" -eq 1 ] || exit 99
case "$dest" in
up-*) ;;
*) exit 255 ;;
esac
if [ "${SYNC_FLEET_TEST_INNER:-0}" -eq 1 ]; then
	case " ${SYNC_FLEET_TEST_ISOLATED:-} " in
	*" $dest "*) exit 255 ;;
	esac
fi
n="${dest#*-rpc-}"
n="${n%%-*}"
case "$command" in
true) exit 0 ;;
'n=$(ls /etc/nginx'*) echo "api$n active 7200" && exit 0 ;;
esac
command="${command//"$SYNC_FLEET_TEST_ROOT"/"$SYNC_FLEET_TEST_ROOT/$dest"}"
export SYNC_FLEET_TEST_INNER=1 SYNC_FLEET_TEST_DEST="$dest"
tee -a "$SYNC_FLEET_TEST_CALLS.stdin" | bash -c "$command"
SH

# systemctl stand-in: records the call for its destination as a unit line, reports active,
# and lets a started or restarted node catch up by dropping its lag from the
# lag file, unless the destination is listed in SYNC_FLEET_TEST_STUCK; a
# restart fails for a destination listed in SYNC_FLEET_TEST_UNIT_FAIL.
cat >"$work/bin/systemctl" <<'SH'
#!/usr/bin/env bash
set -eu
printf '%s unit %s\n' "${SYNC_FLEET_TEST_DEST:-local}" "$*" >>"$SYNC_FLEET_TEST_CALLS"
n="${SYNC_FLEET_TEST_DEST#*-rpc-}"
n="${n%%-*}"
case "$1" in
show) echo active ;;
is-active) echo active ;;
start | restart)
	case " ${SYNC_FLEET_TEST_UNIT_FAIL:-} " in
	*" ${SYNC_FLEET_TEST_DEST:-} "*) exit 1 ;;
	esac
	case " ${SYNC_FLEET_TEST_STUCK:-} " in
	*" ${SYNC_FLEET_TEST_DEST:-} "*) ;;
	*) sed -i "s/api$n:[0-9]*//" "$SYNC_FLEET_TEST_LAG_FILE" ;;
	esac
	;;
esac
SH

# curl stand-in: answers eth_blockNumber for the public names at the fixed
# head minus the lag the lag file ("apiN:blocks ...") assigns, fails to
# connect for the names in SYNC_FLEET_TEST_DOWN, and hands anything else to
# the real curl.
cat >"$work/bin/curl" <<'SH'
#!/usr/bin/env bash
set -eu
url=""
for arg in "$@"; do
	case "$arg" in
	https://*) url="$arg" ;;
	esac
done
[ -n "$url" ] || exec "$SYNC_FLEET_TEST_REAL_CURL" "$@"
name="${url#https://}"
name="${name%%.*}"
printf '%s curl\n' "$name" >>"$SYNC_FLEET_TEST_CALLS"
case " ${SYNC_FLEET_TEST_DOWN:-} " in
*" $name "*) exit 7 ;;
esac
lags="$(cat "$SYNC_FLEET_TEST_LAG_FILE" 2>/dev/null || true)"
lag=0
case " $lags " in
*" $name:"*)
	lag="${lags##*"$name:"}"
	lag="${lag%% *}"
	;;
esac
printf '{"jsonrpc":"2.0","id":1,"result":"0x%x"}\n' "$((26400000 - lag))"
SH

# remap <host:path> -> the path under that fixture host's own directory.
# shellcheck disable=SC2016
remap='
remap() {
	local host="${1%%:*}" path="${1#*:}" rel
	rel="${path#"$SYNC_FLEET_TEST_ROOT"/}"
	case "$rel" in
	up-*/* | down-*/*) rel="${rel#*/}" ;;
	esac
	printf "%s/%s/%s" "$SYNC_FLEET_TEST_ROOT" "$host" "$rel"
}'

# rsync stand-in: the real rsync with the remote target turned into the
# target host's fixture directory.
cat >"$work/bin/rsync" <<SH
#!/usr/bin/env bash
set -eu
$remap
printf '%s rsync %s\n' "\${SYNC_FLEET_TEST_DEST:-local}" "\$*" >>"\$SYNC_FLEET_TEST_CALLS"
args=()
for arg in "\$@"; do
	case "\$arg" in
	*:*) arg="\$(remap "\$arg")" && mkdir -p "\$arg" ;;
	esac
	args+=("\$arg")
done
exec "\$SYNC_FLEET_TEST_REAL_RSYNC" "\${args[@]}"
SH

# scp stand-in: copies the local file to the target host's fixture directory;
# fails for a destination listed in SYNC_FLEET_TEST_SCP_FAIL and appends a
# byte to the copy for one listed in SYNC_FLEET_TEST_CORRUPT.
cat >"$work/bin/scp" <<SH
#!/usr/bin/env bash
set -eu
$remap
printf 'local scp %s\n' "\$*" >>"\$SYNC_FLEET_TEST_CALLS"
src="\${*: -2:1}"
host="\${*: -1}"
host="\${host%%:*}"
case " \${SYNC_FLEET_TEST_SCP_FAIL:-} " in
*" \$host "*) exit 1 ;;
esac
dst="\$(remap "\${*: -1}")"
mkdir -p "\$(dirname "\$dst")"
cp "\$src" "\$dst"
case " \${SYNC_FLEET_TEST_CORRUPT:-} " in
*" \$host "*) printf x >>"\$dst" ;;
esac
SH

# sha256sum stand-in: the fixture release binary hashes to the release sha.
cat >"$work/bin/sha256sum" <<'SH'
#!/usr/bin/env bash
set -eu
for f in "$@"; do
	if [ "$(cat "$f")" = "release binary" ]; then
		echo "$SYNC_FLEET_TEST_RELEASE_SHA256  $f"
	else
		"$SYNC_FLEET_TEST_REAL_SHA256SUM" "$f"
	fi
done
SH

# df stand-in: a destination listed in SYNC_FLEET_TEST_FULL has one byte free.
cat >"$work/bin/df" <<'SH'
#!/usr/bin/env bash
set -eu
case " ${SYNC_FLEET_TEST_FULL:-} " in
*" ${SYNC_FLEET_TEST_DEST:-} "*)
	printf 'Avail\n1\n'
	;;
*) exec "$SYNC_FLEET_TEST_REAL_DF" "$@" ;;
esac
SH
chmod +x "$work/bin/"*
export PATH="$work/bin:$PATH"
export SYNC_FLEET_TEST_RELEASE_SHA256="$release_sha256"

# Fixture hosts: api1 synced, api2 the archive trailing at chain speed, api3
# within the threshold, api4 and api6 beyond the retain window (api6 on an old
# binary and unreachable from api1), api5 a validator host's full node, api8
# a synced archive host outside the RPC list, api9 without the lag keys.
config() {
	mkdir -p "$hosts/$1/.paxeer/config"
	cat >"$hosts/$1/.paxeer/config/config.toml" <<'TOML'
[p2p]
laddr = "tcp://0.0.0.0:26656"

[self-remediation]
p2p-no-peers-available-window-seconds = 0
blocks-behind-threshold = 0
blocks-behind-check-interval = 60
restart-cooldown-seconds = 60
autobahn-config-file = ""

[other]
blocks-behind-threshold = 7
TOML
}
for h in up-rpc-1 up-rpc-2 up-rpc-3 up-rpc-4 up-rpc-5 up-rpc-6 up-rpc-8; do
	config "$h"
	printf 'release binary' >"$hosts/$h/paxd"
done
mkdir -p "$hosts/up-rpc-9/.paxeer/config"
printf '[p2p]\nladdr = "tcp://0.0.0.0:26656"\n\n[self-remediation]\np2p-no-peers-available-window-seconds = 0\n' >"$hosts/up-rpc-9/.paxeer/config/config.toml"
printf 'old binary' >"$hosts/up-rpc-6/paxd"
printf 'release binary' >"$work/release"
printf 'not the release' >"$work/other"

data() {
	local d="$hosts/$1/.paxeer/data"
	mkdir -p "$d/tendermint/blockstore.db" "$d/tendermint/state.db" "$d/state_store" "$d/snapshots/1"
	printf '%s blocks' "$2" >"$d/tendermint/blockstore.db/CURRENT"
	printf '%s snapshot' "$2" >"$d/snapshots/1/metadata"
	printf '{"height":"%s"}' "$3" >"$d/priv_validator_state.json"
}
data up-rpc-1 synced 26400000
data up-rpc-4 stale 26502089
data up-rpc-6 frozen 25070653

cat >"$work/hosts-good.env" <<'ENV'
EDGE_HOST=up-edge
ARCHIVE_HOST=up-rpc-2
VALIDATOR_HOSTS="up-rpc-5 up-validator-b"
RPC_HOSTS="up-rpc-1 up-rpc-2 up-rpc-3 up-rpc-4 up-rpc-5 up-rpc-6"
OLD_WALLET_HOST=up-old-wallet
ENV

cat >"$work/hosts-apart.env" <<'ENV'
EDGE_HOST=up-edge
ARCHIVE_HOST=up-rpc-8
VALIDATOR_HOSTS="up-rpc-5 up-validator-b"
RPC_HOSTS="up-rpc-1 up-rpc-3"
OLD_WALLET_HOST=up-old-wallet
ENV

cat >"$work/hosts-bad.env" <<'ENV'
EDGE_HOST=up-edge
ARCHIVE_HOST=up-rpc-1
VALIDATOR_HOSTS="up-validator-a up-validator-b"
RPC_HOSTS="up-rpc-1 up-rpc-9 down-rpc-7"
OLD_WALLET_HOST=up-old-wallet
ENV

cat >"$work/hosts-refuse.env" <<'ENV'
EDGE_HOST=up-edge
ARCHIVE_HOST=up-rpc-4
VALIDATOR_HOSTS="up-rpc-6 up-validator-b"
RPC_HOSTS="up-rpc-1 up-rpc-4 up-rpc-6"
OLD_WALLET_HOST=up-old-wallet
ENV

cat >"$work/hosts-missing.env" <<'ENV'
EDGE_HOST=up-edge
VALIDATOR_HOSTS="up-validator-a up-validator-b"
RPC_HOSTS="up-rpc-1"
OLD_WALLET_HOST=up-old-wallet
ENV

failures=0
lags="api2:93474 api3:100 api4:529862 api6:1961298"

# expect <name> <hosts file or -> <want exit> <args...> -- <lines...>: runs the
# script with the fixture (or with BRINGUP_HOSTS_FILE unset for -), the lag
# file reset, and wants the exit code, every line, and no fixture destination
# in the output.
expect() {
	local name="$1" hosts_file="$2" want_status="$3" output status=0 ok=1 line
	shift 3
	local args=()
	while [ "$#" -gt 0 ] && [ "$1" != -- ]; do
		args+=("$1")
		shift
	done
	shift
	: >"$SYNC_FLEET_TEST_CALLS"
	printf '%s' "$lags" >"$SYNC_FLEET_TEST_LAG_FILE"
	if [ "$hosts_file" = - ]; then
		output="$(env -u BRINGUP_HOSTS_FILE CHECK_LIVE_TIMEOUT=5 SYNC_FLEET_SETTLE=0 "$syncer" ${args[@]+"${args[@]}"} 2>&1)" || status=$?
	else
		output="$(BRINGUP_HOSTS_FILE="$hosts_file" CHECK_LIVE_TIMEOUT=5 SYNC_FLEET_SETTLE=0 "$syncer" ${args[@]+"${args[@]}"} 2>&1)" || status=$?
	fi
	[ "$status" -eq "$want_status" ] || ok=0
	for line in "$@"; do
		grep -qF -- "$line" <<<"$output" || ok=0
	done
	if grep -qE -- '\b(up|down|hang)-[a-z]' <<<"$output"; then
		ok=0
	fi
	if [ "$ok" -eq 1 ]; then
		echo "ok   $name"
	else
		echo "FAIL $name: want exit $want_status with lines [$*] and no destination, got exit $status"
		printf '%s\n' "$output"
		failures=$((failures + 1))
	fi
}

# check <name> <condition...>: one fixture assertion.
check() {
	local name="$1"
	shift
	if "$@"; then
		echo "ok   $name"
	else
		echo "FAIL $name"
		failures=$((failures + 1))
	fi
}

expect sync_fleet_no_subcommand "$work/hosts-good.env" 2 -- \
	"usage: tools/bringup/sync-fleet.sh"

expect sync_fleet_unknown_subcommand "$work/hosts-good.env" 2 nonexistent -- \
	"usage: tools/bringup/sync-fleet.sh"

expect sync_fleet_extra_argument "$work/hosts-good.env" 2 config extra -- \
	"usage: tools/bringup/sync-fleet.sh"

expect sync_fleet_help "$work/hosts-good.env" 0 --help -- \
	"usage: tools/bringup/sync-fleet.sh" \
	"config" \
	"restart" \
	"resync <paxd>" \
	"--dry-run" \
	"BRINGUP_HOSTS_FILE"

expect sync_fleet_hosts_file_unset - 2 config -- \
	"check-live: BRINGUP_HOSTS_FILE is unset"

expect sync_fleet_hosts_file_lacks_role "$work/hosts-missing.env" 2 restart -- \
	"check-live: BRINGUP_HOSTS_FILE lacks ARCHIVE_HOST"

expect sync_fleet_resync_without_binary "$work/hosts-good.env" 2 resync -- \
	"usage: tools/bringup/sync-fleet.sh"

expect sync_fleet_resync_missing_binary "$work/hosts-good.env" 2 resync "$work/absent" -- \
	"is not a file"

expect sync_fleet_resync_refuses_another_binary "$work/hosts-good.env" 2 resync "$work/other" -- \
	"not the release $release_sha256"
check sync_fleet_resync_refusal_asks_no_host [ ! -s "$SYNC_FLEET_TEST_CALLS" ]

expect sync_fleet_config_dry_run "$work/hosts-good.env" 0 --dry-run config -- \
	"ssh -- RPC_HOSTS[0] set -e; cp -p $HPX_HOME/config/config.toml $HPX_HOME/config/config.toml.bak-" \
	'sed -i "/^\[self-remediation\]/,/^\[/{s/^blocks-behind-threshold = .*/blocks-behind-threshold = 200/;s/^blocks-behind-check-interval = .*/blocks-behind-check-interval = 30/;s/^restart-cooldown-seconds = .*/restart-cooldown-seconds = 300/;}"' \
	"ssh -- RPC_HOSTS[5] set -e; cp -p" \
	"sync-fleet: all hosts passed"
check sync_fleet_config_dry_run_changes_nothing bash -c "! ls $hosts/*/.paxeer/config/config.toml.bak-* >/dev/null 2>&1 && grep -q '^blocks-behind-threshold = 0' $hosts/up-rpc-1/.paxeer/config/config.toml"

expect sync_fleet_config_passing "$work/hosts-good.env" 0 config -- \
	"pass config RPC_HOSTS[0] api1 blocks-behind-threshold=200 blocks-behind-check-interval=30 restart-cooldown-seconds=300 backup=config.toml.bak-" \
	"pass config RPC_HOSTS[1] api2 blocks-behind-threshold=200" \
	"pass config RPC_HOSTS[5] api6 blocks-behind-threshold=200 blocks-behind-check-interval=30 restart-cooldown-seconds=300 backup=config.toml.bak-" \
	"unit=active" \
	"sync-fleet: all hosts passed"
cfg="$hosts/up-rpc-4/.paxeer/config/config.toml"
check sync_fleet_config_writes_the_section bash -c "[ \"\$(sed -n '/^\[self-remediation\]/,/^\[/p' '$cfg' | grep -c -e '^blocks-behind-threshold = 200\$' -e '^blocks-behind-check-interval = 30\$' -e '^restart-cooldown-seconds = 300\$')\" -eq 3 ]"
check sync_fleet_config_leaves_other_sections bash -c "grep -q '^p2p-no-peers-available-window-seconds = 0\$' '$cfg' && [ \"\$(sed -n '/^\[other\]/,\$p' '$cfg' | grep -c '^blocks-behind-threshold = 7\$')\" -eq 1 ]"
check sync_fleet_config_keeps_a_backup bash -c "b=\$(ls '$cfg'.bak-* | head -n 1) && grep -q '^blocks-behind-threshold = 0\$' \"\$b\" && [ \"\$(ls '$hosts'/*/.paxeer/config/config.toml.bak-* | wc -l)\" -eq 6 ]"
check sync_fleet_config_never_restarts bash -c "! grep -q 'systemctl restart' '$SYNC_FLEET_TEST_CALLS'"

expect sync_fleet_config_archive_apart "$work/hosts-apart.env" 0 config -- \
	"pass config RPC_HOSTS[0] api1 blocks-behind-threshold=200" \
	"pass config RPC_HOSTS[1] api3 blocks-behind-threshold=200" \
	"pass config ARCHIVE_HOST api8 blocks-behind-threshold=200 blocks-behind-check-interval=30 restart-cooldown-seconds=300 backup=config.toml.bak-" \
	"sync-fleet: all hosts passed"

expect sync_fleet_config_failing "$work/hosts-bad.env" 1 config -- \
	"pass config RPC_HOSTS[0] api1 blocks-behind-threshold=200" \
	"fail config RPC_HOSTS[1] api9 wrote= want=blocks-behind-threshold=200 blocks-behind-check-interval=30 restart-cooldown-seconds=300" \
	"fail config RPC_HOSTS[2] none ssh=255" \
	"sync-fleet: 2 host(s) failed"

expect sync_fleet_restart_dry_run "$work/hosts-good.env" 0 --dry-run restart -- \
	"keep RPC_HOSTS[0] api1 lag=0" \
	"ssh -- RPC_HOSTS[1] systemctl restart paxd.service && systemctl show paxd.service -p ActiveState --value" \
	"keep RPC_HOSTS[2] api3 lag=100" \
	"skip RPC_HOSTS[3] api4 lag=529862 beyond the retain window" \
	"keep RPC_HOSTS[4] api5 lag=0" \
	"skip RPC_HOSTS[5] api6 lag=1961298 beyond the retain window" \
	"sync-fleet: all hosts passed"
check sync_fleet_restart_dry_run_restarts_nothing bash -c "! grep -q 'systemctl restart' '$SYNC_FLEET_TEST_CALLS'"

expect sync_fleet_restart_passing "$work/hosts-good.env" 0 restart -- \
	"keep RPC_HOSTS[0] api1 lag=0" \
	"restart RPC_HOSTS[1] api2 lag=93474 unit=active" \
	"keep RPC_HOSTS[2] api3 lag=100" \
	"skip RPC_HOSTS[3] api4 lag=529862 beyond the retain window" \
	"skip RPC_HOSTS[5] api6 lag=1961298 beyond the retain window" \
	"sync-fleet: all hosts passed"
check sync_fleet_restart_only_the_trailing_node bash -c "[ \"\$(grep -c ' unit restart paxd.service$' '$SYNC_FLEET_TEST_CALLS')\" -eq 1 ] && grep -q '^up-rpc-2 unit restart paxd.service$' '$SYNC_FLEET_TEST_CALLS' && ! grep -q 'paxd-a' '$SYNC_FLEET_TEST_CALLS'"
check sync_fleet_restart_asks_every_name_at_once bash -c "[ \"\$(grep -c ' curl\$' '$SYNC_FLEET_TEST_CALLS')\" -eq 16 ]"

expect sync_fleet_restart_failing "$work/hosts-bad.env" 1 restart -- \
	"keep RPC_HOSTS[0] api1 lag=0" \
	"keep RPC_HOSTS[1] api9 lag=0" \
	"fail RPC_HOSTS[2] ssh=255" \
	"sync-fleet: 1 host(s) failed"

SYNC_FLEET_TEST_ISOLATED=up-rpc-6 expect sync_fleet_resync_dry_run "$work/hosts-good.env" 0 --dry-run resync "$work/release" -- \
	"ssh -- RPC_HOSTS[0] systemctl stop paxd.service" \
	"ssh -- RPC_HOSTS[0] rsync -a --partial --exclude=priv_validator_state.json --exclude=snapshots $HPX_HOME/data/ RPC_HOSTS[3]:$HPX_HOME/data.incoming-" \
	"ssh -- RPC_HOSTS[0] systemctl start paxd.service" \
	"wait until api1 is within 10 blocks of the head" \
	"ssh -- RPC_HOSTS[3] set -e; test -d $HPX_HOME/data.incoming-" \
	"resync RPC_HOSTS[3] api4 source=api1 mode=rsync binary=kept stale=data.stale-" \
	"ssh -n -- RPC_HOSTS[0] tar -C $HPX_HOME --exclude=priv_validator_state.json --exclude=snapshots -cf - data | ssh -- RPC_HOSTS[5] mkdir -p $HPX_HOME/data.incoming-" \
	"scp -q -- $work/release RPC_HOSTS[5]:$PAXD.new-" \
	"cp -p $PAXD $PAXD.pre-" \
	"resync RPC_HOSTS[5] api6 source=api1 mode=stream binary=installed stale=data.stale-" \
	"sync-fleet: all hosts passed"
check sync_fleet_resync_dry_run_moves_nothing bash -c "! grep -q 'systemctl stop' '$SYNC_FLEET_TEST_CALLS' && [ ! -e '$hosts/up-rpc-4/.paxeer/data.stale-'* ] 2>/dev/null; [ \"\$(cat '$hosts/up-rpc-6/paxd')\" = 'old binary' ] && grep -q 'stale blocks' '$hosts/up-rpc-4/.paxeer/data/tendermint/blockstore.db/CURRENT'"

SYNC_FLEET_TEST_ISOLATED=up-rpc-6 expect sync_fleet_resync_passing "$work/hosts-good.env" 0 resync "$work/release" -- \
	"resync RPC_HOSTS[3] api4 source=api1 mode=rsync binary=kept stale=data.stale-" \
	"resync RPC_HOSTS[5] api6 source=api1 mode=stream binary=installed stale=data.stale-" \
	"unit=active lag=0" \
	"sync-fleet: all hosts passed"
d4="$hosts/up-rpc-4/.paxeer"
d6="$hosts/up-rpc-6/.paxeer"
check sync_fleet_resync_copies_the_data bash -c "grep -q 'synced blocks' '$d4/data/tendermint/blockstore.db/CURRENT' && [ -d '$d4/data/tendermint/state.db' ] && [ -d '$d4/data/state_store' ] && grep -q 'synced blocks' '$d6/data/tendermint/blockstore.db/CURRENT'"
check sync_fleet_resync_keeps_the_targets_own_state bash -c "grep -q 26502089 '$d4/data/priv_validator_state.json' && grep -q 25070653 '$d6/data/priv_validator_state.json' && [ ! -e '$d4/data/snapshots' ] && [ ! -e '$d6/data/snapshots' ]"
check sync_fleet_resync_keeps_the_stale_copy bash -c "grep -q 'stale blocks' '$d4'/data.stale-*/tendermint/blockstore.db/CURRENT && grep -q 'frozen blocks' '$d6'/data.stale-*/tendermint/blockstore.db/CURRENT && [ ! -e '$d4'/data.incoming-* ]"
check sync_fleet_resync_installs_the_release_where_it_differs bash -c "[ \"\$(cat '$hosts/up-rpc-6/paxd')\" = 'release binary' ] && [ \"\$(cat '$hosts'/up-rpc-6/paxd.pre-*)\" = 'old binary' ] && [ ! -e '$hosts'/up-rpc-4/paxd.pre-* ] && [ \"\$(grep -c '^local scp' '$SYNC_FLEET_TEST_CALLS')\" -eq 1 ]"
check sync_fleet_resync_stops_the_source_only_around_the_copy bash -c "c='$SYNC_FLEET_TEST_CALLS'; s=\$(grep -n '^up-rpc-1 systemctl stop' \"\$c\" | head -n 1 | cut -d: -f1); r=\$(grep -n '^up-rpc-1 rsync' \"\$c\" | head -n 1 | cut -d: -f1); t=\$(grep -n '^up-rpc-1 systemctl start' \"\$c\" | head -n 1 | cut -d: -f1); [ -n \"\$s\" ] && [ -n \"\$r\" ] && [ -n \"\$t\" ] && [ \"\$s\" -lt \"\$r\" ] && [ \"\$r\" -lt \"\$t\" ] && [ \"\$(grep -c '^up-rpc-1 systemctl stop' \"\$c\")\" -eq 2 ] && [ \"\$(grep -c '^up-rpc-1 systemctl start' \"\$c\")\" -eq 2 ]"
check sync_fleet_resync_never_touches_the_source_data bash -c "grep -q 'synced blocks' '$hosts/up-rpc-1/.paxeer/data/tendermint/blockstore.db/CURRENT' && [ -e '$hosts/up-rpc-1/.paxeer/data/snapshots/1/metadata' ] && [ ! -e '$hosts'/up-rpc-1/.paxeer/data.stale-* ]"

expect sync_fleet_resync_nothing_beyond_the_window "$work/hosts-apart.env" 0 resync "$work/release" -- \
	"resync none beyond the retain window" \
	"sync-fleet: all hosts passed"

expect sync_fleet_resync_refuses_archive_and_validator "$work/hosts-refuse.env" 1 resync "$work/release" -- \
	"fail resync RPC_HOSTS[1] api4 the archive host keeps its history" \
	"fail resync RPC_HOSTS[2] api6 a validator host is never resynced" \
	"sync-fleet: 2 host(s) failed"
check sync_fleet_resync_refusal_stops_nothing bash -c "! grep -q 'systemctl stop' '$SYNC_FLEET_TEST_CALLS'"

data up-rpc-4 stale 26502089
SYNC_FLEET_TEST_FULL=up-rpc-4 expect sync_fleet_resync_refuses_a_full_target "$work/hosts-good.env" 1 resync "$work/release" -- \
	"fail resync RPC_HOSTS[3] api4 source=api1 free=1 need=" \
	"resync RPC_HOSTS[5] api6 source=api1 mode=rsync binary=kept stale=data.stale-" \
	"sync-fleet: 1 host(s) failed"

data up-rpc-4 stale 26502089
SYNC_FLEET_TEST_STUCK=up-rpc-4 expect sync_fleet_resync_reports_a_target_that_stays_behind "$work/hosts-good.env" 1 resync "$work/release" -- \
	"fail resync RPC_HOSTS[3] api4 source=api1 mode=rsync binary=kept stale=data.stale-" \
	"unit=active lag=529862" \
	"sync-fleet: 1 host(s) failed"

# Rollout fixture: three full nodes on the release binary, api2 the archive
# trailing at chain speed and api3 within the threshold, no validator among
# them; the rollout binary hashes to its real sha256.
printf 'rollout binary' >"$work/rollout"
rollout_sha="$("$SYNC_FLEET_TEST_REAL_SHA256SUM" "$work/rollout" | cut -d' ' -f1)"
export SYNC_FLEET_REPORT="$work/rollout-report"
cat >"$work/hosts-rollout.env" <<'ENV'
EDGE_HOST=up-edge
ARCHIVE_HOST=up-rpc-2
VALIDATOR_HOSTS="up-validator-a up-validator-b"
RPC_HOSTS="up-rpc-1 up-rpc-2 up-rpc-3"
HPX_HOST=up-hpx
OLD_WALLET_HOST=up-old-wallet
ENV
rollout_hosts() {
	local h
	rm -f "$SYNC_FLEET_REPORT"
	for h in up-rpc-1 up-rpc-2 up-rpc-3; do
		rm -f "${hosts:?}/$h/paxd".*
		printf 'release binary' >"$hosts/$h/paxd"
	done
}
# binaries: how many rollout fixture hosts hold the rollout binary.
binaries() {
	local h n=0
	for h in up-rpc-1 up-rpc-2 up-rpc-3; do
		[ "$(cat "$hosts/$h/paxd" 2>/dev/null)" != "rollout binary" ] || n=$((n + 1))
	done
	echo "$n"
}

expect sync_fleet_rollout_without_sha "$work/hosts-rollout.env" 2 rollout "$work/rollout" -- \
	"usage: tools/bringup/sync-fleet.sh"

expect sync_fleet_rollout_malformed_sha "$work/hosts-rollout.env" 2 rollout "$work/rollout" "${rollout_sha^^}" -- \
	"is not a lowercase hex sha256"

expect sync_fleet_rollout_refuses_a_binary_off_its_sha "$work/hosts-rollout.env" 2 rollout "$work/other" "$rollout_sha" -- \
	"not the expected $rollout_sha"
check sync_fleet_rollout_sha_refusal_asks_no_host [ ! -s "$SYNC_FLEET_TEST_CALLS" ]

expect sync_fleet_resync_still_pins_the_release "$work/hosts-rollout.env" 2 resync "$work/rollout" -- \
	"not the release $release_sha256"

rollout_hosts
expect sync_fleet_rollout_refuses_a_validator_destination "$work/hosts-good.env" 1 rollout "$work/rollout" "$rollout_sha" -- \
	"fail rollout RPC_HOSTS[4] refused: the destination is also a validator host, whose validator units run the same paxd; nothing was staged on any host" \
	"sync-fleet: 1 host(s) failed"
check sync_fleet_rollout_validator_refusal_touches_no_host bash -c "[ ! -s '$SYNC_FLEET_TEST_CALLS' ] && [ ! -e '$SYNC_FLEET_REPORT' ] && ! ls '$hosts'/*/paxd.new-* >/dev/null 2>&1"

rollout_hosts
expect sync_fleet_rollout_dry_run "$work/hosts-rollout.env" 0 --dry-run rollout "$work/rollout" "$rollout_sha" -- \
	"scp -q -- $work/rollout RPC_HOSTS[0]:$PAXD.new-" \
	"ssh -- RPC_HOSTS[2] set -e; chmod 0755 $PAXD.new-" \
	"ssh -- RPC_HOSTS[1] set -e; [ \"\$(sha256sum $PAXD.new-" \
	"cp -p $PAXD $PAXD.pre-" \
	"systemctl restart paxd.service" \
	"wait until api2 is within 10 blocks of the head" \
	"record the sha256 of $PAXD on every host into $SYNC_FLEET_REPORT" \
	"sync-fleet: all hosts passed"
check sync_fleet_rollout_dry_run_changes_nothing bash -c "[ \"\$(grep -c ' unit restart ' '$SYNC_FLEET_TEST_CALLS')\" -eq 0 ] && ! grep -q '^local scp' '$SYNC_FLEET_TEST_CALLS' && [ ! -e '$SYNC_FLEET_REPORT' ] && [ '$(binaries)' -eq 0 ]"

rollout_hosts
expect sync_fleet_rollout_passing "$work/hosts-rollout.env" 0 rollout "$work/rollout" "$rollout_sha" -- \
	"staged RPC_HOSTS[0] api1 sha256=$rollout_sha" \
	"staged RPC_HOSTS[2] api3 sha256=$rollout_sha" \
	"swapped RPC_HOSTS[0] api1 unit=active lag=0" \
	"swapped RPC_HOSTS[1] api2 unit=active lag=0" \
	"swapped RPC_HOSTS[2] api3 unit=active lag=0" \
	"report $SYNC_FLEET_REPORT" \
	"sync-fleet: all hosts passed"
c="$SYNC_FLEET_TEST_CALLS"
check sync_fleet_rollout_installs_the_binary_everywhere [ "$(binaries)" -eq 3 ]
check sync_fleet_rollout_keeps_the_previous_binary bash -c "for h in up-rpc-1 up-rpc-2 up-rpc-3; do [ \"\$(cat '$hosts'/\$h/paxd.pre-*)\" = 'release binary' ] || exit 1; done; ! ls '$hosts'/*/paxd.new-* >/dev/null 2>&1"
check sync_fleet_rollout_stages_everywhere_before_the_first_swap bash -c "s=\$(grep -n '^local scp' '$c' | tail -n 1 | cut -d: -f1); r=\$(grep -n ' unit restart ' '$c' | head -n 1 | cut -d: -f1); [ \"\$(grep -c '^local scp' '$c')\" -eq 3 ] && [ \"\$s\" -lt \"\$r\" ]"
check sync_fleet_rollout_restarts_one_host_at_a_time_in_order bash -c "[ \"\$(grep ' unit restart ' '$c' | cut -d' ' -f1 | tr '\n' ' ')\" = 'up-rpc-1 up-rpc-2 up-rpc-3 ' ] && grep -q '^up-rpc-2 unit restart paxd.service\$' '$c'"
check sync_fleet_rollout_waits_for_each_node_before_the_next bash -c "a=\$(grep -n '^up-rpc-2 unit restart' '$c' | cut -d: -f1); b=\$(grep -n '^up-rpc-3 unit restart' '$c' | cut -d: -f1); [ \"\$(sed -n \"\${a},\${b}p\" '$c' | grep -c ' curl\$')\" -eq 16 ]"
check sync_fleet_rollout_records_every_host bash -c "[ \"\$(grep -c '^RPC_HOSTS.* sha256=$rollout_sha\$' '$SYNC_FLEET_REPORT')\" -eq 3 ] && grep -q '^RPC_HOSTS\[1\] api2 sha256=$rollout_sha\$' '$SYNC_FLEET_REPORT' && ! grep -qE '\b(up|down|hang)-' '$SYNC_FLEET_REPORT'"
check sync_fleet_rollout_never_touches_a_validator_unit bash -c "! grep -q 'paxd-a' '$c' && ! grep -q '^up-validator' '$c'"

rollout_hosts
SYNC_FLEET_TEST_CORRUPT=up-rpc-2 expect sync_fleet_rollout_stops_on_a_checksum_mismatch "$work/hosts-rollout.env" 1 rollout "$work/rollout" "$rollout_sha" -- \
	"staged RPC_HOSTS[0] api1 sha256=$rollout_sha" \
	"fail rollout RPC_HOSTS[1] api2 verify sha256=" \
	"want=$rollout_sha" \
	"report $SYNC_FLEET_REPORT" \
	"sync-fleet: 1 host(s) failed"
check sync_fleet_rollout_mismatch_swaps_nothing bash -c "! grep -q ' unit restart ' '$SYNC_FLEET_TEST_CALLS' && [ \"\$(grep -c '^local scp' '$SYNC_FLEET_TEST_CALLS')\" -eq 2 ] && [ '$(binaries)' -eq 0 ] && [ \"\$(grep -c 'sha256=$release_sha256\$' '$SYNC_FLEET_REPORT')\" -eq 3 ]"

rollout_hosts
SYNC_FLEET_TEST_STUCK=up-rpc-2 expect sync_fleet_rollout_stops_on_a_node_that_stays_behind "$work/hosts-rollout.env" 1 rollout "$work/rollout" "$rollout_sha" -- \
	"swapped RPC_HOSTS[0] api1 unit=active lag=0" \
	"fail rollout RPC_HOSTS[1] api2 wait lag=93474 unit=active" \
	"sync-fleet: 1 host(s) failed"
check sync_fleet_rollout_lag_stop_leaves_the_next_host bash -c "! grep -q '^up-rpc-3 unit restart' '$SYNC_FLEET_TEST_CALLS' && [ \"\$(cat '$hosts/up-rpc-3/paxd')\" = 'release binary' ] && grep -q '^RPC_HOSTS\[2\] api3 sha256=$release_sha256\$' '$SYNC_FLEET_REPORT' && grep -q '^RPC_HOSTS\[1\] api2 sha256=$rollout_sha\$' '$SYNC_FLEET_REPORT'"

rollout_hosts
SYNC_FLEET_TEST_SCP_FAIL=up-rpc-3 expect sync_fleet_rollout_stops_on_a_failed_copy "$work/hosts-rollout.env" 1 rollout "$work/rollout" "$rollout_sha" -- \
	"staged RPC_HOSTS[1] api2 sha256=$rollout_sha" \
	"fail rollout RPC_HOSTS[2] api3 stage scp=1" \
	"sync-fleet: 1 host(s) failed"
check sync_fleet_rollout_copy_failure_swaps_nothing bash -c "! grep -q ' unit restart ' '$SYNC_FLEET_TEST_CALLS' && [ '$(binaries)' -eq 0 ]"

rollout_hosts
SYNC_FLEET_TEST_UNIT_FAIL=up-rpc-2 expect sync_fleet_rollout_stops_on_a_failed_swap "$work/hosts-rollout.env" 1 rollout "$work/rollout" "$rollout_sha" -- \
	"swapped RPC_HOSTS[0] api1 unit=active lag=0" \
	"fail rollout RPC_HOSTS[1] api2 swap ssh=1" \
	"report $SYNC_FLEET_REPORT" \
	"sync-fleet: 1 host(s) failed"
check sync_fleet_rollout_swap_failure_leaves_the_next_host bash -c "! grep -q '^up-rpc-3 unit restart' '$SYNC_FLEET_TEST_CALLS' && ! grep -q '^up-rpc-2 unit show' '$SYNC_FLEET_TEST_CALLS' && [ \"\$(cat '$hosts/up-rpc-3/paxd')\" = 'release binary' ] && grep -q '^RPC_HOSTS\[1\] api2 sha256=$rollout_sha\$' '$SYNC_FLEET_REPORT' && grep -q '^RPC_HOSTS\[2\] api3 sha256=$release_sha256\$' '$SYNC_FLEET_REPORT'"

expect sync_fleet_rollout_stops_on_an_unreachable_host "$work/hosts-bad.env" 1 rollout "$work/rollout" "$rollout_sha" -- \
	"fail rollout RPC_HOSTS[2] none survey ssh=255"
check sync_fleet_rollout_unreachable_host_stages_nothing bash -c "! grep -q '^local scp' '$SYNC_FLEET_TEST_CALLS'"

if [ "$failures" -ne 0 ]; then
	echo "sync-fleet.test: $failures case(s) failed"
	exit 1
fi
echo "sync-fleet.test: all cases passed"
