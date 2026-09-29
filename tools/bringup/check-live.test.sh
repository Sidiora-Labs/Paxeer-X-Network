#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
checker="$root/tools/bringup/check-live.sh"
work="$(mktemp -d)"
responder_pid=""

cleanup() {
	if [ -n "$responder_pid" ]; then
		kill "$responder_pid" 2>/dev/null || true
		wait "$responder_pid" 2>/dev/null || true
	fi
	rm -rf "$work"
}
trap cleanup EXIT

for tool in timeout python3 curl sha256sum; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "check-live.test: $tool is required" >&2
		exit 2
	fi
done

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
[ "$command" = true ] || exit 98
case "$dest" in
up-*) exit 0 ;;
hang-*) exec sleep 5 ;;
*) exit 255 ;;
esac
SH
chmod +x "$work/bin/ssh"
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

# A local hpx registry stand-in: a static tree served on a loopback port
# written to a file, with a published-looking release under good/ and a
# tampered one under bad/ (unbound source revision, one artifact rewritten
# after its manifest line, no /api/nodes).
cat >"$work/responder.py" <<'PY'
import functools
import http.server
import sys

root, port_file = sys.argv[1], sys.argv[2]


class Quiet(http.server.SimpleHTTPRequestHandler):
    def log_message(self, *args):
        pass


server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), functools.partial(Quiet, directory=root))
with open(port_file, "w") as handle:
    handle.write(str(server.server_address[1]))
server.serve_forever()
PY

release() {
	local dir="$1"
	mkdir -p "$dir/lib" "$dir/config/fullnode" "$dir/api"
	printf 'not a real binary\n' >"$dir/paxd"
	printf 'not a real runtime\n' >"$dir/lib/libwasmvm.x86_64.so"
	printf '{"chain_id":"hyperpax_125-1"}\n' >"$dir/genesis.json"
	printf 'moniker = "hpx-node"\n' >"$dir/config/fullnode/config.toml"
	printf '{"chain_id":"hyperpax_125-1","release_id":"fixture"}\n' >"$dir/chain-info.json"
	(
		cd "$dir"
		find . -type f ! -name checksums.txt -print0 |
			sort -z |
			xargs -0 sha256sum |
			sed 's#  \./#  #'
	) >"$dir/checksums.txt"
	printf '{"ok":true,"chain_id":"hyperpax_125-1","source_revision":"%s"}\n' "$(printf 'a%.0s' $(seq 40))" >"$dir/healthz"
	printf '{"chain_id":"hyperpax_125-1","count":1,"nodes":[{"node_id":"%s"}]}\n' "$(printf 'b%.0s' $(seq 40))" >"$dir/api/nodes"
}

release "$work/hpx/good"
release "$work/hpx/bad"
printf '{"ok":true,"chain_id":"hyperpax_125-1","source_revision":"development"}\n' >"$work/hpx/bad/healthz"
printf 'rewritten after the manifest\n' >"$work/hpx/bad/paxd"
rm "$work/hpx/bad/api/nodes"

python3 "$work/responder.py" "$work/hpx" "$work/port" 2>/dev/null &
responder_pid=$!
for _ in $(seq 50); do
	[ -s "$work/port" ] && break
	sleep 0.1
done
if [ ! -s "$work/port" ]; then
	echo "check-live.test: the hpx responder did not start" >&2
	exit 2
fi
origin="http://127.0.0.1:$(cat "$work/port")"

CHECK_LIVE_HPX_ORIGIN="$origin/good" expect check_live_hpx_passing "$work/hosts-good.env" 0 hpx -- \
	"pass healthz http=200 ok=true chain_id=hyperpax_125-1 source_revision=$(printf 'a%.0s' $(seq 40))" \
	"pass checksums verified=5/5" \
	"pass api-nodes http=200 chain_id=hyperpax_125-1 count=1" \
	"check-live: all checks passed"

CHECK_LIVE_HPX_ORIGIN="$origin/bad" expect check_live_hpx_failing "$work/hosts-good.env" 1 hpx -- \
	"fail healthz http=200 ok=true chain_id=hyperpax_125-1 source_revision=development" \
	"fail checksums verified=4/5 first=paxd" \
	"fail api-nodes http=404" \
	"check-live: 3 check(s) failed"

expect check_live_hpx_hosts_file_lacks_role "$work/hosts-missing.env" 2 hpx -- \
	"check-live: BRINGUP_HOSTS_FILE lacks ARCHIVE_HOST"

if [ "$failures" -ne 0 ]; then
	echo "check-live.test: $failures case(s) failed"
	exit 1
fi
echo "check-live.test: all cases passed"
