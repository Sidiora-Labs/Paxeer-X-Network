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
real_curl="$(command -v curl)"

# A local ssh stand-in ahead of the real one on PATH: it answers for the
# fixture destinations only, refuses to run without BatchMode, records every
# call and everything sent on stdin, and runs any command other than true on
# this box with the service root moved under a directory of its own per
# destination, so each fixture host keeps its own /etc/layerx.
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
command="${command//"$LAYERX_ETC_DIR"/"$LAYERX_ETC_DIR/$dest"}"
tee -a "$CHECK_LIVE_TEST_STDIN" | bash -c "$command"
SH
chmod +x "$work/bin/ssh"

# A local curl stand-in: answers eth_blockNumber for the public names, at the
# fixed head minus the lag CHECK_LIVE_TEST_LAG ("apiN:blocks ...") assigns,
# fails to connect for the names in CHECK_LIVE_TEST_DOWN, and hands any
# request without a public https name to the real curl, so the hpx cases
# reach the loopback registry stand-in below.
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
export CHECK_LIVE_TEST_STDIN="$work/stdin"
export CHECK_LIVE_TEST_REAL_CURL="$real_curl"
export LAYERX_ETC_DIR="$work/etc"
export LAYERX_CA_DIR="$work/ca"
ca="$root/tools/bringup/ca.sh"

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

LAYERX_CA_DIR="$work/absent-ca" expect check_live_ca_without_a_ca "$work/hosts-good.env" 1 ca -- \
	"fail ca ca cert=absent" \
	"check-live: 1 check(s) failed"

CHECK_LIVE_TEST_PROGRAM="$ca" expect ca_no_subcommand "$work/hosts-good.env" 2 -- \
	"usage: tools/bringup/ca.sh"

CHECK_LIVE_TEST_PROGRAM="$ca" expect ca_issue_without_a_ca "$work/hosts-good.env" 1 issue human KERNEL_HOST -- \
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

CHECK_LIVE_TEST_PROGRAM="$ca" expect ca_issue_unknown_service "$work/hosts-good.env" 2 issue nonexistent KERNEL_HOST -- \
	"ca: unknown service nonexistent"

CHECK_LIVE_TEST_PROGRAM="$ca" expect ca_issue_wrong_role "$work/hosts-good.env" 2 issue human PLATFORM_HOST -- \
	"ca: human lives on KERNEL_HOST, not PLATFORM_HOST"

CHECK_LIVE_TEST_PROGRAM="$ca" expect ca_issue_hosts_file_lacks_role "$work/hosts-missing.env" 2 issue human KERNEL_HOST -- \
	"check-live: BRINGUP_HOSTS_FILE lacks ARCHIVE_HOST"

: >"$CHECK_LIVE_TEST_STDIN"
CHECK_LIVE_TEST_PROGRAM="$ca" expect ca_issue_receipt_authority "$work/hosts-good.env" 0 issue receipt-authority KERNEL_HOST -- \
	"issued receipt-authority KERNEL_HOST fingerprint=" \
	"expires_in=39"

tls="$work/etc/up-kernel/receipt-authority/tls"
san="$(openssl x509 -in "$tls/cert.pem" -noout -ext subjectAltName 2>/dev/null || true)"
if [ "$(stat -c %a "$tls")" = 700 ] &&
	[ "$(stat -c %a "$tls/key.pem")$(stat -c %a "$tls/key.der")$(stat -c %a "$tls/identity.p12")$(stat -c %a "$tls/password")" = 600600600600 ] &&
	[ ! -e "$tls/key.pem.new" ] && [ ! -e "$tls/cert.pem.new" ] && [ ! -e "$tls/ca.pem.new" ] &&
	cmp -s "$tls/ca.pem" "$work/ca/ca.pem" && cmp -s "$tls/ca.der" "$work/ca/ca.der" &&
	openssl verify -CAfile "$work/ca/ca.pem" "$tls/cert.pem" >/dev/null 2>&1 &&
	[ "$(openssl pkey -in "$tls/key.pem" -pubout 2>/dev/null)" = "$(openssl x509 -in "$tls/cert.pem" -noout -pubkey)" ] &&
	[ "$(openssl pkey -inform DER -in "$tls/key.der" -pubout 2>/dev/null)" = "$(openssl x509 -inform DER -in "$tls/cert.der" -noout -pubkey)" ] &&
	openssl pkcs12 -in "$tls/identity.p12" -passin "file:$tls/password" -noout >/dev/null 2>&1 &&
	grep -q 'DNS:layerx-receipt-authority' <<<"$san" && grep -q 'DNS:authority' <<<"$san" &&
	grep -q 'DNS:localhost' <<<"$san" && grep -q 'IP Address:127.0.0.1' <<<"$san" && grep -q 'DNS:up-kernel' <<<"$san" &&
	openssl x509 -in "$tls/cert.pem" -noout -ext extendedKeyUsage | grep -q 'TLS Web Server Authentication'; then
	echo "ok   ca_issue_lands_the_material_on_the_host"
else
	echo "FAIL ca_issue_lands_the_material_on_the_host: want 0600 key, der, p12 and password files in a 0700 directory, no .new leftovers, the CA copied, a chained certificate on the host key with the receipt authority SANs, localhost, the loopback address and the host address"
	ls -la "$tls" 2>&1 || true
	printf '%s\n' "$san"
	failures=$((failures + 1))
fi

if [ "$(grep -c '^up-kernel ' "$CHECK_LIVE_TEST_CALLS")" -eq 2 ] &&
	! grep -q 'PRIVATE KEY' "$CHECK_LIVE_TEST_CALLS" "$CHECK_LIVE_TEST_STDIN" &&
	[ "$(grep -c 'BEGIN CERTIFICATE' "$CHECK_LIVE_TEST_STDIN")" -eq 1 ]; then
	echo "ok   ca_issue_never_moves_the_key"
else
	echo "FAIL ca_issue_never_moves_the_key: want two ssh calls, one certificate on stdin and no private key in any call or on stdin"
	failures=$((failures + 1))
fi

want=0
while read -r _ role _; do
	case "$role" in
	VALIDATOR_HOSTS) want=$((want + 2)) ;;
	*) want=$((want + 1)) ;;
	esac
done < <("$ca" services)
status=0
output="$("$ca" services | while read -r service role _; do
	BRINGUP_HOSTS_FILE="$work/hosts-good.env" "$ca" issue "$service" "$role" </dev/null || exit 1
done 2>&1)" || status=$?
if [ "$status" -eq 0 ] && [ "$(grep -c '^issued ' <<<"$output")" -eq "$want" ] && ! grep -qE '(up|down|hang)-[a-z]' <<<"$output" &&
	grep -q 'DNS:up-validator-a' <<<"$(openssl x509 -in "$work/etc/up-validator-a/x-websearch/tls/cert.pem" -noout -ext subjectAltName)" &&
	grep -q 'DNS:up-validator-b' <<<"$(openssl x509 -in "$work/etc/up-validator-b/x-websearch/tls/cert.pem" -noout -ext subjectAltName)"; then
	echo "ok   ca_issue_every_service"
else
	echo "FAIL ca_issue_every_service: want exit 0 and $want issued lines naming no destination, with one certificate per validator host, got exit $status"
	printf '%s\n' "$output"
	failures=$((failures + 1))
fi

expect check_live_ca_passing "$work/hosts-good.env" 0 ca -- \
	"pass ca ca expires_in=36" \
	"pass receipt-authority@KERNEL_HOST chain=ok san=5/5 expires_in=39" \
	"pass agentd-client@KERNEL_HOST chain=ok san=0/0 expires_in=39" \
	"pass gateway@PLATFORM_HOST chain=ok san=5/5 expires_in=39" \
	"pass x-websearch@VALIDATOR_HOSTS[1] chain=ok san=5/5 expires_in=39" \
	"pass x-websearch@VALIDATOR_HOSTS[2] chain=ok san=5/5 expires_in=39" \
	"check-live: all checks passed"

if [ "$(grep -c '^up-' "$CHECK_LIVE_TEST_CALLS")" -eq "$want" ] && ! grep -qvE '^up-[a-z0-9-]+ cat ' "$CHECK_LIVE_TEST_CALLS"; then
	echo "ok   check_live_ca_reads_one_certificate_per_destination"
else
	echo "FAIL check_live_ca_reads_one_certificate_per_destination: want $want read-only cat calls"
	cat "$CHECK_LIVE_TEST_CALLS"
	failures=$((failures + 1))
fi

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

# Four ways a certificate fails: absent, signed by another CA, short of its
# SANs, and inside thirty days of expiry.
rm "$work/etc/up-kernel/human/tls/cert.pem"
LAYERX_CA_DIR="$work/other-ca" "$ca" init >/dev/null
LAYERX_CA_DIR="$work/other-ca" BRINGUP_HOSTS_FILE="$work/hosts-good.env" "$ca" issue identity PLATFORM_HOST >/dev/null
gateway="$work/etc/up-platform/gateway/tls"
openssl req -new -key "$gateway/key.pem" -subj '/CN=layerx-gateway' -out "$work/gateway.csr" 2>/dev/null
printf 'subjectAltName=DNS:localhost\n' >"$work/gateway.cnf"
openssl x509 -req -in "$work/gateway.csr" -CA "$work/ca/ca.pem" -CAkey "$work/ca/ca.key" -CAcreateserial \
	-days 397 -sha256 -extfile "$work/gateway.cnf" -out "$gateway/cert.pem" 2>/dev/null
relay="$work/etc/up-platform/relay-archive/tls"
openssl req -new -key "$relay/key.pem" -subj '/CN=layerx-relay-archive' -out "$work/relay.csr" 2>/dev/null
printf 'subjectAltName=%s,DNS:up-platform\n' "$("$ca" services | awk '$1 == "relay-archive" {print $5}')" >"$work/relay.cnf"
openssl x509 -req -in "$work/relay.csr" -CA "$work/ca/ca.pem" -CAkey "$work/ca/ca.key" -CAcreateserial \
	-days 10 -sha256 -extfile "$work/relay.cnf" -out "$relay/cert.pem" 2>/dev/null

expect check_live_ca_failing "$work/hosts-good.env" 1 ca -- \
	"pass ca ca expires_in=36" \
	"pass receipt-authority@KERNEL_HOST chain=ok san=5/5 expires_in=39" \
	"fail human@KERNEL_HOST cert=absent" \
	"fail identity@PLATFORM_HOST chain=untrusted san=6/6 expires_in=39" \
	"fail gateway@PLATFORM_HOST chain=ok san=1/5 missing=DNS:layerx-gateway,DNS:api.mainnet-beta.router.paxeer.network,IP:127.0.0.1,host expires_in=39" \
	"fail relay-archive@PLATFORM_HOST chain=ok san=5/5 expires_in=" \
	"check-live: 4 check(s) failed"

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
