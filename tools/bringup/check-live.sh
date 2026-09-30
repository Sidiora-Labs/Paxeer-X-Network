#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'EOF'
usage: tools/bringup/check-live.sh hosts|rpc-nodes|archive-node|ca|hpx|explorer

Checks one system of the Paxeer X Network bring-up against its live answers.
Every subcommand reads the operator's private host map from the file named
by BRINGUP_HOSTS_FILE and never prints a value from it; explorer needs no
host map.

hosts     runs ssh true against every destination of every role of the host
          map and prints one line per role:
  <ROLE>  "pass <ROLE> reachable=<n>/<m>" when all m destinations answered,
          "fail <ROLE> reachable=<n>/<m> ssh=<exit>" otherwise, with the exit
          code of the first destination that did not answer (255 when ssh
          could not connect, 124 when CHECK_LIVE_TIMEOUT elapsed)
          Exits 0 only when every role passes.

rpc-nodes asks each of the sixteen public RPC names api1..api16 of the
          network domain for eth_blockNumber over https, and asks every
          RPC_HOSTS destination over ssh which public name its nginx site
          serves, the ActiveState of its paxd.service unit and how many
          seconds that unit has been active. Prints one line per name:
  apiN    "pass apiN head=<height> lag=<blocks> unit=active active=<s>s"
          when the name answered, its lag behind the highest answer is at
          most ten blocks, a destination serves it, and its unit is active
          and has not restarted in the last hour (3600 seconds);
          "fail apiN head=<height|none> lag=<blocks|none>
          unit=<state|unmapped> active=<s>s|none" otherwise.
          A destination that did not answer prints "fail RPC_HOSTS[k]
          ssh=<exit>" (k is its index in RPC_HOSTS) and one whose nginx
          serves no apiN site prints "fail RPC_HOSTS[k] site=none".
          Exits 0 only when every name passes.
archive-node  reads the archive host's retention keys, paxd unit state and
          public RPC name over ssh, then asks that name and the sixteen
          public RPC names over HTTPS, printing three lines:
  retention  "pass ARCHIVE_HOST retention min-retain-blocks=0 ss-keep-recent=0 paxd=active"
             or the fail line with the values read (ssh=<exit> when the host
             did not answer)
  head       "pass ARCHIVE_HOST eth_blockNumber archive=<n> head=<m> gap=<d>"
             where head is the highest answer of the sixteen public names
             and the gap is at most ten blocks
  history    "pass ARCHIVE_HOST eth_getBlockByNumber height=<h> hash=<hash>"
             for h = archive head minus 200000, twice the 100000 blocks the
             pruned public nodes retain
          Exits 0 only when all three pass.

ca        reads the internal CA under LAYERX_CA_DIR on this host and, over
          ssh, the certificate tools/bringup/ca.sh issue placed under
          LAYERX_ETC_DIR/<service>/tls on the host of every service that
          tools/bringup/ca.sh services lists, one line each:
  ca      "pass ca ca expires_in=<days>d" when the CA certificate is readable
          and more than thirty days from expiry
  <service>@<ROLE>
          "pass <service>@<ROLE> chain=ok san=<m>/<m> expires_in=<days>d"
          when the certificate chains to the CA, carries every SAN the
          service list declares plus the host's own address, and is more
          than thirty days from expiry; "fail <service>@<ROLE> cert=absent"
          when the host holds none; otherwise "fail" with chain=untrusted,
          san=<n>/<m> missing=<names> (the host's address written as host)
          or the expiry as observed. A plural role numbers its destinations
          as <service>@<ROLE>[n].
          Exits 0 only when the CA and every certificate pass.

hpx       reads the hpx registry at CHECK_LIVE_HPX_ORIGIN, by default
          https://node.hyperpaxeer.com, and prints one line per check:
  healthz    GET <origin>/healthz answers 200 with ok true, chain_id
             hyperpax_125-1 and a forty-hex source_revision
  checksums  GET <origin>/checksums.txt lists "<sha256>  <path>" lines and
             every listed path, fetched from the origin, hashes to its line;
             "pass checksums verified=<n>/<n>" or "fail checksums
             verified=<k>/<n> first=<path>" naming the first mismatch
  api-nodes  GET <origin>/api/nodes answers 200 with chain_id hyperpax_125-1
             and a nodes list whose length is count
          Exits 0 only when all three checks pass.

explorer  reads the deployed explorer at CHECK_LIVE_EXPLORER_ORIGIN, by default
          https://paxscan.io, its envs.js, its backend and its databases, and
          asks the sixteen public RPC names for their head and the lowest
          block they serve, one line per check:
  frontend   the origin answers 200 and the NEXT_PUBLIC_NETWORK_RPC_URL of its
             /assets/envs.js is https://<one of the sixteen public names>
  archive    that name serves a lower first block than every other name
             within ten blocks of the head (the pruned floor)
  backend    GET /api/health and /api/v2/stats at the NEXT_PUBLIC_API_HOST of
             envs.js answer 200, one line each, and GET
             /api/v2/blocks/<the archive name's first block> answers 200
  history    over EXPLORER_DATABASE_URL, EXPLORER_LEGACY_DATABASE_URL and
             PAXSCAN_DATABASE_PUBLIC_URL: the consensus blocks the explorer
             database holds from its first block up to the archive name's
             first block equal the legacy database's from that block up to
             its ceiling plus the paxscan database's above the ceiling
  missing-ranges
             every missing_block_ranges row of the explorer database at or
             above its first block lies inside the union of the two sources'
             missing_block_ranges
          Exits 0 only when every check passes.

Environment:
  BRINGUP_HOSTS_FILE   private env file assigning EDGE_HOST, KERNEL_HOST,
                       PLATFORM_HOST, EXPLORER_HOST, ARCHIVE_HOST,
                       VALIDATOR_HOSTS, RPC_HOSTS and HPX_HOST; each value is
                       one ssh destination or, for the plural roles, a
                       space-separated list of them
  CHECK_LIVE_HPX_ORIGIN  origin of the hpx registry, default
                       https://node.hyperpaxeer.com
  CHECK_LIVE_EXPLORER_ORIGIN  origin of the explorer frontend, default
                       https://paxscan.io
  EXPLORER_DATABASE_URL, EXPLORER_LEGACY_DATABASE_URL,
  PAXSCAN_DATABASE_PUBLIC_URL  read-only connection strings of the explorer's
                       production database, its legacy database and the
                       paxscan copy source; never printed
  CHECK_LIVE_TIMEOUT   seconds per request, default 30
  LAYERX_CA_DIR        the internal CA directory on this host, default
                       /etc/layerx/ca
  LAYERX_ETC_DIR       the service directory root on every host, default
                       /etc/layerx

Exits 1 when any check fails, 2 on a usage error, an unset BRINGUP_HOSTS_FILE
or a host map lacking a role.
EOF
}

timeout="${CHECK_LIVE_TIMEOUT:-30}"
ca_dir="${LAYERX_CA_DIR:-/etc/layerx/ca}"
etc_dir="${LAYERX_ETC_DIR:-/etc/layerx}"

roles=(EDGE_HOST KERNEL_HOST PLATFORM_HOST EXPLORER_HOST ARCHIVE_HOST VALIDATOR_HOSTS RPC_HOSTS HPX_HOST)

# The sixteen public RPC names of docs/site/docs/reference/public-rpc.md.
rpc_names=(api{1..16}.mainnet-beta.paxeer.network)

# load_hosts: sources BRINGUP_HOSTS_FILE and exits 2 naming the first role it
# lacks. Nothing read from the file is ever printed.
load_hosts() {
	local role
	if [ -z "${BRINGUP_HOSTS_FILE:-}" ]; then
		echo "check-live: BRINGUP_HOSTS_FILE is unset" >&2
		exit 2
	fi
	if [ ! -r "$BRINGUP_HOSTS_FILE" ]; then
		echo "check-live: BRINGUP_HOSTS_FILE does not name a readable file" >&2
		exit 2
	fi
	# shellcheck disable=SC1090
	. "$BRINGUP_HOSTS_FILE"
	for role in "${roles[@]}"; do
		if [ -z "${!role:-}" ]; then
			echo "check-live: BRINGUP_HOSTS_FILE lacks $role" >&2
			exit 2
		fi
	done
}

# finish <failures>: prints the summary line and exits 1 on any failure.
finish() {
	if [ "$1" -ne 0 ]; then
		echo "check-live: $1 check(s) failed"
		exit 1
	fi
	echo "check-live: all checks passed"
	exit 0
}

# ssh_true <destination>: runs true on the destination without prompting,
# bounded by CHECK_LIVE_TIMEOUT, printing nothing; returns the ssh exit code
# or 124 when the bound elapsed.
ssh_true() {
	timeout "$timeout" ssh -n -o BatchMode=yes -- "$1" true >/dev/null 2>&1
}

# ssh_read <destination> <command>: runs a read-only command on the
# destination without prompting or a terminal, bounded by CHECK_LIVE_TIMEOUT;
# its stdout is the result and its stderr is dropped.
ssh_read() {
	timeout "$timeout" ssh -n -o BatchMode=yes -- "$1" "$2" 2>/dev/null
}

# address_san <destination>: the SAN a server certificate carries for the
# destination's own address: IP: for an address, DNS: for a name.
address_san() {
	local host="${1##*@}"
	host="${host#[}"
	host="${host%]}"
	case "$host" in
	*[!0-9.:]*) printf 'DNS:%s' "$host" ;;
	*) printf 'IP:%s' "$host" ;;
	esac
}

# expected_sans <eku> <sans> <destination>: the comma-separated SAN list the
# certificate of a service must carry: the declared list ("-" for none) plus
# the destination's own address for a server certificate.
expected_sans() {
	local sans="$2"
	[ "$sans" != - ] || sans=""
	case "$1" in
	*serverAuth*) sans="${sans:+$sans,}$(address_san "$3")" ;;
	esac
	printf '%s' "$sans"
}

# days_left: whole days from now until the notAfter of the PEM certificate on
# stdin.
days_left() {
	local end
	end="$(openssl x509 -noout -enddate | cut -d= -f2)"
	echo $((($(date -d "$end" +%s) - $(date +%s)) / 86400))
}

rpc_domain="mainnet-beta.paxeer.network"
rpc_name_count=16
rpc_max_lag=10
rpc_min_active=3600
# shellcheck disable=SC2016
rpc_unit_cmd='n=$(ls /etc/nginx/sites-enabled 2>/dev/null | sed -n "s/^\(api[0-9]*\)\..*\.conf$/\1/p" | head -n 1); s=$(systemctl show paxd.service -p ActiveState --value 2>/dev/null); e=$(systemctl show paxd.service -p ActiveEnterTimestampMonotonic --value 2>/dev/null); u=$(awk "{print int(\$1)}" /proc/uptime); echo "${n:-none} ${s:-none} $((u - ${e:-0} / 1000000))"'

# rpc_head <name>: prints the decimal eth_blockNumber the public name answers
# over https within CHECK_LIVE_TIMEOUT, or nothing (status 1) when it does not.
rpc_head() {
	local hex
	hex="$(curl -s -m "$timeout" -H 'content-type: application/json' \
		-d '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
		"https://$1.$rpc_domain" 2>/dev/null | sed -n 's/.*"result":"0x\([0-9a-fA-F]*\)".*/\1/p')"
	[ -n "$hex" ] && printf '%d\n' "0x$hex"
}

# rpc_unit <destination>: prints "<apiN|none> <ActiveState> <seconds active>"
# for the paxd.service unit behind the destination's nginx site; returns the
# ssh exit code or 124 when CHECK_LIVE_TIMEOUT elapsed.
rpc_unit() {
	timeout "$timeout" ssh -n -o BatchMode=yes -- "$1" "$rpc_unit_cmd" 2>/dev/null
}

check_rpc_nodes() {
	local -a dests heads
	local -A units=()
	local k reply status name state secs n head lag ok polls top=0 failures=0
	read -r -a dests <<<"$RPC_HOSTS"
	for k in "${!dests[@]}"; do
		status=0
		reply="$(rpc_unit "${dests[$k]}")" || status=$?
		if [ "$status" -ne 0 ]; then
			echo "fail RPC_HOSTS[$k] ssh=$status"
			failures=$((failures + 1))
			continue
		fi
		read -r name state secs <<<"$reply"
		case "$name" in
		api[1-9] | api1[0-6]) units[$name]="$state $secs" ;;
		*)
			echo "fail RPC_HOSTS[$k] site=none"
			failures=$((failures + 1))
			;;
		esac
	done
	# The sixteen names are asked at once so that the ten-block window is not
	# eaten by the chain advancing between one request and the next.
	polls="$(mktemp -d)"
	trap 'rm -rf "$polls"' EXIT
	for n in $(seq 1 "$rpc_name_count"); do
		rpc_head "api$n" >"$polls/$n" 2>/dev/null &
	done
	wait
	for n in $(seq 1 "$rpc_name_count"); do
		heads[n]="$(cat "$polls/$n")"
		if [ -n "${heads[n]}" ] && [ "${heads[n]}" -gt "$top" ]; then
			top="${heads[n]}"
		fi
	done
	for n in $(seq 1 "$rpc_name_count"); do
		name="api$n"
		head="${heads[n]}"
		lag=none
		state=unmapped
		secs=none
		ok=1
		if [ -n "$head" ]; then
			lag=$((top - head))
			[ "$lag" -le "$rpc_max_lag" ] || ok=0
		else
			head=none
			ok=0
		fi
		if [ -n "${units[$name]:-}" ]; then
			read -r state secs <<<"${units[$name]}"
			secs="${secs}s"
			[ "$state" = active ] && [ "${secs%s}" -ge "$rpc_min_active" ] || ok=0
		else
			ok=0
		fi
		if [ "$ok" -eq 1 ]; then
			echo "pass $name head=$head lag=$lag unit=$state active=$secs"
		else
			echo "fail $name head=$head lag=$lag unit=$state active=$secs"
			failures=$((failures + 1))
		fi
	done
	finish "$failures"
}

check_hosts() {
	local role dest dests total answered first status failures=0
	for role in "${roles[@]}"; do
		read -r -a dests <<<"${!role}"
		total="${#dests[@]}"
		answered=0
		first=0
		for dest in "${dests[@]}"; do
			status=0
			ssh_true "$dest" || status=$?
			if [ "$status" -eq 0 ]; then
				answered=$((answered + 1))
			elif [ "$first" -eq 0 ]; then
				first=$status
			fi
		done
		if [ "$answered" -eq "$total" ]; then
			echo "pass $role reachable=$answered/$total"
		else
			echo "fail $role reachable=$answered/$total ssh=$first"
			failures=$((failures + 1))
		fi
	done
	finish "$failures"
}

# rpc <name> <method> <params>: posts one JSON-RPC request to https://<name>
# bounded by CHECK_LIVE_TIMEOUT and prints "result <value>" (a string result
# as is, a block as its number and hash) or "error <reason>".
rpc() {
	local body status=0
	body="$(curl -sS --max-time "$timeout" -H 'content-type: application/json' \
		--data "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"$2\",\"params\":$3}" \
		"https://$1" 2>&1)" || status=$?
	if [ "$status" -ne 0 ]; then
		echo "error transport $(printf '%s' "$body" | tr '\n' ' ' | cut -c1-120)"
		return 0
	fi
	printf '%s' "$body" | python3 -c '
import json
import sys

try:
    doc = json.loads(sys.stdin.read())
except ValueError:
    print("error non-json")
    sys.exit(0)
result = doc.get("result") if isinstance(doc, dict) else None
error = doc.get("error") if isinstance(doc, dict) else None
if isinstance(error, dict):
    print("error " + " ".join(str(error.get("message", error)).split())[:120])
elif isinstance(result, str):
    print("result " + result)
elif isinstance(result, dict):
    print("result " + str(result.get("number")) + " " + str(result.get("hash")))
else:
    print("error no-result")
'
}

# hex_to_dec <quantity>: prints the decimal value of a 0x-prefixed hex
# quantity and nothing for anything else.
hex_to_dec() {
	if [[ "$1" =~ ^0x[0-9a-fA-F]+$ ]]; then
		printf '%d' "$((16#${1#0x}))"
	fi
}

# The read-only inspection the archive-node check runs on the archive host:
# the public RPC name its nginx serves, the two retention keys of the hpx
# node home and the paxd unit state, one line, none of it a host address.
# shellcheck disable=SC2016
archive_facts='name=$(sed -n "s/^[[:space:]]*server_name[[:space:]]*\([a-z0-9-]*\.mainnet-beta\.paxeer\.network\);.*/\1/p" /etc/nginx/sites-enabled/*.conf 2>/dev/null | head -n 1)
retain=$(sed -n "s/^min-retain-blocks = //p" /root/.paxeer/config/app.toml 2>/dev/null)
keep=$(sed -n "s/^ss-keep-recent = //p" /root/.paxeer/config/app.toml 2>/dev/null)
unit=$(systemctl is-active paxd 2>/dev/null)
printf "%s %s %s %s\n" "${name:-none}" "${retain:-none}" "${keep:-none}" "${unit:-none}"'

check_archive_node() {
	local failures=0 status=0 facts name retain keep unit dir i verdict value n head=0 archive="" deep hash
	facts="$(timeout "$timeout" ssh -n -o BatchMode=yes -- "$ARCHIVE_HOST" "$archive_facts" 2>/dev/null)" || status=$?
	read -r name retain keep unit <<<"$facts" || true
	if [ "$status" -ne 0 ]; then
		echo "fail ARCHIVE_HOST retention ssh=$status"
		failures=$((failures + 1))
		name=none
	elif [ "$retain" = 0 ] && [ "$keep" = 0 ] && [ "$unit" = active ]; then
		echo "pass ARCHIVE_HOST retention min-retain-blocks=0 ss-keep-recent=0 paxd=active"
	else
		echo "fail ARCHIVE_HOST retention min-retain-blocks=$retain ss-keep-recent=$keep paxd=$unit"
		failures=$((failures + 1))
	fi

	dir="$(mktemp -d)"
	trap 'rm -rf "$dir"' EXIT
	for i in "${!rpc_names[@]}"; do
		rpc "${rpc_names[$i]}" eth_blockNumber '[]' >"$dir/$i" &
	done
	wait
	for i in "${!rpc_names[@]}"; do
		read -r verdict value <"$dir/$i" || true
		n="$(hex_to_dec "${value:-}")"
		if [ "$verdict" = result ] && [ -n "$n" ] && [ "$n" -gt "$head" ]; then
			head=$n
		fi
	done

	verdict=""
	value=""
	if [ "$name" != none ]; then
		read -r verdict value <<<"$(rpc "$name" eth_blockNumber '[]')" || true
		if [ "$verdict" = result ]; then
			archive="$(hex_to_dec "$value")"
		fi
	fi
	if [ "$name" = none ]; then
		echo "fail ARCHIVE_HOST eth_blockNumber public-name=none"
		failures=$((failures + 1))
	elif [ -z "$archive" ]; then
		echo "fail ARCHIVE_HOST eth_blockNumber $verdict $value"
		failures=$((failures + 1))
	elif [ "$head" -gt 0 ] && [ $((head - archive)) -le 10 ]; then
		echo "pass ARCHIVE_HOST eth_blockNumber archive=$archive head=$head gap=$((head - archive))"
	else
		echo "fail ARCHIVE_HOST eth_blockNumber archive=$archive head=$head gap=$((head - archive))"
		failures=$((failures + 1))
	fi

	if [ -z "$archive" ]; then
		echo "fail ARCHIVE_HOST eth_getBlockByNumber no-archive-head"
		failures=$((failures + 1))
	else
		deep=$((archive - 200000))
		hash=""
		read -r verdict value hash <<<"$(rpc "$name" eth_getBlockByNumber "[\"$(printf '0x%x' "$deep")\",false]")" || true
		if [ "$verdict" = result ] && [ "$(hex_to_dec "$value")" = "$deep" ] && [[ "$hash" =~ ^0x[0-9a-f]{64}$ ]]; then
			echo "pass ARCHIVE_HOST eth_getBlockByNumber height=$deep hash=$hash"
		else
			echo "fail ARCHIVE_HOST eth_getBlockByNumber height=$deep $verdict $value $hash"
			failures=$((failures + 1))
		fi
	fi
	finish "$failures"
}

check_ca() {
	local table service role eku sans dests dest i label cert chain
	local want got san missing n m days line failures=0
	if [ ! -r "$ca_dir/ca.pem" ]; then
		echo "fail ca ca cert=absent"
		finish 1
	fi
	days="$(days_left <"$ca_dir/ca.pem")"
	if [ "$days" -gt 30 ]; then
		echo "pass ca ca expires_in=${days}d"
	else
		echo "fail ca ca expires_in=${days}d"
		failures=$((failures + 1))
	fi
	table="$("$(dirname "${BASH_SOURCE[0]}")/ca.sh" services)"
	while read -r service role _ eku sans; do
		read -r -a dests <<<"${!role}"
		for i in "${!dests[@]}"; do
			dest="${dests[$i]}"
			label="$service@$role"
			[ "${#dests[@]}" -eq 1 ] || label="${label}[$((i + 1))]"
			if ! cert="$(ssh_read "$dest" "cat '$etc_dir/$service/tls/cert.pem'")" || [ -z "$cert" ]; then
				echo "fail $label cert=absent"
				failures=$((failures + 1))
				continue
			fi
			chain=ok
			openssl verify -CAfile "$ca_dir/ca.pem" <<<"$cert" >/dev/null 2>&1 || chain=untrusted
			want="$(expected_sans "$eku" "$sans" "$dest")"
			got="$(openssl x509 -noout -ext subjectAltName <<<"$cert" 2>/dev/null | tail -n +2 | sed 's/IP Address:/IP:/g; s/, /,/g; s/^ *//')"
			missing=""
			n=0
			m=0
			while read -r -d, san; do
				[ -n "$san" ] || continue
				m=$((m + 1))
				if [[ ",$got," == *",$san,"* ]]; then
					n=$((n + 1))
				else
					[ "$san" != "$(address_san "$dest")" ] || san=host
					missing="${missing:+$missing,}$san"
				fi
			done <<<"${want:+$want,}"
			days="$(days_left <<<"$cert")"
			line="$label chain=$chain san=$n/$m${missing:+ missing=$missing} expires_in=${days}d"
			if [ "$chain" = ok ] && [ -z "$missing" ] && [ "$days" -gt 30 ]; then
				echo "pass $line"
			else
				echo "fail $line"
				failures=$((failures + 1))
			fi
		done
	done <<<"$table"
	finish "$failures"
}

# check_hpx: reads the hpx registry at its public origin: /healthz with the
# chain id and the source revision, checksums.txt verified against every
# served artifact it lists, and /api/nodes answering.
check_hpx() {
	local origin="${CHECK_LIVE_HPX_ORIGIN:-https://node.hyperpaxeer.com}" failures=0
	local status body verdict manifest line sum path served total=0 verified=0 first=""
	local entry='^([0-9a-f]{64})  ([A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*)$'
	origin="${origin%/}"

	status=0
	body="$(curl -sS --max-time "$timeout" -w '\n%{http_code}' "$origin/healthz" 2>&1)" || status=$?
	if [ "$status" -ne 0 ]; then
		verdict="fail transport $(printf '%s' "$body" | tr '\n' ' ' | cut -c1-200)"
	else
		verdict="$(printf '%s' "$body" | python3 -c '
import json
import re
import sys

raw = sys.stdin.read()
text, _, code = raw.rpartition("\n")
try:
    doc = json.loads(text)
except ValueError:
    print("fail http=" + code + " non-json " + " ".join(text.split())[:200])
    sys.exit(0)
if not isinstance(doc, dict):
    print("fail http=" + code + " not-health " + json.dumps(doc)[:200])
    sys.exit(0)
chain = doc.get("chain_id")
rev = doc.get("source_revision")
ok = (
    code == "200"
    and doc.get("ok") is True
    and chain == "hyperpax_125-1"
    and isinstance(rev, str)
    and re.fullmatch(r"[0-9a-f]{40}", rev) is not None
)
print(("pass " if ok else "fail ") + "http=" + code + " ok=" + str(doc.get("ok") is True).lower()
      + " chain_id=" + str(chain) + " source_revision=" + str(rev))
')"
	fi
	case "$verdict" in
	pass\ *) echo "pass healthz ${verdict#pass }" ;;
	*)
		echo "fail healthz ${verdict#fail }"
		failures=$((failures + 1))
		;;
	esac

	status=0
	manifest="$(curl -sS --max-time "$timeout" "$origin/checksums.txt" 2>&1)" || status=$?
	if [ "$status" -ne 0 ]; then
		echo "fail checksums transport $(printf '%s' "$manifest" | tr '\n' ' ' | cut -c1-200)"
		failures=$((failures + 1))
	else
		while IFS= read -r line; do
			[ -n "$line" ] || continue
			total=$((total + 1))
			if [[ "$line" =~ $entry ]]; then
				sum="${BASH_REMATCH[1]}"
				path="${BASH_REMATCH[2]}"
				status=0
				served="$(curl -sS --max-time "$timeout" "$origin/$path" 2>/dev/null | sha256sum | cut -d' ' -f1)" || status=$?
				if [ "$status" -eq 0 ] && [ "$served" = "$sum" ]; then
					verified=$((verified + 1))
				else
					first="${first:-$path}"
				fi
			else
				first="${first:-malformed:$(printf '%s' "$line" | cut -c1-80)}"
			fi
		done <<<"$manifest"
		if [ "$total" -gt 0 ] && [ "$verified" -eq "$total" ]; then
			echo "pass checksums verified=$verified/$total"
		else
			echo "fail checksums verified=$verified/$total first=${first:-empty-manifest}"
			failures=$((failures + 1))
		fi
	fi

	status=0
	body="$(curl -sS --max-time "$timeout" -w '\n%{http_code}' "$origin/api/nodes" 2>&1)" || status=$?
	if [ "$status" -ne 0 ]; then
		verdict="fail transport $(printf '%s' "$body" | tr '\n' ' ' | cut -c1-200)"
	else
		verdict="$(printf '%s' "$body" | python3 -c '
import json
import sys

raw = sys.stdin.read()
text, _, code = raw.rpartition("\n")
try:
    doc = json.loads(text)
except ValueError:
    print("fail http=" + code + " non-json " + " ".join(text.split())[:200])
    sys.exit(0)
if not isinstance(doc, dict):
    print("fail http=" + code + " not-nodes " + json.dumps(doc)[:200])
    sys.exit(0)
chain = doc.get("chain_id")
count = doc.get("count")
nodes = doc.get("nodes")
ok = (
    code == "200"
    and chain == "hyperpax_125-1"
    and isinstance(count, int)
    and isinstance(nodes, list)
    and len(nodes) == count
)
print(("pass " if ok else "fail ") + "http=" + code + " chain_id=" + str(chain) + " count=" + str(count))
')"
	fi
	case "$verdict" in
	pass\ *) echo "pass api-nodes ${verdict#pass }" ;;
	*)
		echo "fail api-nodes ${verdict#fail }"
		failures=$((failures + 1))
		;;
	esac
	finish "$failures"
}

# earliest <name>: asks https://<name> for block 1 and prints the lowest
# height the node still serves: 1 when block 1 answers, the height its pruning
# error names otherwise, nothing when neither can be read.
earliest() {
	curl -sS --max-time "$timeout" -H 'content-type: application/json' \
		--data '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["0x1",false]}' \
		"https://$1" 2>/dev/null | python3 -c '
import json
import re
import sys

try:
    doc = json.loads(sys.stdin.read())
except ValueError:
    sys.exit(0)
if not isinstance(doc, dict):
    sys.exit(0)
if isinstance(doc.get("result"), dict):
    print(1)
    sys.exit(0)
error = doc.get("error")
match = re.search(r"earliest available height (\d+)", str(error.get("message", "")) if isinstance(error, dict) else "")
if match:
    print(match.group(1))
'
}

# http_code <url>: prints the HTTP status of one GET bounded by
# CHECK_LIVE_TIMEOUT, or "transport" when no answer arrived.
http_code() {
	curl -sS --max-time "$timeout" -o /dev/null -w '%{http_code}' "$1" 2>/dev/null || echo transport
}

# sql <variable> <query>: runs one read-only query against the database whose
# connection string the named variable holds and prints the unaligned rows;
# never prints the connection string or psql's diagnostics.
sql() {
	PGCONNECT_TIMEOUT="$timeout" psql -X -At -F ' ' -v ON_ERROR_STOP=1 -d "${!1}" -c "$2" 2>/dev/null
}

# ranges_inside: reads "dst" and "src" lines of "<kind> <a> <b>" block ranges
# on stdin and prints "inside" when every dst range lies inside the union of
# the src ranges, or the first dst range that does not.
ranges_inside='
import sys

dst, src = [], []
for line in sys.stdin:
    parts = line.split()
    if parts and parts[0] == "error":
        print("error " + " ".join(parts[1:]))
        sys.exit(0)
    if len(parts) != 3:
        continue
    lo, hi = sorted((int(parts[1]), int(parts[2])))
    (dst if parts[0] == "dst" else src).append((lo, hi))
merged = []
for lo, hi in sorted(src):
    if merged and lo <= merged[-1][1] + 1:
        merged[-1] = (merged[-1][0], max(merged[-1][1], hi))
    else:
        merged.append((lo, hi))
for lo, hi in sorted(dst):
    if not any(a <= lo and hi <= b for a, b in merged):
        print("outside %d-%d" % (lo, hi))
        sys.exit(0)
print("inside")
'

check_explorer() {
	local origin="${CHECK_LIVE_EXPLORER_ORIGIN:-https://paxscan.io}" failures=0
	local dir i verdict value n head=0 floor="" code envs api rpc_url name="" archive_low="" var missing=""
	local first ceiling dst legacy paxscan answer
	origin="${origin%/}"

	dir="$(mktemp -d)"
	trap 'rm -rf "$dir"' EXIT
	for i in "${!rpc_names[@]}"; do
		rpc "${rpc_names[$i]}" eth_blockNumber '[]' >"$dir/head.$i" &
		earliest "${rpc_names[$i]}" >"$dir/low.$i" &
	done
	wait
	for i in "${!rpc_names[@]}"; do
		read -r verdict value <"$dir/head.$i" || true
		n="$(hex_to_dec "${value:-}")"
		if [ "$verdict" = result ] && [ -n "$n" ] && [ "$n" -gt "$head" ]; then
			head=$n
		fi
		echo "$n" >"$dir/n.$i"
	done

	code="$(http_code "$origin")"
	envs="$(curl -sS --max-time "$timeout" "$origin/assets/envs.js" 2>/dev/null)" || envs=""
	read -r api rpc_url <<<"$(printf '%s' "$envs" | python3 -c '
import re
import sys

text = sys.stdin.read()
def get(key):
    match = re.search(key + r"\s*:\s*\"([^\"]*)\"", text)
    return match.group(1) if match else ""
host = get("NEXT_PUBLIC_API_HOST")
proto = get("NEXT_PUBLIC_API_PROTOCOL") or "https"
print((proto + "://" + host if host else "none") + " " + (get("NEXT_PUBLIC_NETWORK_RPC_URL") or "none"))
')" || true
	for i in "${!rpc_names[@]}"; do
		if [ "${rpc_url%/}" = "https://${rpc_names[$i]}" ]; then
			name="${rpc_names[$i]}"
			archive_low="$(cat "$dir/low.$i")"
		fi
	done
	if [ "$code" = 200 ] && [ -n "$name" ]; then
		echo "pass frontend $origin http=200 rpc=$name"
	else
		echo "fail frontend $origin http=$code rpc=${rpc_url:-none}"
		failures=$((failures + 1))
	fi

	# The pruned floor: the lowest height any other name at the head serves.
	for i in "${!rpc_names[@]}"; do
		n="$(cat "$dir/n.$i")"
		value="$(cat "$dir/low.$i")"
		if [ "${rpc_names[$i]}" != "$name" ] && [ -n "$n" ] && [ -n "$value" ] && [ $((head - n)) -le 10 ]; then
			if [ -z "$floor" ] || [ "$value" -lt "$floor" ]; then
				floor=$value
			fi
		fi
	done
	if [ -n "$archive_low" ] && [ -n "$floor" ] && [ "$archive_low" -lt "$floor" ]; then
		echo "pass archive $name first_retained=$archive_low pruned_floor=$floor"
	else
		echo "fail archive ${name:-none} first_retained=${archive_low:-none} pruned_floor=${floor:-none}"
		failures=$((failures + 1))
	fi

	for value in /api/health /api/v2/stats; do
		code="none"
		[ "$api" = none ] || code="$(http_code "$api$value")"
		if [ "$code" = 200 ]; then
			echo "pass backend $value http=200"
		else
			echo "fail backend $value http=$code"
			failures=$((failures + 1))
		fi
	done

	code="none"
	if [ "$api" != none ] && [ -n "$archive_low" ]; then
		code="$(http_code "$api/api/v2/blocks/$archive_low")"
	fi
	if [ "$code" = 200 ]; then
		echo "pass backend block=$archive_low below pruned_floor=$floor"
	else
		echo "fail backend block=${archive_low:-none} http=$code"
		failures=$((failures + 1))
	fi

	for var in EXPLORER_DATABASE_URL EXPLORER_LEGACY_DATABASE_URL PAXSCAN_DATABASE_PUBLIC_URL; do
		[ -n "${!var:-}" ] || missing="$missing${missing:+,}$var"
	done
	if [ -n "$missing" ] || [ -z "$archive_low" ]; then
		echo "fail history unset=${missing:-none} first_retained=${archive_low:-none}"
		finish $((failures + 2))
	fi

	first="$(sql EXPLORER_DATABASE_URL 'SELECT min(number) FROM blocks WHERE consensus')" || first=""
	ceiling="$(sql EXPLORER_LEGACY_DATABASE_URL 'SELECT max(number) FROM blocks WHERE consensus')" || ceiling=""
	if ! [[ "$first" =~ ^[0-9]+$ && "$ceiling" =~ ^[0-9]+$ ]]; then
		echo "fail history first=${first:-none} legacy_ceiling=${ceiling:-none}"
		finish $((failures + 2))
	fi
	dst="$(sql EXPLORER_DATABASE_URL "SELECT count(*) FROM blocks WHERE consensus AND number BETWEEN $first AND $((archive_low - 1))")" || dst=""
	legacy="$(sql EXPLORER_LEGACY_DATABASE_URL "SELECT count(*) FROM blocks WHERE consensus AND number BETWEEN $first AND $ceiling")" || legacy=""
	paxscan="$(sql PAXSCAN_DATABASE_PUBLIC_URL "SELECT count(*) FROM blocks WHERE consensus AND number > $ceiling AND number < $archive_low")" || paxscan=""
	if [[ "$dst" =~ ^[0-9]+$ && "$legacy" =~ ^[0-9]+$ && "$paxscan" =~ ^[0-9]+$ ]] && [ "$dst" -eq $((legacy + paxscan)) ]; then
		echo "pass history blocks=$dst from=$first legacy=$legacy paxscan=$paxscan ceiling=$ceiling"
	else
		echo "fail history blocks=${dst:-none} from=$first legacy=${legacy:-none} paxscan=${paxscan:-none} ceiling=$ceiling"
		failures=$((failures + 1))
	fi

	answer="$(
		{
			sql EXPLORER_DATABASE_URL "SELECT 'dst', from_number, to_number FROM missing_block_ranges WHERE greatest(from_number, to_number) >= $first" || echo "error explorer-database"
			sql EXPLORER_LEGACY_DATABASE_URL "SELECT 'src', from_number, to_number FROM missing_block_ranges" || echo "error legacy-database"
			sql PAXSCAN_DATABASE_PUBLIC_URL "SELECT 'src', from_number, to_number FROM missing_block_ranges" || echo "error paxscan-database"
		} | python3 -c "$ranges_inside"
	)"
	if [ "$answer" = inside ]; then
		echo "pass missing-ranges inside-source-lost from=$first"
	else
		echo "fail missing-ranges $answer from=$first"
		failures=$((failures + 1))
	fi
	finish "$failures"
}

# Sourced by tools/bringup/ca.sh for the host map and the ssh helpers: the
# probe's own dispatch below runs only when this file is executed.
[ "${BASH_SOURCE[0]}" = "$0" ] || return 0

mode="${1:-}"
case "$mode" in
-h | --help)
	usage
	exit 0
	;;
hosts | rpc-nodes | archive-node | ca | hpx | explorer) ;;
*)
	usage >&2
	exit 2
	;;
esac

if [ "$#" -ne 1 ]; then
	usage >&2
	exit 2
fi

tools=(ssh timeout curl python3 openssl sha256sum)
[ "$mode" != explorer ] || tools=(curl python3 psql)
for tool in "${tools[@]}"; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "check-live: $tool is required" >&2
		exit 2
	fi
done

[ "$mode" = explorer ] || load_hosts
"check_${mode//-/_}"
