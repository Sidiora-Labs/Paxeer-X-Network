#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'EOF'
usage: tools/bringup/check-live.sh hosts|rpc-nodes|archive-node

Checks one system of the Paxeer X Network bring-up against its live answers.
Every subcommand reads the operator's private host map from the file named
by BRINGUP_HOSTS_FILE and never prints a value from it.

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

Environment:
  BRINGUP_HOSTS_FILE   private env file assigning EDGE_HOST, KERNEL_HOST,
                       PLATFORM_HOST, EXPLORER_HOST, ARCHIVE_HOST,
                       VALIDATOR_HOSTS, RPC_HOSTS and HPX_HOST; each value is
                       one ssh destination or, for the plural roles, a
                       space-separated list of them
  CHECK_LIVE_TIMEOUT   seconds per request, default 30

Exits 1 when any check fails, 2 on a usage error, an unset BRINGUP_HOSTS_FILE
or a host map lacking a role.
EOF
}

mode="${1:-}"
case "$mode" in
-h | --help)
	usage
	exit 0
	;;
hosts | rpc-nodes | archive-node) ;;
*)
	usage >&2
	exit 2
	;;
esac

if [ "$#" -ne 1 ]; then
	usage >&2
	exit 2
fi

timeout="${CHECK_LIVE_TIMEOUT:-30}"

for tool in ssh timeout curl python3; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "check-live: $tool is required" >&2
		exit 2
	fi
done

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

rpc_domain="mainnet-beta.paxeer.network"
rpc_names=16
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
	for n in $(seq 1 "$rpc_names"); do
		rpc_head "api$n" >"$polls/$n" 2>/dev/null &
	done
	wait
	for n in $(seq 1 "$rpc_names"); do
		heads[n]="$(cat "$polls/$n")"
		if [ -n "${heads[n]}" ] && [ "${heads[n]}" -gt "$top" ]; then
			top="${heads[n]}"
		fi
	done
	for n in $(seq 1 "$rpc_names"); do
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

load_hosts
"check_${mode//-/_}"
