#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
checker="$root/tools/bringup/check-live.sh"
work="$(mktemp -d)"

cleanup() {
	rm -rf "$work"
}
trap cleanup EXIT

if ! command -v timeout >/dev/null 2>&1; then
	echo "check-live.test: timeout is required" >&2
	exit 2
fi

# A local ssh stand-in ahead of the real one on PATH: it answers for the
# fixture destinations only, refuses to run without BatchMode, and records
# every call so the test can count one call per destination.
mkdir -p "$work/bin"
cat >"$work/bin/ssh" <<'SH'
#!/usr/bin/env bash
set -eu
batch=0
dest=""
command=""
while [ "$#" -gt 0 ]; do
	case "$1" in
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
printf '%s %s\n' "$dest" "$command" >>"$CHECK_LIVE_TEST_CALLS"
[ "$batch" -eq 1 ] || exit 99
case "$command" in
true) ;;
*systemctl*)
	n="${dest#*-rpc-}"
	n="${n%%-*}"
	case "$dest" in
	up-rpc-*-fresh) echo "api$n active 120" ;;
	up-rpc-*-dead) echo "api$n failed 0" ;;
	up-rpc-*) echo "api$n active 7200" ;;
	up-*) echo "none none 0" ;;
	esac
	;;
*) exit 98 ;;
esac
case "$dest" in
up-*) exit 0 ;;
hang-*) exec sleep 5 ;;
*) exit 255 ;;
esac
SH
chmod +x "$work/bin/ssh"

# A local curl stand-in: answers eth_blockNumber for the public names, at the
# fixed head minus the lag CHECK_LIVE_TEST_LAG ("apiN:blocks ...") assigns,
# and fails to connect for the names in CHECK_LIVE_TEST_DOWN.
cat >"$work/bin/curl" <<'SH'
#!/usr/bin/env bash
set -eu
url=""
for arg in "$@"; do
	case "$arg" in
	https://*) url="$arg" ;;
	esac
done
name="${url#https://}"
name="${name%%.*}"
printf '%s curl\n' "$name" >>"$CHECK_LIVE_TEST_CALLS"
case " ${CHECK_LIVE_TEST_DOWN:-} " in
*" $name "*) exit 7 ;;
esac
lag=0
case " ${CHECK_LIVE_TEST_LAG:-} " in
*" $name:"*)
	lag="${CHECK_LIVE_TEST_LAG##*"$name:"}"
	lag="${lag%% *}"
	;;
esac
printf '{"jsonrpc":"2.0","id":1,"result":"0x%x"}\n' "$((26400000 - lag))"
SH
chmod +x "$work/bin/curl"
export PATH="$work/bin:$PATH"
export CHECK_LIVE_TEST_CALLS="$work/calls"

cat >"$work/hosts-good.env" <<'ENV'
EDGE_HOST=up-edge
KERNEL_HOST=up-kernel
PLATFORM_HOST=up-platform
EXPLORER_HOST=up-explorer
ARCHIVE_HOST=up-archive
VALIDATOR_HOSTS="up-validator-a up-validator-b"
RPC_HOSTS="up-rpc-1 up-rpc-2 up-rpc-3"
HPX_HOST=up-hpx
ENV

cat >"$work/hosts-down.env" <<'ENV'
EDGE_HOST=up-edge
KERNEL_HOST=down-kernel
PLATFORM_HOST=up-platform
EXPLORER_HOST=up-explorer
ARCHIVE_HOST=up-archive
VALIDATOR_HOSTS="up-validator-a up-validator-b"
RPC_HOSTS="up-rpc-1 down-rpc-2 up-rpc-3"
HPX_HOST=up-hpx
ENV

cat >"$work/hosts-hang.env" <<'ENV'
EDGE_HOST=up-edge
KERNEL_HOST=up-kernel
PLATFORM_HOST=up-platform
EXPLORER_HOST=up-explorer
ARCHIVE_HOST=up-archive
VALIDATOR_HOSTS="up-validator-a up-validator-b"
RPC_HOSTS="up-rpc-1 up-rpc-2 up-rpc-3"
HPX_HOST=hang-hpx
ENV

cat >"$work/hosts-missing.env" <<'ENV'
EDGE_HOST=up-edge
KERNEL_HOST=up-kernel
PLATFORM_HOST=up-platform
EXPLORER_HOST=up-explorer
VALIDATOR_HOSTS="up-validator-a up-validator-b"
RPC_HOSTS="up-rpc-1 up-rpc-2 up-rpc-3"
HPX_HOST=up-hpx
ENV

cat >"$work/hosts-rpc-good.env" <<'ENV'
EDGE_HOST=up-edge
KERNEL_HOST=up-kernel
PLATFORM_HOST=up-platform
EXPLORER_HOST=up-explorer
ARCHIVE_HOST=up-archive
VALIDATOR_HOSTS="up-validator-a up-validator-b"
RPC_HOSTS="up-rpc-1 up-rpc-2 up-rpc-3 up-rpc-4 up-rpc-5 up-rpc-6 up-rpc-7 up-rpc-8 up-rpc-9 up-rpc-10 up-rpc-11 up-rpc-12 up-rpc-13 up-rpc-14 up-rpc-15 up-rpc-16"
HPX_HOST=up-hpx
ENV

cat >"$work/hosts-rpc-bad.env" <<'ENV'
EDGE_HOST=up-edge
KERNEL_HOST=up-kernel
PLATFORM_HOST=up-platform
EXPLORER_HOST=up-explorer
ARCHIVE_HOST=up-archive
VALIDATOR_HOSTS="up-validator-a up-validator-b"
RPC_HOSTS="up-rpc-1 up-rpc-2 up-rpc-3 up-rpc-4 up-rpc-5 up-rpc-6 up-rpc-7 up-rpc-8 up-rpc-9-fresh up-rpc-10-dead up-rpc-11 up-rpc-13 up-rpc-14 up-rpc-15 down-rpc-16"
HPX_HOST=up-hpx
ENV

failures=0

# expect <name> <hosts file or -> <want exit> <args...> -- <lines...>: runs the
# checker with the fixture (or with BRINGUP_HOSTS_FILE unset for -) and wants
# the exit code, every line, and no fixture destination in the output.
expect() {
	local name="$1" hosts="$2" want_status="$3" output status=0 ok=1 line
	shift 3
	local args=()
	while [ "$#" -gt 0 ] && [ "$1" != -- ]; do
		args+=("$1")
		shift
	done
	shift
	: >"$CHECK_LIVE_TEST_CALLS"
	if [ "$hosts" = - ]; then
		output="$(env -u BRINGUP_HOSTS_FILE CHECK_LIVE_TIMEOUT=5 "$checker" ${args[@]+"${args[@]}"} 2>&1)" || status=$?
	else
		output="$(BRINGUP_HOSTS_FILE="$hosts" CHECK_LIVE_TIMEOUT="${CHECK_LIVE_TEST_TIMEOUT:-5}" "$checker" ${args[@]+"${args[@]}"} 2>&1)" || status=$?
	fi
	[ "$status" -eq "$want_status" ] || ok=0
	for line in "$@"; do
		grep -qF -- "$line" <<<"$output" || ok=0
	done
	if grep -qE -- '(up|down|hang)-[a-z]' <<<"$output"; then
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

expect check_live_no_subcommand "$work/hosts-good.env" 2 -- \
	"usage: tools/bringup/check-live.sh"

expect check_live_unknown_subcommand "$work/hosts-good.env" 2 nonexistent -- \
	"usage: tools/bringup/check-live.sh"

expect check_live_extra_argument "$work/hosts-good.env" 2 hosts extra -- \
	"usage: tools/bringup/check-live.sh"

expect check_live_help "$work/hosts-good.env" 0 --help -- \
	"usage: tools/bringup/check-live.sh" \
	"rpc-nodes" \
	"BRINGUP_HOSTS_FILE" \
	"CHECK_LIVE_TIMEOUT"

expect check_live_hosts_file_unset - 2 hosts -- \
	"check-live: BRINGUP_HOSTS_FILE is unset"

expect check_live_hosts_file_unreadable "$work/absent.env" 2 hosts -- \
	"check-live: BRINGUP_HOSTS_FILE does not name a readable file"

expect check_live_hosts_file_lacks_role "$work/hosts-missing.env" 2 hosts -- \
	"check-live: BRINGUP_HOSTS_FILE lacks ARCHIVE_HOST"

status=0
output="$(BRINGUP_HOSTS_FILE="$work/hosts-missing.env" "$checker" hosts 2>&1)" || status=$?
if [ "$status" -eq 2 ] && [ "$(wc -l <<<"$output")" -eq 1 ] && [ ! -s "$CHECK_LIVE_TEST_CALLS" ]; then
	echo "ok   check_live_hosts_file_lacks_role_says_nothing_else"
else
	echo "FAIL check_live_hosts_file_lacks_role_says_nothing_else: want exit 2, one line and no ssh call, got exit $status"
	printf '%s\n' "$output"
	failures=$((failures + 1))
fi

expect check_live_hosts_passing "$work/hosts-good.env" 0 hosts -- \
	"pass EDGE_HOST reachable=1/1" \
	"pass KERNEL_HOST reachable=1/1" \
	"pass PLATFORM_HOST reachable=1/1" \
	"pass EXPLORER_HOST reachable=1/1" \
	"pass ARCHIVE_HOST reachable=1/1" \
	"pass VALIDATOR_HOSTS reachable=2/2" \
	"pass RPC_HOSTS reachable=3/3" \
	"pass HPX_HOST reachable=1/1" \
	"check-live: all checks passed"

if [ "$(wc -l <"$CHECK_LIVE_TEST_CALLS")" -eq 11 ] && [ "$(sort -u "$CHECK_LIVE_TEST_CALLS" | wc -l)" -eq 11 ] &&
	! grep -qvE '^(up|down|hang)-[a-z0-9-]+ true$' "$CHECK_LIVE_TEST_CALLS"; then
	echo "ok   check_live_hosts_one_ssh_true_per_destination"
else
	echo "FAIL check_live_hosts_one_ssh_true_per_destination: want eleven distinct 'destination true' calls"
	cat "$CHECK_LIVE_TEST_CALLS"
	failures=$((failures + 1))
fi

expect check_live_hosts_failing "$work/hosts-down.env" 1 hosts -- \
	"pass EDGE_HOST reachable=1/1" \
	"fail KERNEL_HOST reachable=0/1 ssh=255" \
	"pass VALIDATOR_HOSTS reachable=2/2" \
	"fail RPC_HOSTS reachable=2/3 ssh=255" \
	"pass HPX_HOST reachable=1/1" \
	"check-live: 2 check(s) failed"

CHECK_LIVE_TEST_TIMEOUT=1 expect check_live_hosts_timeout "$work/hosts-hang.env" 1 hosts -- \
	"pass RPC_HOSTS reachable=3/3" \
	"fail HPX_HOST reachable=0/1 ssh=124" \
	"check-live: 1 check(s) failed"

expect check_live_rpc_nodes_passing "$work/hosts-rpc-good.env" 0 rpc-nodes -- \
	"pass api1 head=26400000 lag=0 unit=active active=7200s" \
	"pass api12 head=26400000 lag=0 unit=active active=7200s" \
	"pass api16 head=26400000 lag=0 unit=active active=7200s" \
	"check-live: all checks passed"

if [ "$(grep -c ' curl$' "$CHECK_LIVE_TEST_CALLS")" -eq 16 ] && [ "$(grep -c 'systemctl' "$CHECK_LIVE_TEST_CALLS")" -eq 16 ] &&
	[ "$(grep -c '^pass api' <<<"$(BRINGUP_HOSTS_FILE="$work/hosts-rpc-good.env" "$checker" rpc-nodes)")" -eq 16 ]; then
	echo "ok   check_live_rpc_nodes_one_request_per_name_and_destination"
else
	echo "FAIL check_live_rpc_nodes_one_request_per_name_and_destination: want sixteen curl calls, sixteen ssh unit calls and sixteen pass lines"
	cat "$CHECK_LIVE_TEST_CALLS"
	failures=$((failures + 1))
fi

export CHECK_LIVE_TEST_LAG="api14:11 api2:10" CHECK_LIVE_TEST_DOWN="api3"
expect check_live_rpc_nodes_failing "$work/hosts-rpc-bad.env" 1 rpc-nodes -- \
	"pass api1 head=26400000 lag=0 unit=active active=7200s" \
	"pass api2 head=26399990 lag=10 unit=active active=7200s" \
	"fail api3 head=none lag=none unit=active active=7200s" \
	"fail api9 head=26400000 lag=0 unit=active active=120s" \
	"fail api10 head=26400000 lag=0 unit=failed active=0s" \
	"fail api12 head=26400000 lag=0 unit=unmapped active=none" \
	"fail api14 head=26399989 lag=11 unit=active active=7200s" \
	"fail RPC_HOSTS[14] ssh=255" \
	"fail api16 head=26400000 lag=0 unit=unmapped active=none" \
	"check-live: 7 check(s) failed"
unset CHECK_LIVE_TEST_LAG CHECK_LIVE_TEST_DOWN

if [ "$failures" -ne 0 ]; then
	echo "check-live.test: $failures case(s) failed"
	exit 1
fi
echo "check-live.test: all cases passed"
