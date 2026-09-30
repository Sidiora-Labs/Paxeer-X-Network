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

bridge    runs bridge/deploy/checklist.sh for every chain under bridge/evm/chains
          and bridge/solana/chains against PAXEER_BRIDGE_RECORDS_DIR/<chain>.json,
          then reads a machine of the app of interop/deploy/bridge-relayer/fly.toml
          through flyctl ssh console, one line per check:
  checklist  "pass checklist chain=<chain> exit=0", or "fail checklist
             chain=<chain> record=absent" or "fail checklist chain=<chain>
             exit=<n> <last output line, URLs as <url>>"
  processes  "pass processes app=<app> relayer=running signer=running" when
             layerx-bridge-relayer and layerx-mirror-signer both run
  bridge-in  "pass bridge-in app=<app> item=in:<chain id>:<tx>:<log>
             outcome=included" when the journal on the volume records a
             deposit as observed and as completed by the relayer's own
             included bridgeIn transaction; "fail bridge-in app=<app>
             journal=absent|journal=present included=none" otherwise
          Exits 0 only when every check passes; 2 when
          PAXEER_BRIDGE_RECORDS_DIR is unset. The checklist's own inputs
          (each chain's RPC variable, PAXEER_BRIDGE_PAXEER_RPC_URL,
          PAXEER_BRIDGE_GOVERNANCE_AUTHORITY) pass through the environment.

wallet    finds the public wallet endpoint name: the wallet_endpoint value of
          [decision.public_names] in spec/paxeer-x-bringup/spec.kvx when it is
          a bare hostname, otherwise the host of NEXT_PUBLIC_PAXEER_WALLET_API
          that railway variable list --service paxport --kv reads from the
          Railway project linked at the checkout root. Then runs the wallet
          gates of tools/wallet/check-live.sh and prints their check lines:
  endpoint   "pass endpoint name=<host> source=spec|railway", or "fail
             endpoint source=railway api=unset|unusable" when the variable is
             missing or not an https URL, in which case cutover is skipped
  cutover    the served_by and readiness lines of its cutover mode with
             CHECK_LIVE_CUTOVER_HOST set to that name
  gateway    the readiness and me lines of its gateway mode with
             CHECK_LIVE_GATEWAY_BASE https://<app>.fly.dev for the app of
             human/wallet/deploy/gateway.toml
  machines   "pass machines started=<n> regions=<list> app=<app>" when the
             app runs at least two started machines in at least two regions
          Exits 0 only when every check passes; a failing gate counts once.

Environment:
  BRINGUP_HOSTS_FILE   private env file assigning EDGE_HOST, ARCHIVE_HOST,
                       VALIDATOR_HOSTS, RPC_HOSTS and OLD_WALLET_HOST;
                       each value is one ssh destination or,
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
  CHECK_LIVE_GATEWAY_TOKEN  access token of a provisioned test identity of
                       the wallet gateway, required by wallet; never printed
  CHECK_LIVE_TIMEOUT   seconds per request, ssh or flyctl call, default 30
  LAYERX_CA_DIR        the internal CA directory on this host, default
                       /etc/layerx/ca
  LAYERX_FLY_TLS_DIR   the certificate directory root on the volume of a
                       Fly app, default /data/tls
  CHECK_LIVE_GAS_ACCOUNT_KEYSTORE, CHECK_LIVE_GAS_ACCOUNT_PASSWORD_FILE
                       the keystore and password file of the gas check's
                       account, already delegated to the paymaster and
                       holding SID; the sponsored batch spends SID from it;
                       never printed
  CHECK_LIVE_GAS_MAX_TOKEN_AMOUNT  the most SID base units the gas check's
                       quote may charge
  CHECK_LIVE_GAS_ORIGIN  origin of the gas station, default
                       https://chain.paxeer.network
  CHECK_LIVE_GAS_RPC   JSON-RPC URL the gas check reads the chain from,
                       default https://api-mainnet-beta.paxeer.network/rpc
  CHECK_LIVE_GAS_RECEIPT_ATTEMPTS  receipt polls five seconds apart, default 36

Exits 1 when any check fails, 2 on a usage error, an unset BRINGUP_HOSTS_FILE
or a host map lacking a role.
EOF
}

timeout="${CHECK_LIVE_TIMEOUT:-30}"
ca_dir="${LAYERX_CA_DIR:-/etc/layerx/ca}"
fly_tls_dir="${LAYERX_FLY_TLS_DIR:-/data/tls}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

roles=(EDGE_HOST ARCHIVE_HOST VALIDATOR_HOSTS RPC_HOSTS OLD_WALLET_HOST)

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

# fly_regions <app>: prints "pass machines started=<n> regions=<list>
# app=<app>" when the app runs at least two started machines in at least two
# regions, the fail line with the observed values (or machines=unreadable)
# and status 1 otherwise.
fly_regions() {
	local machines
	machines="$(timeout "$timeout" flyctl machines list --app "$1" --json 2>/dev/null | python3 -c '
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
	echo "${machines%% *} ${machines#* } app=$1"
	[ "${machines%% *}" = pass ]
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
	local app status headers code listing mounted expected validators listed first second failures=0
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

	fly_regions "$app" || failures=$((failures + 1))

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

# check_paxeer_boundary: inside the machine of the kernel app of
# human/wallet/deploy/human.toml, the layerx-paxeer-boundary processes are
# found with their listen and node ports, the loopback socat hops to port 443
# with the RPC name each dials, and every boundary is asked for eth_chainId and
# eth_blockNumber over TLS verified against this host's internal CA for the
# name localhost, at once with the sixteen public RPC names, so the ten-block
# window is not eaten between the answers. Two boundaries must answer 0x7d
# within ten blocks of the highest public answer, each through its own hop,
# and the two hops must name two different serving RPC names of
# tools/bringup/search-front.sh names, neither a validator host's. One line
# per check, the boundaries numbered by listen port.
check_paxeer_boundary() {
	# shellcheck disable=SC2016
	local script='d=$(mktemp -d)
ca="$d/ca.pem"
printf "%s\n" "$CA" >"$ca"
ask() {
	curl -sS -m "$T" --cacert "$ca" -H "content-type: application/json" -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"$2\",\"params\":[]}" "https://localhost:$1/" >"$d/$3.body" 2>/dev/null
	echo "$?" >"$d/$3.exit"
}
pub() {
	curl -sS -m "$T" -H "content-type: application/json" -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_blockNumber\",\"params\":[]}" "https://api$1.mainnet-beta.paxeer.network/" >"$d/p$1.body" 2>/dev/null
	echo "$?" >"$d/p$1.exit"
}
result() {
	sed -n "s/.*\"result\":\"\(0x[0-9a-fA-F]*\)\".*/\1/p" "$d/$1.body" 2>/dev/null | head -n 1
}
for p in /proc/[0-9]*; do
	c=$(tr "\000" " " <"$p/cmdline" 2>/dev/null) || continue
	case "$c" in
	*/layerx-paxeer-boundary*)
		e=$(tr "\000" "\n" <"$p/environ" 2>/dev/null) || continue
		l=$(printf "%s\n" "$e" | sed -n "s/^LAYERX_PAXEER_BOUNDARY_LISTEN=.*:\([0-9]*\)$/\1/p")
		u=$(printf "%s\n" "$e" | sed -n "s/^LAYERX_PAXEER_NODE_URL=http:\/\/[^/]*:\([0-9]*\).*$/\1/p")
		echo "boundary ${l:-none} ${u:-none}"
		;;
	socat\ *TCP4-LISTEN:*OPENSSL:*:443,*)
		l=$(printf "%s\n" "$c" | sed -n "s/.*TCP4-LISTEN:\([0-9]*\),.*/\1/p")
		n=$(printf "%s\n" "$c" | sed -n "s/.*OPENSSL:\([a-z0-9.-]*\):443,.*/\1/p")
		echo "hop ${l:-none} ${n:-none}"
		;;
	esac
done | sort -u >"$d/found"
cat "$d/found"
for l in $(sed -n "s/^boundary \([0-9][0-9]*\) .*/\1/p" "$d/found"); do
	ask "$l" eth_chainId "c$l" &
	ask "$l" eth_blockNumber "h$l" &
done
n=1
while [ "$n" -le 16 ]; do
	pub "$n" &
	n=$((n + 1))
done
wait
for l in $(sed -n "s/^boundary \([0-9][0-9]*\) .*/\1/p" "$d/found"); do
	echo "chain $l $(result "c$l") $(cat "$d/c$l.exit")"
	echo "head $l $(result "h$l")"
done
n=1
while [ "$n" -le 16 ]; do
	echo "public $n $(result "p$n")"
	n=$((n + 1))
done
rm -rf "$d"'
	local toml=human/wallet/deploy/human.toml
	local app reply listing serving validators top=0 n value k port node hop chain code head lag tls ok
	local -a ports=() nodes=() hops=()
	local failures=0
	if ! command -v flyctl >/dev/null 2>&1; then
		echo "check-live: flyctl is required" >&2
		exit 2
	fi
	if ! app="$(fly_app "$toml")"; then
		echo "fail paxeer-boundary toml=absent"
		finish 1
	fi
	if [ ! -r "$ca_dir/ca.pem" ]; then
		echo "fail paxeer-boundary app=$app ca=absent"
		finish 1
	fi
	reply="$(fly_ssh "$app" - "sh -s" < <(printf "T=%s\nCA='%s'\n%s\n" "$(((timeout + 1) / 2))" "$(cat "$ca_dir/ca.pem")" "$script"))" || reply=""
	if [ -z "$reply" ]; then
		echo "fail paxeer-boundary app=$app machine=unreadable"
		finish 1
	fi
	while read -r _ n value; do
		value="$(hex_to_dec "${value:-}")"
		if [ -n "$value" ] && [ "$value" -gt "$top" ]; then
			top="$value"
		fi
	done < <(grep '^public ' <<<"$reply")
	mapfile -t ports < <(sed -n 's/^boundary \([0-9][0-9]*\) .*/\1/p' <<<"$reply" | sort -n)
	if [ "${#ports[@]}" -eq 2 ]; then
		echo "pass boundaries app=$app count=2"
	else
		echo "fail boundaries app=$app count=${#ports[@]}"
		failures=$((failures + 1))
	fi
	for k in "${!ports[@]}"; do
		port="${ports[$k]}"
		node="$(sed -n "s/^boundary $port \([0-9a-z]*\)$/\1/p" <<<"$reply" | head -n 1)"
		hop="$(sed -n "s/^hop $node \([a-z0-9.-]*\)$/\1/p" <<<"$reply" | head -n 1)"
		read -r _ _ chain code <<<"$(grep "^chain $port " <<<"$reply" | head -n 1)" || true
		read -r _ _ head <<<"$(grep "^head $port " <<<"$reply" | head -n 1)" || true
		if [ "${code:-none}" = 0 ]; then
			tls=verified
		else
			tls="curl-${code:-none}"
		fi
		head="$(hex_to_dec "${head:-}")"
		lag=none
		ok=1
		if [ -n "$head" ] && [ "$top" -gt 0 ]; then
			lag=$((top - head))
			[ "$lag" -le "$rpc_max_lag" ] || ok=0
		else
			ok=0
		fi
		[ "$tls" = verified ] && [ "${chain:-}" = 0x7d ] && [ -n "$hop" ] || ok=0
		nodes[k]="$node"
		hops[k]="${hop:-none}"
		value="boundary-$((k + 1)) port=$port tls=$tls chain_id=${chain:-none} node_port=$node hop=${hop:-none} head=${head:-none} top=$top lag=$lag"
		if [ "$ok" -eq 1 ]; then
			echo "pass $value"
		else
			echo "fail $value"
			failures=$((failures + 1))
		fi
	done
	if ! listing="$("$(dirname "${BASH_SOURCE[0]}")/search-front.sh" names 2>&1)"; then
		echo "fail hops names=unreadable $(printf '%s' "$listing" | tr '\n' ' ' | cut -c1-160)"
		finish $((failures + 1))
	fi
	serving=0
	validators=0
	for k in "${!hops[@]}"; do
		grep -qxF "serve ${hops[$k]}" <<<"$listing" && serving=$((serving + 1))
		grep -qxF "validator ${hops[$k]}" <<<"$listing" && validators=$((validators + 1))
	done
	value="hops first=${hops[0]:-none} second=${hops[1]:-none} serving=$serving/2 validator=$validators"
	if [ "${#hops[@]}" -eq 2 ] && [ "${hops[0]}" != "${hops[1]}" ] && [ "${nodes[0]}" != "${nodes[1]}" ] && [ "$serving" -eq 2 ] && [ "$validators" -eq 0 ]; then
		echo "pass $value distinct=yes"
	else
		echo "fail $value distinct=$([ "${#hops[@]}" -eq 2 ] && [ "${hops[0]}" != "${hops[1]}" ] && [ "${nodes[0]}" != "${nodes[1]}" ] && echo yes || echo no)"
		failures=$((failures + 1))
	fi
	finish "$failures"
}

# check_agent_public: the agentd mTLS surface of the kernel app of
# human/wallet/deploy/human.toml at https://machine.paxeer.network:9454, the
# name the edge host passes through unchanged to the app's dedicated IPv4.
# The app holds a dedicated IPv4; inside a machine of the app, the running
# layerx-agentd's program bearer and probe program are read from its own
# environment, and platform/hosted/agentd/probe.sh runs against the public
# name with the agentd-client identity tools/bringup/ca.sh left on the
# volume and the internal CA root beside it; then one program.discover read
# envelope posted to /rpc must answer 200 with request_id, value and
# verification_status. The identity and the bearer never leave the machine.
# One line per check.
# shellcheck disable=SC2016
agent_public_head='umask 077
w=$(mktemp -d) || exit 1
trap "rm -rf $w" EXIT
cat >"$w/probe.sh" <<"LXPROBE"'
# shellcheck disable=SC2016
agent_public_tail='pid=
for d in /proc/[0-9]*; do
	case "$(readlink "$d/exe" 2>/dev/null)" in
	*/layerx-agentd)
		pid=$d
		break
		;;
	esac
done
if [ -z "$pid" ]; then
	echo "@@agentd none"
	exit 0
fi
echo "@@agentd found"
env_of() { tr "\000" "\n" <"$pid/environ" | sed -n "s/^$1=//p" | head -n 1; }
env_of LAYERX_AGENT_PROGRAM_BEARER_TOKEN >"$w/bearer"
program=$(env_of LAYERX_AGENT_PROGRAM_PROBE_ID)
status=0
sh "$w/probe.sh" --url "$url" --ca "$tls/ca.pem" --client-cert "$tls/cert.pem" --client-key "$tls/key.pem" --bearer-file "$w/bearer" >"$w/probe.out" 2>&1 </dev/null || status=$?
echo "@@probe $status"
head -n 3 "$w/probe.out"
printf "header = \"Authorization: Bearer %s\"\n" "$(cat "$w/bearer")" >"$w/bearer.conf"
printf "{\"operation\":\"program.discover\",\"request\":{\"program_id\":\"%s\",\"requested_verification_level\":\"sequencer-signed\"}}" "$program" >"$w/rpc.json"
status=0
code=$(curl --silent --show-error --max-time "$limit" --output "$w/rpc.out" --write-out "%{http_code}" --cacert "$tls/ca.pem" --cert "$tls/cert.pem" --key "$tls/key.pem" --config "$w/bearer.conf" -H "Content-Type: application/json" --data-binary "@$w/rpc.json" "$url/rpc" 2>"$w/rpc.err" </dev/null) || status=$?
echo "@@rpc $status ${code:-none}"
if [ "$status" -eq 0 ]; then head -c 65536 "$w/rpc.out"; else head -n 1 "$w/rpc.err"; fi
echo'
agent_public_py='
import json
import sys

try:
    doc = json.loads(sys.stdin.read())
except ValueError:
    doc = None
if not isinstance(doc, dict):
    print("body=unreadable")
    sys.exit(1)
request_id = doc.get("request_id")
status = doc.get("verification_status")
state = status.get("state") if isinstance(status, dict) else None
ok = isinstance(request_id, str) and request_id != "" and doc.get("value") is not None and isinstance(state, str) and state != ""
print("request_id=%s value=%s verification_status=%s" % (
    "present" if isinstance(request_id, str) and request_id else "absent",
    "present" if doc.get("value") is not None else "absent",
    state if isinstance(state, str) and state else "absent"))
sys.exit(0 if ok else 1)
'
check_agent_public() {
	local toml=human/wallet/deploy/human.toml url=https://machine.paxeer.network:9454
	local app answer reply agentd probe_status probe_out rpc_line rpc_status rpc_code rpc_body shape status failures=0
	if ! command -v flyctl >/dev/null 2>&1; then
		echo "check-live: flyctl is required" >&2
		exit 2
	fi
	if ! app="$(fly_app "$toml")"; then
		echo "fail agent-public toml=absent"
		finish 1
	fi
	answer="$(timeout "$timeout" flyctl ips list --app "$app" --json 2>/dev/null | python3 -c 'import json, sys; print(len([i for i in json.load(sys.stdin) or [] if i.get("Type") == "v4"]))' 2>/dev/null)" || answer=""
	if [ -n "$answer" ] && [ "$answer" -ge 1 ]; then
		echo "pass ipv4 app=$app dedicated=$answer"
	else
		echo "fail ipv4 app=$app dedicated=${answer:-unreadable}"
		failures=$((failures + 1))
	fi
	reply="$({
		printf '%s\n' "$agent_public_head"
		cat "$repo_root/platform/hosted/agentd/probe.sh"
		printf '%s\n' LXPROBE "$agent_public_tail"
	} | fly_ssh "$app" - "tls=$fly_tls_dir/agentd-client url=$url limit=$timeout sh -s")" || reply=""
	agentd="$(sed -n 's/^@@agentd //p' <<<"$reply" | head -n 1)"
	if [ "$agentd" != found ]; then
		echo "fail agentd app=$app process=${agentd:-unreachable}"
		finish $((failures + 1))
	fi
	echo "pass agentd app=$app process=found"
	probe_status="$(sed -n 's/^@@probe //p' <<<"$reply" | head -n 1)"
	probe_out="$(sed -n '/^@@probe /,/^@@rpc /{/^@@/d;p}' <<<"$reply" | tr '\n' ' ' | cut -c1-200)"
	if [ "$probe_status" = 0 ]; then
		echo "pass probe $url ready=true bearer=enforced client-cert=enforced"
	else
		echo "fail probe $url exit=${probe_status:-none} ${probe_out% }"
		failures=$((failures + 1))
	fi
	rpc_line="$(sed -n 's/^@@rpc //p' <<<"$reply" | head -n 1)"
	read -r rpc_status rpc_code <<<"${rpc_line:-none none}"
	rpc_body="$(sed -n '/^@@rpc /,$p' <<<"$reply" | sed '1d')"
	if [ "$rpc_status" != 0 ]; then
		echo "fail rpc $url/rpc operation=program.discover transport=curl-$rpc_status $(printf '%s' "$rpc_body" | tr '\n' ' ' | cut -c1-160)"
		finish $((failures + 1))
	fi
	shape="$(python3 -c "$agent_public_py" <<<"$rpc_body")" && status=0 || status=$?
	if [ "$status" -eq 0 ] && [ "$rpc_code" = 200 ]; then
		echo "pass rpc $url/rpc operation=program.discover http=200 $shape"
	else
		echo "fail rpc $url/rpc operation=program.discover http=$rpc_code $shape"
		failures=$((failures + 1))
	fi
	finish "$failures"
}

# check_gas: the gas station app of interop/deploy/gas-station/fly.toml runs
# one started machine with one volume and the configuration its init rendered
# there names chain 125, the SID token, the router first among three
# endpoints and the paymaster; the account of CHECK_LIVE_GAS_ACCOUNT_KEYSTORE
# is delegated to that paymaster; POST /quote at chain.paxeer.network answers
# 200 with a quote for one no-op call whose SID amount lies within the
# contract's spread of the paymaster's currentRate; the account signs the
# batch and its EIP-7702 authorization with cast, POST /submit answers 200
# with a transaction hash, and its receipt succeeds with the SID Transfer of
# the quoted amount from the account to the sponsor. The keystore password
# never leaves cast. One line per check.
gas_sid=0x21f7b20a555199fa73A238B1a91FD0f549068fEe
gas_transfer_topic=0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef
gas_eth_prefix=0x19457468657265756d205369676e6564204d6573736167653a0a3332

# gas_rpc <method> <params json>: the result of one JSON-RPC call to the gas
# check's RPC URL, a string as is and anything else as compact JSON; status 1
# when there is no result.
gas_rpc() {
	curl -sS -m "$timeout" -H 'content-type: application/json' \
		-d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"$1\",\"params\":$2}" "$gas_rpc_url" 2>/dev/null | python3 -c '
import json, sys
try:
    r = json.load(sys.stdin)["result"]
except Exception:
    sys.exit(1)
print(r if isinstance(r, str) else json.dumps(r, separators=(",", ":")))
'
}

# gas_post <route> <request file> <response file>: POSTs the file to the
# station's route and prints the HTTP status.
gas_post() {
	curl -sS --max-time "$timeout" --output "$3" --write-out '%{http_code}' \
		-H 'content-type: application/json' --data-binary "@$2" "$gas_origin$1" 2>/dev/null || true
}

# gas_signed <32-byte hash>: the EIP-191 hash of the 32 bytes, as
# MessageHashUtils.toEthSignedMessageHash computes it.
gas_signed() {
	cast keccak "$gas_eth_prefix${1#0x}"
}

gas_sign() {
	cast wallet sign --no-hash "$1" --keystore "$CHECK_LIVE_GAS_ACCOUNT_KEYSTORE" \
		--password-file "$CHECK_LIVE_GAS_ACCOUNT_PASSWORD_FILE" 2>/dev/null
}

check_gas() {
	local toml=interop/deploy/gas-station/fly.toml name
	local app answer n_machines n_started n_mounts config chain token endpoints first paymaster gas_limit priority
	local account code rate price nonce auth_nonce gas_cost w status body quote sponsor amount maximum deadline qnonce
	local qd calls_hash bd account_sig auth_sig auth_rlp call_data tx receipt attempt verdict failures=0
	for name in CHECK_LIVE_GAS_ACCOUNT_KEYSTORE CHECK_LIVE_GAS_ACCOUNT_PASSWORD_FILE CHECK_LIVE_GAS_MAX_TOKEN_AMOUNT; do
		if [ -z "${!name:-}" ]; then
			echo "check-live: $name is unset" >&2
			exit 2
		fi
	done
	if ! [[ "$CHECK_LIVE_GAS_MAX_TOKEN_AMOUNT" =~ ^[1-9][0-9]*$ ]]; then
		echo "check-live: CHECK_LIVE_GAS_MAX_TOKEN_AMOUNT is not a positive integer" >&2
		exit 2
	fi
	gas_origin="${CHECK_LIVE_GAS_ORIGIN:-https://chain.paxeer.network}"
	gas_rpc_url="${CHECK_LIVE_GAS_RPC:-https://api-mainnet-beta.paxeer.network/rpc}"
	if ! app="$(fly_app "$toml")"; then
		echo "fail gas toml=absent"
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

	config="$(fly_ssh "$app" - "cat /data/gas-station/station.json" </dev/null | python3 -c '
import json, sys
c = json.load(sys.stdin)
print(c["chain_id"], c["token"], len(c["endpoints"]), c["endpoints"][0], c["paymaster"], c["gas_limit"], c["max_priority_fee_per_gas"])
' 2>/dev/null)" || config=""
	read -r chain token endpoints first paymaster gas_limit priority <<<"${config:-none none none none none none none}"
	if [ -z "$config" ]; then
		echo "fail config app=$app station.json=unreadable"
		finish $((failures + 1))
	fi
	if [ "$chain" = 125 ] && [ "${token,,}" = "${gas_sid,,}" ] && [ "$endpoints" = 3 ] && [ "$first" = https://api-mainnet-beta.paxeer.network/rpc ]; then
		echo "pass config app=$app chain_id=125 token=SID endpoints=3 first=router paymaster=$paymaster"
	else
		echo "fail config app=$app chain_id=$chain token=$token endpoints=$endpoints first=$first"
		finish $((failures + 1))
	fi

	account="$(cast wallet address --keystore "$CHECK_LIVE_GAS_ACCOUNT_KEYSTORE" --password-file "$CHECK_LIVE_GAS_ACCOUNT_PASSWORD_FILE" 2>/dev/null)" || account=""
	if [ -z "$account" ]; then
		echo "fail account keystore=unreadable"
		finish $((failures + 1))
	fi
	code="$(gas_rpc eth_getCode "[\"$account\",\"latest\"]")" || code=""
	if [ "${code,,}" = "0xef0100$(tr '[:upper:]' '[:lower:]' <<<"${paymaster#0x}")" ]; then
		echo "pass delegation account=$account delegate=$paymaster"
	else
		echo "fail delegation account=$account code=${code:-none} want=0xef0100${paymaster#0x}"
		finish $((failures + 1))
	fi

	rate="$(gas_rpc eth_call "[{\"to\":\"$paymaster\",\"data\":\"$(cast sig 'currentRate()')\"},\"latest\"]")" || rate=""
	price="$(gas_rpc eth_gasPrice '[]')" || price=""
	nonce="$(gas_rpc eth_call "[{\"to\":\"$account\",\"data\":\"$(cast sig 'nonce()')\"},\"pending\"]")" || nonce=""
	auth_nonce="$(gas_rpc eth_getTransactionCount "[\"$account\",\"pending\"]")" || auth_nonce=""
	if ! [[ "$rate" =~ ^0x[0-9a-fA-F]+$ && "$price" =~ ^0x[0-9a-fA-F]+$ && "$nonce" =~ ^0x[0-9a-fA-F]+$ && "$auth_nonce" =~ ^0x[0-9a-fA-F]+$ ]] || [ "$((rate))" -eq 0 ]; then
		echo "fail chain-state rate=${rate:-none} gas_price=${price:-none} batch_nonce=${nonce:-none} account_nonce=${auth_nonce:-none}"
		finish $((failures + 1))
	fi
	read -r rate nonce auth_nonce gas_cost <<<"$(python3 -c '
import sys
rate, price, nonce, auth, limit, priority = (int(v, 0) for v in sys.argv[1:])
fee = 2 * price + priority
print(rate, nonce, auth, limit * fee)
' "$rate" "$price" "$nonce" "$auth_nonce" "$gas_limit" "$priority")"

	w="$(mktemp -d)"
	printf '{"account":"%s","nonce":"%s","calls":[{"to":"%s","value":"0","data":"0x"}],"maxTokenAmount":"%s","gasCost":"%s","chainId":"125","token":"%s","decimals":6}' \
		"$account" "$nonce" "$account" "$CHECK_LIVE_GAS_MAX_TOKEN_AMOUNT" "$gas_cost" "$gas_sid" >"$w/quote.json"
	status="$(gas_post /quote "$w/quote.json" "$w/quote.out")"
	quote="$(python3 -c '
import json, sys
account, sid, maximum, gas_cost, rate = sys.argv[1].lower(), sys.argv[2].lower(), int(sys.argv[3]), int(sys.argv[4]), int(sys.argv[5])
try:
    doc = json.load(open(sys.argv[6]))
    q = doc["quote"]
    fields = [q["sponsor"], q["token"], int(q["maxTokenAmount"]), int(q["tokenAmount"]), int(q["deadline"]), int(q["quoteNonce"]), int(q["gasCost"]), q["decimals"], doc["relayerSignature"]]
except Exception:
    print("|body=unreadable")
    sys.exit(1)
sponsor, token, qmax, amount, deadline, qnonce, qcost, decimals, signature = fields
expected = -(-gas_cost * rate // 10**18)
lower, upper = -(-expected * 9500 // 10000), expected * 10500 // 10000
ok = (token.lower() == sid and decimals == 6 and qcost == gas_cost and qmax <= maximum and 0 < amount <= qmax
      and lower <= amount <= upper and sponsor.lower() not in ("", account) and len(signature) == 132)
print(sponsor, qmax, amount, deadline, qnonce, signature, "|rate=%d token_amount=%d expected=%d max=%d sponsor=%s" % (rate, amount, expected, qmax, sponsor))
sys.exit(0 if ok else 1)
' "$account" "$gas_sid" "$CHECK_LIVE_GAS_MAX_TOKEN_AMOUNT" "$gas_cost" "$rate" "$w/quote.out" 2>/dev/null)" && verdict=pass || verdict=fail
	[ "$status" = 200 ] || verdict=fail
	if [ "$verdict" != pass ]; then
		echo "fail quote $gas_origin/quote http=${status:-none} ${quote#*|}"
		rm -rf "$w"
		finish $((failures + 1))
	fi
	read -r sponsor maximum amount deadline qnonce body <<<"${quote%%|*}"
	echo "pass quote $gas_origin/quote http=200 ${quote#*|}"

	qd="$(gas_signed "$(cast keccak "$(cast abi-encode 'f(bytes32,uint256,address,address,address,uint256,uint256,uint256,uint256,uint256)' \
		"$(cast keccak 'Quote(uint256 chainId,address account,address sponsor,address token,uint256 maxTokenAmount,uint256 tokenAmount,uint256 deadline,uint256 quoteNonce,uint256 gasCost)')" \
		125 "$account" "$sponsor" "$gas_sid" "$maximum" "$amount" "$deadline" "$qnonce" "$gas_cost")")")"
	calls_hash="$(cast keccak "$(cast abi-encode 'f((address,uint256,bytes)[])' "[($account,0,0x)]")")"
	bd="$(gas_signed "$(cast keccak "$(cast abi-encode 'f(bytes32,uint256,bytes32,bytes32)' \
		"$(cast keccak 'SponsoredBatch(uint256 nonce,bytes32 callsHash,bytes32 quoteDigest)')" "$nonce" "$calls_hash" "$qd")")")"
	auth_rlp="$(cast to-rlp "[\"0x7d\",\"$paymaster\",\"$(python3 -c 'import sys; n = int(sys.argv[1]); print("0x" + (("%x" % n).rjust(len("%x" % n) + len("%x" % n) % 2, "0") if n else ""))' "$auth_nonce")\"]")"
	account_sig="$(gas_sign "$bd")" || account_sig=""
	auth_sig="$(gas_sign "$(cast keccak "0x05${auth_rlp#0x}")")" || auth_sig=""
	if [ "${#account_sig}" -ne 132 ] || [ "${#auth_sig}" -ne 132 ]; then
		echo "fail sign account=$account keystore=refused"
		rm -rf "$w"
		finish $((failures + 1))
	fi
	call_data="$(cast calldata 'executeSponsored((address,uint256,bytes)[],(address,address,uint256,uint256,uint256,uint256,uint256),bytes,bytes)' \
		"[($account,0,0x)]" "($sponsor,$gas_sid,$maximum,$amount,$deadline,$qnonce,$gas_cost)" "$account_sig" "$body")"
	printf '{"call":{"to":"%s","value":"0","data":"%s"},"authorization":{"chainId":"125","address":"%s","nonce":"%s","yParity":%d,"r":"0x%s","s":"0x%s"},"batch":{"chainId":"125","account":"%s","nonce":"%s","calls":[{"to":"%s","value":"0","data":"0x"}],"quote":{"sponsor":"%s","token":"%s","maxTokenAmount":"%s","tokenAmount":"%s","deadline":"%s","quoteNonce":"%s","gasCost":"%s","decimals":6}},"accountSignature":"%s","relayerSignature":"%s"}' \
		"$account" "$call_data" "$paymaster" "$auth_nonce" "$((16#${auth_sig:130:2} - 27))" "${auth_sig:2:64}" "${auth_sig:66:64}" \
		"$account" "$nonce" "$account" "$sponsor" "$gas_sid" "$maximum" "$amount" "$deadline" "$qnonce" "$gas_cost" "$account_sig" "$body" >"$w/submit.json"
	status="$(gas_post /submit "$w/submit.json" "$w/submit.out")"
	tx="$(python3 -c 'import json, sys; print(json.load(open(sys.argv[1]))["transactionHash"])' "$w/submit.out" 2>/dev/null)" || tx=""
	rm -rf "$w"
	if [ "$status" != 200 ] || ! [[ "$tx" =~ ^0x[0-9a-fA-F]{64}$ ]]; then
		echo "fail submit $gas_origin/submit http=${status:-none} tx=${tx:-none}"
		finish $((failures + 1))
	fi
	receipt=""
	for attempt in $(seq "${CHECK_LIVE_GAS_RECEIPT_ATTEMPTS:-36}"); do
		receipt="$(gas_rpc eth_getTransactionReceipt "[\"$tx\"]")" || receipt=""
		[ -z "$receipt" ] || [ "$receipt" = null ] || break
		[ "$attempt" = "${CHECK_LIVE_GAS_RECEIPT_ATTEMPTS:-36}" ] || sleep 5
	done
	answer="$(python3 -c '
import json, sys
sid, topic, account, sponsor, amount = sys.argv[1].lower(), sys.argv[2], sys.argv[3].lower(), sys.argv[4].lower(), int(sys.argv[5])
try:
    r = json.loads(sys.argv[6])
    status = int(r["status"], 16)
    logs = r["logs"]
except Exception:
    print("receipt=none")
    sys.exit(1)
pad = lambda a: "0x" + a[2:].rjust(64, "0")
moved = [l for l in logs if l.get("address", "").lower() == sid and [t.lower() for t in l.get("topics", [])] == [topic, pad(account), pad(sponsor)]]
paid = moved and int(moved[0].get("data", "0x0"), 16) or 0
print("status=%d sid_transfer=%d want=%d to=sponsor" % (status, paid, amount))
sys.exit(0 if status == 1 and paid == amount else 1)
' "$gas_sid" "$gas_transfer_topic" "$account" "$sponsor" "$amount" "${receipt:-null}")" && verdict=pass || verdict=fail
	echo "$verdict submit $gas_origin/submit http=200 tx=$tx $answer"
	[ "$verdict" = pass ] || failures=$((failures + 1))
	finish "$failures"
}

# check_internal: the internal app of platform/hosted/internal/fly.toml and
# the internal Redis app of platform/hosted/internal/redis.toml hold no public
# IP; the router's served chain at api-mainnet-beta.paxeer.network ends at
# ISRG Root X1 or ISRG Root X2, reported as root=X1|X2; inside the payments
# and programs machines the running layerx-event-source of that kind has the
# router as LAYERX_EVENTS_UPSTREAM_URL and as LAYERX_EVENTS_UPSTREAM_CA_DER a
# self-signed root of that same name that verifies the served chain; from the
# kernel machine of human/wallet/deploy/human.toml the five /readyz routes at
# their <group>.process.<app>.internal names answer ready over mTLS with the
# human-event-client identity tools/bringup/ca.sh left on the kernel volume.
# CHECK_LIVE_ROUTER_CONNECT (host:port, default the router name on 443) is
# where the served chain is read. One line per check.
# shellcheck disable=SC2016
internal_pin_script='pid=
for d in /proc/[0-9]*; do
	case "$(readlink "$d/exe" 2>/dev/null)" in
	*/layerx-event-source)
		if tr "\000" "\n" <"$d/environ" 2>/dev/null | grep -qx "LAYERX_EVENTS_KIND=$kind"; then
			pid=$d
			break
		fi
		;;
	esac
done
if [ -z "$pid" ]; then
	echo "@@process none"
	exit 0
fi
env_of() { tr "\000" "\n" <"$pid/environ" | sed -n "s/^$1=//p" | head -n 1; }
echo "@@url $(env_of LAYERX_EVENTS_UPSTREAM_URL)"
echo "@@pin $(base64 <"$(env_of LAYERX_EVENTS_UPSTREAM_CA_DER)" 2>/dev/null | tr -d "\n")"'
# shellcheck disable=SC2016
internal_ready_script='for group in kms journeys payments approvals programs; do
	status=0
	answer=$(curl -sS -m "$limit" --cacert "$tls/ca.pem" --cert "$tls/cert.pem" --key "$tls/key.pem" -w "\n%{http_code}" "https://$group.process.$app.internal:9443/readyz" 2>&1) || status=$?
	echo "@@$group $status $(printf "%s" "$answer" | tail -n 1) $(printf "%s" "$answer" | head -n 1 | tr -d " " | cut -c1-120)"
done'
check_internal() {
	local router=api-mainnet-beta.paxeer.network
	local connect="${CHECK_LIVE_ROUTER_CONNECT:-api-mainnet-beta.paxeer.network:443}"
	local app redis_app kernel name="" answer w root="" group reply url pin subject line status code body failures=0
	if ! command -v flyctl >/dev/null 2>&1; then
		echo "check-live: flyctl is required" >&2
		exit 2
	fi
	if ! app="$(fly_app platform/hosted/internal/fly.toml)" || ! redis_app="$(fly_app platform/hosted/internal/redis.toml)" ||
		! kernel="$(fly_app human/wallet/deploy/human.toml)"; then
		echo "fail internal toml=absent"
		finish 1
	fi
	for name in "$app" "$redis_app"; do
		answer="$(timeout "$timeout" flyctl ips list --app "$name" --json 2>/dev/null | python3 -c 'import json, sys; print(len(json.load(sys.stdin) or []))' 2>/dev/null)" || answer=none
		if [ "$answer" = 0 ]; then
			echo "pass public-ips app=$name count=0"
		else
			echo "fail public-ips app=$name count=${answer:-none}"
			failures=$((failures + 1))
		fi
	done

	name=""
	w="$(mktemp -d)"
	# shellcheck disable=SC2064
	trap "rm -rf '$w'" EXIT
	timeout "$timeout" openssl s_client -connect "$connect" -servername "$router" -showcerts </dev/null 2>/dev/null |
		awk -v dir="$w" '/-BEGIN CERTIFICATE-/ { n++; in_cert = 1 } in_cert { print >(dir "/served-" n ".pem") } /-END CERTIFICATE-/ { in_cert = 0; close(dir "/served-" n ".pem") }' || true
	if [ -s "$w/served-1.pem" ]; then
		cat "$w"/served-[2-9].pem >"$w/untrusted.pem" 2>/dev/null || : >"$w/untrusted.pem"
		# shellcheck disable=SC2012
		name="$(openssl x509 -in "$(ls "$w"/served-*.pem | sort -V | tail -n 1)" -noout -issuer -nameopt sep_multiline,utf8 | sed -n 's/^ *CN=//p')"
		case "$name" in
		"ISRG Root X1") root=X1 ;;
		"ISRG Root X2") root=X2 ;;
		esac
	fi
	if [ -n "$root" ]; then
		echo "pass router-root host=$router root=$root"
	else
		[ -n "$name" ] && name=other || name=unreadable
		echo "fail router-root host=$router root=$name"
		failures=$((failures + 1))
	fi

	for group in payments programs; do
		reply="$(printf '%s\n' "$internal_pin_script" | fly_ssh "$app" "$group" "kind=$group sh -s")" || reply=""
		url="$(sed -n 's/^@@url //p' <<<"$reply" | head -n 1)"
		pin="$(sed -n 's/^@@pin //p' <<<"$reply" | head -n 1)"
		if [ -z "$url" ]; then
			echo "fail upstream-ca group=$group process=$(sed -n 's/^@@process //p' <<<"$reply" | head -n 1 | grep . || echo unreachable)"
			failures=$((failures + 1))
			continue
		fi
		subject=""
		status=1
		if [ -n "$pin" ] && base64 -d <<<"$pin" 2>/dev/null | openssl x509 -inform DER -out "$w/pin-$group.pem" 2>/dev/null; then
			subject="$(openssl x509 -in "$w/pin-$group.pem" -noout -subject -nameopt sep_multiline,utf8 | sed -n 's/^ *CN=//p')"
			if [ -n "$root" ] && [ "$subject" = "ISRG Root $root" ] &&
				[ "$subject" = "$(openssl x509 -in "$w/pin-$group.pem" -noout -issuer -nameopt sep_multiline,utf8 | sed -n 's/^ *CN=//p')" ] &&
				openssl verify -CAfile "$w/pin-$group.pem" -untrusted "$w/untrusted.pem" -verify_hostname "$router" "$w/served-1.pem" >/dev/null 2>&1; then
				status=0
			fi
		fi
		if [ "$url" = "https://$router" ] && [ "$status" -eq 0 ]; then
			echo "pass upstream-ca group=$group url=$url root=$root verifies=yes"
		else
			subject="${subject:-unreadable}"
			echo "fail upstream-ca group=$group url=$url pin=${subject// /-} served=${root:-unreadable}"
			failures=$((failures + 1))
		fi
	done

	reply="$(printf '%s\n' "$internal_ready_script" | fly_ssh "$kernel" - "tls=$fly_tls_dir/human-event-client app=$app limit=$timeout sh -s")" || reply=""
	for group in kms journeys payments approvals programs; do
		url="https://$group.process.$app.internal:9443/readyz"
		line="$(sed -n "s/^@@$group //p" <<<"$reply" | head -n 1)"
		read -r status code body <<<"${line:-none none}"
		if [ "$status" = 0 ] && [ "$code" = 200 ] && [[ "$body" == *'"ready":true'* ]]; then
			echo "pass readiness group=$group url=$url from=$kernel http=200 ready=true"
		else
			echo "fail readiness group=$group url=$url from=$kernel curl=${status:-none} http=${code:-none}"
			failures=$((failures + 1))
		fi
	done
	finish "$failures"
}

# bridge_machine_script: runs inside the bridge relayer machine under sh -s
# with journal set to the relayer's journal path. Prints "@@relayer" and
# "@@signer" with running or none, from the executables of /proc, then
# "@@journal absent" or "@@journal present" and, for the first inbound item
# the journal records as observed and as completed by this relayer's included
# bridgeIn transaction, "@@bridge-in <item>".
# shellcheck disable=SC2016
bridge_machine_script='relayer=none
signer=none
for d in /proc/[0-9]*; do
	case "$(readlink "$d/exe" 2>/dev/null)" in
	*/layerx-bridge-relayer) relayer=running ;;
	*/layerx-mirror-signer) signer=running ;;
	esac
done
echo "@@relayer $relayer"
echo "@@signer $signer"
if [ ! -r "$journal" ]; then
	echo "@@journal absent"
	exit 0
fi
echo "@@journal present"
sed -n "s/^{\"kind\":\"completed\",\"item\":\"\(in:[^\"]*\)\",\"completion\":{\"outcome\":\"included\".*/\1/p" "$journal" | while read -r item; do
	if grep -qF "{\"kind\":\"observed\",\"item\":\"$item\"," "$journal"; then
		echo "@@bridge-in $item"
		break
	fi
done'

# check_bridge: bridge/deploy/checklist.sh exits 0 for every chain under
# bridge/evm/chains and bridge/solana/chains against the deployment record
# PAXEER_BRIDGE_RECORDS_DIR/<chain>.json, each run bounded by ten times
# CHECK_LIVE_TIMEOUT; a machine of the app of
# interop/deploy/bridge-relayer/fly.toml runs both the relayer and its bridge
# signer; and the relayer's journal on the volume records one observed deposit
# completed by its own included bridgeIn transaction. One line per check; a
# failing checklist line carries its last output line with every URL replaced
# by <url>.
check_bridge() {
	local toml=interop/deploy/bridge-relayer/fly.toml journal=/data/relayer/journal.jsonl
	local app dir chain record out status reply relayer signer item failures=0
	local -a chains=()
	if [ -z "${PAXEER_BRIDGE_RECORDS_DIR:-}" ]; then
		echo "check-live: PAXEER_BRIDGE_RECORDS_DIR is unset" >&2
		exit 2
	fi
	if ! command -v flyctl >/dev/null 2>&1; then
		echo "check-live: flyctl is required" >&2
		exit 2
	fi
	for dir in "$repo_root"/bridge/evm/chains/*/ "$repo_root"/bridge/solana/chains/*/; do
		[ -d "$dir" ] && chains+=("$(basename "$dir")")
	done
	if [ "${#chains[@]}" -eq 0 ]; then
		echo "fail checklist chains=none"
		failures=$((failures + 1))
	fi
	for chain in ${chains[@]+"${chains[@]}"}; do
		record="$PAXEER_BRIDGE_RECORDS_DIR/$chain.json"
		if [ ! -r "$record" ]; then
			echo "fail checklist chain=$chain record=absent"
			failures=$((failures + 1))
			continue
		fi
		status=0
		out="$(PAXEER_BRIDGE_DEPLOYMENT_RECORD="$record" timeout "$((timeout * 10))" "$repo_root/bridge/deploy/checklist.sh" "$chain" 2>&1 </dev/null)" || status=$?
		if [ "$status" -eq 0 ]; then
			echo "pass checklist chain=$chain exit=0"
		else
			echo "fail checklist chain=$chain exit=$status $(printf '%s\n' "$out" | tail -n 1 | sed -E 's#[A-Za-z][A-Za-z0-9+.-]*://[^[:space:]]*#<url>#g' | cut -c1-160)"
			failures=$((failures + 1))
		fi
	done

	if ! app="$(fly_app "$toml")"; then
		echo "fail bridge toml=absent"
		finish $((failures + 1))
	fi
	reply="$(fly_ssh "$app" - "journal=$journal sh -s" <<<"$bridge_machine_script")" || reply=""
	relayer="$(sed -n 's/^@@relayer //p' <<<"$reply" | head -n 1)"
	signer="$(sed -n 's/^@@signer //p' <<<"$reply" | head -n 1)"
	if [ -z "$relayer" ]; then
		echo "fail processes app=$app machine=unreachable"
		finish $((failures + 1))
	elif [ "$relayer" = running ] && [ "$signer" = running ]; then
		echo "pass processes app=$app relayer=running signer=running"
	else
		echo "fail processes app=$app relayer=$relayer signer=${signer:-none}"
		failures=$((failures + 1))
	fi
	item="$(sed -n 's/^@@bridge-in //p' <<<"$reply" | head -n 1)"
	if [ -n "$item" ]; then
		echo "pass bridge-in app=$app item=$item outcome=included"
	elif grep -qx '@@journal present' <<<"$reply"; then
		echo "fail bridge-in app=$app journal=present included=none"
		failures=$((failures + 1))
	else
		echo "fail bridge-in app=$app journal=absent"
		failures=$((failures + 1))
	fi
	finish "$failures"
}

# check_wallet: the public wallet endpoint name is the wallet_endpoint of
# [decision.public_names] in the spec when that value is a bare hostname,
# otherwise the host of NEXT_PUBLIC_PAXEER_WALLET_API of the wallet PWA,
# service paxport of the Railway project linked at the checkout root. The
# wallet feature's cutover gate reads that name, its gateway gate reads the
# wallet gateway app of human/wallet/deploy/gateway.toml at its fly.dev name
# with CHECK_LIVE_GATEWAY_TOKEN passed through, and the app runs started
# machines in two regions. The gates' check lines are printed as they come
# without their summary lines, and a failing gate counts as one failure.
check_wallet() {
	local toml=human/wallet/deploy/gateway.toml spec=spec/paxeer-x-bringup/spec.kvx
	local app name source api="" output status failures=0
	local host_re='^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+(:[0-9]{1,5})?$'
	if [ -z "${CHECK_LIVE_GATEWAY_TOKEN:-}" ]; then
		echo "check-live: CHECK_LIVE_GATEWAY_TOKEN is required; it is the access token of a provisioned test identity of the wallet gateway" >&2
		exit 2
	fi
	if ! command -v flyctl >/dev/null 2>&1; then
		echo "check-live: flyctl is required" >&2
		exit 2
	fi
	if ! app="$(fly_app "$toml")"; then
		echo "fail wallet toml=absent"
		finish 1
	fi

	source=spec
	name="$(sed -n '/^\[decision\.public_names\]$/,/^\[/s/^wallet_endpoint[[:space:]]*=[[:space:]]*"\(.*\)"[[:space:]]*$/\1/p' "$repo_root/$spec" 2>/dev/null | head -n 1)" || name=""
	if ! grep -Eq "$host_re" <<<"$name"; then
		source=railway
		if ! command -v railway >/dev/null 2>&1; then
			echo "check-live: railway is required to read NEXT_PUBLIC_PAXEER_WALLET_API while the spec names no wallet endpoint" >&2
			exit 2
		fi
		api="$(cd "$repo_root" && timeout "$timeout" railway variable list --service paxport --kv 2>/dev/null | sed -n 's/^NEXT_PUBLIC_PAXEER_WALLET_API=//p' | head -n 1)" || api=""
		name="$(python3 -c '
import sys
from urllib.parse import urlsplit

try:
    parts = urlsplit(sys.argv[1].strip())
    port = parts.port
except ValueError:
    sys.exit(1)
if parts.scheme != "https" or not parts.hostname:
    sys.exit(1)
print(parts.hostname + (":%d" % port if port else ""))
' "$api" 2>/dev/null)" || name=""
	fi
	if grep -Eq "$host_re" <<<"$name"; then
		echo "pass endpoint name=$name source=$source"
		status=0
		output="$(CHECK_LIVE_CUTOVER_HOST="$name" "$repo_root/tools/wallet/check-live.sh" cutover 2>&1)" || status=$?
		grep -Ev '^check-live: (all checks passed|[0-9]+ check\(s\) failed)$' <<<"$output" || true
		[ "$status" -eq 0 ] || failures=$((failures + 1))
	else
		if [ -n "$api" ]; then
			echo "fail endpoint source=$source api=unusable"
		else
			echo "fail endpoint source=$source api=unset"
		fi
		failures=$((failures + 1))
	fi

	status=0
	output="$(CHECK_LIVE_GATEWAY_BASE="https://$app.fly.dev" "$repo_root/tools/wallet/check-live.sh" gateway 2>&1)" || status=$?
	grep -Ev '^check-live: (all checks passed|[0-9]+ check\(s\) failed)$' <<<"$output" || true
	[ "$status" -eq 0 ] || failures=$((failures + 1))

	fly_regions "$app" || failures=$((failures + 1))
	finish "$failures"
}

# Sourced by tools/bringup/ca.sh for the Fly helpers and the CA settings: the
# probe's own dispatch below runs only when this file is executed.
[ "${BASH_SOURCE[0]}" = "$0" ] || return 0

# check_kernel_app: the kernel app of human/wallet/deploy/human.toml runs
# exactly one machine, started, with its one volume mounted at /data; the
# init of docker/kernel/init.sh runs as root as the entrypoint; and every
# service the init records under /run/layerx/init either runs under its uid
# or waits on the genesis. One line per check.
check_kernel_app() {
	local app answer n_machines n_started n_data line kind name want state detail uid services=0 failures=0
	if ! command -v flyctl >/dev/null 2>&1; then
		echo "check-live: flyctl is required" >&2
		exit 2
	fi
	if ! app="$(fly_app human/wallet/deploy/human.toml)"; then
		echo "fail kernel-app toml=absent"
		finish 1
	fi
	answer="$(timeout "$timeout" flyctl machines list --app "$app" --json 2>/dev/null | python3 -c '
import json, sys
ms = json.load(sys.stdin)
started = [m for m in ms if m.get("state") == "started"]
data = [x for m in started for x in (m.get("config") or {}).get("mounts") or [] if x.get("path") == "/data" and x.get("volume")]
print(len(ms), len(started), len(data))
' 2>/dev/null)" || answer=""
	read -r n_machines n_started n_data <<<"${answer:-none none none}"
	if [ "$n_machines" = 1 ] && [ "$n_started" = 1 ] && [ "$n_data" = 1 ]; then
		echo "pass machines app=$app machines=1 started=1 volume=/data"
	else
		echo "fail machines app=$app machines=$n_machines started=$n_started volume-at-data=$n_data"
		finish $((failures + 1))
	fi
	# shellcheck disable=SC2016 # the command expands on the machine
	answer="$(fly_ssh "$app" - 'cd /run/layerx/init/ && p=$(cat pid) && echo init init 0 $(stat -c %u /proc/$p 2>/dev/null || echo -) $(tr "\000" " " </proc/$p/cmdline 2>/dev/null) && for f in *; do [ "$f" != pid ] || continue; read -r u s d <"$f"; a=-; [ "$s" != running ] || a=$(stat -c %u /proc/$d 2>/dev/null || echo -); echo svc "$f" $u $s $d $a; done' </dev/null)" || answer=""
	if ! grep -q '^init ' <<<"$answer"; then
		echo "fail init app=$app status=absent"
		finish $((failures + 1))
	fi
	while read -r kind name want state detail; do
		case "$kind" in
		init)
			if [ "$state" = 0 ] && [[ " $detail " == *kernel-init* ]]; then
				echo "pass init app=$app uid=0 entrypoint=kernel-init"
			else
				echo "fail init app=$app uid=$state entrypoint=${detail%% *}"
				failures=$((failures + 1))
			fi
			;;
		svc)
			services=$((services + 1))
			read -r detail uid <<<"$detail"
			if [ "$state" = running ] && [ "$uid" = "$want" ]; then
				echo "pass service $name uid=$want state=running"
			elif [ "$state" = waiting ] && [ "$detail" = genesis ]; then
				echo "pass service $name uid=$want state=waiting-genesis"
			elif [ "$state" = running ]; then
				echo "fail service $name uid=$uid want=$want state=running"
				failures=$((failures + 1))
			else
				echo "fail service $name uid=$want state=$state on=$detail"
				failures=$((failures + 1))
			fi
			;;
		esac
	done <<<"$answer"
	if [ "$services" -eq 0 ]; then
		echo "fail services app=$app count=0"
		failures=$((failures + 1))
	fi
	finish "$failures"
}

check_kernel_node() {
	local app answer key value genesis="" network="" public="" core="" status="" lni="" head="" failures=0
	local -A clocks=()
	if ! command -v flyctl >/dev/null 2>&1; then
		echo "check-live: flyctl is required" >&2
		exit 2
	fi
	if ! app="$(fly_app human/wallet/deploy/human.toml)"; then
		echo "fail kernel-node toml=absent"
		finish 1
	fi
	# shellcheck disable=SC2016 # the command expands on the machine
	answer="$(fly_ssh "$app" - 'n=/data/layerx/node; r=/run/layerx/node; e=$n/replica.env; u=http://127.0.0.1:$(sed -n "s/^LAYERX_AUTHORITY_PORT=//p" $e); t="Authorization: Bearer $(sed -n "s/^LAYERX_AUTHORITY_BEARER_TOKEN=//p" $e)"; echo genesis $(sha256sum $n/genesis/genesis.manifest | cut -d" " -f1); echo network $(curl -fsS -m 10 -H "$t" $u/v1/sync/network | jq -c .); echo public $(cat $n/*.env | sed -n "s/^LAYERX_NODE_SEQUENCER_PUBLIC_KEY=//p" | head -1); echo core $(sed -n "s/^LAYERX_CORE_SEQUENCER_ID=//p" $r/core.env); echo status $(printf "status\n" | socat -t 5 - UNIX-CONNECT:$r/supervisor.sock | jq -c .); [ -S $r/layerxd.lni.sock ] && echo lni socket; echo head $(curl -fsS -m 10 -H "$t" $u/v1/sync/head | jq -c .); for s in treasury-signer layerxd layerxd-authority guarantor-1 guarantor-2; do p=; read -r u st p </run/layerx/init/$s 2>/dev/null; echo clock $s $(tr "\000" " " </proc/${p:-0}/cmdline 2>/dev/null | cut -d" " -f1); done' </dev/null 2>/dev/null)" || answer=""
	while read -r key value; do
		case "$key" in
		genesis) genesis=$value ;;
		network) network=$value ;;
		public) public=$value ;;
		core) core=$value ;;
		status) status=$value ;;
		lni) lni=$value ;;
		head) head=$value ;;
		clock)
			key=${value%% *}
			value=${value#"$key"}
			clocks[$key]=${value# }
			;;
		esac
	done <<<"$answer"
	value="$(python3 -c 'import json, sys; print(json.loads(sys.argv[1]).get("genesis_sha256", "none"))' "$network" 2>/dev/null)" || value=none
	if [[ $genesis =~ ^[0-9a-f]{64}$ ]] && [ "$value" = "$genesis" ]; then
		echo "pass genesis app=$app sha256=$genesis replica=match"
	else
		echo "fail genesis app=$app sha256=${genesis:-absent} replica=$value"
		failures=$((failures + 1))
	fi
	value="$(printf 'layerx-sequencer:%s' "$public" | sha256sum | cut -d' ' -f1)"
	if [[ $public =~ ^[0-9a-f]{64}$ ]] && [ "$value" = "$core" ]; then
		echo "pass sequencer public=$public core=match"
	else
		echo "fail sequencer public=${public:-absent} core=${core:-absent}"
		failures=$((failures + 1))
	fi
	if [ "$(python3 -c 'import json, sys; print(json.loads(sys.argv[1]).get("state"))' "$status" 2>/dev/null)" = running ]; then
		echo "pass supervisor state=running"
	else
		echo "fail supervisor status=${status:-absent}"
		failures=$((failures + 1))
	fi
	if [ "$lni" = socket ]; then
		echo "pass lni socket=present"
	else
		echo "fail lni socket=absent"
		failures=$((failures + 1))
	fi
	if [ -n "$head" ] && python3 -c 'import json, sys; json.loads(sys.argv[1])["head"]' "$head" 2>/dev/null; then
		echo "pass replica head=$head"
	else
		echo "fail replica head=${head:-absent}"
		failures=$((failures + 1))
	fi
	for key in treasury-signer layerxd layerxd-authority guarantor-1 guarantor-2; do
		value=${clocks[$key]:-absent}
		if [ "${value##*/}" = layerx-runtime-clock ]; then
			echo "pass clock $key"
		else
			echo "fail clock $key exec=$value"
			failures=$((failures + 1))
		fi
	done
	finish "$failures"
}

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
kernel-app) ;;
kernel-node) ;;
search-front) ;;
edge) ;;
ci) ;;
human-session) ;;
paxeer-boundary) ;;
agent-public) ;;
gas) ;;
internal) ;;
bridge) ;;
wallet) ;;
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
[ "$mode" != gas ] || tools+=(flyctl cast)
for tool in "${tools[@]}"; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "check-live: $tool is required" >&2
		exit 2
	fi
done

[ "$mode" = explorer ] || load_hosts
"check_${mode//-/_}"
