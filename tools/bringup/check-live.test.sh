#!/usr/bin/env bash
set -euo pipefail
mkdir -p /run/lock
exec 9>/run/lock/check-live-harness
flock 9

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
checker="$root/tools/bringup/check-live.sh"
work="$(mktemp -d)"
responder_pid=""
agentd_pid=""
internal_pids=""
bridge_pids=""

cleanup() {
	for pid in $internal_pids; do
		kill "$pid" 2>/dev/null || true
		wait "$pid" 2>/dev/null || true
	done
	if [ -n "$responder_pid" ]; then
		kill "$responder_pid" 2>/dev/null || true
		wait "$responder_pid" 2>/dev/null || true
	fi
	if [ -n "$agentd_pid" ]; then
		kill "$agentd_pid" 2>/dev/null || true
		wait "$agentd_pid" 2>/dev/null || true
	fi
	if [ -n "$bridge_pids" ]; then
		# shellcheck disable=SC2086
		kill $bridge_pids 2>/dev/null || true
		# shellcheck disable=SC2086
		wait $bridge_pids 2>/dev/null || true
	fi
	rm -rf "$work"
}
trap cleanup EXIT

for tool in timeout python3 curl sha256sum openssl base64 cast; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "check-live.test: $tool is required" >&2
		exit 2
	fi
done
real_curl="$(command -v curl)"
real_path="$PATH"

# A local ssh stand-in ahead of the real one on PATH: it answers for the
# fixture destinations only, refuses to run without BatchMode, records every
# call, and answers true, the rpc-nodes unit inspection and the rpc-placement
# validator inspection; any other command is refused.
mkdir -p "$work/bin"
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
printf '%s %s\n' "$dest" "$command" >>"$CHECK_LIVE_TEST_CALLS"
[ "$batch" -eq 1 ] || exit 99
case "$dest" in
up-*) ;;
hang-*) exec sleep 5 ;;
*) exit 255 ;;
esac
case "$command" in
true) exit 0 ;;
*"@@sites"*)
	cat "$CHECK_LIVE_TEST_EDGE"
	exit 0
	;;
*"ss -Hltn"*)
	case "$dest" in
	up-validator-*-node) echo "active none" ;;
	up-validator-*-web) echo "inactive 80,443" ;;
	up-validator-*-kept) echo "active 443,80 api1" ;;
	*) echo "inactive none" ;;
	esac
	exit 0
	;;
*systemctl*)
	n="${dest#*-rpc-}"
	n="${n%%-*}"
	case "$dest" in
	up-rpc-*-fresh) echo "api$n active 120" ;;
	up-rpc-*-dead) echo "api$n failed 0" ;;
	up-rpc-*) echo "api$n active 7200" ;;
	*) echo "none none 0" ;;
	esac
	exit 0
	;;
esac
exit 97
SH
chmod +x "$work/bin/ssh"

# A local flyctl stand-in: answers ssh console and secrets import for the
# fixture apps (fx-*) only, answers machines list with
# CHECK_LIVE_TEST_MACHINES and ips list with CHECK_LIVE_TEST_IPS, records every call, and gives each app a secrets
# directory for /run/secrets and each process group of it a volume of its own
# for /data, so every fixture machine keeps its own files. ssh console runs
# its command on this box and records its stdin; secrets import wants
# --stage, decodes each NAME=base64 line into the app's secrets directory and
# records only the names. machines list answers CHECK_LIVE_TEST_MACHINES;
# secrets list answers the names of the app's secrets directory and apps list
# the fixture apps that have a directory.
cat >"$work/bin/flyctl" <<'SH'
#!/usr/bin/env bash
set -eu
sub="${1:-} ${2:-}"
shift 2
app=""
group=app
command=""
stage=0
while [ "$#" -gt 0 ]; do
	case "$1" in
	--app)
		app="$2"
		shift 2
		;;
	--process-group)
		group="$2"
		shift 2
		;;
	--command)
		command="$2"
		shift 2
		;;
	--stage)
		stage=1
		shift
		;;
	--quiet | --json) shift ;;
	*) exit 98 ;;
	esac
done
printf '%s %s %s %s\n' "$app" "$group" "$sub" "$command" >>"$CHECK_LIVE_TEST_CALLS"
if [ "$sub" = "apps list" ]; then
	ls "$CHECK_LIVE_TEST_FLY" 2>/dev/null | python3 -c 'import json, sys; print(json.dumps([{"Name": n} for n in sys.stdin.read().split()]))'
	exit 0
fi
case "$app" in
fx-*) ;;
*)
	echo "Error: app not found" >&2
	exit 1
	;;
esac
root="$CHECK_LIVE_TEST_FLY/$app"
case "$sub" in
"ssh console")
	mkdir -p "$root/$group/data" "$root/secrets"
	command="${command//\/data\//$root/$group/data/}"
	command="${command//\/run\/secrets\//$root/secrets/}"
	command="${command//\/run\/layerx\//$root/$group/run/layerx/}"
	command="${command//\/var\/lib\//$root/$group/var/lib/}"
	command="${command//\/usr\/local\/bin\//$root/$group/usr/local/bin/}"
	command="${command//\/run\/human-material/$root/$group/run/human-material}"
	tee -a "$CHECK_LIVE_TEST_STDIN" | bash -c "$command"
	;;
"secrets import")
	[ "$stage" -eq 1 ] || exit 97
	mkdir -p "$root/secrets"
	while IFS= read -r line; do
		printf '%s' "${line#*=}" | base64 -d >"$root/secrets/${line%%=*}"
		printf '%s %s\n' "$app" "${line%%=*}" >>"$CHECK_LIVE_TEST_IMPORTS"
	done
	;;
"ips list") printf '%s\n' "${CHECK_LIVE_TEST_IPS:-[]}" ;;
"secrets list") ls "$root/secrets" 2>/dev/null | python3 -c 'import json, sys; print(json.dumps([{"Name": n, "Digest": "fx"} for n in sys.stdin.read().split()]))' ;;
"machines list")
	if [ -n "${CHECK_LIVE_TEST_MACHINES:-}" ]; then printf '%s\n' "$CHECK_LIVE_TEST_MACHINES"; else cat "$root/machines.json"; fi
	;;
"machines list") printf '%s\n' "${CHECK_LIVE_TEST_MACHINES:-[]}" ;;
*) exit 96 ;;
esac
SH
chmod +x "$work/bin/flyctl"

# A local curl stand-in: answers eth_blockNumber for the public names, at the
# fixed head minus the lag CHECK_LIVE_TEST_LAG ("apiN:blocks ...") assigns,
# answers a readiness request to an app's .internal name with the body and
# the -w status code, 503 for the ports CHECK_LIVE_TEST_BOUNDARY_DOWN lists
# and 000 when no readable --cert client identity is presented,
# fails to connect for the names in CHECK_LIVE_TEST_DOWN, and hands any
# request without a public https name to the real curl, so the hpx cases
# reach the loopback registry stand-in below. The machine name answers as the
# agentd boundary does: no client certificate fails the handshake, /healthz
# and /rpc want the bearer CHECK_LIVE_TEST_AGENT_BEARER in the --config file,
# and /rpc answers a program.discover of CHECK_LIVE_TEST_AGENT_PROGRAM with
# CHECK_LIVE_TEST_AGENT_RPC_CODE and CHECK_LIVE_TEST_AGENT_RPC.
# With CHECK_LIVE_TEST_GAS set, the chain name and the router's /rpc answer
# from the files of that directory, as the gas cases below describe.
# CHECK_LIVE_TEST_AGENT_RPC_CODE and CHECK_LIVE_TEST_AGENT_RPC. A process-group
# .internal name answers /readyz ready to a request with a client certificate.
# CHECK_LIVE_TEST_AGENT_RPC_CODE and CHECK_LIVE_TEST_AGENT_RPC. The public
# wallet name walletfx and the wallet gateway's fixture app answer as the
# gateway does: /healthz, /readyz with every component up, and /v1/wallet/me
# for the bearer CHECK_LIVE_TEST_WALLET_TOKEN, each with the x-served-by value
# CHECK_LIVE_TEST_WALLET_SERVED_BY (default paxeer-wallet-gateway, - for none).
cat >"$work/bin/curl" <<'SH'
#!/usr/bin/env bash
set -eu
url=""
for arg in "$@"; do
	case "$arg" in
	https://*) url="$arg" ;;
	esac
done
[ -n "$url" ] || exec "$CHECK_LIVE_TEST_REAL_CURL" "$@"
name="${url#https://}"
name="${name%%.*}"
printf '%s curl\n' "$name" >>"$CHECK_LIVE_TEST_CALLS"
case " ${CHECK_LIVE_TEST_DOWN:-} " in
*" $name "*) exit 7 ;;
esac
# With CHECK_LIVE_TEST_ROUTER set, the router name answers /rpc from the
# fixture file named after the request's method and /readyz from
# readyz-<fly-force-instance-id>, whose first line is the status code the -w
# format appends.
if [ -n "${CHECK_LIVE_TEST_ROUTER:-}" ] && [ "$name" = api-mainnet-beta ]; then
	data=""
	instance=""
	write=0
	prev=""
	for arg in "$@"; do
		case "$prev" in
		-d) data="$arg" ;;
		-H) [[ "$arg" != fly-force-instance-id:* ]] || instance="${arg#*: }" ;;
		-w) write=1 ;;
		esac
		prev="$arg"
	done
	case "$url" in
	*/readyz)
		tail -n +2 "$CHECK_LIVE_TEST_ROUTER/readyz-$instance"
		[ "$write" -eq 0 ] || printf '\n%s' "$(head -n 1 "$CHECK_LIVE_TEST_ROUTER/readyz-$instance")"
		;;
	*) cat "$CHECK_LIVE_TEST_ROUTER/$(sed -n 's/.*"method":"\([A-Za-z_]*\)".*/\1/p' <<<"$data")" ;;
	esac
	exit 0
fi
if [ -n "${CHECK_LIVE_TEST_GAS:-}" ] && { [ "$name" = chain ] || [ "$url" = https://api-mainnet-beta.paxeer.network/rpc ]; }; then
	out=/dev/stdout
	data=""
	wout=""
	while [ "$#" -gt 0 ]; do
		case "$1" in
		--output) out="$2" ;;
		--write-out) wout="$2" ;;
		--data-binary) data="$(cat "${2#@}")" ;;
		-d) data="$2" ;;
		esac
		shift
	done
	if [ "$name" = chain ]; then
		route="${url##*/}"
		printf '%s' "$data" >"$CHECK_LIVE_TEST_GAS/$route.request"
		cat "$CHECK_LIVE_TEST_GAS/$route.body" >"$out"
		[ -z "$wout" ] || cat "$CHECK_LIVE_TEST_GAS/$route.code"
		exit 0
	fi
	file="$(python3 -c '
import json, sys
r = json.loads(sys.argv[1])
m = r["method"]
print(m + "-" + r["params"][0]["data"] if m == "eth_call" else m)
' "$data")"
	printf '{"jsonrpc":"2.0","id":1,"result":%s}\n' "$(cat "$CHECK_LIVE_TEST_GAS/$file" 2>/dev/null || echo null)"
	exit 0
fi
if [ "$name" = walletfx ] || [ "$name" = fx-human-wallet-deploy-gateway ]; then
	out=""
	dump=""
	wout=""
	auth=""
	while [ "$#" -gt 0 ]; do
		case "$1" in
		-o) out="$2" ;;
		-D) dump="$2" ;;
		-w) wout="$2" ;;
		-H) auth="$2" ;;
		esac
		shift
	done
	code=404
	body='{"error":"not_found"}'
	case "$url" in
	*/healthz)
		code=200
		body='{"ok":true}'
		;;
	*/readyz)
		code=200
		body='{"ready":true,"components":{"attestors":{"state":"up","healthy":5,"required":3},"nonce_store":{"state":"up"},"rpc_pool":{"state":"up","healthy":2},"identity_provider":{"state":"up","keys":1}}}'
		;;
	*/v1/wallet/me)
		code=401
		body='{"error":"unauthorized"}'
		if [ "$auth" = "authorization: Bearer $CHECK_LIVE_TEST_WALLET_TOKEN" ]; then
			code=200
			body="{\"wallet\":{\"address\":\"0x$(printf '1%.0s' $(seq 40))\",\"did\":\"did:layerx:$(printf 'a%.0s' $(seq 64))\",\"main_account_id\":\"$(printf 'b%.0s' $(seq 64))\",\"binding_state\":\"bound\"},\"kernel\":{\"state\":\"available\"}}"
		fi
		;;
	esac
	served="${CHECK_LIVE_TEST_WALLET_SERVED_BY:-paxeer-wallet-gateway}"
	if [ -n "$dump" ]; then
		printf 'HTTP/2 %s\r\n' "$code" >"$dump"
		[ "$served" = - ] || printf 'x-served-by: %s\r\n' "$served" >>"$dump"
		printf '\r\n' >>"$dump"
	fi
	if [ -n "$out" ]; then
		printf '%s' "$body" >"$out"
	else
		printf '%s' "$body"
	fi
	[ -z "$wout" ] || printf '%b' "${wout//'%{http_code}'/$code}"
	exit 0
fi
if [ "$name" = machine ]; then
	out=/dev/stdout
	conf=""
	data=""
	wout=""
	cert=0
	while [ "$#" -gt 0 ]; do
		case "$1" in
		--output) out="$2" ;;
		--config) conf="$2" ;;
		--data-binary) data="${2#@}" ;;
		--write-out) wout="$2" ;;
		--cert) cert=1 ;;
		esac
		shift
	done
	if [ "$cert" -eq 0 ]; then
		echo "curl: (56) OpenSSL SSL_read: tlsv13 alert certificate required" >&2
		exit 56
	fi
	code=401
	body='{"error":"unauthorized"}'
	if [ -n "$conf" ] && grep -qxF "header = \"Authorization: Bearer $CHECK_LIVE_TEST_AGENT_BEARER\"" "$conf"; then
		case "$url" in
		*/healthz)
			code=200
			body='{"ready":true}'
			;;
		*/rpc)
			code=400
			body='{"error":"invalid_request"}'
			if [ -n "$data" ] && grep -qF "{\"operation\":\"program.discover\",\"request\":{\"program_id\":\"$CHECK_LIVE_TEST_AGENT_PROGRAM\",\"requested_verification_level\":\"sequencer-signed\"}}" "$data"; then
				code="$CHECK_LIVE_TEST_AGENT_RPC_CODE"
				body="$CHECK_LIVE_TEST_AGENT_RPC"
			fi
			;;
		esac
	fi
	printf '%s' "$body" >"$out"
	[ -z "$wout" ] || printf '%s' "$code"
	exit 0
fi
case "$url" in
https://*.process.*.internal:9443/*)
	wout=""
	cert=0
	while [ "$#" -gt 0 ]; do
		case "$1" in
		-w | --write-out) wout="$2" ;;
		--cert) cert=1 ;;
		esac
		shift
	done
	if [ "$cert" -eq 0 ]; then
		echo "curl: (56) OpenSSL SSL_read: tlsv13 alert certificate required" >&2
		exit 56
	fi
	printf '{"ready":true}'
	[ -z "$wout" ] || printf '%b' "${wout//%\{http_code\}/200}"
	exit 0
	;;
esac
case "$url" in
https://*.internal:*/readyz)
	cert=""
	while [ "$#" -gt 0 ]; do
		[ "$1" != --cert ] || cert="${2:-}"
		shift
	done
	if [ ! -r "$cert" ]; then
		printf ' 000'
		exit 58
	fi
	port="${url##*:}"
	port="${port%%/*}"
	case " ${CHECK_LIVE_TEST_BOUNDARY_DOWN:-} " in
	*" $port "*) printf '{"ready":false,"error":"replica_unavailable"} 503' ;;
	*) printf '{"ready":true,"network_id":"fixture","wire_version":"1"} 200' ;;
	esac
	exit 0
	;;
esac
case "$url" in
https://*/?*)
	body='{"status":"ready"}'
	case " ${CHECK_LIVE_TEST_DIFFER:-} " in
	*" $name "*) body='{"status":"starting"}' ;;
	esac
	printf 'HTTP/2 200\r\nfly-request-id: 01FIXTURE-ams\r\n\r\n%s' "$body"
	exit 0
	;;
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
# A local node stand-in for the command the router case runs inside the wallet
# gateway's machine: prints the readiness answer in CHECK_LIVE_TEST_ROUTER;
# without it, the real node runs.
real_node="$(command -v node || true)"
cat >"$work/bin/node" <<SH
#!/usr/bin/env bash
set -eu
if [ -n "\${CHECK_LIVE_TEST_ROUTER:-}" ]; then
	cat "\$CHECK_LIVE_TEST_ROUTER/wallet-readyz"
	exit 0
fi
[ -n "$real_node" ] || exit 127
exec "$real_node" "\$@"
SH
chmod +x "$work/bin/node"
# A local getent stand-in: resolves each public name apiN to the fixture
# destination up-rpc-N unless CHECK_LIVE_TEST_DNS ("apiN:destination ...")
# assigns another, where - resolves to nothing.
cat >"$work/bin/getent" <<'SH'
#!/usr/bin/env bash
set -eu
[ "${1:-}" = ahosts ] || exit 2
name="${2%%.*}"
addr="up-rpc-${name#api}"
case " ${CHECK_LIVE_TEST_DNS:-} " in
*" $name:"*)
	addr="${CHECK_LIVE_TEST_DNS##*"$name:"}"
	addr="${addr%% *}"
	;;
esac
[ "$addr" != - ] || exit 2
printf '%s STREAM %s\n' "$addr" "$2"
SH
chmod +x "$work/bin/getent"
export PATH="$work/bin:$PATH"
export CHECK_LIVE_TEST_CALLS="$work/calls"
export CHECK_LIVE_TEST_STDIN="$work/stdin"
export CHECK_LIVE_TEST_REAL_CURL="$real_curl"
export CHECK_LIVE_TEST_FLY="$work/fly"
export CHECK_LIVE_TEST_IMPORTS="$work/imports"
export CHECK_LIVE_TEST_EDGE="$work/edge-manifest"
export LAYERX_CA_DIR="$work/ca"

cat >"$work/hosts-good.env" <<'ENV'
EDGE_HOST=up-edge
ARCHIVE_HOST=up-archive
VALIDATOR_HOSTS="up-validator-a up-validator-b"
RPC_HOSTS="up-rpc-1 up-rpc-2 up-rpc-3"
OLD_WALLET_HOST=up-old-wallet
ENV

cat >"$work/hosts-down.env" <<'ENV'
EDGE_HOST=up-edge
ARCHIVE_HOST=up-archive
VALIDATOR_HOSTS="up-validator-a up-validator-b"
RPC_HOSTS="up-rpc-1 down-rpc-2 up-rpc-3"
OLD_WALLET_HOST=down-old-wallet
ENV

cat >"$work/hosts-hang.env" <<'ENV'
EDGE_HOST=up-edge
ARCHIVE_HOST=up-archive
VALIDATOR_HOSTS="up-validator-a up-validator-b"
RPC_HOSTS="up-rpc-1 up-rpc-2 up-rpc-3"
OLD_WALLET_HOST=hang-old-wallet
ENV

cat >"$work/hosts-missing.env" <<'ENV'
EDGE_HOST=up-edge
VALIDATOR_HOSTS="up-validator-a up-validator-b"
RPC_HOSTS="up-rpc-1 up-rpc-2 up-rpc-3"
OLD_WALLET_HOST=up-old-wallet
ENV

cat >"$work/hosts-rpc-good.env" <<'ENV'
EDGE_HOST=up-edge
ARCHIVE_HOST=up-archive
VALIDATOR_HOSTS="up-validator-a up-validator-b"
RPC_HOSTS="up-rpc-1 up-rpc-2 up-rpc-3 up-rpc-4 up-rpc-5 up-rpc-6 up-rpc-7 up-rpc-8 up-rpc-9 up-rpc-10 up-rpc-11 up-rpc-12 up-rpc-13 up-rpc-14 up-rpc-15 up-rpc-16"
OLD_WALLET_HOST=up-old-wallet
ENV

cat >"$work/hosts-rpc-bad.env" <<'ENV'
EDGE_HOST=up-edge
ARCHIVE_HOST=up-archive
VALIDATOR_HOSTS="up-validator-a up-validator-b"
RPC_HOSTS="up-rpc-1 up-rpc-2 up-rpc-3 up-rpc-4 up-rpc-5 up-rpc-6 up-rpc-7 up-rpc-8 up-rpc-9-fresh up-rpc-10-dead up-rpc-11 up-rpc-13 up-rpc-14 up-rpc-15 down-rpc-16"
OLD_WALLET_HOST=up-old-wallet
ENV

cat >"$work/hosts-placement-bad.env" <<'ENV'
EDGE_HOST=up-edge
ARCHIVE_HOST=up-archive
VALIDATOR_HOSTS="up-validator-a-node up-validator-b-web down-validator-c"
RPC_HOSTS="up-validator-a-node up-rpc-2 up-rpc-3 up-rpc-4 up-rpc-5 up-rpc-6 up-rpc-7 up-rpc-8 up-rpc-9 up-rpc-10 up-rpc-11 up-rpc-12 up-rpc-13 up-rpc-14 up-rpc-15"
OLD_WALLET_HOST=up-old-wallet
ENV

rpc_rest="up-rpc-2 up-rpc-3 up-rpc-4 up-rpc-5 up-rpc-6 up-rpc-7 up-rpc-8 up-rpc-9 up-rpc-10 up-rpc-11 up-rpc-12 up-rpc-13 up-rpc-14 up-rpc-15 up-rpc-16"
cat >"$work/hosts-placement-unretained.env" <<ENV
EDGE_HOST=up-edge
ARCHIVE_HOST=up-archive
VALIDATOR_HOSTS="up-validator-a-kept up-validator-b"
RPC_HOSTS="up-validator-a-kept $rpc_rest"
OLD_WALLET_HOST=up-old-wallet
ENV
cp "$work/hosts-placement-unretained.env" "$work/hosts-placement-retained.env"
echo 'RETAINED_ON_VALIDATOR="api1.mainnet-beta.paxeer.network"' >>"$work/hosts-placement-retained.env"

failures=0

# expect <name> <hosts file or -> <want exit> <args...> -- <lines...>: runs the
# checker, or the program CHECK_LIVE_TEST_PROGRAM names, with the fixture (or
# with BRINGUP_HOSTS_FILE unset for -) and wants the exit code, every line,
# and no fixture destination in the output.
expect() {
	local name="$1" hosts="$2" want_status="$3" output status=0 ok=1 line
	local program="${CHECK_LIVE_TEST_PROGRAM:-$checker}"
	shift 3
	local args=()
	while [ "$#" -gt 0 ] && [ "$1" != -- ]; do
		args+=("$1")
		shift
	done
	shift
	: >"$CHECK_LIVE_TEST_CALLS"
	if [ "$hosts" = - ]; then
		output="$(env -u BRINGUP_HOSTS_FILE CHECK_LIVE_TIMEOUT=5 "$program" ${args[@]+"${args[@]}"} 2>&1)" || status=$?
	else
		output="$(BRINGUP_HOSTS_FILE="$hosts" CHECK_LIVE_TIMEOUT="${CHECK_LIVE_TEST_TIMEOUT:-5}" "$program" ${args[@]+"${args[@]}"} 2>&1)" || status=$?
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
	"pass ARCHIVE_HOST reachable=1/1" \
	"pass VALIDATOR_HOSTS reachable=2/2" \
	"pass RPC_HOSTS reachable=3/3" \
	"pass OLD_WALLET_HOST reachable=1/1" \
	"check-live: all checks passed"

if [ "$(wc -l <"$CHECK_LIVE_TEST_CALLS")" -eq 8 ] && [ "$(sort -u "$CHECK_LIVE_TEST_CALLS" | wc -l)" -eq 8 ] &&
	! grep -qvE '^(up|down|hang)-[a-z0-9-]+ true$' "$CHECK_LIVE_TEST_CALLS"; then
	echo "ok   check_live_hosts_one_ssh_true_per_destination"
else
	echo "FAIL check_live_hosts_one_ssh_true_per_destination: want eight distinct 'destination true' calls"
	cat "$CHECK_LIVE_TEST_CALLS"
	failures=$((failures + 1))
fi

expect check_live_hosts_failing "$work/hosts-down.env" 1 hosts -- \
	"pass EDGE_HOST reachable=1/1" \
	"pass VALIDATOR_HOSTS reachable=2/2" \
	"fail RPC_HOSTS reachable=2/3 ssh=255" \
	"fail OLD_WALLET_HOST reachable=0/1 ssh=255" \
	"check-live: 2 check(s) failed"

CHECK_LIVE_TEST_TIMEOUT=1 expect check_live_hosts_timeout "$work/hosts-hang.env" 1 hosts -- \
	"pass RPC_HOSTS reachable=3/3" \
	"fail OLD_WALLET_HOST reachable=0/1 ssh=124" \
	"check-live: 1 check(s) failed"

# The CA cases run both scripts from a fixture tree whose tomls stand in for
# the Fly apps: every toml of the service list gets an app line naming its
# fixture app, and every secret prefix a [[files]] entry mounting
# <PREFIX>_CERT.
fx="$work/repo"
mkdir -p "$fx/tools/bringup"
cp "$root/tools/bringup/check-live.sh" "$root/tools/bringup/ca.sh" "$root/tools/bringup/human-state-preserve.sh" "$fx/tools/bringup/"
ca="$fx/tools/bringup/ca.sh"
mkdir -p "$fx/tools/qualification/paxeer-x"
cp "$root/tools/qualification/paxeer-x/registry-router-bootstrap.py" "$fx/tools/qualification/paxeer-x/"
fx_checker="$fx/tools/bringup/check-live.sh"
fx_app() {
	local app="${1%.toml}"
	app="${app//\//-}"
	printf 'fx-%s' "${app//./-}"
}
while read -r _ toml _ custody _; do
	mkdir -p "$fx/$(dirname "$toml")"
	[ -e "$fx/$toml" ] || printf 'app = "%s"\n' "$(fx_app "$toml")" >"$fx/$toml"
	if [ "$custody" != volume ]; then
		printf '\n[[files]]\n  guest_path = "/run/secrets/%s_CERT"\n  secret_name = "%s_CERT"\n' "$custody" "$custody" >>"$fx/$toml"
	fi
done < <("$ca" services)
kernel="$(fx_app human/wallet/deploy/human.toml)"
redis="$(fx_app human/wallet/deploy/redis.toml)"
endpoint="$(fx_app human/wallet/deploy/endpoint.toml)"
identity="$(fx_app platform/hosted/identity/fly.toml)"
internal="$(fx_app platform/hosted/internal/fly.toml)"
interop="$(fx_app platform/hosted/interop/fly.toml)"
webhooks="$(fx_app platform/hosted/webhooks/fly.toml)"
fly="$CHECK_LIVE_TEST_FLY"

CHECK_LIVE_TEST_PROGRAM="$fx_checker" LAYERX_CA_DIR="$work/absent-ca" expect check_live_ca_without_a_ca "$work/hosts-good.env" 1 ca -- \
	"fail ca ca cert=absent" \
	"check-live: 1 check(s) failed"

CHECK_LIVE_TEST_PROGRAM="$ca" expect ca_no_subcommand "$work/hosts-good.env" 2 -- \
	"usage: tools/bringup/ca.sh"

CHECK_LIVE_TEST_PROGRAM="$ca" expect ca_issue_refuses_the_host_role_form "$work/hosts-good.env" 2 issue human KERNEL_HOST -- \
	"usage: tools/bringup/ca.sh"

CHECK_LIVE_TEST_PROGRAM="$ca" expect ca_issue_without_a_ca "$work/hosts-good.env" 1 issue human -- \
	"ca: no CA under"

status=0
fingerprint="$("$ca" init 2>&1)" || status=$?
if [ "$status" -eq 0 ] && [[ "$fingerprint" =~ ^([0-9A-F]{2}:){31}[0-9A-F]{2}$ ]] &&
	[ "$(stat -c %a "$work/ca")" = 700 ] &&
	[ "$(stat -c %a "$work/ca/ca.key")" = 600 ] && [ "$(stat -c %a "$work/ca/ca.pem")" = 600 ] && [ "$(stat -c %a "$work/ca/ca.der")" = 600 ] &&
	[ "$(openssl x509 -in "$work/ca/ca.pem" -noout -fingerprint -sha256 | cut -d= -f2)" = "$fingerprint" ] &&
	openssl x509 -in "$work/ca/ca.pem" -noout -ext basicConstraints | grep -q 'CA:TRUE'; then
	echo "ok   ca_init_prints_only_the_fingerprint"
else
	echo "FAIL ca_init_prints_only_the_fingerprint: want exit 0, one sha256 fingerprint and 0600 files under a 0700 directory, got exit $status"
	printf '%s\n' "$fingerprint"
	failures=$((failures + 1))
fi

before="$(sha256sum "$work/ca/ca.key")"
CHECK_LIVE_TEST_PROGRAM="$ca" expect ca_init_refuses_an_existing_ca "$work/hosts-good.env" 1 init -- \
	"already holds a CA"
if [ "$(sha256sum "$work/ca/ca.key")" = "$before" ]; then
	echo "ok   ca_init_keeps_the_existing_key"
else
	echo "FAIL ca_init_keeps_the_existing_key: the refused init changed ca.key"
	failures=$((failures + 1))
fi

CHECK_LIVE_TEST_PROGRAM="$ca" expect ca_issue_unknown_service "$work/hosts-good.env" 2 issue nonexistent -- \
	"ca: unknown service nonexistent"

mv "$fx/platform/hosted/identity/fly.toml" "$work/identity.toml"
CHECK_LIVE_TEST_PROGRAM="$ca" expect ca_issue_refuses_a_missing_toml "$work/hosts-good.env" 1 issue identity -- \
	"ca: identity: platform/hosted/identity/fly.toml names no app"
if [ ! -s "$CHECK_LIVE_TEST_CALLS" ]; then
	echo "ok   ca_issue_missing_toml_calls_no_app"
else
	echo "FAIL ca_issue_missing_toml_calls_no_app: want no flyctl call"
	cat "$CHECK_LIVE_TEST_CALLS"
	failures=$((failures + 1))
fi
mv "$work/identity.toml" "$fx/platform/hosted/identity/fly.toml"

shm_before="$(ls /dev/shm)"
: >"$CHECK_LIVE_TEST_STDIN"
CHECK_LIVE_TEST_PROGRAM="$ca" expect ca_issue_receipt_authority_on_the_volume "$work/hosts-good.env" 0 issue receipt-authority -- \
	"issued receipt-authority app=$kernel custody=volume fingerprint=" \
	"expires_in=39"

tls="$fly/$kernel/app/data/tls/receipt-authority"
san="$(openssl x509 -in "$tls/cert.pem" -noout -ext subjectAltName 2>/dev/null || true)"
if [ "$(stat -c %a "$tls")" = 700 ] &&
	[ "$(stat -c %a "$tls/key.pem")$(stat -c %a "$tls/key.der")$(stat -c %a "$tls/identity.p12")$(stat -c %a "$tls/password")" = 600600600600 ] &&
	[ ! -e "$tls/key.pem.new" ] && [ ! -e "$tls/cert.pem.new" ] && [ ! -e "$tls/ca.pem.new" ] && [ ! -e "$tls/bundle.new" ] &&
	cmp -s "$tls/ca.pem" "$work/ca/ca.pem" && cmp -s "$tls/ca.der" "$work/ca/ca.der" &&
	openssl verify -CAfile "$work/ca/ca.pem" "$tls/cert.pem" >/dev/null 2>&1 &&
	[ "$(openssl pkey -in "$tls/key.pem" -pubout 2>/dev/null)" = "$(openssl x509 -in "$tls/cert.pem" -noout -pubkey)" ] &&
	[ "$(openssl pkey -inform DER -in "$tls/key.der" -pubout 2>/dev/null)" = "$(openssl x509 -inform DER -in "$tls/cert.der" -noout -pubkey)" ] &&
	openssl pkcs12 -in "$tls/identity.p12" -passin "file:$tls/password" -noout >/dev/null 2>&1 &&
	grep -q 'DNS:layerx-receipt-authority' <<<"$san" && grep -q 'DNS:authority' <<<"$san" &&
	grep -q "DNS:$kernel.internal" <<<"$san" &&
	grep -q 'DNS:localhost' <<<"$san" && grep -q 'IP Address:127.0.0.1' <<<"$san" &&
	[ "$(grep -o 'DNS:\|IP Address:' <<<"$san" | wc -l)" -eq 5 ] &&
	openssl x509 -in "$tls/cert.pem" -noout -ext extendedKeyUsage | grep -q 'TLS Web Server Authentication'; then
	echo "ok   ca_issue_lands_the_material_on_the_volume"
else
	echo "FAIL ca_issue_lands_the_material_on_the_volume: want 0600 key, der, p12 and password files in a 0700 directory, no .new leftovers, the CA copied, a chained certificate on the machine's key with exactly the receipt authority SANs, the app's .internal name, localhost and the loopback address"
	ls -la "$tls" 2>&1 || true
	printf '%s\n' "$san"
	failures=$((failures + 1))
fi

if [ "$(wc -l <"$CHECK_LIVE_TEST_CALLS")" -eq 2 ] && [ "$(grep -c "^$kernel app ssh console sh -c '" "$CHECK_LIVE_TEST_CALLS")" -eq 2 ] &&
	! grep -q 'PRIVATE KEY' "$CHECK_LIVE_TEST_CALLS" "$CHECK_LIVE_TEST_STDIN" &&
	[ "$(grep -c 'BEGIN CERTIFICATE' "$CHECK_LIVE_TEST_STDIN")" -eq 2 ] &&
	[ "$(ls /dev/shm)" = "$shm_before" ]; then
	echo "ok   ca_issue_never_moves_a_volume_key"
else
	echo "FAIL ca_issue_never_moves_a_volume_key: want two ssh console calls to the kernel app, the certificate and the CA on stdin, no private key in any call or on stdin and no directory left in /dev/shm"
	cat "$CHECK_LIVE_TEST_CALLS"
	failures=$((failures + 1))
fi

: >"$CHECK_LIVE_TEST_CALLS"
: >"$CHECK_LIVE_TEST_IMPORTS"
status=0
output="$(CHECK_LIVE_TIMEOUT=5 "$ca" issue gateway-redis 2>&1)" || status=$?
sec="$fly/$redis/secrets"
san="$(openssl x509 -in "$sec/REDIS_TLS_CERT" -noout -ext subjectAltName 2>/dev/null || true)"
if [ "$status" -eq 0 ] && [ "$(wc -l <<<"$output")" -eq 1 ] &&
	[[ "$output" =~ ^issued\ gateway-redis\ app=$redis\ custody=secrets\ fingerprint=([0-9A-F]{2}:){31}[0-9A-F]{2}\ expires_in=39[0-9]d$ ]] &&
	[ "$(sort "$CHECK_LIVE_TEST_IMPORTS" | tr '\n' ' ')" = "$redis REDIS_TLS_CA $redis REDIS_TLS_CA_DER $redis REDIS_TLS_CERT $redis REDIS_TLS_CERT_DER $redis REDIS_TLS_KEY $redis REDIS_TLS_KEY_DER $redis REDIS_TLS_P12 $redis REDIS_TLS_PASSWORD " ] &&
	[ "$(wc -l <"$CHECK_LIVE_TEST_CALLS")" -eq 1 ] && grep -q "^$redis app secrets import $" "$CHECK_LIVE_TEST_CALLS" &&
	cmp -s "$sec/REDIS_TLS_CA" "$work/ca/ca.pem" && cmp -s "$sec/REDIS_TLS_CA_DER" "$work/ca/ca.der" &&
	openssl verify -CAfile "$work/ca/ca.pem" "$sec/REDIS_TLS_CERT" >/dev/null 2>&1 &&
	[ "$(openssl pkey -in "$sec/REDIS_TLS_KEY" -pubout 2>/dev/null)" = "$(openssl x509 -in "$sec/REDIS_TLS_CERT" -noout -pubkey)" ] &&
	[ "$(openssl pkey -inform DER -in "$sec/REDIS_TLS_KEY_DER" -pubout 2>/dev/null)" = "$(openssl x509 -inform DER -in "$sec/REDIS_TLS_CERT_DER" -noout -pubkey)" ] &&
	openssl pkcs12 -in "$sec/REDIS_TLS_P12" -passin "file:$sec/REDIS_TLS_PASSWORD" -noout >/dev/null 2>&1 &&
	grep -q 'DNS:layerx-gateway-redis' <<<"$san" && grep -q "DNS:$redis.internal" <<<"$san" &&
	[ "$(ls /dev/shm)" = "$shm_before" ]; then
	echo "ok   ca_issue_gateway_redis_as_staged_secrets"
else
	echo "FAIL ca_issue_gateway_redis_as_staged_secrets: want one issued line with the fingerprint only, the eight staged secrets of the REDIS_TLS prefix holding a chained identity with the app's .internal name, one secrets import call and no directory left in /dev/shm, got exit $status"
	printf '%s\n' "$output"
	cat "$CHECK_LIVE_TEST_CALLS" "$CHECK_LIVE_TEST_IMPORTS"
	failures=$((failures + 1))
fi

want="$("$ca" services | wc -l)"
LAYERX_CA_DIR="$work/attestor-ca" "$ca" init >/dev/null
status=0
output="$("$ca" services | while read -r service _; do
	CHECK_LIVE_TIMEOUT=5 LAYERX_ATTESTOR_CA_DIR="$work/attestor-ca" "$ca" issue "$service" </dev/null || exit 1
done 2>&1)" || status=$?
attestor_client="$fly/$kernel/app/data/tls/human-attestor-client"
if [ "$status" -eq 0 ] && [ "$(grep -c '^issued ' <<<"$output")" -eq "$want" ] && [ "$want" -eq 30 ] &&
	openssl verify -CAfile "$work/attestor-ca/ca.pem" "$attestor_client/cert.pem" >/dev/null 2>&1 &&
	! openssl verify -CAfile "$work/ca/ca.pem" "$attestor_client/cert.pem" >/dev/null 2>&1 &&
	cmp -s "$attestor_client/ca.der" "$work/attestor-ca/ca.der" && [ -s "$attestor_client/key.der" ] && [ -s "$attestor_client/cert.der" ] &&
	! grep -q 'PRIVATE KEY' <<<"$output" &&
	grep -q "DNS:kms.process.$internal.internal" <<<"$(openssl x509 -in "$fly/$internal/kms/data/tls/internal-kms/cert.pem" -noout -ext subjectAltName)" &&
	grep -q "DNS:programs.process.$internal.internal" <<<"$(openssl x509 -in "$fly/$internal/programs/data/tls/internal-programs/cert.pem" -noout -ext subjectAltName)" &&
	grep -q "DNS:ingress.process.$webhooks.internal" <<<"$(openssl x509 -in "$fly/$webhooks/secrets/WEBHOOKS_INGRESS_TLS_CERT" -noout -ext subjectAltName)" &&
	[ -s "$fly/$endpoint/secrets/ENDPOINT_CLIENT_P12" ] && [ -s "$fly/$interop/secrets/INTEROP_CLIENT_P12" ] &&
	grep -q "DNS:$(fx_app platform/ramps/fly.toml).internal" <<<"$(openssl x509 -in "$fly/$(fx_app platform/ramps/fly.toml)/secrets/RAMP_CLIENT_CERT" -noout -ext subjectAltName)" &&
	[ -s "$fly/$(fx_app platform/ramps/fly.toml)/secrets/RAMP_CLIENT_P12" ]; then
	echo "ok   ca_issue_every_service"
else
	echo "FAIL ca_issue_every_service: want exit 0 and $want issued lines, the internal groups' certificates on their own volumes under their process-group names and the webhooks ingress certificate staged under its group name, got exit $status"
	printf '%s\n' "$output"
	failures=$((failures + 1))
fi

CHECK_LIVE_TEST_PROGRAM="$fx_checker" LAYERX_ATTESTOR_CA_DIR="$work/attestor-ca" expect check_live_ca_passing "$work/hosts-good.env" 0 ca -- \
	"pass ca ca expires_in=36" \
	"pass receipt-authority app=$kernel chain=ok san=5/5 expires_in=39" \
	"pass human-attestor-client app=$kernel chain=ok san=0/0 expires_in=39" \
	"pass agentd-client app=$kernel chain=ok san=0/0 expires_in=39" \
	"pass agentd app=$kernel chain=ok san=5/5 expires_in=39" \
	"pass internal-kms app=$internal chain=ok san=4/4 expires_in=39" \
	"pass gateway-redis app=$redis chain=ok san=4/4 expires_in=39" \
	"pass gateway-client app=$endpoint chain=ok san=0/0 expires_in=39" \
	"pass developer app=$webhooks chain=ok san=4/4 expires_in=39" \
	"check-live: all checks passed"

if [ "$(wc -l <"$CHECK_LIVE_TEST_CALLS")" -eq "$want" ] && ! grep -qvE "^fx-[a-z0-9-]+ [a-z]+ ssh console sh -c 'cat /[^ ']+'$" "$CHECK_LIVE_TEST_CALLS" &&
	[ "$(grep -c "^$internal kms ssh console " "$CHECK_LIVE_TEST_CALLS")" -eq 1 ] &&
	[ "$(grep -c "^$webhooks ingress ssh console sh -c 'cat /run/secrets/WEBHOOKS_INGRESS_TLS_CERT'" "$CHECK_LIVE_TEST_CALLS")" -eq 1 ]; then
	echo "ok   check_live_ca_reads_one_certificate_per_service"
else
	echo "FAIL check_live_ca_reads_one_certificate_per_service: want $want read-only cat calls, in the process group a row names"
	cat "$CHECK_LIVE_TEST_CALLS"
	failures=$((failures + 1))
fi

# Six ways a certificate fails: absent, signed by another CA, short of its
# SANs, inside thirty days of expiry, its app's toml missing, and its secret
# not mounted by the toml.
rm "${fly:?}/${kernel:?}/app/data/tls/human/cert.pem"
LAYERX_CA_DIR="$work/other-ca" "$ca" init >/dev/null
LAYERX_CA_DIR="$work/other-ca" "$ca" issue identity >/dev/null
openssl req -new -key "$sec/REDIS_TLS_KEY" -subj '/CN=layerx-gateway-redis' -out "$work/redis.csr" 2>/dev/null
printf 'subjectAltName=DNS:localhost\n' >"$work/redis.cnf"
openssl x509 -req -in "$work/redis.csr" -CA "$work/ca/ca.pem" -CAkey "$work/ca/ca.key" -CAcreateserial \
	-days 397 -sha256 -extfile "$work/redis.cnf" -out "$sec/REDIS_TLS_CERT" 2>/dev/null
relay="$fly/$kernel/app/data/tls/relay-archive"
openssl req -new -key "$relay/key.pem" -subj '/CN=layerx-relay-archive' -out "$work/relay.csr" 2>/dev/null
sans="$("$ca" services | awk '$1 == "relay-archive" {print $7}')"
printf 'subjectAltName=%s\n' "${sans//<app>/$kernel}" >"$work/relay.cnf"
openssl x509 -req -in "$work/relay.csr" -CA "$work/ca/ca.pem" -CAkey "$work/ca/ca.key" -CAcreateserial \
	-days 10 -sha256 -extfile "$work/relay.cnf" -out "$relay/cert.pem" 2>/dev/null
rm "${fx:?}/platform/hosted/registry/fly.toml"
printf 'app = "%s"\n' "$interop" >"$fx/platform/hosted/interop/fly.toml"

CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_ca_failing "$work/hosts-good.env" 1 ca -- \
	"pass ca ca expires_in=36" \
	"pass receipt-authority app=$kernel chain=ok san=5/5 expires_in=39" \
	"fail human app=$kernel cert=absent" \
	"fail identity app=$identity chain=untrusted san=5/5 expires_in=39" \
	"fail gateway-redis app=$redis chain=ok san=1/4 missing=DNS:layerx-gateway-redis,DNS:$redis.internal,IP:127.0.0.1 expires_in=39" \
	"fail relay-archive app=$kernel chain=ok san=4/4 expires_in=" \
	"fail registry toml=absent" \
	"fail registry-event-client toml=absent" \
	"fail interop-client app=$interop cert=unmounted" \
	"fail human-attestor-client app=$kernel attestor_ca=absent LAYERX_ATTESTOR_CA_DIR=unset" \
	"pass developer app=$webhooks chain=ok san=4/4 expires_in=39" \
	"check-live: 8 check(s) failed"

# The kernel boundary cases reach the kernel fixture app, whose volume holds
# the agentd-client identity issued above, through the flyctl stand-in.
export CHECK_LIVE_TEST_MACHINES='[{"state":"started","config":{"services":[{"internal_port":8080}]}}]'
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_kernel_boundaries_passing "$work/hosts-good.env" 0 kernel-boundaries -- \
	"pass public-services app=$kernel machines=1 ports=8080 boundaries=none" \
	"pass core app=$kernel url=https://$kernel.internal:9443/readyz identity=agentd-client http=200 ready=true" \
	"pass core-admin app=$kernel url=https://$kernel.internal:9444/readyz identity=agentd-client http=200 ready=true" \
	"pass receipt-authority app=$kernel url=https://$kernel.internal:9445/readyz identity=agentd-client http=200 ready=true" \
	"pass agent-boundary app=$kernel url=https://$kernel.internal:9446/readyz identity=agentd-client http=200 ready=true" \
	"check-live: all checks passed"

if [ "$(grep -c "^$kernel app ssh console " "$CHECK_LIVE_TEST_CALLS")" -eq 1 ] && [ "$(grep -c "^$kernel app machines list" "$CHECK_LIVE_TEST_CALLS")" -eq 1 ] &&
	[ "$(grep -c "^$kernel curl$" "$CHECK_LIVE_TEST_CALLS")" -eq 4 ]; then
	echo "ok   check_live_kernel_boundaries_one_machine_call"
else
	echo "FAIL check_live_kernel_boundaries_one_machine_call: want one machines list, one ssh console call into the kernel app and four readiness requests"
	cat "$CHECK_LIVE_TEST_CALLS"
	failures=$((failures + 1))
fi

export CHECK_LIVE_TEST_MACHINES='[{"state":"started","config":{"services":[{"internal_port":8080},{"internal_port":9445}]}}]'
CHECK_LIVE_TEST_BOUNDARY_DOWN="9446" CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_kernel_boundaries_failing "$work/hosts-good.env" 1 kernel-boundaries -- \
	"fail public-services app=$kernel machines=1 ports=8080,9445 boundaries=9445" \
	"pass core app=$kernel url=https://$kernel.internal:9443/readyz identity=agentd-client http=200 ready=true" \
	"pass receipt-authority app=$kernel url=https://$kernel.internal:9445/readyz identity=agentd-client http=200 ready=true" \
	"fail agent-boundary app=$kernel url=https://$kernel.internal:9446/readyz identity=agentd-client http=503" \
	"check-live: 2 check(s) failed"

export CHECK_LIVE_TEST_MACHINES='[{"state":"started","config":{"services":[{"internal_port":8080}]}}]'
mv "$fly/$kernel/app/data/tls/agentd-client/cert.pem" "$work/agentd-client.pem"
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_kernel_boundaries_without_the_client_identity "$work/hosts-good.env" 1 kernel-boundaries -- \
	"pass public-services app=$kernel machines=1 ports=8080 boundaries=none" \
	"fail core app=$kernel url=https://$kernel.internal:9443/readyz identity=agentd-client http=000" \
	"fail agent-boundary app=$kernel url=https://$kernel.internal:9446/readyz identity=agentd-client http=000" \
	"check-live: 4 check(s) failed"
mv "$work/agentd-client.pem" "$fly/$kernel/app/data/tls/agentd-client/cert.pem"
unset CHECK_LIVE_TEST_MACHINES

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

expect check_live_rpc_placement_passing "$work/hosts-rpc-good.env" 0 rpc-placement -- \
	"pass api1 host=RPC_HOSTS[0] validator=no head=26400000 lag=0" \
	"pass api16 host=RPC_HOSTS[15] validator=no head=26400000 lag=0" \
	"pass VALIDATOR_HOSTS[0] paxd=inactive listen=none" \
	"pass VALIDATOR_HOSTS[1] paxd=inactive listen=none" \
	"check-live: all checks passed"

export CHECK_LIVE_TEST_LAG="api7:11" CHECK_LIVE_TEST_DNS="api1:up-validator-a-node api4:- api5:up-validator-b-web"
expect check_live_rpc_placement_failing "$work/hosts-placement-bad.env" 1 rpc-placement -- \
	"fail api1 host=RPC_HOSTS[0] validator=yes head=26400000 lag=0" \
	"pass api2 host=RPC_HOSTS[1] validator=no head=26400000 lag=0" \
	"fail api4 host=none validator=no head=26400000 lag=0" \
	"fail api5 host=VALIDATOR_HOSTS[1] validator=yes head=26400000 lag=0" \
	"fail api7 host=RPC_HOSTS[6] validator=no head=26399989 lag=11" \
	"fail api16 host=none validator=no head=26400000 lag=0" \
	"fail VALIDATOR_HOSTS[0] paxd=active listen=none" \
	"fail VALIDATOR_HOSTS[1] paxd=inactive listen=80,443" \
	"fail VALIDATOR_HOSTS[2] ssh=255" \
	"check-live: 8 check(s) failed"
unset CHECK_LIVE_TEST_LAG CHECK_LIVE_TEST_DNS

# api1 is served from a validator host that is also RPC_HOSTS[0]: retained by
# owner ruling it passes with its unit and listeners tolerated, a second name
# on that host still fails, and without the retained list it fails as before.
export CHECK_LIVE_TEST_DNS="api1:up-validator-a-kept"
expect check_live_rpc_placement_retained "$work/hosts-placement-retained.env" 0 rpc-placement -- \
	"retained api1: on a validator host by owner ruling" \
	"pass api1 host=RPC_HOSTS[0] validator=retained head=26400000 lag=0" \
	"pass api2 host=RPC_HOSTS[1] validator=no head=26400000 lag=0" \
	"retained VALIDATOR_HOSTS[0] paxd=active listen=443,80 sites=api1 for api1 by owner ruling" \
	"pass VALIDATOR_HOSTS[1] paxd=inactive listen=none" \
	"check-live: all checks passed"

export CHECK_LIVE_TEST_DNS="api1:up-validator-a-kept api2:up-validator-a-kept"
expect check_live_rpc_placement_unretained_name_on_validator "$work/hosts-placement-retained.env" 1 rpc-placement -- \
	"retained api1: on a validator host by owner ruling" \
	"fail api2 host=RPC_HOSTS[0] validator=yes head=26400000 lag=0" \
	"retained VALIDATOR_HOSTS[0] paxd=active listen=443,80 sites=api1 for api1 by owner ruling" \
	"check-live: 1 check(s) failed"

export CHECK_LIVE_TEST_DNS="api1:up-validator-a-kept"
expect check_live_rpc_placement_retained_unset "$work/hosts-placement-unretained.env" 1 rpc-placement -- \
	"fail api1 host=RPC_HOSTS[0] validator=yes head=26400000 lag=0" \
	"fail VALIDATOR_HOSTS[0] paxd=active listen=443,80 sites=api1" \
	"check-live: 2 check(s) failed"
unset CHECK_LIVE_TEST_DNS

# A local hpx registry stand-in: a static tree served on a loopback port
# written to a file, with a published-looking release and landing page under
# good/, which answers with the Fly edge's fly-request-id header as the app
# does, the same release under local/ without that header, as the registry on
# the edge host answers, and a tampered one under bad/ (unbound source
# revision, one artifact rewritten after its manifest line, no /api/nodes, a
# directory listing in place of the landing page).
cat >"$work/responder.py" <<'PY'
import functools
import http.server
import sys

root, port_file = sys.argv[1], sys.argv[2]


class Quiet(http.server.SimpleHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def end_headers(self):
        if self.path.startswith("/good/"):
            self.send_header("Fly-Request-Id", "01FIXTURE-ams")
        super().end_headers()


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
printf '<title>HPX — HyperPax Node Network</title>\n' >"$work/hpx/good/index.html"
cp -r "$work/hpx/good" "$work/hpx/local"
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

hpx_app="$(sed -n 's/^app = "\(.*\)"$/\1/p' "$root/hpx/hosting/fly.toml")"

CHECK_LIVE_HPX_ORIGIN="$origin/good" CHECK_LIVE_HPX_APP_ORIGIN="$origin/good" expect check_live_hpx_passing "$work/hosts-good.env" 0 hpx -- \
	"pass healthz http=200 ok=true chain_id=hyperpax_125-1 source_revision=$(printf 'a%.0s' $(seq 40))" \
	"pass checksums verified=5/5" \
	"pass api-nodes http=200 chain_id=hyperpax_125-1 count=1" \
	"pass app-healthz http=200 ok=true chain_id=hyperpax_125-1 source_revision=$(printf 'a%.0s' $(seq 40))" \
	"pass app-checksums verified=5/5" \
	"pass app-api-nodes http=200 chain_id=hyperpax_125-1 count=1" \
	"pass landing http=200" \
	"pass served-by app=$hpx_app fly-request-id=present healthz=match" \
	"check-live: all checks passed"

CHECK_LIVE_HPX_ORIGIN="$origin/bad" CHECK_LIVE_HPX_APP_ORIGIN="$origin/bad" expect check_live_hpx_failing "$work/hosts-good.env" 1 hpx -- \
	"fail healthz http=200 ok=true chain_id=hyperpax_125-1 source_revision=development" \
	"fail checksums verified=4/5 first=paxd" \
	"fail api-nodes http=404" \
	"fail app-healthz http=200 ok=true chain_id=hyperpax_125-1 source_revision=development" \
	"fail app-checksums verified=4/5 first=paxd" \
	"fail app-api-nodes http=404" \
	"fail landing http=200" \
	"fail served-by app=$hpx_app fly-request-id=absent healthz=match" \
	"check-live: 8 check(s) failed"

CHECK_LIVE_HPX_ORIGIN="$origin/local" CHECK_LIVE_HPX_APP_ORIGIN="$origin/good" expect check_live_hpx_served_locally "$work/hosts-good.env" 1 hpx -- \
	"pass healthz http=200" \
	"pass app-checksums verified=5/5" \
	"pass landing http=200" \
	"fail served-by app=$hpx_app fly-request-id=absent healthz=match" \
	"check-live: 1 check(s) failed"

expect check_live_hpx_hosts_file_lacks_role "$work/hosts-missing.env" 2 hpx -- \
	"check-live: BRINGUP_HOSTS_FILE lacks ARCHIVE_HOST"

# The edge cases read the manifest and rendered sites that the ssh stand-in
# answers from CHECK_LIVE_TEST_EDGE; the registered names resolve to the edge
# fixture destination through CHECK_LIVE_TEST_DNS.
printf '@@sites\n' >"$CHECK_LIVE_TEST_EDGE"
expect check_live_edge_empty_manifest "$work/hosts-good.env" 1 edge -- \
	"fail manifest names=0" \
	"check-live: 1 check(s) failed"

printf '%s\n' "api-mainnet-beta.paxeer.network http paxeer-shared-endpoint 443" @@sites \
	"# rendered by tools/bringup/edge.sh; edit the manifest through it, not this file" \
	"proxy_pass https://\$edge_upstream;" >"$CHECK_LIVE_TEST_EDGE"
export CHECK_LIVE_TEST_DNS="api-mainnet-beta:up-edge up-edge:up-edge search:up-edge machine:up-edge"
expect check_live_edge_passing "$work/hosts-good.env" 0 edge -- \
	"pass manifest names=1" \
	"pass sites validator=0" \
	"pass api-mainnet-beta.paxeer.network mode=http app=paxeer-shared-endpoint edge=yes tls=verified route=/readyz http=200 fly-request-id=present body=match" \
	"check-live: all checks passed"

printf '%s\n' "api-mainnet-beta.paxeer.network http paxeer-shared-endpoint 443" \
	"search.paxeer.network http paxeer-search-front 443" \
	"hooks.paxeer.network http paxeer-no-such-app 443" \
	"machine.paxeer.network stream fx-stream-app 9454" @@sites \
	"	server up-validator-a:443;" >"$CHECK_LIVE_TEST_EDGE"
export CHECK_LIVE_TEST_DNS="api-mainnet-beta:up-rpc-1 up-edge:up-edge search:up-edge machine:up-edge hooks:up-edge" CHECK_LIVE_TEST_DIFFER="search"
expect check_live_edge_failing "$work/hosts-good.env" 1 edge -- \
	"pass manifest names=4" \
	"fail sites validator=1" \
	"fail api-mainnet-beta.paxeer.network mode=http app=paxeer-shared-endpoint edge=no tls=verified route=/readyz http=200 fly-request-id=present body=match" \
	"fail search.paxeer.network mode=http app=paxeer-search-front edge=yes tls=verified route=/healthz http=200 fly-request-id=present body=differ" \
	"fail hooks.paxeer.network mode=http app=paxeer-no-such-app edge=yes route=unknown" \
	"fail machine.paxeer.network mode=stream app=fx-stream-app port=9454 edge=yes presented=none" \
	"check-live: 5 check(s) failed"
unset CHECK_LIVE_TEST_DNS CHECK_LIVE_TEST_DIFFER

# The paxeer-boundary case reads the kernel machine through the flyctl
# stand-in, which runs no layerx-paxeer-boundary process and no hop, and the
# fixture repo carries no search-front.sh to list the serving names.
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_paxeer_boundary_failing "$work/hosts-good.env" 1 paxeer-boundary -- \
	"fail boundaries app=$kernel count=0" \
	"fail hops names=unreadable" \
	"check-live: 2 check(s) failed"
if [ "$(grep -c "^$kernel [a-z]* ssh console " "$CHECK_LIVE_TEST_CALLS")" -eq 1 ] && grep -qF "$kernel app ssh console sh -c 'sh -s'" "$CHECK_LIVE_TEST_CALLS" && grep -q "^CA='-----BEGIN CERTIFICATE-----" "$CHECK_LIVE_TEST_STDIN"; then
	echo "ok   check_live_paxeer_boundary_reads_the_kernel_machine_once"
else
	echo "FAIL check_live_paxeer_boundary_reads_the_kernel_machine_once: want one sh -s call on $kernel carrying the internal CA"
	cat "$CHECK_LIVE_TEST_CALLS"
	failures=$((failures + 1))
fi

# The agent-public cases run the probe of the fixture tree against the kernel
# app's fixture machine: its volume holds the agentd-client identity files, a
# copy of sleep named layerx-agentd stands in for the running daemon with its
# program bearer and probe program in its environment, and the curl stand-in
# answers the machine name as the agentd boundary does.
mkdir -p "$fx/platform/hosted/agentd" "$work/fly/$kernel/app/data/tls/agentd-client" "$work/agentd"
cp "$root/platform/hosted/agentd/probe.sh" "$fx/platform/hosted/agentd/"
for file in ca.pem cert.pem key.pem; do
	printf 'fixture %s\n' "$file" >"$work/fly/$kernel/app/data/tls/agentd-client/$file"
done
cp "$(command -v sleep)" "$work/agentd/layerx-agentd"
CHECK_LIVE_TEST_AGENT_BEARER="fixture-program-bearer-$(openssl rand -hex 16)"
CHECK_LIVE_TEST_AGENT_PROGRAM="$(openssl rand -hex 32)"
export CHECK_LIVE_TEST_AGENT_BEARER CHECK_LIVE_TEST_AGENT_PROGRAM
export CHECK_LIVE_TEST_AGENT_RPC_CODE=200
export CHECK_LIVE_TEST_AGENT_RPC='{"request_id":"req_fixture","value":{"program_id":"fixture","state":"active"},"verification_status":{"state":"Achieved","level":"SequencerSigned"}}'

CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_agent_public_no_daemon "$work/hosts-good.env" 1 agent-public -- \
	"fail ipv4 app=$kernel dedicated=0" \
	"fail agentd app=$kernel process=none" \
	"check-live: 2 check(s) failed"

LAYERX_AGENT_PROGRAM_BEARER_TOKEN="$CHECK_LIVE_TEST_AGENT_BEARER" LAYERX_AGENT_PROGRAM_PROBE_ID="$CHECK_LIVE_TEST_AGENT_PROGRAM" \
	"$work/agentd/layerx-agentd" 600 &
agentd_pid=$!
export CHECK_LIVE_TEST_IPS='[{"Type":"shared_v4"},{"Type":"v6"},{"Type":"v4"}]'
: >"$CHECK_LIVE_TEST_STDIN"
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_agent_public_passing "$work/hosts-good.env" 0 agent-public -- \
	"pass ipv4 app=$kernel dedicated=1" \
	"pass agentd app=$kernel process=found" \
	"pass probe https://machine.paxeer.network:9454 ready=true bearer=enforced client-cert=enforced" \
	"pass rpc https://machine.paxeer.network:9454/rpc operation=program.discover http=200 request_id=present value=present verification_status=Achieved" \
	"check-live: all checks passed"

if grep -qF "$CHECK_LIVE_TEST_AGENT_BEARER" "$CHECK_LIVE_TEST_STDIN" "$CHECK_LIVE_TEST_CALLS"; then
	echo "FAIL check_live_agent_public_bearer_stays_in_machine: the bearer reached the ssh console input or a call line"
	failures=$((failures + 1))
else
	echo "ok   check_live_agent_public_bearer_stays_in_machine"
fi

export CHECK_LIVE_TEST_IPS='[{"Type":"shared_v4"}]' CHECK_LIVE_TEST_AGENT_RPC_CODE=403
export CHECK_LIVE_TEST_AGENT_RPC='{"class":"CapabilityRefusal","protocol_result_code":null,"retriability":"NonRetriable","request_id":"req_fixture","reason":"operation_not_granted"}'
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_agent_public_refused "$work/hosts-good.env" 1 agent-public -- \
	"fail ipv4 app=$kernel dedicated=0" \
	"pass agentd app=$kernel process=found" \
	"pass probe https://machine.paxeer.network:9454 ready=true bearer=enforced client-cert=enforced" \
	"fail rpc https://machine.paxeer.network:9454/rpc operation=program.discover http=403 request_id=present value=absent verification_status=absent" \
	"check-live: 2 check(s) failed"

rm "$work/fly/$kernel/app/data/tls/agentd-client/key.pem"
CHECK_LIVE_TEST_DOWN="machine" CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_agent_public_unreachable "$work/hosts-good.env" 1 agent-public -- \
	"pass agentd app=$kernel process=found" \
	"fail probe https://machine.paxeer.network:9454 exit=2 probe.sh:" \
	"fail rpc https://machine.paxeer.network:9454/rpc operation=program.discover transport=curl-7" \
	"check-live: 3 check(s) failed"
kill "$agentd_pid" 2>/dev/null || true
wait "$agentd_pid" 2>/dev/null || true
agentd_pid=""
unset CHECK_LIVE_TEST_IPS CHECK_LIVE_TEST_AGENT_RPC CHECK_LIVE_TEST_AGENT_RPC_CODE
# kernel-app: the fixture machine list and the init status directory of the
# kernel app; the init and each running service are live local processes, so
# their uid is this test's uid.
me="$(id -u)"
kinit="$CHECK_LIVE_TEST_FLY/$kernel/app/run/layerx/init"
mkdir -p "$kinit"
bash -c 'exec -a /usr/local/bin/kernel-init sleep 300' &
kernel_init_pid=$!
sleep 300 &
kernel_service_pid=$!
echo "$kernel_init_pid" >"$kinit/pid"
echo "$me running $kernel_service_pid" >"$kinit/human"
echo "$me running $kernel_service_pid" >"$kinit/human-tls"
echo "4020 waiting genesis" >"$kinit/layerxd"
printf '[{"state":"started","config":{"mounts":[{"volume":"vol_kernel","path":"/data"}]}}]' \
	>"$CHECK_LIVE_TEST_FLY/$kernel/machines.json"
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_kernel_app_passing "$work/hosts-good.env" 0 kernel-app -- \
	"pass machines app=$kernel machines=1 started=1 volume=/data" \
	"pass init app=$kernel uid=0 entrypoint=kernel-init" \
	"pass service human uid=$me state=running" \
	"pass service human-tls uid=$me state=running" \
	"pass service layerxd uid=4020 state=waiting-genesis" \
	"check-live: all checks passed"
echo "4020 running $kernel_service_pid" >"$kinit/human"
echo "4020 waiting /data/layerx/keys/publication/binding-policy.json" >"$kinit/treasury-signer"
echo "4020 waiting /data/tls/human-attestor-client/ca.der" >"$kinit/human-components"
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_kernel_app_failing "$work/hosts-good.env" 1 kernel-app -- \
	"fail service human uid=$me want=4020 state=running" \
	"fail service human-components uid=4020 state=waiting on=/data/tls/human-attestor-client/ca.der" \
	"fail service treasury-signer uid=4020 state=waiting on=/data/layerx/keys/publication/binding-policy.json" \
	"check-live: 3 check(s) failed"
printf '[{"state":"started","config":{"mounts":[]}},{"state":"stopped","config":{}}]' \
	>"$CHECK_LIVE_TEST_FLY/$kernel/machines.json"
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_kernel_app_volumeless "$work/hosts-good.env" 1 kernel-app -- \
	"fail machines app=$kernel machines=2 started=1 volume-at-data=0" \
	"check-live: 1 check(s) failed"
kn_report="$work/kernel-node-real"
if ! PATH="$real_path" python3 "$root/tests/daemon/paxeer_x_kernel_readiness.py" \
	--legacy-check-live "$kn_report" --app "$kernel" >"$work/kernel-node-real.log" 2>&1; then
	cat "$work/kernel-node-real.log" >&2
	exit 1
fi
read -r kn_genesis kn_public < <(python3 -I - "$kn_report/report.json" <<'PY'
import json, re, sys
from pathlib import Path
value = json.loads(Path(sys.argv[1]).read_text())
for field in ('genesis_sha256', 'sequencer_public_key'):
    if not re.fullmatch('[0-9a-f]{64}', value[field]): raise ValueError('real kernel identity missing')
print(value['genesis_sha256'], value['sequencer_public_key'])
PY
)
expect_kernel_evidence() {
	local name=$1 case_name=$2 expected_exit=$3
	shift 3
	if python3 -I - "$kn_report/report.json" "$root" "$case_name" "$expected_exit" "$@" <<'PY'
import json, subprocess, sys
from pathlib import Path
path, root, name, expected, *lines = sys.argv[1:]
value = json.loads(Path(path).read_text())
revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip()
assert value['source_revision'] == revision, 'kernel fixture source revision changed'
case = value['cases'][name]
assert case['exit_code'] == int(expected), case
assert isinstance(case['stdout'], str), 'missing real checker output'
for line in lines:
    assert line in case['stdout'], (line, case['stdout'])
PY
	then echo "ok   $name"; else echo "FAIL $name: real kernel readiness evidence mismatch"; failures=$((failures + 1)); fi
}
expect_kernel_evidence check_live_kernel_node_passing passing 0 \
	"pass genesis app=$kernel sha256=$kn_genesis replica=match" \
	"pass sequencer public=$kn_public core=match" \
	"pass supervisor state=running" \
	"pass lni socket=present" \
	'pass replica head={"head":3}' \
	"pass clock layerxd" \
	"pass clock guarantor-2" \
	"check-live: all checks passed"
expect_kernel_evidence check_live_kernel_node_one_failing one_failing 1 \
	"pass genesis app=$kernel sha256=$kn_genesis replica=match" \
	"fail clock guarantor-1 exec=sleep" \
	"check-live: 1 check(s) failed"
expect_kernel_evidence check_live_kernel_node_no_kernel_image no_kernel_image 1 \
	"fail genesis app=$kernel sha256=absent replica=none" \
	"fail sequencer public=absent core=absent" \
	"fail supervisor status=absent" \
	"fail lni socket=absent" \
	"fail replica head=absent" \
	"fail clock layerxd exec=absent" \
	"check-live: 10 check(s) failed"
kill "$kernel_init_pid" "$kernel_service_pid" 2>/dev/null || true

# The gas cases run the probe of the fixture tree against the gas station
# app's fixture machine, whose volume holds the rendered station.json, with a
# real cast keystore as the check account; the curl stand-in answers the
# chain name's /quote and /submit and the router's JSON-RPC from the files of
# CHECK_LIVE_TEST_GAS, and keeps each request the station received.
gas="$(fx_app interop/deploy/gas-station/fly.toml)"
gas_sid=0x21f7b20a555199fa73A238B1a91FD0f549068fEe
gas_paymaster=0x1234567890abcdef1234567890abcdef12345678
gas_sponsor=0x5ce0000000000000000000000000000000000001
gas_password="fixture-gas-password-$(openssl rand -hex 16)"
export CHECK_LIVE_TEST_GAS="$work/gas"
mkdir -p "$CHECK_LIVE_TEST_GAS" "$work/gas-keystore"
printf '%s\n' "$gas_password" >"$work/gas-password"
cast wallet new "$work/gas-keystore" --unsafe-password "$gas_password" >/dev/null
gas_account="$(cast wallet address --keystore "$work/gas-keystore"/* --password-file "$work/gas-password")"

CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_gas_inputs_unset "$work/hosts-good.env" 2 gas -- \
	"check-live: CHECK_LIVE_GAS_ACCOUNT_KEYSTORE is unset"

CHECK_LIVE_GAS_ACCOUNT_KEYSTORE="$(echo "$work/gas-keystore"/*)"
export CHECK_LIVE_GAS_ACCOUNT_KEYSTORE CHECK_LIVE_GAS_ACCOUNT_PASSWORD_FILE="$work/gas-password"
export CHECK_LIVE_GAS_MAX_TOKEN_AMOUNT=5000 CHECK_LIVE_GAS_RECEIPT_ATTEMPTS=1
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_gas_toml_absent "$work/hosts-good.env" 1 gas -- \
	"fail gas toml=absent" \
	"check-live: 1 check(s) failed"

mkdir -p "$fx/interop/deploy/gas-station" "$work/fly/$gas/app/data/gas-station"
printf 'app = "%s"\n' "$gas" >"$fx/interop/deploy/gas-station/fly.toml"
printf '{"listen":"[::]:8080","chain_id":125,"endpoints":["https://api-mainnet-beta.paxeer.network/rpc","https://api4.mainnet-beta.paxeer.network","https://api5.mainnet-beta.paxeer.network"],"paymaster":"%s","token":"%s","decimals":6,"gas_limit":200000,"max_priority_fee_per_gas":1000000000,"max_rate_age":300}\n' \
	"$gas_paymaster" "$gas_sid" >"$work/fly/$gas/app/data/gas-station/station.json"
export CHECK_LIVE_TEST_MACHINES='[{"state":"started","config":{"mounts":[{"volume":"vol_fixture"}]}}]'
printf '"0xef0100%s"' "${gas_paymaster#0x}" >"$CHECK_LIVE_TEST_GAS/eth_getCode"
printf '"0x%064x"' 3114000 >"$CHECK_LIVE_TEST_GAS/eth_call-$(cast sig 'currentRate()')"
printf '"0x%064x"' 0 >"$CHECK_LIVE_TEST_GAS/eth_call-$(cast sig 'nonce()')"
printf '"0x%064x"' 1899999880 >"$CHECK_LIVE_TEST_GAS/eth_call-$(cast sig 'rateUpdatedAt()')"
printf '{"timestamp":"0x%x"}' 1900000000 >"$CHECK_LIVE_TEST_GAS/eth_getBlockByNumber"
printf '"0x3b9aca00"' >"$CHECK_LIVE_TEST_GAS/eth_gasPrice"
printf '"0x0"' >"$CHECK_LIVE_TEST_GAS/eth_getTransactionCount"
# gasCost = 200000 * (2 * 1 gwei + 1 gwei) = 6e14 wei; at 3114000 SID base
# units per PAX the expected amount is ceil(1868.4) = 1869.
gas_quote() {
	printf '{"quote":{"sponsor":"%s","token":"%s","maxTokenAmount":"5000","tokenAmount":"%s","deadline":"1900000000","quoteNonce":"7","gasCost":"600000000000000","decimals":6},"relayerSignature":"0x%s"}' \
		"$gas_sponsor" "$gas_sid" "$1" "$(printf 'ab%.0s' {1..65})" >"$CHECK_LIVE_TEST_GAS/quote.body"
}
gas_quote 1869
printf '200' >"$CHECK_LIVE_TEST_GAS/quote.code"
printf '{"transactionHash":"0x%s"}' "$(printf 'cd%.0s' {1..32})" >"$CHECK_LIVE_TEST_GAS/submit.body"
printf '200' >"$CHECK_LIVE_TEST_GAS/submit.code"
printf '{"status":"0x1","logs":[{"address":"%s","topics":["0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef","0x%064s","0x%064s"],"data":"0x%064x"}]}' \
	"$gas_sid" "${gas_account#0x}" "${gas_sponsor#0x}" 1869 | tr " " 0 >"$CHECK_LIVE_TEST_GAS/eth_getTransactionReceipt"
: >"$CHECK_LIVE_TEST_STDIN"
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_gas_passing "$work/hosts-good.env" 0 gas -- \
	"pass machines app=$gas machines=1 started=1 volumes=1" \
	"pass config app=$gas chain_id=125 token=SID endpoints=3 first=router paymaster=$gas_paymaster" \
	"pass rate-age paymaster=$gas_paymaster updated_at=1899999880 age=120 max_rate_age=300" \
	"pass delegation account=$gas_account delegate=$gas_paymaster" \
	"pass quote https://chain.paxeer.network/quote http=200 rate=3114000 token_amount=1869 expected=1869 max=5000 sponsor=$gas_sponsor" \
	"pass submit https://chain.paxeer.network/submit http=200 tx=0x$(printf 'cd%.0s' {1..32}) status=1 sid_transfer=1869 want=1869 to=sponsor" \
	"check-live: all checks passed"

if python3 - "$CHECK_LIVE_TEST_GAS/submit.request" "$gas_account" "$gas_paymaster" "$(cast sig 'executeSponsored((address,uint256,bytes)[],(address,address,uint256,uint256,uint256,uint256,uint256),bytes,bytes)')" <<'PY'
import json, sys
r = json.load(open(sys.argv[1]))
account, paymaster, selector = sys.argv[2].lower(), sys.argv[3].lower(), sys.argv[4]
q = r["batch"]["quote"]
assert r["call"]["to"].lower() == account and r["call"]["value"] == "0" and r["call"]["data"].startswith(selector)
assert r["authorization"]["address"].lower() == paymaster and r["authorization"]["nonce"] == "0" and r["authorization"]["yParity"] in (0, 1)
assert len(r["authorization"]["r"]) == 66 and len(r["authorization"]["s"]) == 66
assert r["batch"]["account"].lower() == account and r["batch"]["nonce"] == "0" and r["batch"]["chainId"] == "125"
assert q["tokenAmount"] == "1869" and q["quoteNonce"] == "7" and q["gasCost"] == "600000000000000" and q["decimals"] == 6
assert len(r["accountSignature"]) == 132 and r["relayerSignature"] == "0x" + "ab" * 65
PY
then
	echo "ok   check_live_gas_submit_request_shape"
else
	echo "FAIL check_live_gas_submit_request_shape: the /submit request the station received is not the sponsored batch of the quote"
	failures=$((failures + 1))
fi
if grep -qF -e "$gas_password" -e "$CHECK_LIVE_GAS_ACCOUNT_KEYSTORE" "$CHECK_LIVE_TEST_STDIN" "$CHECK_LIVE_TEST_CALLS" "$CHECK_LIVE_TEST_GAS"/*.request; then
	echo "FAIL check_live_gas_keystore_stays_local: the password or keystore path reached a machine, a call line or a station request"
	failures=$((failures + 1))
else
	echo "ok   check_live_gas_keystore_stays_local"
fi

# The rate-spend case gives the machine the publisher's rate.json and a
# journal of a publication settled today, one settled the day before and one
# unsettled today; only today's settled cost counts as spent.
gas_hash() { printf '[%s%d]' "$(printf "$1,%.0s" {1..31})" "$1"; }
printf '{"rate_gas_budget_per_day":1000000,"rate_max_fee_per_gas":2000000000}\n' >"$work/fly/$gas/app/data/gas-station/rate.json"
{
	printf '{"kind":"rate_published","publication":{"owner":[],"nonce":7,"hash":%s,"rate":[],"gas_limit":60000,"max_fee_per_gas":"2000000000","signed_at":1899999000}}\n' "$(gas_hash 1)"
	printf '{"kind":"rate_settled","hash":%s,"settlement":{"block_number":16,"gas_used":35000,"cost_wei":"52500000000000","succeeded":true}}\n' "$(gas_hash 1)"
	printf '{"kind":"rate_published","publication":{"owner":[],"nonce":6,"hash":%s,"rate":[],"gas_limit":60000,"max_fee_per_gas":"2000000000","signed_at":1899900000}}\n' "$(gas_hash 2)"
	printf '{"kind":"rate_settled","hash":%s,"settlement":{"block_number":15,"gas_used":35000,"cost_wei":"7","succeeded":true}}\n' "$(gas_hash 2)"
	printf '{"kind":"rate_published","publication":{"owner":[],"nonce":8,"hash":%s,"rate":[],"gas_limit":60000,"max_fee_per_gas":"2000000000","signed_at":1899999900}}\n' "$(gas_hash 3)"
} >"$work/fly/$gas/app/data/gas-station/rate.jsonl"
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_gas_rate_spend "$work/hosts-good.env" 0 gas -- \
	"pass rate-age paymaster=$gas_paymaster updated_at=1899999880 age=120 max_rate_age=300" \
	"info rate-spend app=$gas daily_wei_max=2000000000000000 spent_wei=52500000000000" \
	"pass quote https://chain.paxeer.network/quote http=200 rate=3114000 token_amount=1869 expected=1869 max=5000 sponsor=$gas_sponsor" \
	"check-live: all checks passed"
rm "$work/fly/$gas/app/data/gas-station/rate.json" "$work/fly/$gas/app/data/gas-station/rate.jsonl"

gas_quote 2000
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_gas_quote_outside_spread "$work/hosts-good.env" 1 gas -- \
	"pass delegation account=$gas_account delegate=$gas_paymaster" \
	"fail quote https://chain.paxeer.network/quote http=200 rate=3114000 token_amount=2000 expected=1869 max=5000" \
	"check-live: 1 check(s) failed"

printf '"0x%064x"' 1899999699 >"$CHECK_LIVE_TEST_GAS/eth_call-$(cast sig 'rateUpdatedAt()')"
gas_quote 1869
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_gas_rate_stale "$work/hosts-good.env" 1 gas -- \
	"fail rate-age paymaster=$gas_paymaster updated_at=1899999699 age=301 max_rate_age=300" \
	"pass delegation account=$gas_account delegate=$gas_paymaster" \
	"check-live: 1 check(s) failed"
printf '"0x%064x"' 1899999880 >"$CHECK_LIVE_TEST_GAS/eth_call-$(cast sig 'rateUpdatedAt()')"

gas_quote 1869
printf '"0x"' >"$CHECK_LIVE_TEST_GAS/eth_getCode"
export CHECK_LIVE_TEST_MACHINES='[]'
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_gas_undelegated "$work/hosts-good.env" 1 gas -- \
	"fail machines app=$gas machines=0 started=0 volumes=0" \
	"fail delegation account=$gas_account code=0x want=0xef0100${gas_paymaster#0x}" \
	"check-live: 2 check(s) failed"
unset CHECK_LIVE_TEST_GAS CHECK_LIVE_TEST_MACHINES CHECK_LIVE_GAS_ACCOUNT_KEYSTORE CHECK_LIVE_GAS_ACCOUNT_PASSWORD_FILE CHECK_LIVE_GAS_MAX_TOKEN_AMOUNT CHECK_LIVE_GAS_RECEIPT_ATTEMPTS

# The internal cases serve a fixture chain for the router name from a local
# openssl s_server whose last issuer is a fixture root named ISRG Root X1, run
# copies of sleep named layerx-event-source with the payments and programs
# environment for the pin reads, and read the five process-group names from
# the kernel app's fixture machine, whose volume holds the human-event-client
# identity from the ca cases.
chain="$work/router-chain"
mkdir -p "$chain" "$work/internal"
for n in X1 X2; do
	openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 30 -subj "/O=Internet Security Research Group/CN=ISRG Root $n" \
		-addext 'basicConstraints=critical,CA:TRUE' -addext 'keyUsage=critical,keyCertSign,cRLSign' \
		-keyout "$chain/root-$n.key" -out "$chain/root-$n.pem" 2>/dev/null
	openssl x509 -in "$chain/root-$n.pem" -outform DER -out "$chain/root-$n.der"
done
openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj "/O=Fixture/CN=Fixture E1" -keyout "$chain/inter.key" -out "$chain/inter.csr" 2>/dev/null
printf 'basicConstraints=critical,CA:TRUE,pathlen:0\nkeyUsage=critical,keyCertSign,cRLSign\n' >"$chain/inter.ext"
openssl x509 -req -in "$chain/inter.csr" -CA "$chain/root-X1.pem" -CAkey "$chain/root-X1.key" -CAcreateserial -days 30 -extfile "$chain/inter.ext" -out "$chain/inter.pem" 2>/dev/null
openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj "/CN=api-mainnet-beta.paxeer.network" -keyout "$chain/leaf.key" -out "$chain/leaf.csr" 2>/dev/null
printf 'subjectAltName=DNS:api-mainnet-beta.paxeer.network\nextendedKeyUsage=serverAuth\n' >"$chain/leaf.ext"
openssl x509 -req -in "$chain/leaf.csr" -CA "$chain/inter.pem" -CAkey "$chain/inter.key" -CAcreateserial -days 30 -extfile "$chain/leaf.ext" -out "$chain/leaf.pem" 2>/dev/null
router_port="$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
openssl s_server -quiet -accept "127.0.0.1:$router_port" -cert "$chain/leaf.pem" -key "$chain/leaf.key" -cert_chain "$chain/inter.pem" </dev/null >/dev/null 2>&1 &
internal_pids="$!"
for _ in $(seq 50); do
	(: <"/dev/tcp/127.0.0.1/$router_port") 2>/dev/null && break
	sleep 0.1
done
export CHECK_LIVE_ROUTER_CONNECT="127.0.0.1:$router_port"

CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_internal_nothing_running "$work/hosts-good.env" 1 internal -- \
	"pass public-ips app=$internal count=0" \
	"pass public-ips app=$(fx_app platform/hosted/internal/redis.toml) count=0" \
	"pass router-root host=api-mainnet-beta.paxeer.network root=X1" \
	"fail upstream-ca group=payments process=none" \
	"fail upstream-ca group=programs process=none" \
	"pass readiness group=kms url=https://kms.process.$internal.internal:9443/readyz from=$kernel http=200 ready=true" \
	"check-live: 2 check(s) failed"

cp "$(command -v sleep)" "$work/internal/layerx-event-source"
for group in payments programs; do
	LAYERX_EVENTS_KIND="$group" LAYERX_EVENTS_UPSTREAM_URL=https://api-mainnet-beta.paxeer.network LAYERX_EVENTS_UPSTREAM_CA_DER="$chain/root-X1.der" \
		"$work/internal/layerx-event-source" 600 &
	internal_pids="$internal_pids $!"
done
: >"$CHECK_LIVE_TEST_STDIN"
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_internal_passing "$work/hosts-good.env" 0 internal -- \
	"pass router-root host=api-mainnet-beta.paxeer.network root=X1" \
	"pass upstream-ca group=payments url=https://api-mainnet-beta.paxeer.network root=X1 verifies=yes" \
	"pass upstream-ca group=programs url=https://api-mainnet-beta.paxeer.network root=X1 verifies=yes" \
	"pass readiness group=kms url=https://kms.process.$internal.internal:9443/readyz from=$kernel http=200 ready=true" \
	"pass readiness group=journeys url=https://journeys.process.$internal.internal:9443/readyz from=$kernel http=200 ready=true" \
	"pass readiness group=payments url=https://payments.process.$internal.internal:9443/readyz from=$kernel http=200 ready=true" \
	"pass readiness group=approvals url=https://approvals.process.$internal.internal:9443/readyz from=$kernel http=200 ready=true" \
	"pass readiness group=programs url=https://programs.process.$internal.internal:9443/readyz from=$kernel http=200 ready=true" \
	"check-live: all checks passed"
if [ "$(grep -c "^$kernel app ssh console sh -c 'tls=/data/tls/human-event-client app=$internal limit=5 sh -s'$" "$CHECK_LIVE_TEST_CALLS")" -eq 1 ] &&
	[ "$(grep -c "^$internal payments ssh console sh -c 'kind=payments sh -s'$" "$CHECK_LIVE_TEST_CALLS")" -eq 1 ] &&
	[ "$(grep -c "^$internal programs ssh console sh -c 'kind=programs sh -s'$" "$CHECK_LIVE_TEST_CALLS")" -eq 1 ] &&
	! grep -q 'PRIVATE KEY' "$CHECK_LIVE_TEST_STDIN"; then
	echo "ok   check_live_internal_reads_each_machine_once"
else
	echo "FAIL check_live_internal_reads_each_machine_once: want one sh -s call on $kernel with the event client identity path and one on each pinned group, and no key on the console input"
	cat "$CHECK_LIVE_TEST_CALLS"
	failures=$((failures + 1))
fi

kill "${internal_pids##* }" 2>/dev/null || true
wait "${internal_pids##* }" 2>/dev/null || true
LAYERX_EVENTS_KIND=programs LAYERX_EVENTS_UPSTREAM_URL=https://api-mainnet-beta.paxeer.network LAYERX_EVENTS_UPSTREAM_CA_DER="$chain/root-X2.der" \
	"$work/internal/layerx-event-source" 600 &
internal_pids="$internal_pids $!"
export CHECK_LIVE_TEST_IPS='[{"Type":"v6"}]'
CHECK_LIVE_TEST_DOWN="approvals" CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_internal_failing "$work/hosts-good.env" 1 internal -- \
	"fail public-ips app=$internal count=1" \
	"fail public-ips app=$(fx_app platform/hosted/internal/redis.toml) count=1" \
	"pass upstream-ca group=payments url=https://api-mainnet-beta.paxeer.network root=X1 verifies=yes" \
	"fail upstream-ca group=programs url=https://api-mainnet-beta.paxeer.network pin=ISRG-Root-X2 served=X1" \
	"fail readiness group=approvals url=https://approvals.process.$internal.internal:9443/readyz from=$kernel curl=7" \
	"pass readiness group=kms url=https://kms.process.$internal.internal:9443/readyz from=$kernel http=200 ready=true" \
	"check-live: 4 check(s) failed"
unset CHECK_LIVE_TEST_IPS

export CHECK_LIVE_ROUTER_CONNECT="127.0.0.1:1"
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_internal_router_unreadable "$work/hosts-good.env" 1 internal -- \
	"fail router-root host=api-mainnet-beta.paxeer.network root=unreadable" \
	"fail upstream-ca group=payments url=https://api-mainnet-beta.paxeer.network pin=ISRG-Root-X1 served=unreadable" \
	"check-live: 3 check(s) failed"
unset CHECK_LIVE_ROUTER_CONNECT

# The bridge cases read the relayer app's fixture machine: the fixture tree
# carries two chains and a checklist stand-in that passes a record holding
# "match" and otherwise fails naming an RPC URL, as the checklist's errors
# can; copies of sleep named layerx-bridge-relayer and layerx-mirror-signer
# stand in for the two running processes, and the volume holds the journal.
bridge="$(fx_app interop/deploy/bridge-relayer/fly.toml)"
mkdir -p "$fx/interop/deploy/bridge-relayer" "$fx/bridge/deploy" "$fx/bridge/evm/chains/base" "$fx/bridge/solana/chains/solana" \
	"$work/bridge/records" "$work/bridge/bin" "$work/fly/$bridge/app/data/relayer"
printf 'app = "%s"\n' "$bridge" >"$fx/interop/deploy/bridge-relayer/fly.toml"
cat >"$fx/bridge/deploy/checklist.sh" <<'SH'
#!/usr/bin/env bash
set -eu
if grep -q match "$PAXEER_BRIDGE_DEPLOYMENT_RECORD"; then
	echo "checklist: $1 every value matches"
	exit 0
fi
echo "checklist: error: $1 vault owner read through https://fixture-rpc.invalid/secret-key differs" >&2
exit 1
SH
chmod +x "$fx/bridge/deploy/checklist.sh"
cp "$(command -v sleep)" "$work/bridge/bin/layerx-bridge-relayer"
cp "$(command -v sleep)" "$work/bridge/bin/layerx-mirror-signer"
printf '{"chain":"base","match":true}\n' >"$work/bridge/records/base.json"
bridge_item="in:8453:0x$(printf 'ab%.0s' {1..32}):7"

expect check_live_bridge_records_unset "$work/hosts-good.env" 2 bridge -- \
	"check-live: PAXEER_BRIDGE_RECORDS_DIR is unset"

PAXEER_BRIDGE_RECORDS_DIR="$work/bridge/records" CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_bridge_nothing_running "$work/hosts-good.env" 1 bridge -- \
	"pass checklist chain=base exit=0" \
	"fail checklist chain=solana record=absent" \
	"fail processes app=$bridge relayer=none signer=none" \
	"fail bridge-in app=$bridge journal=absent" \
	"check-live: 3 check(s) failed"

"$work/bridge/bin/layerx-bridge-relayer" 600 &
bridge_pids="$!"
"$work/bridge/bin/layerx-mirror-signer" 600 &
bridge_pids="$bridge_pids $!"
printf '{"chain":"solana","vault":"differs"}\n' >"$work/bridge/records/solana.json"
printf '%s\n' \
	"{\"kind\":\"observed\",\"item\":\"$bridge_item\",\"observation\":{}}" \
	"{\"kind\":\"completed\",\"item\":\"$bridge_item\",\"completion\":{\"outcome\":\"already_bridged\"}}" \
	>"$work/fly/$bridge/app/data/relayer/journal.jsonl"
PAXEER_BRIDGE_RECORDS_DIR="$work/bridge/records" CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_bridge_not_bridged "$work/hosts-good.env" 1 bridge -- \
	"pass checklist chain=base exit=0" \
	"fail checklist chain=solana exit=1 checklist: error: solana vault owner read through <url> differs" \
	"pass processes app=$bridge relayer=running signer=running" \
	"fail bridge-in app=$bridge journal=present included=none" \
	"check-live: 2 check(s) failed"

printf '{"chain":"solana","match":true}\n' >"$work/bridge/records/solana.json"
printf '%s\n' \
	"{\"kind\":\"observed\",\"item\":\"$bridge_item\",\"observation\":{}}" \
	"{\"kind\":\"signed\",\"item\":\"$bridge_item\",\"signature\":\"0x00\"}" \
	"{\"kind\":\"submitted\",\"item\":\"$bridge_item\",\"submitter\":\"0x00\",\"nonce\":0,\"tx_hash\":\"0x00\",\"raw\":\"0x00\"}" \
	"{\"kind\":\"completed\",\"item\":\"$bridge_item\",\"completion\":{\"outcome\":\"included\",\"tx_hash\":\"0x00\",\"block_number\":1}}" \
	>"$work/fly/$bridge/app/data/relayer/journal.jsonl"
PAXEER_BRIDGE_RECORDS_DIR="$work/bridge/records" CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_bridge_passing "$work/hosts-good.env" 0 bridge -- \
	"pass checklist chain=base exit=0" \
	"pass checklist chain=solana exit=0" \
	"pass processes app=$bridge relayer=running signer=running" \
	"pass bridge-in app=$bridge item=$bridge_item outcome=included" \
	"check-live: all checks passed"
if [ "$(grep -c "^$bridge [a-z]* ssh console " "$CHECK_LIVE_TEST_CALLS")" -eq 1 ] && grep -qF "$bridge app ssh console sh -c 'journal=/data/relayer/journal.jsonl sh -s'" "$CHECK_LIVE_TEST_CALLS"; then
	echo "ok   check_live_bridge_reads_the_relayer_machine_journal"
else
	echo "FAIL check_live_bridge_reads_the_relayer_machine_journal: want one sh -s call on $bridge naming the volume journal"
	cat "$CHECK_LIVE_TEST_CALLS"
	failures=$((failures + 1))
fi
# shellcheck disable=SC2086
kill $bridge_pids 2>/dev/null || true
# shellcheck disable=SC2086
wait $bridge_pids 2>/dev/null || true
bridge_pids=""

# The wallet cases run the probe of the fixture tree, which carries the
# wallet gates of tools/wallet/check-live.sh and a gateway toml naming its
# fixture app; the public wallet name comes from the spec's wallet_endpoint.
mkdir -p "$fx/tools/wallet" "$fx/spec/paxeer-x-bringup"
cp "$root/tools/wallet/check-live.sh" "$fx/tools/wallet/"
gateway="$(fx_app human/wallet/deploy/gateway.toml)"
printf 'app = "%s"\n' "$gateway" >"$fx/human/wallet/deploy/gateway.toml"
CHECK_LIVE_TEST_WALLET_TOKEN="fixture-wallet-token-$(openssl rand -hex 16)"
export CHECK_LIVE_TEST_WALLET_TOKEN
printf '[decision.public_names]\nwallet_endpoint = "walletfx.example.com"\n\n[design]\n' >"$fx/spec/paxeer-x-bringup/spec.kvx"
export CHECK_LIVE_TEST_MACHINES='[{"state":"started","region":"ams"},{"state":"started","region":"iad"},{"state":"stopped","region":"ams"}]'

CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_wallet_requires_the_gateway_token "$work/hosts-good.env" 2 wallet -- \
	"check-live: CHECK_LIVE_GATEWAY_TOKEN is required"

CHECK_LIVE_GATEWAY_TOKEN="$CHECK_LIVE_TEST_WALLET_TOKEN" CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_wallet_passing "$work/hosts-good.env" 0 wallet -- \
	"pass endpoint name=walletfx.example.com source=spec" \
	"pass served_by http=200 x-served-by=paxeer-wallet-gateway" \
	"pass readiness http=200 x-served-by=paxeer-wallet-gateway ready=true" \
	"pass readiness http=200 ready=true attestors=up(5/3) nonce_store=up rpc_pool=up(2) identity_provider=up(1)" \
	"pass me http=200 binding_state=bound address=set did=set main_account_id=set kernel=available" \
	"pass machines started=2 regions=ams,iad app=$gateway" \
	"check-live: all checks passed"
if grep -qx 'walletfx curl' "$CHECK_LIVE_TEST_CALLS" &&
	[ "$(grep -c "^$gateway curl$" "$CHECK_LIVE_TEST_CALLS")" -eq 2 ] && grep -q "^$gateway app machines list" "$CHECK_LIVE_TEST_CALLS"; then
	echo "ok   check_live_wallet_reads_the_spec_name_and_the_gateway_app"
else
	echo "FAIL check_live_wallet_reads_the_spec_name_and_the_gateway_app: want the name, two gateway requests and the machines list"
	cat "$CHECK_LIVE_TEST_CALLS"
	failures=$((failures + 1))
fi

printf '[decision.public_names]\nwallet = "walletfx.example.com"\n\n[design]\nwallet_endpoint = "walletfx.example.com"\n' >"$fx/spec/paxeer-x-bringup/spec.kvx"
export CHECK_LIVE_TEST_MACHINES='[{"state":"started","region":"ams"},{"state":"started","region":"ams"}]'
CHECK_LIVE_GATEWAY_TOKEN=wrong-token CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_wallet_spec_name_unset_one_region "$work/hosts-good.env" 1 wallet -- \
	"fail endpoint source=spec name=unset" \
	"pass readiness http=200 ready=true" \
	"fail me http=401" \
	"fail machines started=2 regions=ams app=$gateway" \
	"check-live: 3 check(s) failed"
if grep -q '^walletfx curl$' "$CHECK_LIVE_TEST_CALLS"; then
	echo "FAIL check_live_wallet_skips_cutover_without_a_name: the cutover gate ran without a name"
	failures=$((failures + 1))
else
	echo "ok   check_live_wallet_skips_cutover_without_a_name"
fi

printf '[decision.public_names]\nwallet_endpoint = "walletfx.example.com"\n\n[design]\n' >"$fx/spec/paxeer-x-bringup/spec.kvx"
export CHECK_LIVE_TEST_MACHINES='[{"state":"started","region":"ams"},{"state":"started","region":"iad"}]'
CHECK_LIVE_TEST_WALLET_SERVED_BY=- CHECK_LIVE_GATEWAY_TOKEN="$CHECK_LIVE_TEST_WALLET_TOKEN" CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_wallet_spec_name_old_proxy "$work/hosts-good.env" 1 wallet -- \
	"pass endpoint name=walletfx.example.com source=spec" \
	"fail served_by http=200 x-served-by=none" \
	"fail readiness http=200 x-served-by=none ready=true" \
	"pass me http=200 binding_state=bound" \
	"pass machines started=2 regions=ams,iad app=$gateway" \
	"check-live: 1 check(s) failed"

CHECK_LIVE_TEST_DOWN="walletfx" CHECK_LIVE_GATEWAY_TOKEN="$CHECK_LIVE_TEST_WALLET_TOKEN" CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_wallet_name_unreachable "$work/hosts-good.env" 1 wallet -- \
	"pass endpoint name=walletfx.example.com source=spec" \
	"fail served_by transport" \
	"fail readiness transport" \
	"pass me http=200" \
	"check-live: 1 check(s) failed"
rm "$fx/spec/paxeer-x-bringup/spec.kvx"
unset CHECK_LIVE_TEST_MACHINES

# retire.sh container mode: stand-ins for curl and docker answer the public
# name as the gateway (or as the old proxy) and keep one container's state.
mkdir -p "$work/retire/bin" "$work/retire/state"
cat >"$work/retire/bin/curl" <<'SH'
#!/usr/bin/env bash
set -eu
headers="" body=""
while [ "$#" -gt 0 ]; do
	case "$1" in
	-D) headers="$2"; shift 2 ;;
	-o) body="$2"; shift 2 ;;
	*) shift ;;
	esac
done
printf 'HTTP/1.1 200 OK\r\nx-served-by: %s\r\n\r\n' "${RETIRE_TEST_SERVED:-paxeer-wallet-gateway}" >"$headers"
printf '{"ready":true}' >"$body"
printf '200'
SH
cat >"$work/retire/bin/docker" <<'SH'
#!/usr/bin/env bash
set -eu
printf 'docker %s\n' "$*" >>"$CHECK_LIVE_TEST_CALLS"
state="$RETIRE_TEST_STATE"
name="${!#}"
[ "$name" = legacy-wallet ] || exit 1
case "$1" in
inspect)
	case "$3" in
	'{{.Id}}') echo 0123456789abcdef ;;
	*) cat "$state" ;;
	esac
	;;
update) read -r running _ <"$state"; echo "$running no" >"$state" ;;
stop) read -r _ policy <"$state"; echo "false $policy" >"$state" ;;
*) exit 1 ;;
esac
SH
chmod +x "$work/retire/bin/curl" "$work/retire/bin/docker"
export RETIRE_TEST_STATE="$work/retire/container"
echo "$(($(date -u +%s) - 120)) paxeer-wallet-gateway.fly.dev" >"$work/retire/state/applied-at"
retire="$root/human/wallet/deploy/cutover/retire.sh"

echo "true unless-stopped" >"$RETIRE_TEST_STATE"
CUTOVER_CONTAINER=legacy-wallet CUTOVER_COMPOSE_DIR="$work/retire" CUTOVER_PUBLIC_HOST=walletfx.example.com \
	CUTOVER_STATE_DIR="$work/retire/state" CUTOVER_SOAK_SECONDS=60 PATH="$work/retire/bin:$PATH" \
	CHECK_LIVE_TEST_PROGRAM="$retire" expect retire_container_and_compose_refused - 2 -- \
	"retire: set exactly one of CUTOVER_CONTAINER and CUTOVER_COMPOSE_DIR"

CUTOVER_PUBLIC_HOST=walletfx.example.com CUTOVER_STATE_DIR="$work/retire/state" CUTOVER_SOAK_SECONDS=60 \
	PATH="$work/retire/bin:$PATH" CHECK_LIVE_TEST_PROGRAM="$retire" expect retire_neither_mode_refused - 2 -- \
	"retire: set exactly one of CUTOVER_CONTAINER and CUTOVER_COMPOSE_DIR"

RETIRE_TEST_SERVED=old-proxy CUTOVER_CONTAINER=legacy-wallet CUTOVER_PUBLIC_HOST=walletfx.example.com \
	CUTOVER_STATE_DIR="$work/retire/state" CUTOVER_SOAK_SECONDS=60 PATH="$work/retire/bin:$PATH" \
	CHECK_LIVE_TEST_PROGRAM="$retire" expect retire_container_not_served_keeps_it - 1 -- \
	"pass soak" \
	"fail served_before /healthz 200 old-proxy true" \
	"retire: the public hostname is not served by the gateway; nothing stopped"
if grep -qE '^docker (update|stop) ' "$CHECK_LIVE_TEST_CALLS" || [ "$(cat "$RETIRE_TEST_STATE")" != "true unless-stopped" ]; then
	echo "FAIL retire_container_not_served_touches_nothing: the container was changed"
	failures=$((failures + 1))
else
	echo "ok   retire_container_not_served_touches_nothing"
fi

CUTOVER_CONTAINER=legacy-wallet CUTOVER_PUBLIC_HOST=walletfx.example.com CUTOVER_STATE_DIR="$work/retire/state" \
	CUTOVER_SOAK_SECONDS=60 PATH="$work/retire/bin:$PATH" CHECK_LIVE_TEST_PROGRAM="$retire" \
	expect retire_container_passing - 0 -- \
	"pass soak" \
	"pass served_before /healthz 200 paxeer-wallet-gateway true" \
	"pass served_before /readyz 200 paxeer-wallet-gateway true" \
	"pass retired container=legacy-wallet running=false restart=no" \
	"pass served_after /readyz 200 paxeer-wallet-gateway true" \
	"retire: retired container legacy-wallet"
if [ "$(grep -E '^docker (update|stop) ' "$CHECK_LIVE_TEST_CALLS" | tr '\n' ';')" = "docker update --restart=no legacy-wallet;docker stop legacy-wallet;" ]; then
	echo "ok   retire_container_clears_restart_then_stops"
else
	echo "FAIL retire_container_clears_restart_then_stops: want docker update --restart=no then docker stop"
	cat "$CHECK_LIVE_TEST_CALLS"
	failures=$((failures + 1))
fi

echo "true unless-stopped" >"$RETIRE_TEST_STATE"
echo "$(date -u +%s) paxeer-wallet-gateway.fly.dev" >"$work/retire/state/applied-at"
CUTOVER_CONTAINER=legacy-wallet CUTOVER_PUBLIC_HOST=walletfx.example.com CUTOVER_STATE_DIR="$work/retire/state" \
	CUTOVER_SOAK_SECONDS=600 PATH="$work/retire/bin:$PATH" CHECK_LIVE_TEST_PROGRAM="$retire" \
	expect retire_container_soak_not_over - 1 -- \
	"of the 600s soak"
unset RETIRE_TEST_STATE

# mirrors: a loopback stand-in for the publisher's status listener answers
# /readyz with the code and body its arguments give, and a stand-in for
# layerx-mirror-verify answers as the verifier does for its config and the
# request on stdin, refusing when a LayerX origin reaches its environment.
mkdir -p "$work/mirrors/bin"
cat >"$work/mirrors/status.py" <<'PY'
import http.server
import sys

code, body = int(sys.argv[2]), sys.argv[3].encode()


class Status(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(code if self.path == "/readyz" else 404)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_):
        pass


http.server.HTTPServer(("127.0.0.1", int(sys.argv[1])), Status).serve_forever()
PY
cat >"$work/mirrors/bin/layerx-mirror-verify" <<'SH'
#!/usr/bin/env bash
set -eu
if [ -n "${LAYERX_NODE_URL:-}${LAYERX_GATEWAY_URL:-}${LAYERX_EXPLORER_API_ORIGIN:-}" ] ||
	! cmp -s - "$CHECK_LIVE_TEST_MIRROR_REQUEST" || [ ! -r "$1" ]; then
	echo '{"ok":false,"error":"configuration"}'
	exit 0
fi
case "${CHECK_LIVE_TEST_MIRROR_VERIFY:-pass}" in
pass) echo '{"ok":true,"verification":{"level":"SequencerSigned","batchNumber":"7","sourceId":"ethereum-primary","provenance":"Canonical","failoverCount":0}}' ;;
*) echo "{\"ok\":false,\"error\":\"$CHECK_LIVE_TEST_MIRROR_VERIFY\"}" ;;
esac
SH
chmod +x "$work/mirrors/bin/layerx-mirror-verify"
printf '{"sources":[{"kind":"ethereum","id":"ethereum-primary"},{"kind":"solana","id":"solana-primary"}]}' >"$work/mirrors/verify.json"
printf '{"sources":[{"kind":"ethereum","id":"ethereum-primary"}]}' >"$work/mirrors/verify-ethereum.json"
printf '{"batch_number":"7","evidence":{"kind":"receipt","canonical_hex":"00"},"policy":{"kind":"exact","candidate":{"source":0,"commitment_hex":"00"}}}' >"$work/mirrors/request.json"
export CHECK_LIVE_TEST_MIRROR_REQUEST="$work/mirrors/request.json"
mirror_port="$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
mirror_status() {
	python3 "$work/mirrors/status.py" "$mirror_port" "$1" "$2" &
	internal_pids="$internal_pids $!"
	for _ in $(seq 50); do
		(: <"/dev/tcp/127.0.0.1/$mirror_port") 2>/dev/null && break
		sleep 0.1
	done
}

CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_mirrors_inputs_unset "$work/hosts-good.env" 2 mirrors -- \
	"check-live: CHECK_LIVE_MIRRORS_VERIFY_CONFIG is unset"

mirror_status 200 '{"ready":true}'
LAYERX_NODE_URL=https://api-mainnet-beta.paxeer.network CHECK_LIVE_MIRRORS_STATUS="127.0.0.1:$mirror_port" \
	CHECK_LIVE_MIRRORS_VERIFY_BIN="$work/mirrors/bin/layerx-mirror-verify" CHECK_LIVE_MIRRORS_VERIFY_CONFIG="$work/mirrors/verify.json" \
	CHECK_LIVE_MIRRORS_REQUEST="$work/mirrors/request.json" CHECK_LIVE_TEST_PROGRAM="$fx_checker" \
	expect check_live_mirrors_passing "$work/hosts-good.env" 0 mirrors -- \
	"pass mirror-readyz app=$kernel listen=127.0.0.1:$mirror_port http=200 ready=true" \
	"pass mirror-sources ethereum=1 solana=1" \
	"pass mirror-verify source=ethereum-primary batch=7 provenance=Canonical level=SequencerSigned" \
	"check-live: all checks passed"
if [ "$(grep -c "^$kernel app ssh console sh -c 'status=127.0.0.1:$mirror_port limit=5 sh -s'$" "$CHECK_LIVE_TEST_CALLS")" -eq 1 ]; then
	echo "ok   check_live_mirrors_reads_the_kernel_machine_once"
else
	echo "FAIL check_live_mirrors_reads_the_kernel_machine_once: want one sh -s call on $kernel with the status listener"
	cat "$CHECK_LIVE_TEST_CALLS"
	failures=$((failures + 1))
fi

kill "${internal_pids##* }" 2>/dev/null || true
wait "${internal_pids##* }" 2>/dev/null || true
mirror_status 503 '{"ready":false}'
CHECK_LIVE_TEST_MIRROR_VERIFY=divergent CHECK_LIVE_MIRRORS_STATUS="127.0.0.1:$mirror_port" \
	CHECK_LIVE_MIRRORS_VERIFY_BIN="$work/mirrors/bin/layerx-mirror-verify" CHECK_LIVE_MIRRORS_VERIFY_CONFIG="$work/mirrors/verify-ethereum.json" \
	CHECK_LIVE_MIRRORS_REQUEST="$work/mirrors/request.json" CHECK_LIVE_TEST_PROGRAM="$fx_checker" \
	expect check_live_mirrors_failing "$work/hosts-good.env" 1 mirrors -- \
	"fail mirror-readyz app=$kernel listen=127.0.0.1:$mirror_port curl=0 http=503" \
	"fail mirror-sources ethereum=1 solana=0" \
	"fail mirror-verify error=divergent provenance=none" \
	"check-live: 3 check(s) failed"
kill "${internal_pids##* }" 2>/dev/null || true
wait "${internal_pids##* }" 2>/dev/null || true
unset CHECK_LIVE_TEST_MIRROR_REQUEST

# The interop cases run the check against a loopback stand-in of the gateway
# and the router's JSON-RPC (CHECK_LIVE_INTEROP_ORIGIN and _RPC point at it):
# it wants the payer's LayerX-Key on every call and an Idempotency-Key on every
# POST to the gateway, answers /readyz from its mode file, the x402 routes with
# the shapes of layerx-interop-service, and settles only the payload the build
# returned, for the activity the encoder signed. The encoder stand-in answers
# the signing request of platform/cli/examples/hosted-send.rs only for the
# payer's seed and the kernel asset.
mkdir -p "$work/interop" "$fx/platform/hosted/node"
cp "$root/platform/hosted/node/bootstrap.sh" "$fx/platform/hosted/node/"
interop_asset="$(sed -n 's/^ASSET_ID="\([0-9a-f]*\)"$/\1/p' "$root/platform/hosted/node/bootstrap.sh")"
interop_secret="lxp_live_$(openssl rand -hex 24)"
printf 'key_fixture:%s\n' "$interop_secret" >"$work/interop/api-key"
openssl genpkey -algorithm ed25519 -out "$work/interop/payer.pem" 2>/dev/null
interop_seed="$(python3 -c '
import sys
from cryptography.hazmat.primitives.serialization import Encoding, NoEncryption, PrivateFormat, load_pem_private_key
print(load_pem_private_key(open(sys.argv[1], "rb").read(), None).private_bytes(Encoding.Raw, PrivateFormat.Raw, NoEncryption()).hex())
' "$work/interop/payer.pem")"
interop_payee="did:layerx:$(openssl rand -hex 32)"
interop_activity="$(openssl rand -hex 32)"
cat >"$work/interop/encoder" <<SH
#!/usr/bin/env python3
import json, sys
request = json.load(sys.stdin)
ok = (request["seed"] == "$interop_seed" and request["asset"] == "$interop_asset" and request["network_id"] == 125
      and request["source_sequence"] == 7 and request["identity_sequence"] == 3 and request["to_name"] == "agent:$interop_payee:main")
if not ok:
    sys.exit(1)
print(json.dumps({"canonical": "ab" * 40, "activity_id": "$interop_activity"}))
SH
chmod +x "$work/interop/encoder"
cat >"$work/interop/gateway.py" <<'PY'
import hashlib, http.server, json, sys

work, authorization, activity = sys.argv[1:]


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def answer(self, code, doc):
        body = json.dumps(doc).encode()
        self.send_response(code)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def mode(self):
        return open(work + "/mode").read().strip()

    def do_GET(self):
        if self.path == "/readyz":
            waiting = "pending" if self.mode() == "not-ready" else "ready"
            return self.answer(200 if waiting == "ready" else 503, {
                "status": "ready" if waiting == "ready" else "not_ready",
                "components": {"durable_gateway_store": "ready", "hosted_gateway": "ready", "receipt_authority": waiting}})
        if self.headers.get("authorization") != authorization:
            return self.answer(401, {"ok": False, "error": {"code": "api_key_required"}})
        if self.path == "/v1/http/x402/supported":
            return self.answer(200, {"ok": True, "result": {"transport": "http", "supported": {
                "kinds": [{"x402Version": 2, "scheme": "exact", "network": "layerx:125"}], "extensions": [], "signers": {}}}})
        self.answer(404, {"ok": False, "error": {"code": "unknown_route"}})

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers.get("content-length", "0"))))
        if self.path == "/rpc":
            method, params = body["method"], body["params"]
            if method == "lx_getBalances":
                result = {"accounts": [{"name": "agent:%s:main" % params[0], "asset_id": ASSET,
                                        "account_id": hashlib.sha256(params[0].encode()).hexdigest()}]}
            elif method == "lx_getAccount":
                result = {"account_id": params[0], "next_sequence": "7"}
            else:
                result = {"next_sequence": "3"}
            return self.answer(200, {"jsonrpc": "2.0", "id": body["id"], "result": result})
        if self.headers.get("authorization") != authorization or not self.headers.get("idempotency-key"):
            return self.answer(401, {"ok": False, "error": {"code": "api_key_required"}})
        if self.path == "/v1/http/x402/seller/offer":
            accepts = body["accepts"][0]
            if accepts["scheme"] != "exact" or accepts["asset"] != ASSET or accepts["network"] != "layerx:125":
                return self.answer(400, {"ok": False, "error": {"code": "invalid_x402_offer"}})
            return self.answer(200, {"ok": True, "result": {"transport": "http", "status": 402,
                                     "payment_required_header": "cmVxdWlyZWQ=", "payment_required": body}})
        if self.path == "/v1/http/x402/buyer/build":
            payload = {"x402Version": 2, "accepted": body["payment_required"]["accepts"][0], "payload": body["scheme_payload"]}
            return self.answer(200, {"ok": True, "result": {"payment_header": "c2lnbmVk", "payment_payload": payload,
                                     "idempotency_key": body["scheme_payload"]["layerxIdempotencyKey"]}})
        if self.path == "/v1/http/x402/settle":
            settled = body["paymentPayload"]["accepted"] == body["paymentRequirements"]
            transaction = "lxp:" + (activity if self.mode() != "other-activity" else "00" * 32)
            return self.answer(200, {"ok": True, "result": {"success": settled, "payer": "fixture", "transaction": transaction,
                                     "network": "layerx:125", "amount": body["paymentRequirements"]["amount"],
                                     "extensions": {"layerx": {"receipt": "cmVjZWlwdA==", "receiptDigest": "ab" * 32}}}})
        self.answer(404, {"ok": False, "error": {"code": "unknown_route"}})


ASSET = open(work + "/asset").read().strip()
server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
open(work + "/port", "w").write(str(server.server_address[1]))
server.serve_forever()
PY
printf '%s' "$interop_asset" >"$work/interop/asset"
printf 'ready' >"$work/interop/mode"
python3 "$work/interop/gateway.py" "$work/interop" "LayerX-Key key_fixture:$interop_secret" "$interop_activity" &
interop_pid=$!
for _ in $(seq 50); do [ -s "$work/interop/port" ] && break; python3 -c 'import time; time.sleep(0.1)'; done
interop_origin="http://127.0.0.1:$(cat "$work/interop/port")"

CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_interop_inputs_unset "$work/hosts-good.env" 2 interop -- \
	"check-live: CHECK_LIVE_INTEROP_API_KEY_FILE is unset"

export CHECK_LIVE_INTEROP_API_KEY_FILE="$work/interop/api-key" CHECK_LIVE_INTEROP_PAYER_KEY_FILE="$work/interop/payer.pem"
export CHECK_LIVE_INTEROP_PAYEE_DID="$interop_payee" CHECK_LIVE_INTEROP_ENCODER="$work/interop/encoder"
export CHECK_LIVE_INTEROP_ORIGIN="$interop_origin" CHECK_LIVE_INTEROP_RPC="$interop_origin/rpc"
export LAYERX_INTEROP_PROTOCOL_NETWORK_ID=125
export CHECK_LIVE_TEST_MACHINES='[{"state":"started","region":"ams"},{"state":"started","region":"fra"},{"state":"stopped","region":"ams"}]'
: >"$CHECK_LIVE_TEST_STDIN"
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_interop_passing "$work/hosts-good.env" 0 interop -- \
	"pass machines app=$interop started=2 regions=ams,fra" \
	"pass readyz $interop_origin/readyz http=200 status=ready components=3/3" \
	"pass supported $interop_origin/v1/http/x402/supported http=200 scheme=exact network=layerx:125" \
	"pass offer $interop_origin/v1/http/x402/seller/offer http=200 status=402 PAYMENT-REQUIRED=present" \
	"pass build $interop_origin/v1/http/x402/buyer/build http=200 PAYMENT-SIGNATURE=present" \
	"pass settle $interop_origin/v1/http/x402/settle http=200 success=true transaction=lxp:$interop_activity receipt=present" \
	"check-live: all checks passed"
output="$(BRINGUP_HOSTS_FILE="$work/hosts-good.env" CHECK_LIVE_TIMEOUT=5 "$fx_checker" interop 2>&1)" || true
if grep -qF -e "$interop_secret" -e "$interop_seed" - "$CHECK_LIVE_TEST_CALLS" "$CHECK_LIVE_TEST_STDIN" <<<"$output"; then
	echo "FAIL check_live_interop_keeps_the_payer_secrets: the router key or the payer seed left the check"
	failures=$((failures + 1))
else
	echo "ok   check_live_interop_keeps_the_payer_secrets"
fi

printf 'not-ready' >"$work/interop/mode"
export CHECK_LIVE_TEST_MACHINES='[{"state":"started","region":"ams"},{"state":"stopped","region":"fra"}]'
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_interop_failing "$work/hosts-good.env" 1 interop -- \
	"fail machines app=$interop started=1 regions=ams" \
	"fail readyz $interop_origin/readyz http=503 status=not_ready components=2/3 not_ready=receipt_authority" \
	"pass settle $interop_origin/v1/http/x402/settle http=200 success=true" \
	"check-live: 2 check(s) failed"

printf 'other-activity' >"$work/interop/mode"
export CHECK_LIVE_TEST_MACHINES='[{"state":"started","region":"ams"},{"state":"started","region":"fra"}]'
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_interop_settles_another_activity "$work/hosts-good.env" 1 interop -- \
	"pass readyz $interop_origin/readyz http=200 status=ready components=3/3" \
	"fail settle $interop_origin/v1/http/x402/settle http=200 success=true transaction=lxp:$(printf '0%.0s' {1..64}) receipt=present want=lxp:$interop_activity" \
	"check-live: 1 check(s) failed"
kill "$interop_pid" 2>/dev/null || true
wait "$interop_pid" 2>/dev/null || true
unset CHECK_LIVE_INTEROP_API_KEY_FILE CHECK_LIVE_INTEROP_PAYER_KEY_FILE CHECK_LIVE_INTEROP_PAYEE_DID CHECK_LIVE_INTEROP_ENCODER
unset CHECK_LIVE_INTEROP_ORIGIN CHECK_LIVE_INTEROP_RPC LAYERX_INTEROP_PROTOCOL_NETWORK_ID CHECK_LIVE_TEST_MACHINES

# The ramp cases: the fixture repository carries the ramp toml of the ca
# fixtures, naming the fixture app, and the real sandbox journey; the edge manifest registers the
# name and it resolves to the edge fixture destination. A ramp stand-in curl
# ahead of the others answers the journey's order and operator requests as
# the ramp does, moving each order through its stages on the operator's work
# actions, and hands every other request to the curl stand-in above.
ramp="$(fx_app platform/ramps/fly.toml)"
mkdir -p "$work/ramp-bin" "$work/ramp-state"
cat >"$work/ramp-bin/curl" <<'SH'
#!/usr/bin/env bash
set -eu
url=""
data=""
prev=""
for arg in "$@"; do
	case "$arg" in
	https://*) url="$arg" ;;
	esac
	[ "$prev" != --data ] || data="$arg"
	prev="$arg"
done
state="$CHECK_LIVE_TEST_RAMP"
case "$url" in
https://ramp.paxeer.network/v1/orders)
	case "$data" in
	*'"order_id":"sandbox-on-ramp-'*) printf '{"order_digest":[%s1]}' "$(printf '1,%.0s' {1..31})" ;;
	*) printf '{"order_digest":[%s2]}' "$(printf '2,%.0s' {1..31})" ;;
	esac
	;;
https://ramp.paxeer.network/v1/orders/*)
	hex="${url##*/}"
	stage="$(cat "$state/$hex" 2>/dev/null || echo created)"
	if [ "$stage" = done ]; then
		printf '{"stage":"done","presentation":{"status":"done","receipt_digest":"ab","provider_evidence_digest":"cd","external_custody_label":"operator vault"}}'
	else
		printf '{"stage":"%s"}' "$stage"
	fi
	;;
*/internal/v1/work)
	first="$(sed -n 's/.*"order_digest":\[\([0-9]*\),.*/\1/p' <<<"$data")"
	action="$(sed -n 's/.*"action":"\([a-z_]*\)".*/\1/p' <<<"$data")"
	hex="$(printf "%02x" $(seq 32 | sed "s/.*/$first/"))"
	case "$first:$action" in
	1:submit_provider) echo provider_settled >"$state/$hex" ;;
	1:submit_layerx | 2:submit_provider) echo done >"$state/$hex" ;;
	2:submit_layerx) echo layerx_verified >"$state/$hex" ;;
	esac
	printf '{}'
	;;
*) exec "$CHECK_LIVE_TEST_RAMP_NEXT" "$@" ;;
esac
SH
chmod +x "$work/ramp-bin/curl"
export CHECK_LIVE_TEST_RAMP="$work/ramp-state" CHECK_LIVE_TEST_RAMP_NEXT="$work/bin/curl"

CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_ramp_inputs_unset "$work/hosts-good.env" 2 ramp -- \
	"check-live: LAYERX_RAMP_CUSTOMER_TOKEN is unset"

export LAYERX_RAMP_CUSTOMER_TOKEN=fixture-customer LAYERX_RAMP_OPERATOR_URL="https://$ramp.fly.dev" LAYERX_RAMP_OPERATOR_TOKEN=fixture-operator
export LAYERX_RAMP_ON_QUOTE_ID=on-quote LAYERX_RAMP_OFF_QUOTE_ID=off-quote LAYERX_RAMP_OFF_GRANT_JSON='{"grant":1}'
export LAYERX_RAMP_ON_ACCOUNT_SEQUENCE=4 LAYERX_RAMP_OFF_RECEIVER_SEQUENCE=5
mv "$fx/platform/ramps/fly.toml" "$work/ramp-fly.toml"
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_ramp_toml_absent "$work/hosts-good.env" 1 ramp -- \
	"fail ramp toml=absent" \
	"check-live: 1 check(s) failed"
mv "$work/ramp-fly.toml" "$fx/platform/ramps/fly.toml"

cp "$root/platform/ramps/sandbox-journey.sh" "$fx/platform/ramps/"
export CHECK_LIVE_TEST_MACHINES='[{"state":"started","config":{"mounts":[{"volume":"vol_fixture"}]}}]'
printf '%s\n' "api-mainnet-beta.paxeer.network http paxeer-shared-endpoint 443" "ramp.paxeer.network http $ramp 443" @@sites \
	"# rendered by tools/bringup/edge.sh; edit the manifest through it, not this file" >"$CHECK_LIVE_TEST_EDGE"
export CHECK_LIVE_TEST_DNS="ramp:up-edge up-edge:up-edge"
PATH="$work/ramp-bin:$PATH" CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_ramp_passing "$work/hosts-good.env" 0 ramp -- \
	"pass machines app=$ramp machines=1 started=1 volumes=1" \
	"pass edge ramp.paxeer.network mode=http app=$ramp edge=yes" \
	"pass readyz https://ramp.paxeer.network/readyz tls=verified http=200 fly-request-id=present body=match" \
	"pass journey https://ramp.paxeer.network on-ramp=done off-ramp=done" \
	"check-live: all checks passed"

rm -f "$work/ramp-state"/*
printf '%s\n' "ramp.paxeer.network http paxeer-other-app 443" @@sites >"$CHECK_LIVE_TEST_EDGE"
export CHECK_LIVE_TEST_DNS="ramp:up-rpc-1 up-edge:up-edge" CHECK_LIVE_TEST_DIFFER="ramp"
export CHECK_LIVE_TEST_MACHINES='[{"state":"started","config":{"mounts":[]}},{"state":"stopped","config":{"mounts":[]}}]'
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_ramp_failing "$work/hosts-good.env" 1 ramp -- \
	"fail machines app=$ramp machines=2 started=1 volumes=0" \
	"fail edge ramp.paxeer.network mode=http app=paxeer-other-app want=$ramp edge=no" \
	"fail readyz https://ramp.paxeer.network/readyz tls=verified http=200 fly-request-id=present body=differ" \
	"fail journey https://ramp.paxeer.network exit=" \
	"check-live: 4 check(s) failed"

export CHECK_LIVE_TEST_DOWN="ramp"
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_ramp_down "$work/hosts-good.env" 1 ramp -- \
	"fail readyz https://ramp.paxeer.network/readyz curl=7" \
	"check-live: 4 check(s) failed"
unset CHECK_LIVE_TEST_DOWN CHECK_LIVE_TEST_DNS CHECK_LIVE_TEST_DIFFER CHECK_LIVE_TEST_MACHINES
unset LAYERX_RAMP_CUSTOMER_TOKEN LAYERX_RAMP_OPERATOR_URL LAYERX_RAMP_OPERATOR_TOKEN LAYERX_RAMP_ON_QUOTE_ID
unset LAYERX_RAMP_OFF_QUOTE_ID LAYERX_RAMP_OFF_GRANT_JSON LAYERX_RAMP_ON_ACCOUNT_SEQUENCE LAYERX_RAMP_OFF_RECEIVER_SEQUENCE

# The indexer cases put the archive node behind up-rpc-7, so its public name
# is api7; a local openssl s_server serves a fixture chain for that name and
# archive.paxeer.network ending at the fixture ISRG Root X1; a copy of sleep
# named layerx-indexer carries the indexer environment and a real SQLite
# database with the cursor tables; and a curl wrapper ahead on PATH answers
# the indexer's .internal /healthz to the internal CA the ca cases left on its
# volume, the /comet location and the router's history methods.
indexer="$(fx_app platform/hosted/indexer/fly.toml)"
ix="$work/indexer"
mkdir -p "$ix/bin" "$ix/state"
sed 's/^RPC_HOSTS=.*/RPC_HOSTS="up-rpc-1 up-rpc-2 up-rpc-3"/; s/^ARCHIVE_HOST=.*/ARCHIVE_HOST=up-rpc-7/' "$work/hosts-good.env" >"$work/hosts-indexer.env"
openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj "/CN=api7.mainnet-beta.paxeer.network" -keyout "$chain/archive.key" -out "$chain/archive.csr" 2>/dev/null
printf 'subjectAltName=DNS:api7.mainnet-beta.paxeer.network,DNS:archive.paxeer.network\nextendedKeyUsage=serverAuth\n' >"$chain/archive.ext"
openssl x509 -req -in "$chain/archive.csr" -CA "$chain/inter.pem" -CAkey "$chain/inter.key" -CAcreateserial -days 30 -extfile "$chain/archive.ext" -out "$chain/archive.pem" 2>/dev/null
archive_port="$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
openssl s_server -quiet -accept "127.0.0.1:$archive_port" -cert "$chain/archive.pem" -key "$chain/archive.key" -cert_chain "$chain/inter.pem" </dev/null >/dev/null 2>&1 &
internal_pids="$internal_pids $!"
for _ in $(seq 50); do
	(: <"/dev/tcp/127.0.0.1/$archive_port") 2>/dev/null && break
	sleep 0.1
done
cat >"$ix/bin/curl" <<'SH'
#!/usr/bin/env bash
set -eu
url=""
out=/dev/stdout
wout=""
data=""
cacert=""
for arg in "$@"; do
	case "$arg" in
	https://*) url="$arg" ;;
	esac
done
args=("$@")
while [ "$#" -gt 0 ]; do
	case "$1" in
	-o) out="$2" ;;
	-w) wout="$2" ;;
	--data) data="$2" ;;
	--cacert) cacert="$2" ;;
	esac
	shift
done
reply() {
	printf '%s' "$2" >"$out"
	[ -z "$wout" ] || printf '%b' "${wout//%\{http_code\}/$1}"
	exit 0
}
case "$url" in
https://*.internal:8095/healthz)
	[ "${CHECK_LIVE_TEST_INDEXER_HEALTH:-up}" = up ] || exit 7
	grep -q 'BEGIN CERTIFICATE' "$cacert" || exit 77
	reply 200 '{"status":"ok"}'
	;;
https://api7.mainnet-beta.paxeer.network/comet)
	[ -n "$data" ] || reply "${CHECK_LIVE_TEST_COMET_GET:-403}" '<html>403 Forbidden</html>'
	reply 200 '{"jsonrpc":"2.0","id":1,"result":{"sync_info":{"earliest_block_height":"25000000"}}}'
	;;
https://api-mainnet-beta.paxeer.network/rpc)
	method="$(python3 -c 'import json, sys; r = json.loads(sys.argv[1]); print(r["method"], r["params"][0])' "$data")"
	case "$method" in
	"px_getUnifiedHistory 0x00000000000000000000000000000000000000aa")
		reply 200 '{"jsonrpc":"2.0","id":1,"result":{"items":[{"id":"1"},{"id":"2"}],"next_cursor":null,"account":"0x00000000000000000000000000000000000000aa","accounts":[{"side":"layerx","account":"'"$CHECK_LIVE_TEST_LAYERX"'"},{"side":"paxeer","account":"0x00000000000000000000000000000000000000aa"}]}}'
		;;
	"lx_getHistory $CHECK_LIVE_TEST_LAYERX" | "px_getHistory 0x00000000000000000000000000000000000000aa")
		reply 200 '{"jsonrpc":"2.0","id":1,"result":{"items":[{"id":"1"}],"next_cursor":null}}'
		;;
	esac
	reply 200 '{"jsonrpc":"2.0","id":1,"error":{"code":-32001,"message":"History unavailable","data":{"code":"indexer_unavailable"}}}'
	;;
esac
exec "$CHECK_LIVE_TEST_OUTER_CURL" "${args[@]}"
SH
chmod +x "$ix/bin/curl"
export CHECK_LIVE_TEST_OUTER_CURL="$work/bin/curl"
export CHECK_LIVE_TEST_LAYERX=1111111111111111111111111111111111111111111111111111111111111111
export CHECK_LIVE_INDEXER_CONNECT="127.0.0.1:$archive_port"
export CHECK_LIVE_TEST_MACHINES='[{"state":"started","config":{"mounts":[{"path":"/data","volume":"vol_fixture"}]}}]'
sqlite3 "$ix/state/indexer.sqlite" "CREATE TABLE cursors(chain TEXT PRIMARY KEY, position INTEGER NOT NULL, hash TEXT NOT NULL, finalized_position INTEGER, finalized_boundary INTEGER, updated_at INTEGER NOT NULL);
CREATE TABLE backfill_cursors(chain TEXT PRIMARY KEY, position INTEGER NOT NULL, hash TEXT NOT NULL, updated_at INTEGER NOT NULL);
INSERT INTO backfill_cursors VALUES('paxeer', 26400000, '0xaa', 0);
INSERT INTO cursors VALUES('paxeer', 26400120, '0xbb', 26400100, 0, 0);"
echo 26400000 >"$ix/state/cutover-height"
cp "$(command -v sleep)" "$ix/layerx-indexer"

PATH="$ix/bin:$PATH" CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_indexer_account_unset "$work/hosts-indexer.env" 2 indexer -- \
	"check-live: CHECK_LIVE_INDEXER_ACCOUNT must be a 0x EVM address with indexed history"
export CHECK_LIVE_INDEXER_ACCOUNT=0x00000000000000000000000000000000000000aa
PATH="$ix/bin:$PATH" CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_indexer_nothing_running "$work/hosts-indexer.env" 1 indexer -- \
	"pass public-ips app=$indexer count=0" \
	"pass machines app=$indexer machines=1 started=1 volume=/data" \
	"pass archive-name name=api7.mainnet-beta.paxeer.network" \
	"fail indexer app=$indexer process=none" \
	"check-live: 1 check(s) failed"

LAYERX_INDEXER_DB="$ix/state/indexer.sqlite" LAYERX_INDEXER_LISTEN='[::]:8095' LAYERX_INDEXER_START_BLOCK=25000000 \
	LAYERX_INDEXER_EVM_URL=https://api7.mainnet-beta.paxeer.network LAYERX_INDEXER_EVM_CA_DER="$chain/root-X1.der" \
	LAYERX_INDEXER_COMET_URL=https://api7.mainnet-beta.paxeer.network/comet LAYERX_INDEXER_COMET_CA_DER="$chain/root-X1.der" \
	LAYERX_INDEXER_RELAY_URL=https://archive.paxeer.network LAYERX_INDEXER_RELAY_CA_DER="$chain/root-X1.der" \
	"$ix/layerx-indexer" 600 &
internal_pids="$internal_pids $!"
: >"$CHECK_LIVE_TEST_STDIN"
PATH="$ix/bin:$PATH" CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_indexer_passing "$work/hosts-indexer.env" 0 indexer -- \
	"pass public-ips app=$indexer count=0" \
	"pass machines app=$indexer machines=1 started=1 volume=/data" \
	"pass archive-name name=api7.mainnet-beta.paxeer.network" \
	"pass listener url=https://$indexer.internal:8095/healthz listen=[::]:8095 tls=internal-ca http=200" \
	"pass upstream-ca side=evm url=https://api7.mainnet-beta.paxeer.network root=X1 verifies=yes" \
	"pass upstream-ca side=comet url=https://api7.mainnet-beta.paxeer.network/comet root=X1 verifies=yes" \
	"pass upstream-ca side=relay url=https://archive.paxeer.network root=X1 verifies=yes" \
	"pass comet-location url=https://api7.mainnet-beta.paxeer.network/comet post=200 get=403 earliest=25000000" \
	"pass start-block block=25000000 earliest=25000000" \
	"pass backfill chain=paxeer cutover=26400000 backfill=26400000 live=26400120" \
	"pass history method=px_getUnifiedHistory account=0x00000000000000000000000000000000000000aa items=2" \
	"pass history method=lx_getHistory account=$CHECK_LIVE_TEST_LAYERX items=1" \
	"pass history method=px_getHistory account=0x00000000000000000000000000000000000000aa items=1" \
	"check-live: all checks passed"
if [ "$(grep -c "^$indexer app ssh console sh -c 'tls=/data/tls/indexer app=$indexer limit=5 sh -s'$" "$CHECK_LIVE_TEST_CALLS")" -eq 1 ] &&
	! grep -q 'PRIVATE KEY' "$CHECK_LIVE_TEST_STDIN"; then
	echo "ok   check_live_indexer_reads_the_machine_once"
else
	echo "FAIL check_live_indexer_reads_the_machine_once: want one sh -s call on $indexer with the indexer identity path and no key on the console input"
	cat "$CHECK_LIVE_TEST_CALLS"
	failures=$((failures + 1))
fi

sqlite3 "$ix/state/indexer.sqlite" "UPDATE backfill_cursors SET position = 26399000 WHERE chain = 'paxeer';"
export CHECK_LIVE_TEST_IPS='[{"Type":"v6"}]'
CHECK_LIVE_TEST_INDEXER_HEALTH=down CHECK_LIVE_TEST_COMET_GET=200 CHECK_LIVE_INDEXER_ACCOUNT=0x00000000000000000000000000000000000000bb \
	PATH="$ix/bin:$PATH" CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_indexer_failing "$work/hosts-indexer.env" 1 indexer -- \
	"fail public-ips app=$indexer count=1" \
	"fail listener app=$indexer listen=[::]:8095 curl=7 http=" \
	"pass upstream-ca side=relay url=https://archive.paxeer.network root=X1 verifies=yes" \
	"fail comet-location url=https://api7.mainnet-beta.paxeer.network/comet post=200 get=200 earliest=25000000" \
	"pass start-block block=25000000 earliest=25000000" \
	"fail backfill chain=paxeer cutover=26400000 backfill=26399000 live=26400120" \
	"fail history method=px_getUnifiedHistory account=0x00000000000000000000000000000000000000bb http=200 answer=error indexer_unavailable" \
	"fail history method=lx_getHistory account=none via=px_getUnifiedHistory" \
	"fail history method=px_getHistory account=0x00000000000000000000000000000000000000bb http=200 answer=error indexer_unavailable" \
	"check-live: 7 check(s) failed"
unset CHECK_LIVE_TEST_IPS CHECK_LIVE_TEST_MACHINES CHECK_LIVE_INDEXER_ACCOUNT CHECK_LIVE_INDEXER_CONNECT CHECK_LIVE_TEST_OUTER_CURL CHECK_LIVE_TEST_LAYERX

# The developers cases put a curl stand-in for the three developer names in
# front of the one above: /healthz answers ready unless the name is in
# CHECK_LIVE_TEST_DEV_DOWN, the webhooks routes want the bearer of the token
# file in the --config file, registration wants the Idempotency-Key and the
# receiver URL, and deliveries answers CHECK_LIVE_TEST_DEV_DELIVERIES. Each
# fixture app lists its machines from its own machines.json.
mkdir -p "$work/devbin" "$fx/platform/hosted/dashboard/web"
printf 'app = "%s"\n' "$(fx_app platform/hosted/dashboard/web/fly.toml)" >"$fx/platform/hosted/dashboard/web/fly.toml"
dashboard="$(fx_app platform/hosted/dashboard/fly.toml)"
dashboard_web="$(fx_app platform/hosted/dashboard/web/fly.toml)"
cat >"$work/devbin/curl" <<'SH'
#!/usr/bin/env bash
set -eu
url=""
for arg in "$@"; do
	case "$arg" in
	https://*) url="$arg" ;;
	esac
done
case "$url" in
https://hooks.paxeer.network/* | https://api-dev.paxeer.network/* | https://dev.paxeer.network/*) ;;
*) exec "$CHECK_LIVE_TEST_BIN/curl" "$@" ;;
esac
name="${url#https://}"
name="${name%%.*}"
printf '%s curl %s\n' "$name" "${url#https://*/}" >>"$CHECK_LIVE_TEST_CALLS"
out=/dev/stdout
conf=""
data=""
wout=""
idem=""
while [ "$#" -gt 0 ]; do
	case "$1" in
	--output) out="$2" ;;
	--config) conf="$2" ;;
	--data-binary) data="${2#@}" ;;
	--write-out) wout="$2" ;;
	--header) case "$2" in Idempotency-Key:*) idem="$2" ;; esac ;;
	esac
	shift
done
case " ${CHECK_LIVE_TEST_DEV_DOWN:-} " in
*" $name "*)
	[ -z "$wout" ] || printf '000'
	exit 7
	;;
esac
code=401
body='{"error":"unauthorized"}'
case "$url" in
*/healthz)
	code=200
	body='{"ready":true}'
	;;
https://dev.paxeer.network/)
	code=200
	body='<!doctype html>'
	;;
*/v1/webhooks/*)
	if [ -n "$conf" ] && grep -qxF "header = \"Authorization: Bearer $CHECK_LIVE_TEST_DEV_BEARER\"" "$conf"; then
		case "$url:$data" in
		*/endpoints:?*)
			code=400
			body='{"error":"idempotency_key_required"}'
			if [ "$idem" = "Idempotency-Key: check-live-developers" ] &&
				python3 -c 'import json, sys; b = json.load(open(sys.argv[1])); sys.exit(0 if b["url"] == sys.argv[2] and len(b["kinds"]) == 4 else 1)' "$data" "$CHECK_LIVE_DEVELOPERS_RECEIVER"; then
				code=201
				body='{"endpoint":"ep_fixture","key_id":"key_fixture"}'
			fi
			;;
		*/endpoints:)
			code=200
			body='[{"endpoint":"ep_fixture","url":"fixture"}]'
			;;
		*/deliveries:)
			code=200
			body="${CHECK_LIVE_TEST_DEV_DELIVERIES:-[]}"
			;;
		esac
	fi
	;;
esac
printf '%s' "$body" >"$out"
[ -z "$wout" ] || printf '%s' "$code"
SH
chmod +x "$work/devbin/curl"
dev_machines() {
	python3 -c '
import json, sys
print(json.dumps([{"state": "started", "region": r, "config": {"metadata": {"fly_process_group": g}}} for g, r in (a.split(":") for a in sys.argv[1:])]))
' "$@"
}
for app in "$webhooks" "$dashboard" "$dashboard_web"; do mkdir -p "$fly/$app"; done
dev_machines public:ams public:fra ingress:ams ingress:fra >"$fly/$webhooks/machines.json"
dev_machines app:ams app:fra >"$fly/$dashboard/machines.json"
dev_machines app:ams app:fra app:fra >"$fly/$dashboard_web/machines.json"
printf 'fixture-session-token\n' >"$work/dev-token"
export CHECK_LIVE_TEST_BIN="$work/bin" CHECK_LIVE_TEST_DEV_BEARER=fixture-session-token CHECK_LIVE_DEVELOPERS_RECEIVER=https://receiver.example.test/hook
unset CHECK_LIVE_TEST_MACHINES

CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_developers_without_a_token "$work/hosts-good.env" 2 developers -- \
	"check-live: CHECK_LIVE_DEVELOPERS_TOKEN_FILE does not name a readable file"

export CHECK_LIVE_DEVELOPERS_TOKEN_FILE="$work/dev-token"
CHECK_LIVE_TEST_DEV_DELIVERIES='[{"delivery":"dl_fixture","endpoint":"ep_fixture","event":"ev_fixture","kind":"payment","state":{"state":"delivered"}}]' \
	PATH="$work/devbin:$PATH" CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_developers_passing "$work/hosts-good.env" 0 developers -- \
	"pass machines app=$webhooks group=public started=2 regions=ams,fra" \
	"pass machines app=$webhooks group=ingress started=2 regions=ams,fra" \
	"pass machines app=$dashboard group=app started=2 regions=ams,fra" \
	"pass machines app=$dashboard_web group=app started=3 regions=ams,fra" \
	"pass readiness url=https://hooks.paxeer.network/healthz http=200" \
	"pass readiness url=https://api-dev.paxeer.network/healthz http=200" \
	"pass readiness url=https://dev.paxeer.network/ http=200" \
	"pass subscription endpoint=ep_fixture registered=201 listed=yes" \
	"pass delivery endpoint=ep_fixture kind=payment delivery=dl_fixture state=delivered" \
	"check-live: all checks passed"
if ! grep -q fixture-session-token "$CHECK_LIVE_TEST_CALLS"; then
	echo "ok   check_live_developers_never_prints_the_token"
else
	echo "FAIL check_live_developers_never_prints_the_token: the session token reached the recorded calls"
	failures=$((failures + 1))
fi

dev_machines public:ams public:ams ingress:fra >"$fly/$webhooks/machines.json"
CHECK_LIVE_TEST_DEV_DOWN="api-dev" CHECK_LIVE_TEST_DEV_DELIVERIES='[{"delivery":"dl_fixture","endpoint":"ep_fixture","event":"ev_fixture","kind":"payment","state":{"state":"pending"}}]' \
	CHECK_LIVE_DEVELOPERS_DELIVERY_ATTEMPTS=1 PATH="$work/devbin:$PATH" CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_developers_failing "$work/hosts-good.env" 1 developers -- \
	"fail machines app=$webhooks group=public started=2 regions=ams" \
	"fail machines app=$webhooks group=ingress started=1 regions=fra" \
	"pass machines app=$dashboard group=app started=2 regions=ams,fra" \
	"fail readiness url=https://api-dev.paxeer.network/healthz http=000" \
	"pass subscription endpoint=ep_fixture registered=201 listed=yes" \
	"fail delivery endpoint=ep_fixture state=none-delivered attempts=1 http=200" \
	"check-live: 4 check(s) failed"

CHECK_LIVE_TEST_DEV_BEARER=another-token CHECK_LIVE_DEVELOPERS_DELIVERY_ATTEMPTS=1 PATH="$work/devbin:$PATH" CHECK_LIVE_TEST_PROGRAM="$fx_checker" \
	expect check_live_developers_refused_session "$work/hosts-good.env" 1 developers -- \
	"fail subscription url=https://hooks.paxeer.network/v1/webhooks/endpoints http=401" \
	"check-live: 3 check(s) failed"
unset CHECK_LIVE_TEST_BIN CHECK_LIVE_TEST_DEV_BEARER CHECK_LIVE_DEVELOPERS_RECEIVER CHECK_LIVE_DEVELOPERS_TOKEN_FILE

# The router cases run the fixture tree's checker: the endpoint app's machines
# come from the flyctl stand-in, /rpc and /readyz from the curl stand-in's
# router fixtures and the wallet gateway's readiness from the node stand-in.
mkdir -p "$CHECK_LIVE_TEST_FLY/$endpoint" "$work/router-good" "$work/router-bad"
router_ready='{"status":"degraded","backends":{"durable_store":{"state":"ready","reason":"ready"},"event_producer":{"state":"unavailable","reason":"not_configured"},"paxeer_chain":{"state":"ready","reason":"ready"},"core_agent_boundary":{"state":"ready","reason":"ready"},"independent_receipt_authority":{"state":"ready","reason":"ready"},"program_registry":{"state":"ready","reason":"ready"}}}'
printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":"0x7d"}' >"$work/router-good/eth_chainId"
printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"chain_id":"0x7d","kernel":{"available":true,"reason":null}}}' >"$work/router-good/px_getNetwork"
printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"account":"fixture","sequence":"0"}}' >"$work/router-good/lx_getAccount"
printf '200\n%s\n' "$router_ready" >"$work/router-good/readyz-m-ams-1"
printf '200\n%s\n' "$router_ready" >"$work/router-good/readyz-m-fra-1"
printf '%s\n' '{"ready":true,"components":{"rpc_pool":{"state":"up","healthy":3,"endpoints":[{"url":"https://api-mainnet-beta.paxeer.network","state":"healthy"},{"url":"https://api1.mainnet-beta.paxeer.network","state":"healthy"}]}}}' >"$work/router-good/wallet-readyz"
printf '%s\n' '[{"id":"m-ams-1","region":"ams","state":"started"},{"id":"m-ams-2","region":"ams","state":"started"},{"id":"m-fra-1","region":"fra","state":"started"},{"id":"m-fra-0","region":"fra","state":"stopped"}]' >"$CHECK_LIVE_TEST_FLY/$endpoint/machines.json"
mkdir -p -m 0700 "$work/stages-passed"
: "${CHECK_LIVE_TEST_STAGE_DIR:?real retained material and registry-bootstrap records required}"
: "${CHECK_LIVE_CANDIDATE_REVISION:?selected source revision required}"
: "${CHECK_LIVE_CANDIDATE_IMAGE:?selected image digest required}"
: "${CHECK_LIVE_MATERIAL_GENERATION:?selected material generation required}"
export CHECK_LIVE_CANDIDATE_REVISION CHECK_LIVE_CANDIDATE_IMAGE CHECK_LIVE_MATERIAL_GENERATION
cp "$CHECK_LIVE_TEST_STAGE_DIR/material.json" "$CHECK_LIVE_TEST_STAGE_DIR/registry-bootstrap.json" "$work/stages-passed/"
python3 "$root/tools/qualification/paxeer-x/registry-router-bootstrap.py" --check-stage registry-bootstrap --stage-dir "$work/stages-passed"
CHECK_LIVE_STAGE_DIR="$work/stages-passed" CHECK_LIVE_TEST_ROUTER="$work/router-good" CHECK_LIVE_ROUTER_ACCOUNT="$(printf 'ab%.0s' $(seq 32))" CHECK_LIVE_TEST_PROGRAM="$fx_checker" \
	expect check_live_router_passing "$work/hosts-good.env" 0 router -- \
	"pass eth_chainId result=0x7d" \
	"pass px_getNetwork kernel.available=true reason=none" \
	"pass lx_getAccount read=answered" \
	"pass machines started=3 regions=ams,fra app=$endpoint" \
	"pass readyz app=$endpoint region=ams http=200 durable_store=ready configured=5/5" \
	"pass readyz app=$endpoint region=fra http=200 durable_store=ready configured=5/5" \
	"pass wallet-gateway app=$gateway rpc_pool=up healthy=3 first=router" \
	"check-live: all checks passed"

printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":"0x1"}' >"$work/router-bad/eth_chainId"
printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"kernel":{"available":false,"reason":"core_unavailable"}}}' >"$work/router-bad/px_getNetwork"
printf '503\n%s\n' '{"status":"degraded","backends":{"durable_store":{"state":"unavailable","reason":"unreachable"},"core_agent_boundary":{"state":"ready","reason":"ready"},"program_registry":{"state":"unavailable","reason":"not_configured"}}}' >"$work/router-bad/readyz-m-ams-1"
printf '%s\n' '{"ready":true,"components":{"rpc_pool":{"state":"up","healthy":1,"endpoints":[{"url":"https://api1.mainnet-beta.paxeer.network","state":"healthy"}]}}}' >"$work/router-bad/wallet-readyz"
printf '%s\n' '[{"id":"m-ams-1","region":"ams","state":"started"},{"id":"m-ams-2","region":"ams","state":"started"}]' >"$CHECK_LIVE_TEST_FLY/$endpoint/machines.json"
CHECK_LIVE_STAGE_DIR="$work/stages-passed" CHECK_LIVE_TEST_ROUTER="$work/router-bad" CHECK_LIVE_TEST_PROGRAM="$fx_checker" \
	expect check_live_router_failing "$work/hosts-good.env" 1 router -- \
	"fail eth_chainId result=0x1" \
	"fail px_getNetwork kernel.available=false reason=core_unavailable" \
	"fail lx_getAccount account=unset" \
	"fail machines started=2 regions=ams app=$endpoint" \
	"fail readyz app=$endpoint region=ams http=503 durable_store=unavailable configured=1/2 unready=durable_store" \
	"fail readyz app=$endpoint region=ams http=503 durable_store=unavailable configured=1/2 unready=durable_store missing=program_registry" \
	"fail wallet-gateway app=$gateway rpc_pool=up healthy=1 first=other" \
	"check-live: 6 check(s) failed"

# Router activation requires the registry bootstrap's stage record: without
# CHECK_LIVE_STAGE_DIR the check is a usage error, and without a passed
# registry-bootstrap.json it names the missing stage and asks nothing.
mkdir -p "$work/stages-failed" "$work/stages-absent"
printf '%s\n' '{"stage":"registry-bootstrap","outcome":"failed"}' >"$work/stages-failed/registry-bootstrap.json"
CHECK_LIVE_TEST_ROUTER="$work/router-good" CHECK_LIVE_TEST_PROGRAM="$fx_checker" \
	expect check_live_router_stage_dir_unset "$work/hosts-good.env" 2 router -- \
	"check-live: CHECK_LIVE_STAGE_DIR is unset"
for stages in absent failed; do
	CHECK_LIVE_STAGE_DIR="$work/stages-$stages" CHECK_LIVE_TEST_ROUTER="$work/router-good" CHECK_LIVE_TEST_PROGRAM="$fx_checker" \
		expect "check_live_router_registry_bootstrap_$stages" "$work/hosts-good.env" 1 router -- \
		"fail router-activation missing=registry-bootstrap producer=stage:registry-bootstrap"
	if [ -s "$CHECK_LIVE_TEST_CALLS" ]; then
		echo "FAIL check_live_router_registry_bootstrap_${stages}_asks_nothing: want no request before the prerequisite"
		cat "$CHECK_LIVE_TEST_CALLS"
		failures=$((failures + 1))
	else
		echo "ok   check_live_router_registry_bootstrap_${stages}_asks_nothing"
	fi
done

# The search cases put a curl stand-in for the serving sidecars ahead of the
# harness's: /xweb/health answers status ok except on the names of
# CHECK_LIVE_TEST_SEARCH_DOWN; an unpaid /search answers 402 with a metered
# PAYMENT-REQUIRED offer in CHECK_LIVE_TEST_SEARCH_CURRENCY to the payer the
# LAYERX-PAYER-DID header names, and a /search with a PAYMENT-SIGNATURE
# answers 200 with a PAYMENT-RESPONSE only when its grant matches that offer,
# its id is the domain digest of its fields and its Ed25519 signature
# verifies under its public key; every other request goes to the harness's
# stand-in.
mkdir -p "$work/search-bin"
cat >"$work/search-bin/curl" <<'SH'
#!/usr/bin/env bash
set -eu
url=""
wout=""
prev=""
did=""
payment=""
for arg in "$@"; do
	case "$arg" in
	https://*) url="$arg" ;;
	esac
	[ "$prev" != -w ] || wout="$arg"
	if [ "$prev" = -H ]; then
		case "$arg" in
		"LAYERX-PAYER-DID: "*) did="${arg#*: }" ;;
		"PAYMENT-SIGNATURE: "*) payment="${arg#*: }" ;;
		esac
	fi
	prev="$arg"
done
name="${url#https://}"
path="/${name#*/}"
name="${name%%/*}"
case "$path" in
/xweb/health)
	printf '%s search\n' "${name%%.*}" >>"$CHECK_LIVE_TEST_CALLS"
	case " ${CHECK_LIVE_TEST_SEARCH_DOWN:-} " in
	*" ${name%%.*} "*) exit 7 ;;
	esac
	printf '{"status":"ok"}'
	[ -z "$wout" ] || printf '\n200'
	exit 0
	;;
/search\?*)
	printf '%s search\n' "${name%%.*}" >>"$CHECK_LIVE_TEST_CALLS"
	accepted="$(printf '{"scheme":"metered","network":"layerx:125","amount":"2000000000000000","asset":"%064d","payTo":"%064d","maxTimeoutSeconds":60,"extra":{"layerx":{"commitment":"executed","account":"fixture-receiver","currency":"%s","payer":"%s","purposeHash":"%064d"}}}' 7 9 "${CHECK_LIVE_TEST_SEARCH_CURRENCY:-PAX}" "$(printf '%s' "$did" | sha256sum | cut -c1-64)" 5)"
	if [ -z "$payment" ]; then
		printf 'HTTP/2 402\r\npayment-required: %s\r\n\r\n' "$(printf '{"x402Version":2,"accepts":[%s]}' "$accepted" | base64 -w 0)"
		exit 0
	fi
	if python3 - "$payment" "$accepted" <<'PY'; then
import base64
import hashlib
import json
import sys

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey

header = json.loads(base64.b64decode(sys.argv[1]))
offer = json.loads(sys.argv[2])
grant = bytes.fromhex(header["payload"]["grant"])
terms = offer["extra"]["layerx"]
assert header["x402Version"] == 2 and header["accepted"] == offer and len(grant) == 346
assert len(bytes.fromhex(header["payload"]["idempotencyKey"])) == 32
assert grant[32:64].hex() == terms["payer"] and grant[64:96].hex() == offer["payTo"] and grant[96:128].hex() == offer["asset"]
assert int.from_bytes(grant[128:144], "big") >= int(offer["amount"]) and grant[160] == 0 and grant[161:169] == bytes(8)
assert grant[177:209].hex() == terms["purposeHash"] and grant[209] == 0 and grant[210:242] == bytes(32)
digest = hashlib.sha256(b"LXP/v1/authority-hash\0LXP:GRANT:v1" + grant[32:282]).digest()
assert grant[:32] == digest
Ed25519PublicKey.from_public_bytes(grant[250:282]).verify(grant[282:], digest)
PY
		printf 'HTTP/2 200\r\npayment-response: fixture\r\n\r\n'
	else
		printf 'HTTP/2 402\r\n\r\n'
	fi
	exit 0
	;;
esac
exec "$CHECK_LIVE_TEST_HARNESS_CURL" "$@"
SH
chmod +x "$work/search-bin/curl"
export CHECK_LIVE_TEST_HARNESS_CURL="$work/bin/curl"
openssl genpkey -algorithm ed25519 -out "$work/search-payer.pem" 2>/dev/null
export CHECK_LIVE_SEARCH_PAYER_KEY_FILE="$work/search-payer.pem" CHECK_LIVE_SEARCH_PAYER_DID="did:layerx:fixture-payer"

PATH="$work/search-bin:$PATH" expect check_live_search_passing "$work/hosts-good.env" 0 search -- \
	"pass health https://api1.mainnet-beta.paxeer.network/xweb/health http=200 status=ok" \
	"pass health https://api2.mainnet-beta.paxeer.network/xweb/health http=200 status=ok" \
	"pass health https://api3.mainnet-beta.paxeer.network/xweb/health http=200 status=ok" \
	"pass offer https://api1.mainnet-beta.paxeer.network/search http=402 pax=metered amount=2000000000000000" \
	"pass paid https://api1.mainnet-beta.paxeer.network/search http=200 currency=PAX payment-response=present" \
	"pass offer https://search.paxeer.network/search http=402 pax=metered amount=2000000000000000" \
	"pass paid https://search.paxeer.network/search http=200 currency=PAX payment-response=present" \
	"check-live: all checks passed"

CHECK_LIVE_TEST_SEARCH_DOWN="api2" CHECK_LIVE_TEST_SEARCH_CURRENCY="USDC" PATH="$work/search-bin:$PATH" expect check_live_search_failing "$work/hosts-good.env" 1 search -- \
	"pass health https://api1.mainnet-beta.paxeer.network/xweb/health http=200 status=ok" \
	"fail health https://api2.mainnet-beta.paxeer.network/xweb/health http=none" \
	"fail offer https://api1.mainnet-beta.paxeer.network/search http=402 pax=none amount=none" \
	"fail offer https://search.paxeer.network/search http=402 pax=none amount=none" \
	"check-live: 3 check(s) failed"

CHECK_LIVE_SEARCH_PAYER_KEY_FILE="$work/absent.pem" PATH="$work/search-bin:$PATH" expect check_live_search_no_payer_key "$work/hosts-good.env" 1 search -- \
	"pass offer https://api1.mainnet-beta.paxeer.network/search http=402 pax=metered amount=2000000000000000" \
	"fail paid https://api1.mainnet-beta.paxeer.network/search signature=unavailable" \
	"fail paid https://search.paxeer.network/search signature=unavailable" \
	"check-live: 2 check(s) failed"

CHECK_LIVE_SEARCH_PAYER_DID="" PATH="$work/search-bin:$PATH" expect check_live_search_no_payer_did "$work/hosts-good.env" 2 search -- \
	"check-live: CHECK_LIVE_SEARCH_PAYER_DID is unset"

PATH="$work/search-bin:$PATH" expect check_live_search_unmapped "$work/hosts-down.env" 1 search -- \
	"fail names names=unreadable" \
	"check-live: 1 check(s) failed"
unset CHECK_LIVE_SEARCH_PAYER_KEY_FILE CHECK_LIVE_SEARCH_PAYER_DID

# The registry cases put a curl of their own ahead of the stand-in: the
# registry's private app ingress answers /healthz ready to a request with a client
# certificate and refuses one without (admits it with
# CHECK_LIVE_TEST_REGISTRY_ANON=1); the fixture router routerfx answers
# lx_getProgramEvents from $work/registry-rpc/from-<from_sequence>.json and
# /readyz with CHECK_LIVE_TEST_REGISTRY_READYZ; anything else goes on to the
# stand-in.
registry="$(fx_app platform/hosted/registry/fly.toml)"
mkdir -p "$work/registry-bin" "$work/registry-rpc"
cat >"$work/registry-bin/curl" <<'SH'
#!/usr/bin/env bash
set -eu
url=""
data=""
wout=""
out=/dev/stdout
cert=0
prev=""
for arg in "$@"; do
	case "$prev" in
	-d) data="$arg" ;;
	-w) wout="$arg" ;;
	-o) out="$arg" ;;
	esac
	case "$arg" in
	https://*) url="$arg" ;;
	--cert) cert=1 ;;
	esac
	prev="$arg"
done
case "$url" in
https://*.internal:9420/*)
	printf 'index curl\n' >>"$CHECK_LIVE_TEST_CALLS"
	if [ "$cert" -eq 0 ] && [ "${CHECK_LIVE_TEST_REGISTRY_ANON:-0}" != 1 ]; then
		echo "curl: (56) OpenSSL SSL_read: tlsv13 alert certificate required" >&2
		exit 56
	fi
	printf '{"status":"ready","service":"program-registry"}' >"$out"
	[ -z "$wout" ] || printf '%b' "${wout//%\{http_code\}/200}"
	exit 0
	;;
https://routerfx.example.com/rpc)
	from="$(sed -n 's/.*"from_sequence":\([0-9]*\).*/\1/p' <<<"$data")"
	printf 'routerfx curl %s\n' "$from" >>"$CHECK_LIVE_TEST_CALLS"
	cat "$CHECK_LIVE_TEST_REGISTRY_RPC/from-$from.json"
	exit 0
	;;
https://routerfx.example.com/readyz)
	printf 'routerfx curl readyz\n' >>"$CHECK_LIVE_TEST_CALLS"
	printf '%s' "$CHECK_LIVE_TEST_REGISTRY_READYZ"
	[ -z "$wout" ] || printf '%b' "${wout//%\{http_code\}/200}"
	exit 0
	;;
esac
PATH="${PATH#*registry-bin:}" exec curl "$@"
SH
chmod +x "$work/registry-bin/curl"
export CHECK_LIVE_TEST_REGISTRY_RPC="$work/registry-rpc"
registry_program="$(printf 'c%.0s' $(seq 64))"
registry_other="$(printf 'd%.0s' $(seq 64))"
registry_topic=6c782e7265662e657363726f772e637573746f6479
printf '{"jsonrpc":"2.0","id":1,"result":{"events":[{"sequence":3,"program_id":"%s","topic":"%s","data":"00"}],"next_sequence":9}}\n' "$registry_other" "$registry_topic" >"$work/registry-rpc/from-0.json"
printf '{"jsonrpc":"2.0","id":2,"result":{"events":[{"sequence":12,"program_id":"%s","topic":"%s","data":"01"}],"next_sequence":13}}\n' "$registry_program" "$registry_topic" >"$work/registry-rpc/from-9.json"
printf '{"jsonrpc":"2.0","id":3,"result":{"events":[],"next_sequence":13}}\n' >"$work/registry-rpc/from-13.json"
registry_ready='{"status":"ready","service":"layerx-gateway","components":{"durable_store":"ready","core_agent_boundary":"ready","independent_receipt_authority":"ready","program_registry":"ready","principal_state_boundary":"unavailable"},"backends":{}}'
registry_unready="$(sed 's/"program_registry":"ready"/"program_registry":"unavailable"/' <<<"$registry_ready")"

PATH="$work/registry-bin:$PATH" CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_registry_toml_absent "$work/hosts-good.env" 1 registry -- \
	"fail registry toml=absent" \
	"check-live: 1 check(s) failed"

printf 'app = "%s"\n[env]\nLAYERX_REGISTRY_LISTEN = "[::]:9420"\n[[services]]\ninternal_port = 9420\n[[services.ports]]\nport = 443\n' "$registry" >"$fx/platform/hosted/registry/fly.toml"
PATH="$work/registry-bin:$PATH" CHECK_LIVE_ROUTER_URL=https://routerfx.example.com CHECK_LIVE_TEST_REGISTRY_ANON=1 \
	CHECK_LIVE_TEST_REGISTRY_READYZ="$registry_unready" \
	CHECK_LIVE_TEST_MACHINES='[{"state":"stopped","config":{"mounts":[{"path":"/data","volume":"vol_1"}]}}]' \
	CHECK_LIVE_TEST_IPS='[{"Type":"shared_v4"},{"Type":"v6"}]' \
	CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_registry_failing "$work/hosts-good.env" 1 registry -- \
	"fail machines app=$registry machines=1 started=0 volumes=1" \
	"fail private-ingress app=$registry public-service=present" \
	"fail healthz url=https://$registry.internal:9420/healthz from=$kernel curl=0 http=200 anonymous=admitted" \
	"fail program-events program=unset want=CHECK_LIVE_REGISTRY_PROGRAM_ID" \
	"fail router-readyz url=https://routerfx.example.com/readyz http=200 program_registry=unavailable" \
	"check-live: 5 check(s) failed"

printf 'app = "%s"\n[env]\nLAYERX_REGISTRY_LISTEN = "[::]:9420"\n' "$registry" >"$fx/platform/hosted/registry/fly.toml"

PATH="$work/registry-bin:$PATH" CHECK_LIVE_ROUTER_URL=https://routerfx.example.com CHECK_LIVE_REGISTRY_PROGRAM_ID="$(printf 'e%.0s' $(seq 64))" \
	CHECK_LIVE_TEST_REGISTRY_READYZ="$registry_ready" \
	CHECK_LIVE_TEST_MACHINES='[{"state":"started","config":{"mounts":[{"path":"/data","volume":"vol_1"}]}}]' \
	CHECK_LIVE_TEST_IPS='[{"Type":"v4"},{"Type":"v6"}]' \
	CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_registry_no_event_of_the_program "$work/hosts-good.env" 1 registry -- \
	"pass machines app=$registry machines=1 started=1 volumes=1" \
	"pass private-ingress app=$registry url=https://$registry.internal:9420/healthz" \
	"pass healthz url=https://$registry.internal:9420/healthz from=$kernel http=200 status=ready anonymous=refused" \
	"fail program-events program=$(printf 'e%.0s' $(seq 64)) events=none next_sequence=13" \
	"pass router-readyz url=https://routerfx.example.com/readyz http=200 program_registry=ready" \
	"check-live: 1 check(s) failed"

PATH="$work/registry-bin:$PATH" CHECK_LIVE_ROUTER_URL=https://routerfx.example.com CHECK_LIVE_REGISTRY_PROGRAM_ID="$registry_program" \
	CHECK_LIVE_TEST_REGISTRY_READYZ="$registry_ready" \
	CHECK_LIVE_TEST_MACHINES='[{"state":"started","config":{"mounts":[{"path":"/data","volume":"vol_1"}]}}]' \
	CHECK_LIVE_TEST_IPS='[{"Type":"v4"},{"Type":"v6"}]' \
	CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_registry_passing "$work/hosts-good.env" 0 registry -- \
	"pass machines app=$registry machines=1 started=1 volumes=1" \
	"pass private-ingress app=$registry url=https://$registry.internal:9420/healthz" \
	"pass healthz url=https://$registry.internal:9420/healthz from=$kernel http=200 status=ready anonymous=refused" \
	"pass program-events program=$registry_program sequence=12" \
	"pass router-readyz url=https://routerfx.example.com/readyz http=200 program_registry=ready" \
	"check-live: all checks passed"
if [ "$(grep -c "^$kernel app ssh console " "$CHECK_LIVE_TEST_CALLS")" -eq 1 ] && [ "$(grep -c '^index curl$' "$CHECK_LIVE_TEST_CALLS")" -eq 2 ] &&
	grep -qx 'routerfx curl 0' "$CHECK_LIVE_TEST_CALLS" && grep -qx 'routerfx curl 9' "$CHECK_LIVE_TEST_CALLS" &&
	grep -q "^$registry app machines list" "$CHECK_LIVE_TEST_CALLS" && ! grep -q "^$registry app ips list" "$CHECK_LIVE_TEST_CALLS"; then
	echo "ok   check_live_registry_asks_the_kernel_machine_once_and_pages_the_events"
else
	echo "FAIL check_live_registry_asks_the_kernel_machine_once_and_pages_the_events: want one kernel ssh call, two healthz requests, pages from 0 and 9, registry machines and no public IP dependency"
	cat "$CHECK_LIVE_TEST_CALLS"
	failures=$((failures + 1))
fi

# registry-bootstrap runs the registry's own checks with no router: it passes
# with the router name never asked.
PATH="$work/registry-bin:$PATH" CHECK_LIVE_ROUTER_URL=https://routerfx.example.com \
	CHECK_LIVE_TEST_MACHINES='[{"state":"started","config":{"mounts":[{"path":"/data","volume":"vol_1"}]}}]' \
	CHECK_LIVE_TEST_IPS='[{"Type":"v4"},{"Type":"v6"}]' \
	CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_registry_bootstrap_needs_no_router "$work/hosts-good.env" 0 registry-bootstrap -- \
	"pass machines app=$registry machines=1 started=1 volumes=1" \
	"pass private-ingress app=$registry url=https://$registry.internal:9420/healthz" \
	"pass healthz url=https://$registry.internal:9420/healthz from=$kernel http=200 status=ready anonymous=refused" \
	"check-live: all checks passed"
if grep -q routerfx "$CHECK_LIVE_TEST_CALLS"; then
	echo "FAIL check_live_registry_bootstrap_asks_no_router: the bootstrap asked the router"
	cat "$CHECK_LIVE_TEST_CALLS"
	failures=$((failures + 1))
else
	echo "ok   check_live_registry_bootstrap_asks_no_router"
fi

# The rendered plan of this checkout: four stages in order, each requiring
# only the one before it, read with no host map and no request.
plan_lines=(
	"stage 1 material requires=- needs=request-token,publication-token,gateway-client,registry,registry-event-client,REGISTRY_IDENTITY_TOKEN,REGISTRY_PROGRAM_EVENTS_TOKEN,REGISTRY_WEBHOOKS_EVENTS_TOKEN,LAYERX_REGISTRY_NODE_AUTHORIZATION,LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION,builder-rootfs,environment-tree-digest,replica-id,trust-history producers=request-token:init.sh:--prepare-material,publication-token:init.sh:--prepare-material,gateway-client:ca.sh:gateway-client,registry:ca.sh:registry,registry-event-client:ca.sh:registry-event-client,REGISTRY_IDENTITY_TOKEN:fly-secret:REGISTRY_IDENTITY_TOKEN,REGISTRY_PROGRAM_EVENTS_TOKEN:fly-secret:REGISTRY_PROGRAM_EVENTS_TOKEN,REGISTRY_WEBHOOKS_EVENTS_TOKEN:fly-secret:REGISTRY_WEBHOOKS_EVENTS_TOKEN,LAYERX_REGISTRY_NODE_AUTHORIZATION:fly-secret:LAYERX_REGISTRY_NODE_AUTHORIZATION,LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION:fly-secret:LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION,builder-rootfs:deploy:builder-environment,environment-tree-digest:deploy:builder-environment,replica-id:deploy:kernel-material,trust-history:deploy:kernel-material"
	"stage 2 registry-bootstrap requires=material needs=material-record,request-token,publication-token producers=material-record:stage:material,request-token:init.sh:--prepare-material,publication-token:init.sh:--prepare-material"
	"stage 3 router-activation requires=registry-bootstrap needs=registry-bootstrap,program-registry-token,client-identity,client-password producers=registry-bootstrap:stage:registry-bootstrap,program-registry-token:fly-secret:ENDPOINT_PROGRAM_REGISTRY_TOKEN,client-identity:fly-secret:ENDPOINT_CLIENT_P12,client-password:fly-secret:ENDPOINT_CLIENT_PASSWORD"
	"stage 4 routed-proof requires=router-activation needs=router-activation,registry-receipt producers=router-activation:stage:router-activation,registry-receipt:deploy:routed-proof"
)
: >"$CHECK_LIVE_TEST_CALLS"
status=0
output="$(env -u BRINGUP_HOSTS_FILE "$checker" registry-plan 2>&1)" || status=$?
if [ "$status" -eq 0 ] && [ "$output" = "$(printf '%s\n' "${plan_lines[@]}")" ] && [ ! -s "$CHECK_LIVE_TEST_CALLS" ]; then
	echo "ok   check_live_registry_plan_rendered"
else
	echo "FAIL check_live_registry_plan_rendered: want exit 0, the four stage lines in order and no request, got exit $status"
	printf '%s\n' "$output"
	failures=$((failures + 1))
fi

# The fixture tree's registry toml holds only its app line, so the plan
# refuses it by path.
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_registry_plan_toml_line_absent - 1 registry-plan -- \
	"fail registry-plan toml=absent platform/hosted/registry/fly.toml"

# The interop-adapters cases run the check against a loopback stand-in of the
# gateway's /v1/adapters (CHECK_LIVE_INTEROP_ORIGIN points at it), which
# answers the conformance pins render.py derives from this checkout's vectors,
# and a fixture Makefile whose three conformance targets exit with the code in
# their leg file; the sandbox target also wants both bearer tokens in its
# environment exactly as their files hold them.
mkdir -p "$work/adapters" "$fx/interop/deploy/gateway" "$fx/interop/specs/conformance"
cp "$root/interop/deploy/gateway/render.py" "$fx/interop/deploy/gateway/"
cp -r "$root/interop/specs/conformance/ap2" "$root/interop/specs/conformance/visa-tap" \
	"$root/interop/specs/conformance/fiat" "$fx/interop/specs/conformance/"
cat >"$fx/Makefile" <<'MK'
interop-test-mandates interop-test-visa-tap:
	@exit $$(cat $(CHECK_LIVE_TEST_LEGS)/$@)
interop-test-ramps-sandbox:
	@test "$$LAYERX_RAMP_CUSTOMER_TOKEN" = "$$(cat $(CHECK_LIVE_INTEROP_ADAPTERS_CUSTOMER_TOKEN_FILE))"
	@test "$$LAYERX_RAMP_OPERATOR_TOKEN" = "$$(cat $(CHECK_LIVE_INTEROP_ADAPTERS_OPERATOR_TOKEN_FILE))"
	@exit $$(cat $(CHECK_LIVE_TEST_LEGS)/$@)
MK
for leg in interop-test-mandates interop-test-visa-tap interop-test-ramps-sandbox; do printf '0' >"$work/adapters/$leg"; done
adapters_customer="$(openssl rand -hex 24)"
adapters_operator="$(openssl rand -hex 24)"
printf '%s' "$adapters_customer" >"$work/adapters/customer"
printf '%s' "$adapters_operator" >"$work/adapters/operator"
python3 - "$root" >"$work/adapters/doc.json" <<'PY'
import importlib.util, json, pathlib, sys

spec = importlib.util.spec_from_file_location("render", sys.argv[1] + "/interop/deploy/gateway/render.py")
render = importlib.util.module_from_spec(spec)
spec.loader.exec_module(render)
adapters = []
for adapter in ("ap2", "visa-tap", "fiat"):
    suite, count, digest = render.first_party_suite(pathlib.Path(sys.argv[1]), adapter)
    adapters.append({"id": adapter, "conformance_suite": suite, "conformance_vectors": count, "conformance_sha256": digest,
                     "readiness": {k: "ready" for k in ("configuration", "ingress", "settlement", "receipt_verification")}})
print(json.dumps({"adapters": adapters, "transports": []}))
PY
cat >"$work/adapters/gateway.py" <<'PY'
import http.server, sys

work = sys.argv[1]


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_GET(self):
        code, body = (200, open(work + "/doc.json", "rb").read()) if self.path == "/v1/adapters" else (404, b"{}")
        self.send_response(code)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
open(work + "/port", "w").write(str(server.server_address[1]))
server.serve_forever()
PY
python3 "$work/adapters/gateway.py" "$work/adapters" &
adapters_pid=$!
for _ in $(seq 50); do [ -s "$work/adapters/port" ] && break; python3 -c 'import time; time.sleep(0.1)'; done
adapters_origin="http://127.0.0.1:$(cat "$work/adapters/port")"

CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_interop_adapters_inputs_unset "$work/hosts-good.env" 2 interop-adapters -- \
	"check-live: LAYERX_RAMP_URL is unset"

export CHECK_LIVE_TEST_LEGS="$work/adapters" CHECK_LIVE_INTEROP_ORIGIN="$adapters_origin"
export LAYERX_RAMP_URL=https://ramp.example LAYERX_RAMP_CA_PEM="$work/adapters/ca.pem" LAYERX_RAMP_OPERATOR_URL=https://operator.example
export LAYERX_RAMP_ON_QUOTE_ID=quote-on LAYERX_RAMP_OFF_QUOTE_ID=quote-off LAYERX_RAMP_OFF_GRANT_JSON='{}'
export LAYERX_RAMP_ON_ACCOUNT_SEQUENCE=1 LAYERX_RAMP_OFF_RECEIVER_SEQUENCE=2
export CHECK_LIVE_INTEROP_ADAPTERS_CUSTOMER_TOKEN_FILE="$work/adapters/customer"
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_interop_adapters_token_unset "$work/hosts-good.env" 2 interop-adapters -- \
	"check-live: CHECK_LIVE_INTEROP_ADAPTERS_OPERATOR_TOKEN_FILE is unset"

export CHECK_LIVE_INTEROP_ADAPTERS_OPERATOR_TOKEN_FILE="$work/adapters/operator"
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_interop_adapters_passing "$work/hosts-good.env" 0 interop-adapters -- \
	"pass mandates app=$interop make=interop-test-mandates exit=0 adapter=ap2 http=200 pins=match readiness=4/4" \
	"pass visa-tap app=$interop make=interop-test-visa-tap exit=0 adapter=visa-tap http=200 pins=match readiness=4/4" \
	"pass ramps-sandbox app=$interop make=interop-test-ramps-sandbox exit=0 adapter=fiat http=200 pins=match readiness=4/4" \
	"check-live: all checks passed"
output="$(BRINGUP_HOSTS_FILE="$work/hosts-good.env" CHECK_LIVE_TIMEOUT=5 "$fx_checker" interop-adapters 2>&1)" || true
if grep -qF -e "$adapters_customer" -e "$adapters_operator" - "$CHECK_LIVE_TEST_CALLS" <<<"$output"; then
	echo "FAIL check_live_interop_adapters_keeps_the_ramp_tokens: a bearer token left the check"
	failures=$((failures + 1))
else
	echo "ok   check_live_interop_adapters_keeps_the_ramp_tokens"
fi

printf '1' >"$work/adapters/interop-test-visa-tap"
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_interop_adapters_one_leg_failing "$work/hosts-good.env" 1 interop-adapters -- \
	"pass mandates app=$interop make=interop-test-mandates exit=0" \
	"fail visa-tap app=$interop make=interop-test-visa-tap exit=2 adapter=visa-tap http=200 pins=match readiness=4/4" \
	"pass ramps-sandbox app=$interop make=interop-test-ramps-sandbox exit=0" \
	"check-live: 1 check(s) failed"

printf '0' >"$work/adapters/interop-test-visa-tap"
python3 - "$work/adapters/doc.json" <<'PY'
import json, sys

doc = json.load(open(sys.argv[1]))
fiat = next(entry for entry in doc["adapters"] if entry["id"] == "fiat")
fiat["conformance_sha256"] = "00" * 32
fiat["readiness"]["configuration"] = "unavailable"
open(sys.argv[1], "w").write(json.dumps(doc))
PY
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_interop_adapters_served_pins_differ "$work/hosts-good.env" 1 interop-adapters -- \
	"fail ramps-sandbox app=$interop make=interop-test-ramps-sandbox exit=0 adapter=fiat http=200 pins=differ readiness=3/4" \
	"check-live: 1 check(s) failed"
kill "$adapters_pid" 2>/dev/null || true
wait "$adapters_pid" 2>/dev/null || true
unset CHECK_LIVE_TEST_LEGS CHECK_LIVE_INTEROP_ORIGIN LAYERX_RAMP_URL LAYERX_RAMP_CA_PEM LAYERX_RAMP_OPERATOR_URL
unset LAYERX_RAMP_ON_QUOTE_ID LAYERX_RAMP_OFF_QUOTE_ID LAYERX_RAMP_OFF_GRANT_JSON LAYERX_RAMP_ON_ACCOUNT_SEQUENCE
unset LAYERX_RAMP_OFF_RECEIVER_SEQUENCE CHECK_LIVE_INTEROP_ADAPTERS_CUSTOMER_TOKEN_FILE CHECK_LIVE_INTEROP_ADAPTERS_OPERATOR_TOKEN_FILE
# The human state preservation round trip without Fly: export from a fixture
# old machine whose serving process is a copy of sleep that the export stops,
# import into a fixture kernel volume, verify, and each refusal.
preserve="$fx/tools/bringup/human-state-preserve.sh"
old="$work/preserve-old/$kernel/app"
new="$work/preserve-new/$kernel/app"
mkdir -p "$old/var/lib/layerx/human/store/a" "$old/var/lib/layerx/human/custody" "$old/data/human-state/kms" \
	"$old/data/layerx/keys" "$old/run/human-material" "$old/usr/local/bin" "$new/data" "$work/preserve-empty/$kernel/app"
printf 'journal\n' >"$old/var/lib/layerx/human/store/a/journal"
printf 'sealed\n' >"$old/var/lib/layerx/human/custody/keystore"
printf 'seal\n' >"$old/data/human-state/kms/seal"
printf 'treasury\n' >"$old/data/layerx/keys/treasury.key"
printf 'recovery\n' >"$old/run/human-material/recovery-policy.json"
python3 -c 'import socket, sys; socket.socket(socket.AF_UNIX).bind(sys.argv[1])' "$old/var/lib/layerx/human/components.sock"
cp "$(command -v sleep)" "$old/usr/local/bin/layerx-human-service"
LAYERX_HUMAN_STORE_ROOT="$old/var/lib/layerx/human/store" "$old/usr/local/bin/layerx-human-service" 300 &
serving_pid=$!
out_dir="$work/preserve-out"

export CHECK_LIVE_TEST_FLY="$work/preserve-old"
CHECK_LIVE_TEST_PROGRAM="$preserve" expect human_state_preserve_export - 0 export "$out_dir" -- \
	"pass quiesce app=$kernel stopped=1" \
	"pass export app=$kernel files=5 manifest=match"
if [ "$(awk '{print $3}' "/proc/$serving_pid/stat")" = T ] &&
	grep -q '  human-state/components/store/a/journal$' "$out_dir/manifest.sha256" &&
	grep -q '  layerx/keys/human-material/recovery-policy.json$' "$out_dir/manifest.sha256" &&
	! grep -q 'components.sock' "$out_dir/manifest.sha256" && [ "$(stat -c %a "$out_dir/state.tar")" = 600 ]; then
	echo "ok   human_state_preserve_export_quiesces_and_maps"
else
	echo "FAIL human_state_preserve_export_quiesces_and_maps: want the serving process stopped, mapped manifest paths, no socket and a 0600 tar"
	failures=$((failures + 1))
fi
kill -KILL "$serving_pid" 2>/dev/null || true
wait "$serving_pid" 2>/dev/null || true
CHECK_LIVE_TEST_PROGRAM="$preserve" expect human_state_preserve_export_twice - 1 export "$out_dir" -- \
	"already holds an export"
export CHECK_LIVE_TEST_FLY="$work/preserve-empty"
CHECK_LIVE_TEST_PROGRAM="$preserve" expect human_state_preserve_export_empty - 1 export "$work/preserve-none" -- \
	"nothing to preserve"
[ ! -e "$work/preserve-none/state.tar" ] && [ ! -e "$work/preserve-none/manifest.sha256" ] ||
	{ echo "FAIL human_state_preserve_export_empty_leaves_nothing"; failures=$((failures + 1)); }

export CHECK_LIVE_TEST_FLY="$work/preserve-new"
CHECK_LIVE_TEST_PROGRAM="$preserve" expect human_state_preserve_import - 0 import "$out_dir" -- \
	"pass import app=$kernel files=5" \
	"pass verify app=$kernel files=5 sha256=match owners=match"
if [ "$(stat -c '%u:%g %a' "$new/data/human-state/components")" = "4020:4020 700" ] &&
	[ "$(stat -c '%u:%g %a' "$new/data/human-state/kms")" = "4026:4020 700" ] &&
	[ "$(stat -c '%u:%g %a' "$new/data/layerx/keys/human-material")" = "0:4020 750" ] &&
	[ ! -e "$new/data/human-state/components/components.sock" ]; then
	echo "ok   human_state_preserve_import_owners"
else
	echo "FAIL human_state_preserve_import_owners: want components 4020:4020 700, kms 4026:4020 700, human-material 0:4020 750 and no socket"
	failures=$((failures + 1))
fi
CHECK_LIVE_TEST_PROGRAM="$preserve" expect human_state_preserve_import_twice - 1 import "$out_dir" -- \
	"already exists under /data"
mkdir "$new/data/human-state/identity"
CHECK_LIVE_TEST_PROGRAM="$preserve" expect human_state_preserve_verify_owner - 1 verify "$out_dir" -- \
	"another owner than the init expects"
rmdir "$new/data/human-state/identity"
printf 'changed\n' >"$new/data/human-state/components/store/a/journal"
CHECK_LIVE_TEST_PROGRAM="$preserve" expect human_state_preserve_verify_mismatch - 1 verify "$out_dir" -- \
	"differs from $out_dir/manifest.sha256"
CHECK_LIVE_TEST_PROGRAM="$preserve" expect human_state_preserve_verify_no_export - 1 verify "$work/preserve-none" -- \
	"holds no export"
CHECK_LIVE_TEST_PROGRAM="$preserve" expect human_state_preserve_usage - 2 export -- \
	"usage: tools/bringup/human-state-preserve.sh"
export CHECK_LIVE_TEST_FLY="$fly"

# The xweb-attestors cases: four fixture attestor apps whose tomls sit in the
# fixture repository, a socat stand-in that answers each loopback port's
# /health request with 200 unless CHECK_LIVE_TEST_XWEB_DOWN lists the port,
# and an ssh stand-in ahead of the harness one that answers the validator
# host unit count with CHECK_LIVE_TEST_XWEB_UNITS and the attestor listener
# count with CHECK_LIVE_TEST_XWEB_LISTENERS for the up-* destinations, so the
# validators cases see every remote port unreachable and exit 1.
mkdir -p "$fx/interop/deploy/x-websearch"
for n in 1 2 3 4; do
	printf 'app = "%s"\n' "$(fx_app "interop/deploy/x-websearch/attestor-$n.toml")" >"$fx/interop/deploy/x-websearch/attestor-$n.toml"
done
xweb1="$(fx_app interop/deploy/x-websearch/attestor-1.toml)"
xweb2="$(fx_app interop/deploy/x-websearch/attestor-2.toml)"
xweb4="$(fx_app interop/deploy/x-websearch/attestor-4.toml)"
mkdir -p "$work/xweb-bin"
cat >"$work/xweb-bin/socat" <<'SH'
#!/usr/bin/env bash
set -eu
port="${*: -1}"
port="${port##*:}"
cat >/dev/null
case " ${CHECK_LIVE_TEST_XWEB_DOWN:-} " in
*" $port "*) exit 1 ;;
esac
printf 'HTTP/1.1 200 OK\r\ncontent-length: 2\r\n\r\nok'
SH
cat >"$work/xweb-bin/ssh" <<'SH'
#!/usr/bin/env bash
set -eu
last="${*: -1}"
dest="${*: -2:1}"
# ssh -G resolves a fixture destination to an address no connect reaches.
if [ "$1" = -G ]; then
	echo "hostname 256.0.0.1"
	exit 0
fi
case "$last" in
*'"x-websearch*"'* | *'sport = :8480'*)
	printf '%s %s\n' "$dest" "$last" >>"$CHECK_LIVE_TEST_CALLS"
	case "$dest" in
	up-*) ;;
	*) exit 255 ;;
	esac
	case "$last" in
	*x-websearch*) echo "${CHECK_LIVE_TEST_XWEB_UNITS:-0}" ;;
	*) echo "${CHECK_LIVE_TEST_XWEB_LISTENERS:-0}" ;;
	esac
	exit 0
	;;
esac
exec "$CHECK_LIVE_TEST_HARNESS_SSH" "$@"
SH
chmod +x "$work/xweb-bin/socat" "$work/xweb-bin/ssh"
export CHECK_LIVE_TEST_HARNESS_SSH="$work/bin/ssh"
xweb_machine='[{"state":"started","region":"ams","config":{"mounts":[{"volume":"vol_fx"}]}}]'
{ cat "$work/hosts-good.env"; echo "XWEB_ATTESTORS_ON_FLY=yes"; } >"$work/hosts-xweb-fly.env"
sed 's/^VALIDATOR_HOSTS=.*/VALIDATOR_HOSTS="down-validator-a"/' "$work/hosts-good.env" >"$work/hosts-xweb-down.env"

CHECK_LIVE_TEST_MACHINES="$xweb_machine" CHECK_LIVE_TEST_IPS='[]' CHECK_LIVE_TEST_PROGRAM="$fx_checker" PATH="$work/xweb-bin:$PATH" expect check_live_xweb_attestors_passing "$work/hosts-good.env" 0 xweb-attestors -- \
	"pass machines app=$xweb1 machines=1 started=1 volumes=1" \
	"pass public-ips app=$xweb1 count=0" \
	"pass health app=$xweb1 http=200" \
	"pass hop app=$xweb1 peer=$xweb2 port=8492 http=200" \
	"pass hop app=$xweb4 peer=$xweb1 port=8491 http=200" \
	"pass VALIDATOR_HOSTS[0] x-websearch-units=0" \
	"pass VALIDATOR_HOSTS[1] x-websearch-units=0" \
	"check-live: all checks passed"

CHECK_LIVE_TEST_XWEB_DOWN="8480 8493" CHECK_LIVE_TEST_XWEB_UNITS=2 CHECK_LIVE_TEST_MACHINES='[{"state":"stopped","config":{"mounts":[]}}]' CHECK_LIVE_TEST_IPS='[{"address":"x","type":"v4"}]' CHECK_LIVE_TEST_PROGRAM="$fx_checker" PATH="$work/xweb-bin:$PATH" expect check_live_xweb_attestors_failing "$work/hosts-good.env" 1 xweb-attestors -- \
	"fail machines app=$xweb1 machines=1 started=0 volumes=0" \
	"fail public-ips app=$xweb1 count=1" \
	"fail health app=$xweb1 http=none" \
	"fail hop app=$xweb1 peer=$(fx_app interop/deploy/x-websearch/attestor-3.toml) port=8493 http=none" \
	"pass hop app=$xweb1 peer=$xweb2 port=8492 http=200" \
	"fail VALIDATOR_HOSTS[0] x-websearch-units=2" \
	"check-live: 17 check(s) failed"

mv "$fx/interop/deploy/x-websearch/attestor-2.toml" "$work/attestor-2.toml"
CHECK_LIVE_TEST_MACHINES="$xweb_machine" CHECK_LIVE_TEST_IPS='[]' CHECK_LIVE_TEST_PROGRAM="$fx_checker" PATH="$work/xweb-bin:$PATH" expect check_live_xweb_attestors_missing_toml "$work/hosts-xweb-down.env" 1 xweb-attestors -- \
	"fail attestor-2 toml=absent" \
	"pass hop app=$xweb1 peer=attestor-2 port=8492 http=200" \
	"fail VALIDATOR_HOSTS[0] x-websearch-units ssh=255" \
	"check-live: 2 check(s) failed"
mv "$work/attestor-2.toml" "$fx/interop/deploy/x-websearch/attestor-2.toml"

CHECK_LIVE_TEST_PROGRAM="$fx_checker" PATH="$work/xweb-bin:$PATH" expect check_live_validators_after_the_move "$work/hosts-xweb-fly.env" 1 validators -- \
	"pass VALIDATOR_HOSTS[0] listeners-8480-8481=0" \
	"pass VALIDATOR_HOSTS[1] listeners-8480-8481=0"
if grep -q '/health' "$CHECK_LIVE_TEST_CALLS"; then
	echo "FAIL check_live_validators_after_the_move_skips_health: want no /health request over ssh once the attestors run on Fly"
	failures=$((failures + 1))
fi

CHECK_LIVE_TEST_XWEB_LISTENERS=1 CHECK_LIVE_TEST_PROGRAM="$fx_checker" PATH="$work/xweb-bin:$PATH" expect check_live_validators_listener_left "$work/hosts-xweb-fly.env" 1 validators -- \
	"fail VALIDATOR_HOSTS[0] listeners-8480-8481=1" \
	"fail VALIDATOR_HOSTS[1] listeners-8480-8481=1"

# kernel-value-loop: the kernel app's fixture machine holds the genesis
# outputs, the publication policy, a bound LNI socket and the receipt
# authority's CA; a copy of sleep named layerx-receipt-authority carries the
# token file in its environment. Stand-ins for setpriv, layerx-node-probe,
# layerxctl, layerx-custody-proof and sign-credit keep their state under
# $vl, and a curl stand-in answers the pod's relay (nativeAssetId, the deposit
# receipt, statusOf from CHECK_LIVE_TEST_VL_FINAL_FROM on) and the receipt
# authority; the sender's opening credit comes from the named deposit.
cp "$root/tools/bringup/value-loop.sh" "$fx/tools/bringup/"
vl="$work/value-loop"
vlroot="$CHECK_LIVE_TEST_FLY/$kernel/app"
vl_asset="$(printf 'layerx-asset:125:PAX' | sha256sum | cut -d' ' -f1)"
vl_deposit="0x$(printf '5%.0s' $(seq 64))"
vl_activity="$(printf 'ab%.0s' $(seq 32))"
vl_batch="$(printf 'cd%.0s' $(seq 32))"
mkdir -p "$vl/bin" "$vl/state" "$vlroot/data/layerx/genesis" "$vlroot/data/layerx/keys/publication" \
	"$vlroot/data/tls/receipt-authority" "$vlroot/run/layerx/node"
printf '%s\n' "$vl_asset" >"$vlroot/data/layerx/genesis/asset-id"
printf '%064d\n' 9 >"$vlroot/data/layerx/genesis/replica-id"
head -c 223 /dev/zero >"$vlroot/data/layerx/genesis/custody.profile"
printf 'https://comet.invalid/comet\n' >"$vlroot/data/layerx/genesis/comet-url"
echo '{}' >"$vlroot/data/layerx/keys/publication/authorization.json"
echo '{}' >"$vlroot/data/layerx/keys/publication/binding-policy.json"
echo 'fixture ca' >"$vlroot/data/tls/receipt-authority/ca.pem"
printf 'fixture-authority-token\n' >"$vl/authority-token"
cat >"$vl/bin/setpriv" <<'SH'
#!/usr/bin/env bash
while [ "$#" -gt 0 ] && [ "$1" != -- ]; do shift; done
shift
exec "$@"
SH
cat >"$vl/bin/layerx-node-probe" <<'SH'
#!/usr/bin/env bash
set -eu
command=$1
shift
declare -A flag=()
while [ "$#" -gt 1 ]; do flag[${1#--}]=$2; shift 2; done
case "$command" in
handshake) printf '{"latest_sealed_batch":5}\n' ;;
balance)
	account=${flag[account]#agent:did:layerx:}
	account=${account%:main}
	if [ -s "$CHECK_LIVE_TEST_VL/state/balance-$account" ]; then
		printf '{"account":"%s","balance":"%s"}\n' "${flag[account]}" "$(cat "$CHECK_LIVE_TEST_VL/state/balance-$account")"
	else
		printf '{"refused":{"class":4,"result":-208}}\n'
	fi
	;;
write-send)
	[ -s "${flag[seed-file]}" ] || exit 2
	printf '%s' "${flag[destination-did]#did:layerx:}" >"$CHECK_LIVE_TEST_VL/state/send-destination"
	printf 'send' >"${flag[output]}"
	printf '%s\n' "$(printf 'ab%.0s' $(seq 32))"
	;;
*) exit 2 ;;
esac
SH
cat >"$vl/bin/layerxctl" <<'SH'
#!/usr/bin/env bash
set -eu
command=$1
shift
declare -A flag=()
while [ "$#" -gt 1 ]; do flag[${1#--}]=$2; shift 2; done
state=$CHECK_LIVE_TEST_VL/state
case "$command" in
submit)
	case "$(cat "${flag[activity]}")" in
	credit) printf '1000' >"$state/balance-${flag[public-key]}" ;;
	send)
		printf '999' >"$state/balance-${flag[public-key]}"
		printf '1' >"$state/balance-$(cat "$state/send-destination")"
		;;
	*) exit 2 ;;
	esac
	printf '{"state":"acknowledged","activity_id":"%s"}\n' "$(printf 'ab%.0s' $(seq 32))"
	;;
read-state) printf '{"sequence":1}\n' ;;
*) exit 2 ;;
esac
SH
cat >"$vl/bin/layerx-custody-proof" <<'SH'
#!/usr/bin/env bash
[ "$1" = light-credit ] || exit 2
while [ "$#" -gt 1 ] && [ "$1" != --output ]; do shift; done
printf 'proof' >"$2"
SH
cat >"$vl/bin/sign-credit" <<'SH'
#!/usr/bin/env bash
[ "$#" -eq 7 ] && [ -s "$1" ] && [ -s "$2" ] && [ -s "$4" ] || exit 2
printf 'credit' >"$7"
SH
cat >"$vl/bin/curl" <<'SH'
#!/usr/bin/env bash
data="" url="" auth=""
while [ "$#" -gt 0 ]; do
	case "$1" in
	-d) data=$2; shift 2 ;;
	-H) [[ $2 != Authorization:* ]] || auth=${2#Authorization: Bearer }; shift 2 ;;
	-m | --cacert) shift 2 ;;
	-*) shift ;;
	*) url=$1; shift ;;
	esac
done
exec python3 - "$url" "$data" "$auth" <<'PY'
import hashlib, json, os, sys
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
url, data, auth = sys.argv[1:]
vl = os.environ["CHECK_LIVE_TEST_VL"]
if url == "http://127.0.0.1:18545":
    request = json.loads(data)
    if request["method"] == "eth_call":
        call = request["params"][0]
        if call["to"].endswith("1013") and call["data"] == "0xaafcde84":
            result = "0x" + hashlib.sha256(b"layerx-asset:125:PAX").hexdigest()
        elif call["to"].endswith("1014") and call["data"].startswith("0x4eb47710"):
            batch = int(call["data"][10:], 16)
            result = "0x%064x" % (1 if batch >= int(os.environ.get("CHECK_LIVE_TEST_VL_FINAL_FROM", "5")) else 0)
        else:
            sys.exit(22)
    elif request["method"] == "eth_getTransactionReceipt":
        seed = open(os.environ["CHECK_LIVE_TEST_VL_SENDER"], "rb").read()
        public = Ed25519PrivateKey.from_private_bytes(seed).public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
        name = ("agent:did:layerx:" + public.hex() + ":main").encode()
        main = hashlib.sha256(b"LX:ACCOUNT:v1" + len(name).to_bytes(4, "big") + name).hexdigest()
        result = {"status": "0x1", "logs": [{"address": "0x" + "0" * 36 + "1013", "topics": [
            "0x7edb71c9100c656847896d0b5b194f69f7da287eb57964a81e7f807a6a944028", "0x" + "77" * 32,
            "0x" + hashlib.sha256(b"layerx-asset:125:PAX").hexdigest(), "0x" + "00" * 32],
            "data": "0x" + main + "%064x" % 1000 + "%064x" % 1}]}
    else:
        sys.exit(22)
    print(json.dumps({"jsonrpc": "2.0", "id": 1, "result": result}))
elif url == "https://127.0.0.1:9445/readyz":
    print(json.dumps({"ready": True, "network_id": 125, "wire_version": 3}))
elif url.startswith("https://127.0.0.1:9445/v1/authorized-batches/by-activity/") and auth == "fixture-authority-token":
    print(json.dumps({"activity_id": url.rsplit("/", 1)[1], "batch_id": "cd" * 32, "network_id": 125}))
else:
    sys.exit(7)
PY
SH
cp "$(command -v sleep)" "$vl/bin/layerx-receipt-authority"
chmod +x "$vl/bin/"*
env LAYERX_AUTHORITY_TOKEN_FILES="$vl/authority-token" "$vl/bin/layerx-receipt-authority" 300 &
vl_authority_pid=$!
python3 -c 'import socket, sys, time; s = socket.socket(socket.AF_UNIX); s.bind(sys.argv[1]); time.sleep(300)' \
	"$vlroot/run/layerx/node/layerxd.lni.sock" &
vl_lni_pid=$!
for _ in $(seq 50); do [ -S "$vlroot/run/layerx/node/layerxd.lni.sock" ] && break; sleep 0.1; done
export CHECK_LIVE_TEST_VL="$vl" CHECK_LIVE_TEST_VL_SENDER="$vlroot/data/layerx/keys/value-loop/sender.key"
CHECK_LIVE_VALUE_LOOP_DEPOSIT_TX="$vl_deposit" PATH="$vl/bin:$PATH" CHECK_LIVE_TEST_PROGRAM="$fx_checker" \
	expect check_live_kernel_value_loop_passing "$work/hosts-good.env" 0 kernel-value-loop -- \
	"pass asset PAX id=$vl_asset" \
	"pass account sender did=did:layerx:" \
	"pass account recipient did=did:layerx:" \
	"pass credit deposit=$vl_deposit amount=1000" \
	"pass activity id=$vl_activity" \
	'pass balance sender {"balance":"999","refused":null}' \
	'pass balance recipient {"balance":"1","refused":null}' \
	"pass batch id=$vl_batch sealed=5" \
	"pass checkpoint batch=5 status=submitted" \
	"check-live: all checks passed"
if [ "$(stat -c '%s %a %u' "$vlroot/data/layerx/keys/value-loop/sender.key")" = "32 400 4021" ] &&
	! grep -qF "$(od -An -tx1 "$vlroot/data/layerx/keys/value-loop/sender.key" | tr -d ' \n')" "$CHECK_LIVE_TEST_CALLS"; then
	echo "ok   check_live_kernel_value_loop_keeps_the_seeds"
else
	echo "FAIL check_live_kernel_value_loop_keeps_the_seeds: want a 32-byte 0400 seed owned by 4021 that never leaves the machine"
	failures=$((failures + 1))
fi
CHECK_LIVE_TEST_VL_FINAL_FROM=99 CHECK_LIVE_VALUE_LOOP_CHECKPOINT_SECONDS=0 PATH="$vl/bin:$PATH" CHECK_LIVE_TEST_PROGRAM="$fx_checker" \
	expect check_live_kernel_value_loop_one_failing "$work/hosts-good.env" 1 kernel-value-loop -- \
	"pass activity id=$vl_activity" \
	"pass batch id=$vl_batch sealed=5" \
	"fail checkpoint batch=5 status=none" \
	"check-live: 1 check(s) failed"
kill "$vl_authority_pid" 2>/dev/null || true
wait "$vl_authority_pid" 2>/dev/null || true
PATH="$vl/bin:$PATH" CHECK_LIVE_TEST_PROGRAM="$fx_checker" \
	expect check_live_kernel_value_loop_receipt_authority_absent "$work/hosts-good.env" 1 kernel-value-loop -- \
	"fail precondition receipt-authority no layerx-receipt-authority process" \
	"check-live: 1 check(s) failed"
rm -f "$vlroot/data/layerx/genesis/asset-id"
PATH="$vl/bin:$PATH" CHECK_LIVE_TEST_PROGRAM="$fx_checker" \
	expect check_live_kernel_value_loop_precondition_absent "$work/hosts-good.env" 1 kernel-value-loop -- \
	"fail precondition genesis-ids" \
	"run kernel-genesis.sh genesis (step C) first" \
	"check-live: 1 check(s) failed"
kill "$vl_lni_pid" 2>/dev/null || true
wait "$vl_lni_pid" 2>/dev/null || true
rm -rf "${vlroot:?}/data/layerx" "${vlroot:?}/data/tls/receipt-authority" "${vlroot:?}/run/layerx/node"
unset CHECK_LIVE_TEST_VL CHECK_LIVE_TEST_VL_SENDER

# The rollback restore of the preserved human state without Fly: the export
# of the preservation cases above goes back onto a fixture clone of the old
# machine. Its volume first sits at the stage path (a mode the restore must
# replace with the old directory's), then, as a machine update would move it,
# at the old path, with a loopback /livez stand-in on the old service's bind
# port; the machine config comes from CHECK_LIVE_TEST_MACHINES.
rb="$work/restore-rb/$kernel/app"
stage="$rb/var/lib/layerx/human-restore"
mkdir -p "$stage/lost+found" "$work/restore-livez"
chmod 0700 "$stage"
: >"$work/restore-livez/livez"
rb_stage='[{"id":"m-rb","state":"started","config":{"mounts":[{"volume":"vol_rb","path":"/var/lib/layerx/human-restore"}]}},{"id":"m-old","state":"stopped","config":{}}]'
rb_old='[{"id":"m-rb","state":"started","config":{"mounts":[{"volume":"vol_rb","path":"/var/lib/layerx/human"}]}}]'
export CHECK_LIVE_TEST_FLY="$work/restore-rb"

CHECK_LIVE_TEST_PROGRAM="$preserve" expect human_state_restore_usage - 2 restore "$out_dir" -- \
	"usage: tools/bringup/human-state-preserve.sh"
CHECK_LIVE_TEST_MACHINES="$rb_stage" CHECK_LIVE_TEST_PROGRAM="$preserve" expect human_state_restore_no_export - 1 restore "$work/preserve-none" m-rb -- \
	"holds no export"
cp -r "$out_dir" "$work/restore-bad"
awk 'NR == 1 { $0 = (substr($0, 1, 1) == "0" ? "1" : "0") substr($0, 2) } 1' "$out_dir/manifest.sha256" >"$work/restore-bad/manifest.sha256"
CHECK_LIVE_TEST_MACHINES="$rb_stage" CHECK_LIVE_TEST_PROGRAM="$preserve" expect human_state_restore_manifest_mismatch - 1 restore "$work/restore-bad" m-rb -- \
	"does not match $work/restore-bad/manifest.sha256"
CHECK_LIVE_TEST_MACHINES='[{"id":"m-new","state":"started","config":{"mounts":[{"volume":"vol_k","path":"/data"}]}}]' CHECK_LIVE_TEST_PROGRAM="$preserve" expect human_state_restore_kernel_machine - 1 restore "$out_dir" m-new -- \
	"m-new is not a restore target of $kernel (mounts=/data)"
CHECK_LIVE_TEST_MACHINES='[{"id":"m-rb","state":"started","config":{"mounts":[{"volume":"vol_rb","path":"/var/lib/layerx/human-restore"}]}},{"id":"m-new","state":"started","config":{}}]' CHECK_LIVE_TEST_PROGRAM="$preserve" expect human_state_restore_two_started - 1 restore "$out_dir" m-rb -- \
	"m-rb is not a restore target of $kernel (started=m-rb,m-new)"
printf 'fresh\n' >"$stage/store"
CHECK_LIVE_TEST_MACHINES="$rb_stage" CHECK_LIVE_TEST_PROGRAM="$preserve" expect human_state_restore_stage_holds_state - 1 restore "$out_dir" m-rb -- \
	"the volume of m-rb already holds state"
rm "$stage/store"

CHECK_LIVE_TEST_MACHINES="$rb_stage" CHECK_LIVE_TEST_PROGRAM="$preserve" expect human_state_restore_stage - 0 restore "$out_dir" m-rb -- \
	"pass restore-stage app=$kernel machine=m-rb files=2 sha256=match"
if [ "$(stat -c %A "$stage")" = "$(tar -tvf "$out_dir/state.tar" human-state/components | head -n 1 | awk '{print $1}')" ] &&
	[ "$(cat "$stage/store/a/journal")" = journal ] && [ "$(cat "$stage/custody/keystore")" = sealed ] &&
	[ ! -e "$rb/data/human-state" ] && [ ! -e "$rb/data/layerx" ] && [ ! -e "$rb/run/human-material" ]; then
	echo "ok   human_state_restore_stage_maps"
else
	echo "FAIL human_state_restore_stage_maps: want the components on the volume with the old directory's mode and nothing on the root"
	failures=$((failures + 1))
fi

mv "$stage" "$rb/var/lib/layerx/human"
livez_port="$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')"
LAYERX_HUMAN_BIND="127.0.0.1:$livez_port" CHECK_LIVE_TEST_MACHINES="$rb_old" CHECK_LIVE_TEST_PROGRAM="$preserve" expect human_state_restore_livez_down - 1 restore "$out_dir" m-rb -- \
	"pass restore-volume app=$kernel machine=m-rb files=2 sha256=match" \
	"the old service on m-rb does not answer /livez"
[ ! -e "$rb/data/human-state" ] && [ ! -e "$rb/data/layerx" ] && [ ! -e "$rb/run/human-material" ] ||
	{ echo "FAIL human_state_restore_livez_down_writes_nothing"; failures=$((failures + 1)); }
python3 -m http.server --bind 127.0.0.1 --directory "$work/restore-livez" "$livez_port" >/dev/null 2>&1 &
livez_pid=$!
for _ in $(seq 50); do
	"$real_curl" -fsS -o /dev/null "http://127.0.0.1:$livez_port/livez" 2>/dev/null && break
	sleep 0.1
done
LAYERX_HUMAN_BIND="127.0.0.1:$livez_port" CHECK_LIVE_TEST_MACHINES="$rb_old" CHECK_LIVE_TEST_PROGRAM="$preserve" expect human_state_restore_old - 0 restore "$out_dir" m-rb -- \
	"pass restore-volume app=$kernel machine=m-rb files=2 sha256=match" \
	"pass livez app=$kernel machine=m-rb http=200" \
	"pass restore app=$kernel machine=m-rb files=5 sha256=match"
if [ "$(cat "$rb/data/human-state/kms/seal")" = seal ] && [ "$(cat "$rb/data/layerx/keys/treasury.key")" = treasury ] &&
	[ "$(cat "$rb/run/human-material/recovery-policy.json")" = recovery ] && [ ! -e "$rb/data/layerx/keys/human-material" ] &&
	[ ! -e "$rb/data/human-state/components" ]; then
	echo "ok   human_state_restore_old_maps"
else
	echo "FAIL human_state_restore_old_maps: want kms and keys under /data, the material at /run/human-material and no components under /data"
	failures=$((failures + 1))
fi
LAYERX_HUMAN_BIND="127.0.0.1:$livez_port" CHECK_LIVE_TEST_MACHINES="$rb_old" CHECK_LIVE_TEST_PROGRAM="$preserve" expect human_state_restore_old_twice - 1 restore "$out_dir" m-rb -- \
	"already exists at its old path on m-rb"
printf 'changed\n' >"$rb/var/lib/layerx/human/store/a/journal"
LAYERX_HUMAN_BIND="127.0.0.1:$livez_port" CHECK_LIVE_TEST_MACHINES="$rb_old" CHECK_LIVE_TEST_PROGRAM="$preserve" expect human_state_restore_old_volume_mismatch - 1 restore "$out_dir" m-rb -- \
	"a file of the volume of m-rb differs from $out_dir/manifest.sha256"
kill "$livez_pid" 2>/dev/null || true
wait "$livez_pid" 2>/dev/null || true
export CHECK_LIVE_TEST_FLY="$fly"

# The events-upstream case: the internal fixture app's journeys and
# approvals volumes hold a producer token, the webhooks and registry fixture
# apps exist, and the kernel, webhooks, endpoint and registry fixture apps
# hold their event-token and trigger secrets. Taking any one away fails it,
# and every value starts with up- so a printed value fails the no-destination
# check.
for group in journeys approvals; do
	mkdir -p "$fly/$internal/$group/data/run"
	printf 'up-token-%s' "$group" >"$fly/$internal/$group/data/run/producer-token"
done
for row in "$kernel HUMAN_EVENTS_JOURNEY_TOKEN HUMAN_EVENTS_APPROVAL_TOKEN HUMAN_EVENTS_WEBHOOKS_TOKEN" \
	"$webhooks WEBHOOKS_SOURCE_TRIGGER_TOKEN" "$endpoint ENDPOINT_EVENTS_WEBHOOKS_TOKEN" "$registry REGISTRY_WEBHOOKS_EVENTS_TOKEN"; do
	read -r app rest <<<"$row"
	mkdir -p "$fly/$app/secrets"
	for name in $rest; do
		printf 'up-secret-%s' "$name" >"$fly/$app/secrets/$name"
	done
done
CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_events_upstream_passing "$work/hosts-good.env" 0 events-upstream -- \
	"pass producer-token app=$internal group=journeys bytes=17" \
	"pass producer-token app=$internal group=approvals bytes=18" \
	"pass app app=$webhooks exists=yes" \
	"pass app app=$registry exists=yes" \
	"pass secret app=$kernel name=HUMAN_EVENTS_JOURNEY_TOKEN listed=yes" \
	"pass secret app=$kernel name=HUMAN_EVENTS_APPROVAL_TOKEN listed=yes" \
	"pass secret app=$kernel name=HUMAN_EVENTS_WEBHOOKS_TOKEN listed=yes" \
	"pass secret app=$webhooks name=WEBHOOKS_SOURCE_TRIGGER_TOKEN listed=yes" \
	"pass secret app=$endpoint name=ENDPOINT_EVENTS_WEBHOOKS_TOKEN listed=yes" \
	"pass secret app=$registry name=REGISTRY_WEBHOOKS_EVENTS_TOKEN listed=yes" \
	"check-live: all checks passed"

# A local human service stand-in on a loopback port: under good/ it answers
# the wallet checks, /readyz ready and a registration ceremony naming
# paxportwallet.com as rp.id; under bad/ /readyz is 503 not ready and the
# ceremony names another relying party. It replaces the hpx responder.
kill "$responder_pid" 2>/dev/null || true
wait "$responder_pid" 2>/dev/null || true
cat >"$work/human.py" <<'PY'
import base64
import http.server
import json
import sys

port_file = sys.argv[1]


class Human(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def answer(self, code, doc, headers=()):
        body = json.dumps(doc).encode()
        self.send_response(code)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(body)))
        for name, value in headers:
            self.send_header(name, value)
        self.end_headers()
        self.wfile.write(body)

    def do_OPTIONS(self):
        self.answer(204, {}, [("access-control-allow-origin", self.headers.get("origin", ""))])

    def do_GET(self):
        good = self.path.startswith("/good/")
        if self.path.endswith("/livez"):
            self.answer(200, {"ok": True, "result": {"live": True, "service": "layerx-human-service"}, "trace": "trc_fixture"})
        elif self.path.endswith("/readyz"):
            self.answer(200 if good else 503, {"ok": True, "result": {"ready": good}, "trace": "trc_fixture"})
        else:
            self.answer(404, {"ok": False})

    def do_POST(self):
        good = self.path.startswith("/good/")
        self.rfile.read(int(self.headers.get("content-length", "0")))
        if self.path.endswith("/v1/intents/plan"):
            self.answer(401, {"ok": False, "error": {"code": "unauthenticated"}, "trace": "trc_fixture"})
        elif self.path.endswith("/v1/accounts") and self.headers.get("idempotency-key"):
            self.answer(201, {"ok": True, "result": {"account_id": "act_fixture"}, "trace": "trc_fixture"})
        elif self.path.endswith("/v1/passkeys/registrations"):
            rp = "paxportwallet.com" if good else "app.paxeer.network"
            ceremony = base64.urlsafe_b64encode(json.dumps({"rp": {"id": rp, "name": "LayerX Human"}, "challenge": "Zml4dHVyZQ"}).encode()).decode().rstrip("=")
            self.answer(201, {"ok": True, "result": {"registration_id": "reg_fixture", "ceremony": ceremony}, "trace": "trc_fixture"})
        else:
            self.answer(404, {"ok": False})


server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Human)
with open(port_file, "w") as handle:
    handle.write(str(server.server_address[1]))
server.serve_forever()
PY
rm -f "$work/port"
python3 "$work/human.py" "$work/port" 2>/dev/null &
responder_pid=$!
for _ in $(seq 50); do
	[ -s "$work/port" ] && break
	sleep 0.1
done
if [ ! -s "$work/port" ]; then
	echo "check-live.test: the human responder did not start" >&2
	exit 2
fi
origin="http://127.0.0.1:$(cat "$work/port")"

CHECK_LIVE_HUMAN_BASE="$origin/good" CHECK_LIVE_HUMAN_PROBE_EMAIL=probe@example.com expect check_live_human_passing "$work/hosts-good.env" 0 human -- \
	"pass live http=200 live=true service=layerx-human-service" \
	"pass preflight http=204 allow-origin=https://paxportwallet.com" \
	"pass plan http=401 code=unauthenticated trace=present" \
	"pass readyz http=200 ready=true" \
	"pass rp-id http=201 rp.id=paxportwallet.com" \
	"check-live: all checks passed"

CHECK_LIVE_HUMAN_BASE="$origin/bad" CHECK_LIVE_HUMAN_PROBE_EMAIL=probe@example.com expect check_live_human_failing "$work/hosts-good.env" 1 human -- \
	"pass live http=200" \
	"fail readyz http=503" \
	"fail rp-id http=201 rp.id=app.paxeer.network want=paxportwallet.com" \
	"check-live: 2 check(s) failed"

CHECK_LIVE_HUMAN_BASE="$origin/good" expect check_live_human_no_probe_email "$work/hosts-good.env" 1 human -- \
	"pass readyz http=200 ready=true" \
	"fail rp-id email=unset" \
	"check-live: 1 check(s) failed"

# The fleet script shares the host map and the ssh helpers, so its own test
# runs as the last case, with this test's stand-ins off the PATH.
status=0
output="$(PATH="$real_path" timeout 5m "$root/tools/bringup/sync-fleet.test.sh" 2>&1)" || status=$?
if [ "$status" -eq 0 ] && grep -qx 'sync-fleet.test: all cases passed' <<<"$output"; then
	echo "ok   sync_fleet_test"
else
	echo "FAIL sync_fleet_test: want exit 0 and every case passed, got exit $status"
	grep -v '^ok ' <<<"$output" || true
	failures=$((failures + 1))
fi

if [ "$failures" -ne 0 ]; then
	echo "check-live.test: $failures case(s) failed"
	exit 1
fi
echo "check-live.test: all cases passed"
