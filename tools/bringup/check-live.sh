#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'EOF'
usage: tools/bringup/check-live.sh hosts|rpc-nodes|archive-node|ca|hpx|explorer

Checks one system of the Paxeer X Network bring-up against its live answers.
Every subcommand reads the operator's private host map from the file named
by BRINGUP_HOSTS_FILE and never prints a value from it; explorer needs no
host map. A check of a Fly app reads the app's name from the app line of
its toml and runs its command inside a machine of the app through flyctl
ssh console, bounded by CHECK_LIVE_TIMEOUT.

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

ca        reads the internal CA under LAYERX_CA_DIR on this host and, through
          flyctl ssh console, the certificate of every service that
          tools/bringup/ca.sh services lists, inside a machine of the Fly app
          whose toml the service's row names (of the row's process group when
          it names one): LAYERX_FLY_TLS_DIR/<service>/cert.pem on the volume
          for a volume row, the guest path of the toml's [[files]] entry
          <PREFIX>_CERT for a row whose custody is the secret prefix PREFIX.
          One line each:
  ca      "pass ca ca expires_in=<days>d" when the CA certificate is readable
          and more than thirty days from expiry
  <service>
          "pass <service> app=<app> chain=ok san=<m>/<m> expires_in=<days>d"
          when the certificate chains to the CA, carries every SAN the
          service list declares with <app> read as the app's name, and is
          more than thirty days from expiry; "fail <service> toml=absent"
          when the toml or its app line is missing; "fail <service>
          app=<app> cert=unmounted" when the toml mounts no <PREFIX>_CERT;
          "fail <service> app=<app> cert=absent" when the machine holds none;
          otherwise "fail" with chain=untrusted, san=<n>/<m> missing=<names>
          or the expiry as observed.
          Exits 0 only when the CA and every certificate pass.

hpx       reads the hpx registry at CHECK_LIVE_HPX_ORIGIN, by default
          https://node.hyperpaxeer.com, and at CHECK_LIVE_HPX_APP_ORIGIN, by
          default https://<app>.fly.dev for the app of hpx/hosting/fly.toml,
          and prints one line per check; the app's lines carry the app- prefix:
  healthz    GET <origin>/healthz answers 200 with ok true, chain_id
             hyperpax_125-1 and a forty-hex source_revision
  checksums  GET <origin>/checksums.txt lists "<sha256>  <path>" lines and
             every listed path, fetched from the origin, hashes to its line;
             "pass checksums verified=<n>/<n>" or "fail checksums
             verified=<k>/<n> first=<path>" naming the first mismatch
  api-nodes  GET <origin>/api/nodes answers 200 with chain_id hyperpax_125-1
             and a nodes list whose length is count
  landing    GET <origin>/ answers 200 with the landing page of
             hpx/hosting/index.html
  served-by  the public origin's /healthz carries the Fly edge's
             fly-request-id header and equals the app's /healthz, so the name
             is served by the app and not by the registry on the edge host;
             "fail served-by app=<app> fly-request-id=<present|absent>
             healthz=<match|differ>" otherwise
          Exits 0 only when all eight checks pass.

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
  BRINGUP_HOSTS_FILE   private env file assigning EDGE_HOST, ARCHIVE_HOST,
                       VALIDATOR_HOSTS, RPC_HOSTS, HPX_HOST and
                       OLD_WALLET_HOST; each value is one ssh destination or,
                       for the plural roles, a space-separated list of them
  CHECK_LIVE_HPX_ORIGIN  origin of the hpx registry, default
                       https://node.hyperpaxeer.com
  CHECK_LIVE_HPX_APP_ORIGIN  origin of the hpx registry's Fly app, default
                       https://<app>.fly.dev
  CHECK_LIVE_EXPLORER_ORIGIN  origin of the explorer frontend, default
                       https://paxscan.io
  EXPLORER_DATABASE_URL, EXPLORER_LEGACY_DATABASE_URL,
  PAXSCAN_DATABASE_PUBLIC_URL  read-only connection strings of the explorer's
                       production database, its legacy database and the
                       paxscan copy source; never printed
  CHECK_LIVE_TIMEOUT   seconds per request, ssh or flyctl call, default 30
  LAYERX_CA_DIR        the internal CA directory on this host, default
                       /etc/layerx/ca
  LAYERX_FLY_TLS_DIR   the certificate directory root on the volume of a
                       Fly app, default /data/tls

Exits 1 when any check fails, 2 on a usage error, an unset BRINGUP_HOSTS_FILE
or a host map lacking a role.
EOF
}

timeout="${CHECK_LIVE_TIMEOUT:-30}"
ca_dir="${LAYERX_CA_DIR:-/etc/layerx/ca}"
fly_tls_dir="${LAYERX_FLY_TLS_DIR:-/data/tls}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

roles=(EDGE_HOST ARCHIVE_HOST VALIDATOR_HOSTS RPC_HOSTS HPX_HOST OLD_WALLET_HOST)

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

# fly_app <toml>: prints the Fly app name from the app line of the toml, a
# path relative to the repository root; status 1 when the file or its app
# line is missing.
fly_app() {
	local app
	app="$(sed -n 's/^app[[:space:]]*=[[:space:]]*"\([a-z0-9-]*\)"[[:space:]]*$/\1/p' "$repo_root/$1" 2>/dev/null | head -n 1)"
	[ -n "$app" ] && printf '%s' "$app"
}

# fly_ssh <app> <process group or -> <command>: runs the command under sh
# inside a machine of the app, of the process group unless it is -, through
# flyctl ssh console, bounded by CHECK_LIVE_TIMEOUT. stdin passes through and
# stdout is the result; stderr is dropped because flyctl names the machine's
# private address on it. The command carries no single quote.
fly_ssh() {
	local group=()
	[ "$2" = - ] || group=(--process-group "$2")
	timeout "$timeout" flyctl ssh console --quiet --app "$1" ${group[@]+"${group[@]}"} --command "sh -c '$3'" 2>/dev/null
}

# fly_guest_path <toml> <secret name>: prints the guest path of the toml's
# [[files]] entry that mounts the secret; status 1 when none does.
fly_guest_path() {
	python3 - "$repo_root/$1" "$2" 2>/dev/null <<'PY'
import sys
import tomllib

with open(sys.argv[1], "rb") as handle:
    doc = tomllib.load(handle)
for entry in doc.get("files", []):
    if entry.get("secret_name") == sys.argv[2] and entry.get("guest_path"):
        print(entry["guest_path"])
        sys.exit(0)
sys.exit(1)
PY
}

# app_sans <sans> <app>: the comma-separated SAN list a certificate of the
# app carries: the declared list ("-" for none) with <app> read as the app's
# name.
app_sans() {
	[ "$1" = - ] || printf '%s' "${1//<app>/$2}"
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

# The read-only inspection rpc-placement runs on a validator host: the
# ActiveState of the full-node unit paxd.service (never a validator unit) and
# the HTTP and HTTPS ports it listens on beyond loopback, one line, no address.
# shellcheck disable=SC2016
placement_cmd='s=$(systemctl is-active paxd.service 2>/dev/null); p=$(ss -Hltn 2>/dev/null | awk "{print \$4}" | grep -Ev "^(127\.|\[::1\]:|::1:)" | sed -n "s/.*:\(80\|443\)$/\1/p" | sort -u | paste -sd, -); echo "${s:-inactive} ${p:-none}"'

# check_rpc_placement: each public RPC name resolves to an RPC_HOSTS
# destination outside VALIDATOR_HOSTS and answers within ten blocks of the
# highest answer, and no validator host runs the full-node unit or listens on
# a public HTTP or HTTPS port. Destinations are named by role and index only.
check_rpc_placement() {
	local -a rpcs validators heads addrs
	local k j n name head lag at validator ok polls status reply state ports top=0 failures=0
	read -r -a rpcs <<<"$RPC_HOSTS"
	read -r -a validators <<<"$VALIDATOR_HOSTS"
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
		mapfile -t addrs < <(getent ahosts "$name.$rpc_domain" 2>/dev/null | awk '{print $1}' | sort -u)
		at=none
		validator=no
		for k in "${!rpcs[@]}"; do
			if printf '%s\n' "${addrs[@]}" | grep -qxF -- "${rpcs[$k]#*@}"; then
				at="RPC_HOSTS[$k]"
				break
			fi
		done
		for j in "${!validators[@]}"; do
			if printf '%s\n' "${addrs[@]}" | grep -qxF -- "${validators[$j]#*@}"; then
				validator=yes
				[ "$at" != none ] || at="VALIDATOR_HOSTS[$j]"
				break
			fi
		done
		head="${heads[n]}"
		ok=1
		if [ -n "$head" ]; then
			lag=$((top - head))
			[ "$lag" -le "$rpc_max_lag" ] || ok=0
		else
			head=none
			lag=none
			ok=0
		fi
		[ "$at" != none ] && [ "$validator" = no ] && [ "${at%%\[*}" = RPC_HOSTS ] || ok=0
		if [ "$ok" -eq 1 ]; then
			echo "pass $name host=$at validator=$validator head=$head lag=$lag"
		else
			echo "fail $name host=$at validator=$validator head=$head lag=$lag"
			failures=$((failures + 1))
		fi
	done
	for j in "${!validators[@]}"; do
		status=0
		reply="$(ssh_read "${validators[$j]}" "$placement_cmd")" || status=$?
		if [ "$status" -ne 0 ] || [ -z "$reply" ]; then
			echo "fail VALIDATOR_HOSTS[$j] ssh=$status"
			failures=$((failures + 1))
			continue
		fi
		read -r state ports <<<"$reply"
		if [ "$state" = inactive ] && [ "$ports" = none ]; then
			echo "pass VALIDATOR_HOSTS[$j] paxd=$state listen=$ports"
		else
			echo "fail VALIDATOR_HOSTS[$j] paxd=$state listen=$ports"
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
	local table service toml group custody eku sans app path cert chain
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
	while read -r service toml group custody _ eku sans; do
		if ! app="$(fly_app "$toml")"; then
			echo "fail $service toml=absent"
			failures=$((failures + 1))
			continue
		fi
		if [ "$custody" = volume ]; then
			path="$fly_tls_dir/$service/cert.pem"
		elif ! path="$(fly_guest_path "$toml" "${custody}_CERT")"; then
			echo "fail $service app=$app cert=unmounted"
			failures=$((failures + 1))
			continue
		fi
		if ! cert="$(fly_ssh "$app" "$group" "cat $path" </dev/null)" || [ -z "$cert" ]; then
			echo "fail $service app=$app cert=absent"
			failures=$((failures + 1))
			continue
		fi
		chain=ok
		openssl verify -CAfile "$ca_dir/ca.pem" <<<"$cert" >/dev/null 2>&1 || chain=untrusted
		want="$(app_sans "$sans" "$app")"
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
				missing="${missing:+$missing,}$san"
			fi
		done <<<"${want:+$want,}"
		days="$(days_left <<<"$cert")"
		line="$service app=$app chain=$chain san=$n/$m${missing:+ missing=$missing} expires_in=${days}d"
		if [ "$chain" = ok ] && [ -z "$missing" ] && [ "$days" -gt 30 ]; then
			echo "pass $line"
		else
			echo "fail $line"
			failures=$((failures + 1))
		fi
	done <<<"$table"
	finish "$failures"
}

# check_hpx: reads the hpx registry at its public origin and at the fly.dev
# name of the Fly app of hpx/hosting/fly.toml: at both, /healthz with the chain
# id and the source revision, checksums.txt verified against every served
# artifact it lists and /api/nodes answering; the landing page at the public
# origin; and the public origin served by the app: its /healthz carries the
# Fly edge's fly-request-id header and matches the app's /healthz.
check_hpx() {
	local origin="${CHECK_LIVE_HPX_ORIGIN:-https://node.hyperpaxeer.com}" failures=0
	local status body verdict manifest line sum path served total verified first
	local entry='^([0-9a-f]{64})  ([A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*)$'
	local app app_origin base prefix code name_health app_health fly_id
	origin="${origin%/}"
	app="$(fly_app hpx/hosting/fly.toml)" || app=absent
	app_origin="${CHECK_LIVE_HPX_APP_ORIGIN:-https://$app.fly.dev}"
	app_origin="${app_origin%/}"

	for prefix in "" app-; do
		base="$origin"
		[ -z "$prefix" ] || base="$app_origin"

		status=0
		body="$(curl -sS --max-time "$timeout" -w '\n%{http_code}' "$base/healthz" 2>&1)" || status=$?
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
		pass\ *) echo "pass ${prefix}healthz ${verdict#pass }" ;;
		*)
			echo "fail ${prefix}healthz ${verdict#fail }"
			failures=$((failures + 1))
			;;
		esac

		total=0
		verified=0
		first=""
		status=0
		manifest="$(curl -sS --max-time "$timeout" "$base/checksums.txt" 2>&1)" || status=$?
		if [ "$status" -ne 0 ]; then
			echo "fail ${prefix}checksums transport $(printf '%s' "$manifest" | tr '\n' ' ' | cut -c1-200)"
			failures=$((failures + 1))
		else
			while IFS= read -r line; do
				[ -n "$line" ] || continue
				total=$((total + 1))
				if [[ "$line" =~ $entry ]]; then
					sum="${BASH_REMATCH[1]}"
					path="${BASH_REMATCH[2]}"
					status=0
					served="$(curl -sS --max-time "$timeout" "$base/$path" 2>/dev/null | sha256sum | cut -d' ' -f1)" || status=$?
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
				echo "pass ${prefix}checksums verified=$verified/$total"
			else
				echo "fail ${prefix}checksums verified=$verified/$total first=${first:-empty-manifest}"
				failures=$((failures + 1))
			fi
		fi

		status=0
		body="$(curl -sS --max-time "$timeout" -w '\n%{http_code}' "$base/api/nodes" 2>&1)" || status=$?
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
		pass\ *) echo "pass ${prefix}api-nodes ${verdict#pass }" ;;
		*)
			echo "fail ${prefix}api-nodes ${verdict#fail }"
			failures=$((failures + 1))
			;;
		esac
	done

	status=0
	body="$(curl -sS --max-time "$timeout" -w '\n%{http_code}' "$origin/" 2>&1)" || status=$?
	code="${body##*$'\n'}"
	if [ "$status" -ne 0 ]; then
		echo "fail landing transport $(printf '%s' "$body" | tr '\n' ' ' | cut -c1-200)"
		failures=$((failures + 1))
	elif [ "$code" = 200 ] && grep -qF '<title>HPX — HyperPax Node Network</title>' <<<"$body"; then
		echo "pass landing http=200"
	else
		echo "fail landing http=$code"
		failures=$((failures + 1))
	fi

	# The public name is served by the app when its answer passed the Fly edge
	# (fly-request-id) and equals the app's own answer; the registry on the
	# edge host answers without that header.
	name_health="$(curl -sS --max-time "$timeout" -D - "$origin/healthz" 2>/dev/null)" || name_health=""
	fly_id=absent
	grep -qiE '^fly-request-id: *[^[:space:]]' <<<"$name_health" && fly_id=present
	name_health="${name_health#*$'\r\n\r\n'}"
	app_health="$(curl -sS --max-time "$timeout" "$app_origin/healthz" 2>/dev/null)" || app_health=""
	if [ "$fly_id" = present ] && [ -n "$app_health" ] && [ "$name_health" = "$app_health" ]; then
		echo "pass served-by app=$app fly-request-id=present healthz=match"
	else
		code=differ
		if [ -n "$app_health" ] && [ "$name_health" = "$app_health" ]; then
			code=match
		fi
		echo "fail served-by app=$app fly-request-id=$fly_id healthz=$code"
		failures=$((failures + 1))
	fi
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

# check_validators: from this host the attestor ports 8480 and 8481 of every
# VALIDATOR_HOSTS destination refuse the connection (a tcp reset, not a
# timeout); over ssh each attestor answers /health with 200 on loopback, and
# each validator host reaches every other one's attestor ports with 200. One
# line per check, naming hosts by their index only:
#   "pass VALIDATOR_HOSTS[k] remote-<port>=refused" or "=open|timeout|unreachable"
#   "pass VALIDATOR_HOSTS[k] loopback-<port> health=200"
#   "pass VALIDATOR_HOSTS[k] peer-VALIDATOR_HOSTS[j]-<port> health=200"
# with fail and the observed code (ssh=<exit> when the host did not answer).
check_validators() {
	local -a dests addrs
	local k j port err status code line target failures=0
	read -r -a dests <<<"$VALIDATOR_HOSTS"
	for k in "${!dests[@]}"; do
		addrs[k]="$(ssh -G -- "${dests[$k]}" 2>/dev/null | awk '$1 == "hostname" { print $2; exit }')"
	done
	for k in "${!dests[@]}"; do
		for port in 8480 8481; do
			status=0
			# A bare connect through bash's /dev/tcp, because only its error
			# text (the system's ECONNREFUSED message) tells a tcp reset from
			# an unreachable host; curl reports both as exit 7.
			# shellcheck disable=SC2016
			err="$(timeout "$timeout" bash -c 'exec 3<>"/dev/tcp/$1/$2"' - "${addrs[k]}" "$port" 2>&1)" || status=$?
			case "$status:$err" in
			0:*) line="fail VALIDATOR_HOSTS[$k] remote-$port=open" ;;
			124:*) line="fail VALIDATOR_HOSTS[$k] remote-$port=timeout" ;;
			*"Connection refused"*) line="pass VALIDATOR_HOSTS[$k] remote-$port=refused" ;;
			*) line="fail VALIDATOR_HOSTS[$k] remote-$port=unreachable" ;;
			esac
			echo "$line"
			[ "${line%% *}" = pass ] || failures=$((failures + 1))
		done
		for port in 8480 8481; do
			for j in "${!dests[@]}"; do
				target="${addrs[j]}"
				line="peer-VALIDATOR_HOSTS[$j]-$port"
				if [ "$j" -eq "$k" ]; then
					target=127.0.0.1
					line="loopback-$port"
				fi
				status=0
				code="$(ssh_read "${dests[$k]}" "curl -s -o /dev/null -w %{http_code} -m $timeout http://$target:$port/health")" || status=$?
				if [ "$code" = 200 ]; then
					echo "pass VALIDATOR_HOSTS[$k] $line health=200"
				elif [ "$status" -eq 255 ] || [ "$status" -eq 124 ]; then
					echo "fail VALIDATOR_HOSTS[$k] $line ssh=$status"
					failures=$((failures + 1))
				else
					echo "fail VALIDATOR_HOSTS[$k] $line health=${code:-none}"
					failures=$((failures + 1))
				fi
			done
		done
	done
	finish "$failures"
}

# check_identity: the identity app of platform/hosted/identity/fly.toml runs
# one started machine with a volume, holds no public IP, and answers
# readiness over TLS under the internal CA at its .internal name when asked
# from a machine of the human app of human/wallet/deploy/human.toml, which
# gets only the CA certificate on stdin.
check_identity() {
	local app from answer url code body attempt n_machines n_started n_mounts failures=0
	if ! app="$(fly_app platform/hosted/identity/fly.toml)"; then
		echo "fail identity toml=absent"
		finish 1
	fi
	answer="$(timeout "$timeout" flyctl machines list --app "$app" --json 2>/dev/null | python3 -c '
import json, sys
ms = json.load(sys.stdin)
started = [m for m in ms if m.get("state") == "started"]
mounts = [x.get("volume", "") for m in ms for x in (m.get("config") or {}).get("mounts") or []]
print(len(ms), len(started), len(mounts))
' 2>/dev/null)" || answer=""
	read -r n_machines n_started n_mounts <<<"${answer:-none none none}"
	if [ "$n_machines" = 1 ] && [ "$n_started" = 1 ] && [ "$n_mounts" = 1 ]; then
		echo "pass machines app=$app machines=1 started=1 volumes=1"
	else
		echo "fail machines app=$app machines=$n_machines started=$n_started volumes=$n_mounts"
		failures=$((failures + 1))
	fi
	answer="$(timeout "$timeout" flyctl ips list --app "$app" --json 2>/dev/null | python3 -c 'import json, sys; print(len(json.load(sys.stdin) or []))' 2>/dev/null)" || answer=none
	if [ "$answer" = 0 ]; then
		echo "pass public-ips app=$app count=0"
	else
		echo "fail public-ips app=$app count=${answer:-none}"
		failures=$((failures + 1))
	fi
	url="https://$app.internal:9443/readyz"
	if ! from="$(fly_app human/wallet/deploy/human.toml)"; then
		echo "fail readiness app=$app from=absent"
		finish $((failures + 1))
	fi
	if [ ! -r "$ca_dir/ca.pem" ]; then
		echo "fail readiness app=$app ca=absent"
		finish $((failures + 1))
	fi
	answer=""
	for attempt in 1 2; do
		answer="$(fly_ssh "$from" - "cat >/tmp/identity-ca.pem && curl -sS -m $timeout --cacert /tmp/identity-ca.pem -w \" %{http_code}\" $url; rm -f /tmp/identity-ca.pem" <"$ca_dir/ca.pem" | tail -n 1)" || true
		[ "${answer##* }" != 200 ] || break
		[ "$attempt" = 2 ] || sleep 10
	done
	code="${answer##* }"
	body="${answer% *}"
	if [ "$code" = 200 ] && [[ "$body" == *'"status":"ready"'* ]]; then
		echo "pass readiness app=$app from=$from url=$url http=200 status=ready"
	else
		echo "fail readiness app=$app from=$from url=$url http=${code:-none}"
		failures=$((failures + 1))
	fi
	finish "$failures"
}

# check_search_front: search.paxeer.network answers /healthz 200 over a
# certificate valid for the name, through the edge host that proxies it to the
# app of interop/deploy/search-front/fly.toml; the app runs
# at least two started machines in two regions; the upstream list a machine
# mounts equals the serving RPC names of tools/bringup/search-front.sh names
# and holds no validator host's apiN name; two requests from this address get
# the same X-Search-Node, one of those names. One line per check.
check_search_front() {
	local host=search.paxeer.network toml=interop/deploy/search-front/fly.toml
	local app status headers code machines listing mounted expected validators listed first second failures=0
	if ! command -v flyctl >/dev/null 2>&1; then
		echo "check-live: flyctl is required" >&2
		exit 2
	fi
	if ! app="$(fly_app "$toml")"; then
		echo "fail search-front toml=absent"
		finish 1
	fi

	status=0
	headers="$(curl -sS --max-time "$timeout" -D - -o /dev/null "https://$host/healthz" 2>&1)" || status=$?
	code="$(sed -n '1s/^HTTP\/[0-9.]* \([0-9]*\).*/\1/p' <<<"$headers")"
	if [ "$status" -ne 0 ]; then
		echo "fail healthz https://$host/healthz transport $(printf '%s' "$headers" | tr '\n' ' ' | cut -c1-160)"
		failures=$((failures + 1))
	elif [ "$code" = 200 ]; then
		echo "pass healthz https://$host/healthz http=200 tls=verified"
	else
		echo "fail healthz https://$host/healthz http=${code:-none} tls=verified"
		failures=$((failures + 1))
	fi

	machines="$(timeout "$timeout" flyctl machines list --app "$app" --json 2>/dev/null | python3 -c '
import json
import sys

try:
    doc = json.load(sys.stdin)
except ValueError:
    print("fail machines=unreadable")
    sys.exit(0)
started = [m for m in doc if m.get("state") == "started"]
regions = sorted({m.get("region", "") for m in started})
ok = len(started) >= 2 and len(regions) >= 2
print(("pass" if ok else "fail") + " machines started=%d regions=%s" % (len(started), ",".join(regions) or "none"))
')" || machines="fail machines=unreadable"
	echo "${machines%% *} ${machines#* } app=$app"
	[ "${machines%% *}" = pass ] || failures=$((failures + 1))

	if ! listing="$("$(dirname "${BASH_SOURCE[0]}")/search-front.sh" names 2>&1)"; then
		echo "fail upstreams names=unreadable $(printf '%s' "$listing" | tr '\n' ' ' | cut -c1-160)"
		failures=$((failures + 1))
		expected=""
	else
		expected="$(sed -n 's/^serve //p' <<<"$listing" | sort)"
		validators="$(sed -n 's/^validator //p' <<<"$listing" | sort)"
		mounted="$(fly_ssh "$app" - "cat /etc/nginx/search/upstreams.conf" </dev/null)" || mounted=""
		listed="$(sed -n 's/^[[:space:]]*\([0-9.]*%\|\*\)[[:space:]]\{1,\}\([a-z0-9.-]*\);.*/\2/p' <<<"$mounted" | sort)"
		first="$(comm -23 <(printf '%s\n' "$expected") <(printf '%s\n' "$listed") | grep -c . || true)"
		second="$(comm -13 <(printf '%s\n' "$expected") <(printf '%s\n' "$listed") | grep -c . || true)"
		code="$(comm -12 <(printf '%s\n' "$validators") <(printf '%s\n' "$listed") | grep -c . || true)"
		if [ -z "$mounted" ]; then
			echo "fail upstreams app=$app list=unreadable"
			failures=$((failures + 1))
		elif [ "$first" -eq 0 ] && [ "$second" -eq 0 ] && [ "$code" -eq 0 ]; then
			echo "pass upstreams listed=$(grep -c . <<<"$listed")/$(grep -c . <<<"$expected") validator=0"
		else
			echo "fail upstreams missing=$first extra=$second validator=$code"
			failures=$((failures + 1))
		fi
	fi

	first="$(curl -sS --max-time "$timeout" -D - -o /dev/null "https://$host/xweb/health" 2>/dev/null | sed -n 's/^x-search-node:[[:space:]]*//Ip' | tr -d '\r')" || first=""
	second="$(curl -sS --max-time "$timeout" -D - -o /dev/null "https://$host/xweb/health" 2>/dev/null | sed -n 's/^x-search-node:[[:space:]]*//Ip' | tr -d '\r')" || second=""
	if [ -n "$first" ] && [ "$first" = "$second" ] && grep -qxF "$first" <<<"$expected"; then
		echo "pass sticky node=$first requests=2"
	else
		echo "fail sticky first=${first:-none} second=${second:-none}"
		failures=$((failures + 1))
	fi
	finish "$failures"
}

# check_edge: the names tools/bringup/edge.sh registered on the edge host, read
# with its rendered sites over ssh from EDGE_HOST. The manifest names at least
# one name; no rendered site names a validator host; every name resolves only
# to EDGE_HOST's addresses; an http name answers the readiness route of its
# app's toml (the first http check path) over TLS verified for the name with
# 200, the Fly edge's fly-request-id header and the body the app's fly.dev name
# answers; a stream name presents through the edge the certificate its app
# presents at its fly.dev name for that SNI. One line per check, no address.
# shellcheck disable=SC2016
edge_cmd='cat /etc/nginx/edge/manifest 2>/dev/null; echo @@sites; for f in /etc/nginx/sites-available/* /etc/nginx/edge/stream/*.conf; do head -n 1 "$f" 2>/dev/null | grep -q "^# rendered by tools/bringup/edge.sh" && cat "$f"; done; true'
edge_route_py='
import sys
import tomllib

with open(sys.argv[1], "rb") as handle:
    doc = tomllib.load(handle)
for check in (doc.get("http_service") or {}).get("checks") or []:
    if check.get("path"):
        print(check["path"])
        break
'
check_edge() {
	local reply status=0 manifest sites edge_addrs addrs v named=0 name mode app port at toml route
	local out headers code fly_id body app_body tls first fp_edge fp_app failures=0
	reply="$(ssh_read "$EDGE_HOST" "$edge_cmd")" || status=$?
	if [ "$status" -ne 0 ] || [[ "$reply" != *@@sites* ]]; then
		echo "fail EDGE_HOST manifest ssh=$status"
		finish 1
	fi
	manifest="$(grep -E '^[a-z0-9.-]+ (http|stream) [a-z0-9-]+ [0-9]+$' <<<"${reply%%@@sites*}" || true)"
	sites="${reply#*@@sites}"
	if [ -z "$manifest" ]; then
		echo "fail manifest names=0"
		finish 1
	fi
	echo "pass manifest names=$(grep -c . <<<"$manifest")"
	for v in $VALIDATOR_HOSTS; do
		if grep -qF -- "${v#*@}" <<<"$sites"; then
			named=$((named + 1))
		fi
	done
	if [ "$named" -eq 0 ]; then
		echo "pass sites validator=0"
	else
		echo "fail sites validator=$named"
		failures=$((failures + 1))
	fi
	edge_addrs="$(getent ahosts "${EDGE_HOST#*@}" 2>/dev/null | awk '{print $1}' | sort -u)"
	while read -r name mode app port; do
		addrs="$(getent ahosts "$name" 2>/dev/null | awk '{print $1}' | sort -u)"
		at=no
		if [ -n "$addrs" ] && [ -n "$edge_addrs" ] && [ -z "$(comm -23 <(printf '%s\n' "$addrs") <(printf '%s\n' "$edge_addrs"))" ]; then
			at=yes
		fi
		if [ "$mode" = stream ]; then
			first="$(head -n 1 <<<"$addrs")"
			fp_edge=""
			[ -z "$first" ] || fp_edge="$(timeout "$timeout" openssl s_client -connect "$first:$port" -servername "$name" </dev/null 2>/dev/null | openssl x509 -noout -fingerprint -sha256 2>/dev/null | cut -d= -f2)" || fp_edge=""
			fp_app="$(timeout "$timeout" openssl s_client -connect "$app.fly.dev:$port" -servername "$name" </dev/null 2>/dev/null | openssl x509 -noout -fingerprint -sha256 2>/dev/null | cut -d= -f2)" || fp_app=""
			out="$name mode=stream app=$app port=$port edge=$at presented=${fp_edge:-none} app-presents=${fp_app:-none}"
			if [ "$at" = yes ] && [ -n "$fp_edge" ] && [ "$fp_edge" = "$fp_app" ]; then
				echo "pass $out"
			else
				echo "fail $out"
				failures=$((failures + 1))
			fi
			continue
		fi
		route=""
		toml="$(git -C "$repo_root" grep -l -E "^app = \"$app\"$" -- '*.toml' 2>/dev/null | head -n 1)" || toml=""
		[ -z "$toml" ] || route="$(python3 -c "$edge_route_py" "$repo_root/$toml" 2>/dev/null)" || route=""
		if [ -z "$route" ]; then
			echo "fail $name mode=http app=$app edge=$at route=unknown"
			failures=$((failures + 1))
			continue
		fi
		status=0
		out="$(curl -sS --max-time "$timeout" -D - "https://$name$route" 2>&1)" || status=$?
		tls=verified
		code=none
		fly_id=absent
		body=""
		if [ "$status" -ne 0 ]; then
			tls="curl-$status"
		else
			headers="${out%%$'\r\n\r\n'*}"
			body="${out#*$'\r\n\r\n'}"
			code="$(sed -n '1s/^HTTP\/[0-9.]* \([0-9]*\).*/\1/p' <<<"$headers")"
			if grep -qiE '^fly-request-id: *[^[:space:]]' <<<"$headers"; then
				fly_id=present
			fi
		fi
		app_body="$(curl -sS --max-time "$timeout" -D - "https://$app.fly.dev$route" 2>/dev/null)" || app_body=""
		app_body="${app_body#*$'\r\n\r\n'}"
		v=differ
		if [ -n "$body" ] && [ "$body" = "$app_body" ]; then
			v=match
		fi
		out="$name mode=http app=$app edge=$at tls=$tls route=$route http=${code:-none} fly-request-id=$fly_id body=$v"
		if [ "$at" = yes ] && [ "$tls" = verified ] && [ "$code" = 200 ] && [ "$fly_id" = present ] && [ "$v" = match ]; then
			echo "pass $out"
		else
			echo "fail $out"
			failures=$((failures + 1))
		fi
	done <<<"$manifest"
	finish "$failures"
}

# check_ci: the CI controller app of tools/flyci/controller/fly.toml runs one
# started machine, the repository variable CI_LINUX_RUNNER is the runner label
# the controller serves, and runner-canary.yml dispatched on the default
# branch completes with conclusion success and a successful job on a runner
# whose name starts with fly-. A run still unfinished after the wait is
# cancelled. One line per check.
check_ci() {
	local toml=tools/flyci/controller/fly.toml workflow=runner-canary.yml wait=1500
	local app label answer n_machines n_started variable branch since run attempt status conclusion runner jobs failures=0
	if ! command -v gh >/dev/null 2>&1 || ! command -v flyctl >/dev/null 2>&1; then
		echo "check-live: gh and flyctl are required" >&2
		exit 2
	fi
	if ! app="$(fly_app "$toml")"; then
		echo "fail ci toml=absent"
		finish 1
	fi
	label="$(sed -n 's/^[[:space:]]*RUNNER_LABELS[[:space:]]*=[[:space:]]*"\([^",]*\).*"[[:space:]]*$/\1/p' "$repo_root/$toml" | head -n 1)"
	answer="$(timeout "$timeout" flyctl machines list --app "$app" --json 2>/dev/null | python3 -c '
import json, sys
ms = json.load(sys.stdin)
print(len(ms), len([m for m in ms if m.get("state") == "started"]))
' 2>/dev/null)" || answer=""
	read -r n_machines n_started <<<"${answer:-none none}"
	if [ "$n_started" = 1 ]; then
		echo "pass controller app=$app machines=$n_machines started=1"
	else
		echo "fail controller app=$app machines=$n_machines started=$n_started"
		failures=$((failures + 1))
	fi
	variable="$(cd "$repo_root" && timeout "$timeout" gh variable get CI_LINUX_RUNNER 2>/dev/null)" || variable=""
	if [ -n "$label" ] && [ "$variable" = "$label" ]; then
		echo "pass variable CI_LINUX_RUNNER=$variable"
	else
		echo "fail variable CI_LINUX_RUNNER=${variable:-unset} want=${label:-none}"
		failures=$((failures + 1))
	fi
	branch="$(cd "$repo_root" && timeout "$timeout" gh repo view --json defaultBranchRef --jq .defaultBranchRef.name 2>/dev/null)" || branch=""
	if [ -z "$branch" ]; then
		echo "fail canary workflow=$workflow branch=none"
		finish $((failures + 1))
	fi
	since="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	if ! (cd "$repo_root" && timeout "$timeout" gh workflow run "$workflow" --ref "$branch" >/dev/null 2>&1); then
		echo "fail canary workflow=$workflow branch=$branch dispatch=refused"
		finish $((failures + 1))
	fi
	run=""
	for attempt in 1 2 3 4 5 6 7 8 9 10 11 12; do
		sleep 5
		run="$(cd "$repo_root" && timeout "$timeout" gh run list --workflow "$workflow" --branch "$branch" --event workflow_dispatch --limit 10 --json databaseId,createdAt --jq "[.[] | select(.createdAt >= \"$since\")] | last | .databaseId // empty" 2>/dev/null)" || run=""
		[ -z "$run" ] || break
	done
	if [ -z "$run" ]; then
		echo "fail canary workflow=$workflow branch=$branch run=none"
		finish $((failures + 1))
	fi
	(cd "$repo_root" && timeout "$wait" gh run watch "$run" --interval 15 >/dev/null 2>&1) || true
	answer="$(cd "$repo_root" && timeout "$timeout" gh run view "$run" --json status,conclusion,jobs --jq '[.status, (.conclusion | if . == "" then "none" else . end), ([.jobs[] | select((.runnerName // "") | startswith("fly-")) | select(.conclusion == "success") | .runnerName] | first // "none"), ([.jobs[] | (.runnerName // "") | if . == "" then "none" else gsub(" "; "_") end] | join(","))] | join(" ")' 2>/dev/null)" || answer=""
	read -r status conclusion runner jobs <<<"${answer:-none none none none}"
	if [ "$status" != completed ]; then
		(cd "$repo_root" && timeout "$timeout" gh run cancel "$run" >/dev/null 2>&1) || true
	fi
	if [ "$status" = completed ] && [ "$conclusion" = success ] && [ "$runner" != none ]; then
		echo "pass canary workflow=$workflow branch=$branch run=$run conclusion=success runner=$runner"
	else
		echo "fail canary workflow=$workflow branch=$branch run=$run status=$status conclusion=$conclusion runners=${jobs:-none}"
		failures=$((failures + 1))
	fi
	finish "$failures"
}

# check_human_session: the wallet check-live harness tools/wallet/check-live.test.sh
# passes, then the wallet feature's human-session gate plans the golden intent
# at https://api-hull.paxeer.network from the wallet origin
# https://paxportwallet.com under the wallet identity assertion of
# CHECK_LIVE_HUMAN_ASSERTION, with CHECK_LIVE_HUMAN_ASSET and
# CHECK_LIVE_HUMAN_DESTINATION passed through; its check lines are printed as
# they come and any failure of either fails the subcommand.
check_human_session() {
	local output status=0 failures=0
	output="$("$repo_root/tools/wallet/check-live.test.sh" 2>&1)" || status=$?
	if [ "$status" -eq 0 ]; then
		echo "pass harness tools/wallet/check-live.test.sh"
	else
		echo "fail harness tools/wallet/check-live.test.sh exit=$status first=$(grep -m 1 '^FAIL ' <<<"$output" | cut -c1-160 || echo none)"
		failures=$((failures + 1))
	fi
	status=0
	CHECK_LIVE_HUMAN_BASE=https://api-hull.paxeer.network CHECK_LIVE_HUMAN_ORIGIN=https://paxportwallet.com \
		"$repo_root/tools/wallet/check-live.sh" human-session || status=$?
	[ "$status" -eq 0 ] || failures=$((failures + 1))
	finish "$failures"
}

# Sourced by tools/bringup/ca.sh for the Fly helpers and the CA settings: the
# probe's own dispatch below runs only when this file is executed.
[ "${BASH_SOURCE[0]}" = "$0" ] || return 0

mode="${1:-}"
case "$mode" in
-h | --help)
	usage
	exit 0
	;;
hosts | rpc-nodes | archive-node | ca | hpx | explorer) ;;
rpc-placement) ;;
validators) ;;
identity) ;;
search-front) ;;
edge) ;;
ci) ;;
human-session) ;;
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
[ "$mode" != ca ] || tools+=(flyctl)
for tool in "${tools[@]}"; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "check-live: $tool is required" >&2
		exit 2
	fi
done

[ "$mode" = explorer ] || load_hosts
"check_${mode//-/_}"
