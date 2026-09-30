#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
checker="$root/tools/bringup/check-live.sh"
work="$(mktemp -d)"
responder_pid=""
agentd_pid=""
internal_pids=""

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
	rm -rf "$work"
}
trap cleanup EXIT

for tool in timeout python3 curl sha256sum openssl base64; do
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
# fixture apps (fx-*) only, records every call, and gives each app a secrets
# directory for /run/secrets and each process group of it a volume of its own
# for /data, so every fixture machine keeps its own files. ssh console runs
# its command on this box and records its stdin; secrets import wants
# --stage, decodes each NAME=base64 line into the app's secrets directory and
# records only the names.
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
*) exit 96 ;;
esac
SH
chmod +x "$work/bin/flyctl"

# A local curl stand-in: answers eth_blockNumber for the public names, at the
# fixed head minus the lag CHECK_LIVE_TEST_LAG ("apiN:blocks ...") assigns,
# fails to connect for the names in CHECK_LIVE_TEST_DOWN, and hands any
# request without a public https name to the real curl, so the hpx cases
# reach the loopback registry stand-in below. The machine name answers as the
# agentd boundary does: no client certificate fails the handshake, /healthz
# and /rpc want the bearer CHECK_LIVE_TEST_AGENT_BEARER in the --config file,
# and /rpc answers a program.discover of CHECK_LIVE_TEST_AGENT_PROGRAM with
# CHECK_LIVE_TEST_AGENT_RPC_CODE and CHECK_LIVE_TEST_AGENT_RPC. A process-group
# .internal name answers /readyz ready to a request with a client certificate.
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
HPX_HOST=up-hpx
OLD_WALLET_HOST=up-old-wallet
ENV

cat >"$work/hosts-down.env" <<'ENV'
EDGE_HOST=up-edge
ARCHIVE_HOST=up-archive
VALIDATOR_HOSTS="up-validator-a up-validator-b"
RPC_HOSTS="up-rpc-1 down-rpc-2 up-rpc-3"
HPX_HOST=up-hpx
OLD_WALLET_HOST=down-old-wallet
ENV

cat >"$work/hosts-hang.env" <<'ENV'
EDGE_HOST=up-edge
ARCHIVE_HOST=up-archive
VALIDATOR_HOSTS="up-validator-a up-validator-b"
RPC_HOSTS="up-rpc-1 up-rpc-2 up-rpc-3"
HPX_HOST=hang-hpx
OLD_WALLET_HOST=up-old-wallet
ENV

cat >"$work/hosts-missing.env" <<'ENV'
EDGE_HOST=up-edge
VALIDATOR_HOSTS="up-validator-a up-validator-b"
RPC_HOSTS="up-rpc-1 up-rpc-2 up-rpc-3"
HPX_HOST=up-hpx
OLD_WALLET_HOST=up-old-wallet
ENV

cat >"$work/hosts-rpc-good.env" <<'ENV'
EDGE_HOST=up-edge
ARCHIVE_HOST=up-archive
VALIDATOR_HOSTS="up-validator-a up-validator-b"
RPC_HOSTS="up-rpc-1 up-rpc-2 up-rpc-3 up-rpc-4 up-rpc-5 up-rpc-6 up-rpc-7 up-rpc-8 up-rpc-9 up-rpc-10 up-rpc-11 up-rpc-12 up-rpc-13 up-rpc-14 up-rpc-15 up-rpc-16"
HPX_HOST=up-hpx
OLD_WALLET_HOST=up-old-wallet
ENV

cat >"$work/hosts-rpc-bad.env" <<'ENV'
EDGE_HOST=up-edge
ARCHIVE_HOST=up-archive
VALIDATOR_HOSTS="up-validator-a up-validator-b"
RPC_HOSTS="up-rpc-1 up-rpc-2 up-rpc-3 up-rpc-4 up-rpc-5 up-rpc-6 up-rpc-7 up-rpc-8 up-rpc-9-fresh up-rpc-10-dead up-rpc-11 up-rpc-13 up-rpc-14 up-rpc-15 down-rpc-16"
HPX_HOST=up-hpx
OLD_WALLET_HOST=up-old-wallet
ENV

cat >"$work/hosts-placement-bad.env" <<'ENV'
EDGE_HOST=up-edge
ARCHIVE_HOST=up-archive
VALIDATOR_HOSTS="up-validator-a-node up-validator-b-web down-validator-c"
RPC_HOSTS="up-validator-a-node up-rpc-2 up-rpc-3 up-rpc-4 up-rpc-5 up-rpc-6 up-rpc-7 up-rpc-8 up-rpc-9 up-rpc-10 up-rpc-11 up-rpc-12 up-rpc-13 up-rpc-14 up-rpc-15"
HPX_HOST=up-hpx
OLD_WALLET_HOST=up-old-wallet
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
	"pass ARCHIVE_HOST reachable=1/1" \
	"pass VALIDATOR_HOSTS reachable=2/2" \
	"pass RPC_HOSTS reachable=3/3" \
	"pass HPX_HOST reachable=1/1" \
	"pass OLD_WALLET_HOST reachable=1/1" \
	"check-live: all checks passed"

if [ "$(wc -l <"$CHECK_LIVE_TEST_CALLS")" -eq 9 ] && [ "$(sort -u "$CHECK_LIVE_TEST_CALLS" | wc -l)" -eq 9 ] &&
	! grep -qvE '^(up|down|hang)-[a-z0-9-]+ true$' "$CHECK_LIVE_TEST_CALLS"; then
	echo "ok   check_live_hosts_one_ssh_true_per_destination"
else
	echo "FAIL check_live_hosts_one_ssh_true_per_destination: want nine distinct 'destination true' calls"
	cat "$CHECK_LIVE_TEST_CALLS"
	failures=$((failures + 1))
fi

expect check_live_hosts_failing "$work/hosts-down.env" 1 hosts -- \
	"pass EDGE_HOST reachable=1/1" \
	"pass VALIDATOR_HOSTS reachable=2/2" \
	"fail RPC_HOSTS reachable=2/3 ssh=255" \
	"pass HPX_HOST reachable=1/1" \
	"fail OLD_WALLET_HOST reachable=0/1 ssh=255" \
	"check-live: 2 check(s) failed"

CHECK_LIVE_TEST_TIMEOUT=1 expect check_live_hosts_timeout "$work/hosts-hang.env" 1 hosts -- \
	"pass RPC_HOSTS reachable=3/3" \
	"fail HPX_HOST reachable=0/1 ssh=124" \
	"check-live: 1 check(s) failed"

# The CA cases run both scripts from a fixture tree whose tomls stand in for
# the Fly apps: every toml of the service list gets an app line naming its
# fixture app, and every secret prefix a [[files]] entry mounting
# <PREFIX>_CERT.
fx="$work/repo"
mkdir -p "$fx/tools/bringup"
cp "$root/tools/bringup/check-live.sh" "$root/tools/bringup/ca.sh" "$fx/tools/bringup/"
ca="$fx/tools/bringup/ca.sh"
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
status=0
output="$("$ca" services | while read -r service _; do
	CHECK_LIVE_TIMEOUT=5 "$ca" issue "$service" </dev/null || exit 1
done 2>&1)" || status=$?
if [ "$status" -eq 0 ] && [ "$(grep -c '^issued ' <<<"$output")" -eq "$want" ] && [ "$want" -eq 26 ] &&
	! grep -q 'PRIVATE KEY' <<<"$output" &&
	grep -q "DNS:kms.process.$internal.internal" <<<"$(openssl x509 -in "$fly/$internal/kms/data/tls/internal-kms/cert.pem" -noout -ext subjectAltName)" &&
	grep -q "DNS:programs.process.$internal.internal" <<<"$(openssl x509 -in "$fly/$internal/programs/data/tls/internal-programs/cert.pem" -noout -ext subjectAltName)" &&
	grep -q "DNS:ingress.process.$webhooks.internal" <<<"$(openssl x509 -in "$fly/$webhooks/secrets/WEBHOOKS_INGRESS_TLS_CERT" -noout -ext subjectAltName)" &&
	[ -s "$fly/$endpoint/secrets/ENDPOINT_CLIENT_P12" ] && [ -s "$fly/$interop/secrets/INTEROP_CLIENT_P12" ]; then
	echo "ok   ca_issue_every_service"
else
	echo "FAIL ca_issue_every_service: want exit 0 and $want issued lines, the internal groups' certificates on their own volumes under their process-group names and the webhooks ingress certificate staged under its group name, got exit $status"
	printf '%s\n' "$output"
	failures=$((failures + 1))
fi

CHECK_LIVE_TEST_PROGRAM="$fx_checker" expect check_live_ca_passing "$work/hosts-good.env" 0 ca -- \
	"pass ca ca expires_in=36" \
	"pass receipt-authority app=$kernel chain=ok san=5/5 expires_in=39" \
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
	"pass developer app=$webhooks chain=ok san=4/4 expires_in=39" \
	"check-live: 7 check(s) failed"

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
