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
          A row of ca.sh attestor_services is verified against the
          attestors' gateway CA under LAYERX_ATTESTOR_CA_DIR instead of the
          internal CA. One line each:
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
          or the expiry as observed; "fail <service> app=<app>
          attestor_ca=absent LAYERX_ATTESTOR_CA_DIR=<dir or unset>" for an
          attestor_services row when that directory holds no ca.pem.
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

wallet    reads the public wallet endpoint name from the wallet_endpoint
          value of [decision.public_names] in
          spec/paxeer-x-bringup/spec.kvx, then runs the wallet gates of
          scripts/wallet/check-live.sh and prints their check lines:
  endpoint   "pass endpoint name=<host> source=spec", or "fail endpoint
             source=spec name=unset" when the value is unset or not a host
             name, in which case cutover is skipped
  cutover    the served_by and readiness lines of its cutover mode with
             CHECK_LIVE_CUTOVER_HOST set to that name
  gateway    the readiness and me lines of its gateway mode with
             CHECK_LIVE_GATEWAY_BASE https://<app>.fly.dev for the app of
             human/wallet/deploy/gateway.toml
  machines   "pass machines started=<n> regions=<list> app=<app>" when the
             app runs at least two started machines in at least two regions
          Exits 0 only when every check passes; a failing gate counts once.

kernel-value-loop pipes tools/bringup/value-loop.sh into a machine of the
          app of human/wallet/deploy/human.toml and prints one line per step:
  precondition "fail precondition <name> <detail>" when a genesis output
             (genesis-ids, custody-profile, publication-policy), the
             sequencer's LNI socket (kernel-node), the receipt authority, a
             kernel image binary (image) or the opening deposit (deposit-tx)
             is absent; no other line follows
  asset      "pass asset PAX id=<hex>" when the genesis asset id equals the
             custody precompile's nativeAssetId()
  account    "pass account sender|recipient did=<did> main=<id>"
  credit     "pass credit deposit=<hash> amount=<n>" when the sender was
             opened by the first-credit path from DEPOSIT_TX
  activity   "pass activity id=<hex>" for the SEND sealed by the sequencer
  balance    "pass balance sender|recipient <json>"
  batch      "pass batch id=<hex> sealed=<n>" when the receipt authority's
             /v1/authorized-batches/by-activity answers for the SEND
  checkpoint "pass checkpoint batch=<n> status=submitted|final" when the
             anchor precompile's statusOf answers 1 or 2 for a sealed batch at
             or after it; a failing step prints its fail line and stops
          CHECK_LIVE_VALUE_LOOP_DEPOSIT_TX names the owner's deposit
          transaction for the sender's opening credit and
          CHECK_LIVE_VALUE_LOOP_CHECKPOINT_SECONDS bounds the checkpoint wait,
          default 600; the whole run is bounded by
          CHECK_LIVE_VALUE_LOOP_TIMEOUT, default 840.

registry-plan  prints the registry/router bring-up order from this checkout's
          tomls, no network, no host map, exactly four lines in order:
  stage   "stage <n> <name> requires=<previous stage|-> needs=<prerequisite>,...
          producers=<prerequisite>:<producer>,..." for material,
          registry-bootstrap, router-activation and routed-proof, a producer
          being ca.sh:<service>, fly-secret:<NAME>, init.sh:--prepare-material,
          deploy:<step> or stage:<name>; "fail registry-plan toml=absent
          <path>" and exit 1 when a toml line it reads is absent.

registry-bootstrap  the machines, private-ingress and mTLS healthz checks of
          registry, which ask no router; registry adds program-events and
          router-readyz read through the router.

router    requires protected material and registry-bootstrap records under
          CHECK_LIVE_STAGE_DIR, bound to CHECK_LIVE_CANDIDATE_REVISION,
          CHECK_LIVE_CANDIDATE_IMAGE and CHECK_LIVE_MATERIAL_GENERATION.
          Unless their ordered outcomes pass with the same pins, prints "fail router-activation
          missing=registry-bootstrap producer=stage:registry-bootstrap" and
          exits 1 before any request; its readyz line also fails with
          missing=program_registry when the router's program_registry backend
          is absent or not configured.

relay     reads the relay archive of platform/relay_archive/fly.toml against
          its origin in the kernel app of human/wallet/deploy/human.toml and
          prints one line per check: relay-origin (ready, fresh, genesis equal
          to the kernel's manifest), relay-public and one relay-replica per
          started machine (ready, fresh, pins equal, head within one batch of
          the origin read before and no further than the one after, exact
          batch bytes), machines (two started in two regions),
          relay-origin-recheck, relay-forward (the original signed activity
          answered with the router's verdict) and relay-altered (the flipped
          bytes refused with a 4xx). Needs no host map.

Environment:
  BRINGUP_HOSTS_FILE   private env file assigning EDGE_HOST, ARCHIVE_HOST,
                       VALIDATOR_HOSTS, RPC_HOSTS and OLD_WALLET_HOST;
                       each value is one ssh destination or,
                       for the plural roles, a space-separated list of them;
                       optionally RETAINED_ON_VALIDATOR, the public RPC names
                       retained on a validator host by owner ruling
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
  CHECK_LIVE_STAGE_DIR the operator's stage records, <stage>.json each,
                       required by router
  CHECK_LIVE_KERNEL_ARCHIVE_ORIGIN  configured relay/archive sync origin, required by kernel-node
  CHECK_LIVE_KERNEL_ARCHIVE_CA  optional CA file for that HTTPS archive origin
  CHECK_LIVE_KERNEL_LOCAL  1 selects explicitly provisioned local kernel processes
  CHECK_LIVE_KERNEL_DATA_DIR, CHECK_LIVE_KERNEL_RUN_DIR, CHECK_LIVE_KERNEL_INIT_DIR
                       explicit local node, sockets and service PID directories
  CHECK_LIVE_KERNEL_CTL  path of the actual prebuilt layerxctl operator executable
  CHECK_LIVE_RELAY_ORIGIN  public relay archive route, default
                       https://archive.paxeer.network
  CHECK_LIVE_RELAY_CA  optional CA file for that route
  CHECK_LIVE_RELAY_ACTIVITY  file of original signed activity bytes, required by relay
  CHECK_LIVE_RELAY_ACTIVITY_ID  the activity id the router answers for it
  CHECK_LIVE_RELAY_KEY_FILE  file holding the router API key; never printed
  CHECK_LIVE_TIMEOUT   seconds per request, ssh or flyctl call, default 30
  LAYERX_CA_DIR        the internal CA directory on this host, default
                       /etc/layerx/ca
  LAYERX_ATTESTOR_CA_DIR
                       the attestors' gateway CA directory on this host,
                       required by ca for the rows of ca.sh attestor_services
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
# ActiveState of the full-node unit paxd.service (never a validator unit), the
# HTTP and HTTPS ports it listens on beyond loopback and the apiN names of its
# enabled nginx sites (other for a site that is no apiN name), one line, no
# address.
# shellcheck disable=SC2016
placement_cmd='s=$(systemctl is-active paxd.service 2>/dev/null); p=$(ss -Hltn 2>/dev/null | awk "{print \$4}" | grep -Ev "^(127\.|\[::1\]:|::1:)" | sed -n "s/.*:\(80\|443\)$/\1/p" | sort -u | paste -sd, -); n=$(ls /etc/nginx/sites-enabled 2>/dev/null | sed -e "s/^\(api[0-9]*\)\..*/\1/" -e "/^api[0-9]*$/!s/.*/other/" | sort -u | paste -sd, -); echo "${s:-inactive} ${p:-none} ${n:-none}"'

# rpc_addrs <apiN>: the addresses the public name resolves to, one per line.
rpc_addrs() {
	getent ahosts "$1.$rpc_domain" 2>/dev/null | awk '{print $1}' | sort -u
}

# check_rpc_placement: each public RPC name resolves to an RPC_HOSTS
# destination outside VALIDATOR_HOSTS and answers within ten blocks of the
# highest answer, and no validator host runs the full-node unit or listens on
# a public HTTP or HTTPS port. A name RETAINED_ON_VALIDATOR lists (full public
# names or apiN) may resolve to an RPC_HOSTS destination that is a validator
# host by owner ruling: it is reported as retained, and that validator host
# may run the full-node unit and listen on HTTP and HTTPS for the retained
# names' nginx sites only. Destinations are named by role and index only.
check_rpc_placement() {
	local -a rpcs validators heads addrs
	local -A retained=() kept=()
	local k j n r name head lag at validator ok polls status reply state ports sites site top=0 failures=0
	read -r -a rpcs <<<"$RPC_HOSTS"
	read -r -a validators <<<"$VALIDATOR_HOSTS"
	for r in ${RETAINED_ON_VALIDATOR:-}; do
		retained[${r%%.*}]=1
	done
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
		mapfile -t addrs < <(rpc_addrs "$name")
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
		if [ "$validator" = yes ] && [ -n "${retained[$name]:-}" ] && [ "${at%%\[*}" = RPC_HOSTS ]; then
			echo "retained $name: on a validator host by owner ruling"
			kept[$j]="${kept[$j]:-}${kept[$j]:+,}$name"
			validator=retained
		fi
		if [ -n "$head" ]; then
			lag=$((top - head))
			[ "$lag" -le "$rpc_max_lag" ] || ok=0
		else
			head=none
			lag=none
			ok=0
		fi
		[ "$at" != none ] && [ "$validator" != yes ] && [ "${at%%\[*}" = RPC_HOSTS ] || ok=0
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
		read -r state ports sites <<<"$reply"
		if [ "$state" = inactive ] && [ "$ports" = none ]; then
			echo "pass VALIDATOR_HOSTS[$j] paxd=$state listen=$ports"
			continue
		fi
		ok=0
		if [ -n "${kept[$j]:-}" ]; then
			ok=1
			for site in ${sites//,/ }; do
				[ "$site:$ports" != none:none ] || continue
				[[ ",${kept[$j]}," == *",$site,"* ]] || ok=0
			done
		fi
		if [ "$ok" -eq 1 ]; then
			echo "retained VALIDATOR_HOSTS[$j] paxd=$state listen=$ports sites=$sites for ${kept[$j]} by owner ruling"
		else
			echo "fail VALIDATOR_HOSTS[$j] paxd=$state listen=$ports sites=${sites:-none}"
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
	local want got san missing n m days line failures=0 attestors trust
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
	attestors="$(sed -n 's/^attestor_services="\(.*\)"$/\1/p' "$(dirname "${BASH_SOURCE[0]}")/ca.sh")"
	while read -r service toml group custody _ eku sans; do
		if ! app="$(fly_app "$toml")"; then
			echo "fail $service toml=absent"
			failures=$((failures + 1))
			continue
		fi
		trust="$ca_dir/ca.pem"
		if [[ " $attestors " == *" $service "* ]]; then
			trust="${LAYERX_ATTESTOR_CA_DIR:-}/ca.pem"
			if [ -z "${LAYERX_ATTESTOR_CA_DIR:-}" ] || [ ! -r "$trust" ]; then
				echo "fail $service app=$app attestor_ca=absent LAYERX_ATTESTOR_CA_DIR=${LAYERX_ATTESTOR_CA_DIR:-unset}"
				failures=$((failures + 1))
				continue
			fi
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
		openssl verify -CAfile "$trust" <<<"$cert" >/dev/null 2>&1 || chain=untrusted
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
# With XWEB_ATTESTORS_ON_FLY=yes in the host map the loopback and peer checks
# give way to "pass VALIDATOR_HOSTS[k] listeners-8480-8481=0": nothing listens
# on the attestor ports.
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
		# Once the four attestors run as Fly apps (XWEB_ATTESTORS_ON_FLY=yes
		# in the host map), nothing listens on the attestor ports here.
		if [ "${XWEB_ATTESTORS_ON_FLY:-}" = yes ]; then
			status=0
			code="$(ssh_read "${dests[$k]}" 'ss -Hltn "( sport = :8480 or sport = :8481 )" | wc -l')" || status=$?
			if [ "$status" -ne 0 ]; then
				echo "fail VALIDATOR_HOSTS[$k] listeners-8480-8481 ssh=$status"
				failures=$((failures + 1))
			elif [ "$code" = 0 ]; then
				echo "pass VALIDATOR_HOSTS[$k] listeners-8480-8481=0"
			else
				echo "fail VALIDATOR_HOSTS[$k] listeners-8480-8481=${code:-none}"
				failures=$((failures + 1))
			fi
			continue
		fi
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
ci_dispatch_id() {
	python3 -c '
import json, sys
try:
    value = json.load(sys.stdin)
    run = value["workflow_run_id"]
    if type(run) is not int or run <= 0:
        raise ValueError("dispatch run identity")
    print(run)
except (ValueError, KeyError, TypeError):
    sys.exit(1)
'
}

ci_canary_evidence() {
	python3 -c '
import json, re, sys
try:
    expected_run, branch, candidate, workflow = sys.argv[1:]
    if not re.fullmatch(r"[1-9][0-9]*", expected_run) or not re.fullmatch(r"[0-9a-f]{40}", candidate):
        raise ValueError("expected identity")
    decoder = json.JSONDecoder()
    raw = sys.stdin.read().strip()
    run, offset = decoder.raw_decode(raw)
    pages = json.loads(raw[offset:].strip())
    if type(run.get("id")) is not int or run["id"] != int(expected_run):
        raise ValueError("run identity")
    if run.get("event") != "workflow_dispatch" or run.get("head_branch") != branch or run.get("head_sha") != candidate:
        raise ValueError("source identity")
    if run.get("path", "").split("@")[0] != ".github/workflows/" + workflow:
        raise ValueError("workflow identity")
    if run.get("status") != "completed" or run.get("conclusion") != "success":
        raise ValueError("run incomplete or failed")
    if not isinstance(pages, list) or not pages:
        raise ValueError("job pages")
    jobs, seen = [], set()
    total = pages[0].get("total_count")
    if type(total) is not int or total <= 0:
        raise ValueError("job count")
    for page in pages:
        if page.get("total_count") != total or not isinstance(page.get("jobs"), list):
            raise ValueError("job inventory")
        for job in page["jobs"]:
            ident = job.get("id")
            if type(ident) is not int or ident <= 0 or ident in seen or type(job.get("run_id")) is not int or job["run_id"] != int(expected_run):
                raise ValueError("job identity")
            seen.add(ident)
            if job.get("status") != "completed" or job.get("conclusion") != "success":
                raise ValueError("job incomplete or failed")
            jobs.append(job)
    if len(jobs) != total:
        raise ValueError("incomplete job pagination")
    runners = [job["runner_name"] for job in jobs if type(job.get("runner_id")) is int and job["runner_id"] > 0 and isinstance(job.get("runner_name"), str) and re.fullmatch(r"fly-[1-9][0-9]*", job["runner_name"])]
    if not runners:
        raise ValueError("runner identity")
    print("completed success", candidate, runners[0], len(jobs))
except (ValueError, KeyError, TypeError, AttributeError):
    sys.exit(1)
' "$@"
}

check_ci() {
	local toml=tools/flyci/controller/fly.toml workflow=runner-canary.yml wait=1500
	local app label answer n_machines n_started variable branch run status conclusion head_sha runner jobs candidate run_json jobs_json failures=0
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
	candidate="$(git -C "$repo_root" rev-parse --verify HEAD)" || candidate=""
	if [[ ! "$candidate" =~ ^[0-9a-f]{40}$ ]]; then
		echo "fail canary workflow=$workflow candidate=absent"
		finish $((failures + 1))
	fi
	if ! answer="$(cd "$repo_root" && timeout "$timeout" gh api --method POST "repos/{owner}/{repo}/actions/workflows/$workflow/dispatches" -H 'X-GitHub-Api-Version: 2022-11-28' -f ref="$branch" -F return_run_details=true 2>/dev/null)"; then
		echo "fail canary workflow=$workflow branch=$branch dispatch=unacknowledged"
		finish $((failures + 1))
	fi
	if ! run="$(printf '%s' "$answer" | ci_dispatch_id)"; then
		echo "fail canary workflow=$workflow branch=$branch run=unbound"
		finish $((failures + 1))
	fi
	(cd "$repo_root" && timeout "$wait" gh run watch "$run" --interval 15 >/dev/null 2>&1) || true
	run_json="$(cd "$repo_root" && timeout "$timeout" gh api "repos/{owner}/{repo}/actions/runs/$run" 2>/dev/null)" || run_json=""
	jobs_json="$(cd "$repo_root" && timeout "$timeout" gh api "repos/{owner}/{repo}/actions/runs/$run/jobs?per_page=100&filter=latest" --paginate --slurp 2>/dev/null)" || jobs_json=""
	answer="$(printf '%s\n%s\n' "$run_json" "$jobs_json" | ci_canary_evidence "$run" "$branch" "$candidate" "$workflow")" || answer=""
	read -r status conclusion head_sha runner jobs <<<"${answer:-none none none none none}"
	if [ "$status" != completed ]; then
		(cd "$repo_root" && timeout "$timeout" gh run cancel "$run" >/dev/null 2>&1) || true
	fi
	if [ "$status" = completed ] && [ "$conclusion" = success ] && [ "$runner" != none ]; then
		echo "pass canary workflow=$workflow branch=$branch run=$run head_sha=$head_sha conclusion=success runner=$runner"
	else
		echo "fail canary workflow=$workflow branch=$branch run=$run head_sha=$head_sha status=$status conclusion=$conclusion runners=${jobs:-none}"
		failures=$((failures + 1))
	fi
	finish "$failures"
}

# check_human_session: the wallet check-live harness tests/wallet/check-live.test.sh
# passes, then the wallet feature's human-session gate plans the golden intent
# at https://api-hull.paxeer.network from the wallet origin
# https://paxportwallet.com under the wallet identity assertion of
# CHECK_LIVE_HUMAN_ASSERTION, with CHECK_LIVE_HUMAN_ASSET and
# CHECK_LIVE_HUMAN_DESTINATION passed through; its check lines are printed as
# they come and any failure of either fails the subcommand.
check_human_session() {
	local output status=0 failures=0
	output="$("$repo_root/tests/wallet/check-live.test.sh" 2>&1)" || status=$?
	if [ "$status" -eq 0 ]; then
		echo "pass harness tests/wallet/check-live.test.sh"
	else
		echo "fail harness tests/wallet/check-live.test.sh exit=$status first=$(grep -m 1 '^FAIL ' <<<"$output" | cut -c1-160 || echo none)"
		failures=$((failures + 1))
	fi
	status=0
	CHECK_LIVE_HUMAN_BASE=https://api-hull.paxeer.network CHECK_LIVE_HUMAN_ORIGIN=https://paxportwallet.com \
		"$repo_root/scripts/wallet/check-live.sh" human-session || status=$?
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
# contract's spread of the paymaster's currentRate; the paymaster's
# rateUpdatedAt is no older than the station's max_rate_age at the latest
# block, which the machine's rate publisher keeps true; an info line reports
# the publisher's worst-case daily wei (rate_max_fee_per_gas times
# rate_gas_budget_per_day of its rate.json) and the wei its journal rate.jsonl
# records as spent on the latest block's chain day, none for what the machine
# does not answer; the account signs the
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
	local max_age updated head age updated_at rate_config journal worst spent
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
print(c["chain_id"], c["token"], len(c["endpoints"]), c["endpoints"][0], c["paymaster"], c["gas_limit"], c["max_priority_fee_per_gas"], c["max_rate_age"])
' 2>/dev/null)" || config=""
	read -r chain token endpoints first paymaster gas_limit priority max_age <<<"${config:-none none none none none none none none}"
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

	updated="$(gas_rpc eth_call "[{\"to\":\"$paymaster\",\"data\":\"$(cast sig 'rateUpdatedAt()')\"},\"latest\"]")" || updated=""
	head="$(gas_rpc eth_getBlockByNumber '["latest",false]')" || head=""
	read -r age updated_at <<<"$(python3 -c '
import json, sys
updated, head = int(sys.argv[1], 16), json.loads(sys.argv[2])
print(int(head["timestamp"], 16) - updated, updated)
' "$updated" "$head" 2>/dev/null || echo none none)"
	if [[ "$age" =~ ^[0-9]+$ && "$max_age" =~ ^[0-9]+$ ]] && [ "$updated_at" -gt 0 ] && [ "$age" -le "$max_age" ]; then
		echo "pass rate-age paymaster=$paymaster updated_at=$updated_at age=$age max_rate_age=$max_age"
	else
		echo "fail rate-age paymaster=$paymaster updated_at=${updated_at:-none} age=${age:-none} max_rate_age=$max_age"
		failures=$((failures + 1))
	fi
	rate_config="$(fly_ssh "$app" - "cat /data/gas-station/rate.json" </dev/null)" || rate_config=""
	worst="$(python3 -c '
import json, sys
c = json.loads(sys.argv[1])
print(int(c["rate_max_fee_per_gas"]) * int(c["rate_gas_budget_per_day"]))
' "$rate_config" 2>/dev/null)" || worst=none
	spent=none
	if journal="$(fly_ssh "$app" - "cat /data/gas-station/rate.jsonl" </dev/null)"; then
		spent="$(python3 -c '
import json, sys
day = int(json.loads(sys.argv[1])["timestamp"], 16) // 86400
signed, spent = {}, 0
for line in sys.stdin:
    if not line.strip():
        continue
    e = json.loads(line)
    if e["kind"] == "rate_published":
        signed[tuple(e["publication"]["hash"])] = e["publication"]["signed_at"] // 86400
    elif e["kind"] == "rate_settled" and signed.get(tuple(e["hash"])) == day:
        spent += int(e["settlement"]["cost_wei"])
print(spent)
' "$head" <<<"$journal" 2>/dev/null)" || spent=none
	fi
	echo "info rate-spend app=$app daily_wei_max=$worst spent_wei=$spent"

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
			if [ "$status" = 0 ] && [ "$code" = 503 ] && [[ "$body" == *'"state":"waiting-principals"'* ]]; then
				echo "fail readiness group=$group url=$url from=$kernel http=503 state=waiting-principals"
			else
			echo "fail readiness group=$group url=$url from=$kernel curl=${status:-none} http=${code:-none}"
			fi
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
# [decision.public_names] in the spec, which must be a host name. The
# wallet feature's cutover gate reads that name, its gateway gate reads the
# wallet gateway app of human/wallet/deploy/gateway.toml at its fly.dev name
# with CHECK_LIVE_GATEWAY_TOKEN passed through, and the app runs started
# machines in two regions. The gates' check lines are printed as they come
# without their summary lines, and a failing gate counts as one failure.
check_wallet() {
	local toml=human/wallet/deploy/gateway.toml spec=spec/paxeer-x-bringup/spec.kvx
	local app name output status failures=0
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

	name="$(sed -n '/^\[decision\.public_names\]$/,/^\[/s/^wallet_endpoint[[:space:]]*=[[:space:]]*"\(.*\)"[[:space:]]*$/\1/p' "$repo_root/$spec" 2>/dev/null | head -n 1)" || name=""
	if grep -Eq "$host_re" <<<"$name"; then
		echo "pass endpoint name=$name source=spec"
		status=0
		output="$(CHECK_LIVE_CUTOVER_HOST="$name" "$repo_root/scripts/wallet/check-live.sh" cutover 2>&1)" || status=$?
		grep -Ev '^check-live: (all checks passed|[0-9]+ check\(s\) failed)$' <<<"$output" || true
		[ "$status" -eq 0 ] || failures=$((failures + 1))
	else
		echo "fail endpoint source=spec name=unset"
		failures=$((failures + 1))
	fi

	status=0
	output="$(CHECK_LIVE_GATEWAY_BASE="https://$app.fly.dev" "$repo_root/scripts/wallet/check-live.sh" gateway 2>&1)" || status=$?
	grep -Ev '^check-live: (all checks passed|[0-9]+ check\(s\) failed)$' <<<"$output" || true
	[ "$status" -eq 0 ] || failures=$((failures + 1))

	fly_regions "$app" || failures=$((failures + 1))
	finish "$failures"
}

# check_router: the router URL https://api-mainnet-beta.paxeer.network serves
# the gateway of human/wallet/deploy/endpoint.toml in kernel mode. Through its
# /rpc: eth_chainId 0x7d, px_getNetwork kernel.available true and lx_getAccount
# answering a read for the bound account CHECK_LIVE_ROUTER_ACCOUNT. The app
# runs started machines in at least two regions (fly_regions); /readyz through the router,
# forced to the first started machine of each region with
# fly-force-instance-id, answers 200 with durable_store (its Redis round trip)
# and every configured backend ready. The wallet gateway of
# human/wallet/deploy/gateway.toml, read inside its machine, reports its
# rpc_pool up with the router URL first. One line per check.
check_router() {
	local url=https://api-mainnet-beta.paxeer.network account="${CHECK_LIVE_ROUTER_ACCOUNT:-}"
	local app wallet method params body answer machines region id code failures=0
	router_prerequisite
	if ! command -v flyctl >/dev/null 2>&1; then
		echo "check-live: flyctl is required" >&2
		exit 2
	fi
	if ! app="$(fly_app human/wallet/deploy/endpoint.toml)"; then
		echo "fail router toml=absent"
		finish 1
	fi

	for method in eth_chainId px_getNetwork lx_getAccount; do
		params='[]'
		if [ "$method" = lx_getAccount ]; then
			if [[ ! "$account" =~ ^[A-Za-z0-9:._-]+$ ]]; then
				echo "fail lx_getAccount account=${account:+invalid}${account:-unset}"
				failures=$((failures + 1))
				continue
			fi
			params="[\"$account\"]"
		fi
		body="$(curl -sS -m "$timeout" -H 'content-type: application/json' \
			-d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"$method\",\"params\":$params}" "$url/rpc" 2>/dev/null)" || body=""
		answer="$(python3 -c '
import json
import sys

method = sys.argv[1]
try:
    doc = json.loads(sys.stdin.read())
except ValueError:
    print("fail %s answer=unreadable" % method)
    sys.exit(0)
if not isinstance(doc, dict) or "result" not in doc:
    error = doc.get("error") if isinstance(doc, dict) else None
    print("fail %s error=%s" % (method, (error or {}).get("code", "none") if isinstance(error, dict) else "none"))
    sys.exit(0)
result = doc["result"]
if method == "eth_chainId":
    print("%s eth_chainId result=%s" % ("pass" if result == "0x7d" else "fail", result))
elif method == "px_getNetwork":
    kernel = (result or {}).get("kernel") if isinstance(result, dict) else None
    kernel = kernel if isinstance(kernel, dict) else {}
    ok = kernel.get("available") is True
    print("%s px_getNetwork kernel.available=%s reason=%s" % ("pass" if ok else "fail", json.dumps(kernel.get("available")), kernel.get("reason") or "none"))
else:
    ok = isinstance(result, dict) and len(result) > 0
    print("%s lx_getAccount read=%s" % ("pass" if ok else "fail", "answered" if ok else "empty"))
' "$method" <<<"$body")"
		echo "$answer"
		[ "${answer%% *}" = pass ] || failures=$((failures + 1))
	done

	machines="$(timeout "$timeout" flyctl machines list --app "$app" --json 2>/dev/null | python3 -c '
import json
import sys

first = {}
for machine in json.load(sys.stdin):
    if machine.get("state") == "started" and machine.get("id"):
        first.setdefault(machine.get("region") or "none", machine["id"])
for region in sorted(first):
    print(region, first[region])
' 2>/dev/null)" || machines=""
	fly_regions "$app" || failures=$((failures + 1))
	while read -r region id; do
		[ -n "$id" ] || continue
		body="$(curl -sS -m "$timeout" -H "fly-force-instance-id: $id" -w '\n%{http_code}' "$url/readyz" 2>/dev/null)" || body=""
		code="${body##*$'\n'}"
		answer="$(python3 -c '
import json
import sys

code = sys.argv[1]
try:
    backends = json.loads(sys.stdin.read()).get("backends") or {}
except (ValueError, AttributeError):
    print("fail http=%s backends=unreadable" % (code or "none"))
    sys.exit(0)
configured = sorted(n for n, b in backends.items() if (b or {}).get("reason") != "not_configured")
unready = [n for n in configured if (backends[n] or {}).get("state") != "ready"]
store = (backends.get("durable_store") or {}).get("state", "absent")
registry = (backends.get("program_registry") or {}).get("reason", "absent")
missing = registry in ("absent", "not_configured")
ok = code == "200" and not unready and store == "ready" and not missing
line = "http=%s durable_store=%s configured=%d/%d" % (code or "none", store, len(configured) - len(unready), len(configured))
print(("pass " if ok else "fail ") + line + (" unready=" + ",".join(unready) if unready else "") + (" missing=program_registry" if missing else ""))
' "$code" <<<"${body%$'\n'*}")"
		echo "${answer%% *} readyz app=$app region=$region ${answer#* }"
		[ "${answer%% *}" = pass ] || failures=$((failures + 1))
	done <<<"$machines"

	if ! wallet="$(fly_app human/wallet/deploy/gateway.toml)"; then
		echo "fail wallet-gateway toml=absent"
		finish $((failures + 1))
	fi
	body="$(fly_ssh "$wallet" - 'node -e "fetch(\"http://127.0.0.1:8080/readyz\").then(r => r.text()).then(t => process.stdout.write(t))"' </dev/null | tail -n 1)" || body=""
	answer="$(python3 -c '
import json
import sys

router = sys.argv[1]
try:
    pool = json.loads(sys.stdin.read())["components"]["rpc_pool"]
except (ValueError, KeyError, TypeError):
    print("fail rpc_pool=unreadable")
    sys.exit(0)
endpoints = pool.get("endpoints") or []
first = (endpoints[0].get("url") or "").rstrip("/") if endpoints else ""
place = "router" if first == router else ("other" if first else "none")
ok = pool.get("state") == "up" and place == "router"
print("%s rpc_pool=%s healthy=%s first=%s" % ("pass" if ok else "fail", pool.get("state") or "none", pool.get("healthy", 0), place))
' "$url" <<<"$body")"
	echo "${answer%% *} wallet-gateway app=$wallet ${answer#* }"
	[ "${answer%% *}" = pass ] || failures=$((failures + 1))
	finish "$failures"
}

# check_kernel_boundaries: inside the machine of the kernel app of
# human/wallet/deploy/human.toml, the core boundary (9443), its admin plane
# (9444), the receipt authority (9445) and the agent boundary (9446) each
# answer /readyz 200 with "ready":true over TLS verified under the internal CA
# at the app's .internal name, presenting the agentd-client identity that
# tools/bringup/ca.sh issued to the app's volume; and no service of any
# machine of the app exposes one of those ports. One line per check.
check_kernel_boundaries() {
	local app answer n_machines ports exposed port name line code body attempt failures=0
	local -A names=([9443]=core [9444]=core-admin [9445]=receipt-authority [9446]=agent-boundary)
	if ! app="$(fly_app human/wallet/deploy/human.toml)"; then
		echo "fail kernel-boundaries toml=absent"
		finish 1
	fi
	answer="$(timeout "$timeout" flyctl machines list --app "$app" --json 2>/dev/null | python3 -c '
import json, sys
ms = json.load(sys.stdin)
ports = sorted({s.get("internal_port") for m in ms for s in (m.get("config") or {}).get("services") or [] if s.get("internal_port")})
print(len(ms), ",".join(str(p) for p in ports) or "none")
' 2>/dev/null)" || answer=""
	read -r n_machines ports <<<"${answer:-none none}"
	exposed=""
	for port in 9443 9444 9445 9446; do
		[[ ",$ports," != *",$port,"* ]] || exposed="${exposed:+$exposed,}$port"
	done
	if [ "$n_machines" != none ] && [ "$n_machines" -gt 0 ] && [ -z "$exposed" ]; then
		echo "pass public-services app=$app machines=$n_machines ports=$ports boundaries=none"
	else
		echo "fail public-services app=$app machines=$n_machines ports=$ports boundaries=${exposed:-none}"
		failures=$((failures + 1))
	fi
	answer=""
	for attempt in 1 2; do
		answer="$(fly_ssh "$app" - "d=$fly_tls_dir/agentd-client; for p in 9443 9444 9445 9446; do printf \"%s \" \$p; curl -sS -m $timeout --cacert \$d/ca.pem --cert \$d/cert.pem --key \$d/key.pem -w \" %{http_code}\" https://$app.internal:\$p/readyz 2>/dev/null | tr -d \"\\n\"; echo; done")" || true
		[ "$(grep -c ' 200$' <<<"$answer")" != 4 ] || break
		[ "$attempt" = 2 ] || sleep 10
	done
	for port in 9443 9444 9445 9446; do
		name="${names[$port]}"
		line="$(grep -m 1 "^$port " <<<"$answer")" || line="$port "
		line="${line#"$port "}"
		code="${line##* }"
		body="${line% *}"
		[[ "$code" =~ ^[0-9]{3}$ ]] || code=none
		if [ "$code" = 200 ] && [[ "$body" == *'"ready":true'* ]]; then
			echo "pass $name app=$app url=https://$app.internal:$port/readyz identity=agentd-client http=200 ready=true"
		else
			echo "fail $name app=$app url=https://$app.internal:$port/readyz identity=agentd-client http=$code"
			failures=$((failures + 1))
		fi
	done
	finish "$failures"
}

# check_human: the human service of the kernel app answers at
# CHECK_LIVE_HUMAN_BASE, by default https://api-hull.paxeer.network, as the
# wallet origin CHECK_LIVE_HUMAN_ORIGIN, by default https://paxportwallet.com:
# the live, preflight and plan checks of scripts/wallet/check-live.sh human,
# /readyz 200 with ready true, and the passkey registration options of the
# probe account CHECK_LIVE_HUMAN_PROBE_EMAIL, created under an idempotency key
# derived from that address so every run converges on one account, naming the
# origin's host as rp.id. One line per check.
check_human() {
	local base="${CHECK_LIVE_HUMAN_BASE:-https://api-hull.paxeer.network}"
	local origin="${CHECK_LIVE_HUMAN_ORIGIN:-https://paxportwallet.com}"
	local email="${CHECK_LIVE_HUMAN_PROBE_EMAIL:-}" rp="${origin#https://}"
	local failures=0 line answer code body account key
	base="${base%/}"
	while IFS= read -r line; do
		case "$line" in
		pass\ *) echo "$line" ;;
		fail\ *)
			echo "$line"
			failures=$((failures + 1))
			;;
		esac
	done < <(CHECK_LIVE_HUMAN_BASE="$base" CHECK_LIVE_HUMAN_ORIGIN="$origin" CHECK_LIVE_TIMEOUT="$timeout" "$repo_root/scripts/wallet/check-live.sh" human 2>&1 || true)
	answer="$(curl -sS --max-time "$timeout" -H "origin: $origin" -w ' %{http_code}' "$base/readyz" 2>/dev/null)" || answer=""
	code="${answer##* }"
	body="${answer% *}"
	if [ "$code" = 200 ] && printf '%s' "$body" | python3 -c 'import json, sys; sys.exit(0 if json.load(sys.stdin)["result"]["ready"] is True else 1)' 2>/dev/null; then
		echo "pass readyz http=200 ready=true"
	else
		echo "fail readyz http=${code:-none} $(printf '%s' "$body" | tr '\n' ' ' | cut -c1-200)"
		failures=$((failures + 1))
	fi
	if [ -z "$email" ]; then
		echo "fail rp-id email=unset"
		finish $((failures + 1))
	fi
	key="$(printf 'check-live human %s' "$email" | sha256sum | cut -c1-32)"
	answer="$(python3 -c 'import json, sys; print(json.dumps({"email": sys.argv[1], "display_name": "check-live human"}))' "$email" |
		curl -sS --max-time "$timeout" -X POST -H "origin: $origin" -H 'content-type: application/json' -H "idempotency-key: $key" --data-binary @- -w ' %{http_code}' "$base/v1/accounts" 2>/dev/null)" || answer=""
	code="${answer##* }"
	account="$(printf '%s' "${answer% *}" | python3 -c 'import json, sys; print(json.load(sys.stdin)["result"]["account_id"])' 2>/dev/null)" || account=""
	if [ -z "$account" ]; then
		echo "fail rp-id account=none http=${code:-none}"
		finish $((failures + 1))
	fi
	answer="$(python3 -c 'import json, sys; print(json.dumps({"account_id": sys.argv[1]}))' "$account" |
		curl -sS --max-time "$timeout" -X POST -H "origin: $origin" -H 'content-type: application/json' --data-binary @- -w ' %{http_code}' "$base/v1/passkeys/registrations" 2>/dev/null)" || answer=""
	code="${answer##* }"
	answer="$(printf '%s' "${answer% *}" | python3 -c '
import base64, json, sys
ceremony = json.load(sys.stdin)["result"]["ceremony"]
print(json.loads(base64.urlsafe_b64decode(ceremony + "=" * (-len(ceremony) % 4)))["rp"]["id"])
' 2>/dev/null)" || answer=""
	if [ "$answer" = "$rp" ]; then
		echo "pass rp-id http=$code rp.id=$answer"
	else
		echo "fail rp-id http=${code:-none} rp.id=${answer:-none} want=$rp"
		failures=$((failures + 1))
	fi
	finish "$failures"
}

# kernel_roster: the expected service roster of the kernel app's launch
# contract, the full profile docker/kernel/init.sh runs from the final stage of
# docker/kernel/Dockerfile that human/wallet/deploy/human.toml builds. One row
# per role: name, uid, "genesis" when the role's waits name a genesis output
# (so waiting on the genesis is its permitted bootstrap state) or "-", and the
# role's material boundary as comma-separated path=uid:gid[:mode] entries, or
# "-" for a role that holds no private material.
kernel_roster() {
	cat <<'ROSTER'
treasury-signer 4020 genesis /data/layerx/keys/treasury.key=4020:4020:400
layerxd 4020 genesis /run/layerx/node=4020:4020:2750
layerxd-authority 4020 genesis /run/layerx/node=4020:4020:2750
guarantor-1 4021 genesis /data/layerx/guarantor-1/state=4020:4020:2770,/data/tls/guarantor=4021:4020
guarantor-2 4021 genesis /data/layerx/guarantor-2/state=4020:4020:2770,/data/tls/guarantor=4021:4020
paxeer-hop-1 4020 - -
paxeer-boundary-loopback 4020 - /data/tls/paxeer-boundary-loopback=4020:4020
paxeer-hop-2 4020 - -
paxeer-boundary-public 4020 - /data/tls/paxeer-boundary-public=4020:4020
paxeer-relay 4020 - -
core-boundary 4021 genesis /data/layerx/core=4021:4020:2700,/data/tls/pending-core=4021:4020,/data/tls/pending-core-admin=4021:4020
receipt-authority 4021 genesis /run/authority-private=4021:4020:700,/data/tls/receipt-authority=4021:4020
agent-boundary 4021 genesis /data/layerx/agent-boundary=4021:4020:2700,/data/tls/agent-boundary=4021:4020
human-kms 4026 genesis /run/human-private/kms=4026:4020:700,/data/human-state/kms=4026:4020:700,/run/layerx/human-material/human-kms=4026:4020:2500
human-components 4020 genesis /run/human-private/components=4020:4020:700,/data/human-state/components=4020:4020:700,/run/layerx/human-material/human-components=4020:4020:2500
human-identity 4020 genesis /run/human-private/identity=4020:4020:700,/data/human-state/identity=4020:4020:700,/run/layerx/human-material/human-identity=4020:4020:2500
human-security 4020 genesis /run/human-private/security=4020:4020:700,/data/human-state/security=4020:4020:700,/run/layerx/human-material/human-security=4020:4020:2500
human-movement 4020 genesis /run/human-private/movement=4020:4020:700,/data/human-state/movement=4020:4020:700,/run/layerx/human-material/human-movement=4020:4020:2500
human-owner 4021 genesis /run/human-private/agent=4021:4020:700,/data/human-state/agent=4021:4020:700,/run/layerx/human-material/human-owner=4021:4020:2500
human 4020 - /run/human-private/service=4020:4020:700
human-tls 4020 - /run/human-private/service=4020:4020:700,/data/tls/human=4020:4020
mirror-signer 4021 genesis /run/mirror-signer=4021:4020:700
mirror-publisher 4021 genesis /run/mirror-publisher=4021:4020:700
relay-archive 4020 genesis /data/layerx/relay-archive=4020:4020:2700,/data/tls/relay-archive=4020:4020
ROSTER
}

# kernel_roster_paths: every material path of the roster once, space separated.
kernel_roster_paths() {
	kernel_roster | awk '$4 != "-" { n = split($4, items, ","); for (i = 1; i <= n; i++) { sub(/=.*/, "", items[i]); if (!seen[items[i]]++) printf "%s%s", (out++ ? " " : ""), items[i] } }'
}

# kernel_app_probe: run as root inside the kernel machine with the init status
# directory and the roster's material paths as arguments, prints one JSON
# object: the init pid, every status record, every process descending from the
# init with its uids, gids, groups, no_new_privs flag and parent, and the
# owner, group, mode and type of each material path (not followed). A zombie
# or dead process is reported with its state and never counts as running.
# shellcheck disable=SC2016 # Python source
kernel_app_probe='import json, os, stat, sys
status, paths = sys.argv[1], sys.argv[2:]
out = {"init": None, "status": {}, "procs": {}, "material": {}}
def proc(pid):
    try:
        fields = {}
        with open("/proc/%d/status" % pid) as handle:
            for line in handle:
                key, _, value = line.partition(":")
                fields[key] = value.strip()
        with open("/proc/%d/cmdline" % pid, "rb") as handle:
            cmdline = handle.read(4096).replace(b"\0", b" ").decode("utf-8", "replace").strip()
        return {"ppid": int(fields["PPid"]), "uids": [int(x) for x in fields["Uid"].split()],
                "gids": [int(x) for x in fields["Gid"].split()], "groups": [int(x) for x in fields.get("Groups", "").split()],
                "nnp": int(fields.get("NoNewPrivs", "0")), "comm": fields.get("Name", ""), "state": fields.get("State", "?")[:1],
                "cmdline": cmdline[:240]}
    except (OSError, ValueError, KeyError):
        return None
try:
    with open(os.path.join(status, "pid")) as handle:
        out["init"] = int(handle.read(64).strip())
except (OSError, ValueError):
    pass
try:
    names = sorted(os.listdir(status))
except OSError:
    names = []
for name in names:
    if name == "pid":
        continue
    path = os.path.join(status, name)
    try:
        if not stat.S_ISREG(os.lstat(path).st_mode):
            out["status"][name] = "!not-a-file"
            continue
        with open(path) as handle:
            out["status"][name] = handle.read(4096).split("\n", 1)[0]
    except OSError:
        out["status"][name] = "!unreadable"
table = {}
for entry in os.listdir("/proc"):
    if entry.isdigit():
        info = proc(int(entry))
        if info is not None:
            table[int(entry)] = info
if out["init"] in table:
    keep, frontier = {out["init"]}, [out["init"]]
    while frontier:
        parent = frontier.pop()
        for pid, info in table.items():
            if info["ppid"] == parent and pid not in keep:
                keep.add(pid)
                frontier.append(pid)
    out["procs"] = {str(pid): table[pid] for pid in sorted(keep)}
for path in paths:
    try:
        info = os.lstat(path)
    except OSError:
        out["material"][path] = None
        continue
    kind = "dir" if stat.S_ISDIR(info.st_mode) else "file" if stat.S_ISREG(info.st_mode) else "link" if stat.S_ISLNK(info.st_mode) else "other"
    out["material"][path] = [kind, info.st_uid, info.st_gid, format(stat.S_IMODE(info.st_mode), "o")]
print(json.dumps(out, sort_keys=True))
'

# kernel_app_verdict <app> <candidate>: reads the kernel_app_probe JSON on
# stdin and compares it to kernel_roster. Prints one line per check, the
# candidate identity and the role-specific reason on every role line, then
# "verdict <failures> <waiting-genesis>". A role passes when its status names
# its roster uid, its recorded process is alive under a service supervisor of
# the init, every process of its tree runs under exactly that uid with gid 4020,
# no supplementary group and no_new_privs (the pid-namespace unshare wrapper of
# the recorded pid alone excepted), and every material path of its row has
# the row's type, owner, group and mode. A missing, unexpected or wrong-UID
# role, a live role process no status records (a duplicate launch), a
# non-genesis wait, and a genesis wait of a role whose row does not permit it
# each fail; a permitted genesis wait is counted, never passed as running.
kernel_app_verdict() {
	python3 -c '
import json, sys
app, candidate, roster_text = sys.argv[1], sys.argv[2], sys.argv[3]
lines, failures, waiting, running = [], 0, 0, 0
def fail(text):
    global failures
    failures += 1
    lines.append("fail " + text + " candidate=" + candidate)
roster = {}
for row in roster_text.splitlines():
    fields = row.split()
    if len(fields) != 4 or not fields[1].isdigit() or fields[2] not in ("genesis", "-"):
        fail("roster row=%s reason=malformed-row" % (fields[0] if fields else "empty"))
        continue
    if fields[0] in roster:
        fail("roster row=%s reason=duplicate-row" % fields[0])
        continue
    material = []
    if fields[3] != "-":
        for item in fields[3].split(","):
            path, _, spec = item.partition("=")
            want = spec.split(":")
            if not path.startswith("/") or len(want) not in (2, 3) or not all(x.isdigit() for x in want):
                fail("roster row=%s reason=malformed-material item=%s" % (fields[0], item))
                continue
            material.append((path, want))
    roster[fields[0]] = (int(fields[1]), fields[2] == "genesis", material)
try:
    probe = json.loads(sys.stdin.read())
    init = probe["init"]
    procs = {int(pid): info for pid, info in probe["procs"].items()}
    records = probe["status"]
    found = probe["material"]
except (ValueError, KeyError, TypeError, AttributeError):
    probe = None
if probe is None or init not in procs:
    fail("init app=%s status=absent" % app)
    print("\n".join(lines))
    print("verdict %d 0" % failures)
    sys.exit(0)
info = procs[init]
if info["uids"][1] == 0 and any(word.endswith("kernel-init") for word in info["cmdline"].split(" ")[:2]):
    lines.append("pass init app=%s uid=0 entrypoint=kernel-init pid=%d" % (app, init))
else:
    fail("init app=%s uid=%d entrypoint=%s" % (app, info["uids"][1], info["cmdline"].split(" ", 1)[0] or "none"))
children = {}
for pid, value in procs.items():
    children.setdefault(value["ppid"], []).append(pid)
def tree(root):
    seen, frontier = [root], [root]
    while frontier:
        for child in children.get(frontier.pop(), []):
            seen.append(child)
            frontier.append(child)
    return seen
recorded = {}
for name in sorted(set(records) - set(roster)):
    fields = records[name].split(" ")
    if len(fields) == 3 and fields[1] == "running" and fields[2].isdigit():
        recorded.setdefault(int(fields[2]), []).append(name)
    fail("role %s state=unexpected status=%s reason=not-in-roster" % (name, records[name].replace(" ", "_") or "empty"))
for name, (want, genesis, material) in roster.items():
    head = "role %s uid=%d" % (name, want)
    if name not in records:
        fail(head + " state=missing reason=no-status")
        continue
    fields = records[name].split(" ", 2)
    if len(fields) < 3 or not fields[0].isdigit():
        fail(head + " state=malformed status=%s reason=malformed-status" % (records[name].replace(" ", "_") or "empty"))
        continue
    uid, state, detail = int(fields[0]), fields[1], fields[2]
    if uid != want:
        if state == "running" and detail.isdigit():
            recorded.setdefault(int(detail), []).append(name)
        fail("role %s uid=%d want=%d state=%s reason=wrong-uid" % (name, uid, want, state))
        continue
    if state == "waiting":
        if detail == "genesis" and genesis:
            waiting += 1
            lines.append("wait " + head + " state=waiting-genesis candidate=" + candidate)
        elif detail == "genesis":
            fail(head + " state=waiting on=genesis reason=genesis-wait-not-permitted")
        else:
            fail(head + " state=waiting on=%s reason=waiting" % detail.replace(" ", "_"))
        continue
    if state != "running" or not detail.isdigit():
        fail(head + " state=%s reason=malformed-status" % state)
        continue
    pid = int(detail)
    recorded.setdefault(pid, []).append(name)
    if pid not in procs or procs[pid]["state"] in ("Z", "X"):
        fail(head + " state=running pid=%d reason=process-absent" % pid)
        continue
    supervisor = procs[pid]["ppid"]
    if supervisor not in procs or procs[supervisor]["ppid"] != init:
        fail(head + " state=running pid=%d reason=unsupervised" % pid)
        continue
    problems = []
    members = tree(pid)
    role = [p for p in members if not (p == pid and procs[p]["comm"] == "unshare" and set(procs[p]["uids"]) == {0})]
    if not role:
        problems.append("reason=no-role-process")
    for p in role:
        value = procs[p]
        if set(value["uids"]) != {want}:
            problems.append("reason=process-uid pid=%d uids=%s comm=%s" % (p, ",".join(map(str, value["uids"])), value["comm"]))
        elif set(value["gids"]) != {4020} or value["groups"] or value["nnp"] != 1:
            problems.append("reason=process-privileges pid=%d gids=%s groups=%s no_new_privs=%d" % (
                p, ",".join(map(str, value["gids"])), ",".join(map(str, value["groups"])) or "none", value["nnp"]))
    for path, spec in material:
        seen = found.get(path)
        if seen is None:
            problems.append("reason=material-absent path=%s" % path)
            continue
        observed = [str(seen[1]), str(seen[2]), seen[3]][:len(spec)]
        if seen[0] not in ("dir", "file") or observed != spec:
            problems.append("reason=material path=%s want=%s observed=%s:%s" % (path, ":".join(spec), seen[0], ":".join(map(str, seen[1:]))))
    if problems:
        for problem in problems:
            fail(head + " state=running pid=%d " % pid + problem)
        continue
    running += 1
    lines.append("pass " + head + " state=running pid=%d processes=%d material=%d candidate=%s" % (pid, len(role), len(material), candidate))
for pid, names in sorted(recorded.items()):
    if len(names) > 1:
        fail("role %s state=running pid=%d reason=duplicate-process shared=%s" % (names[0], pid, ",".join(names)))
for pid, value in sorted(procs.items()):
    parent = procs.get(value["ppid"])
    if (pid not in recorded and pid != init and parent is not None and parent["ppid"] == init
            and (set(value["uids"]) != {0} or value["comm"] in ("unshare", "setpriv"))):
        fail("role - state=running pid=%d uids=%s comm=%s reason=unrecorded-role-process" % (
            pid, ",".join(map(str, value["uids"])), value["comm"]))
if failures == 0 and waiting:
    lines.append("bootstrap app=%s candidate=%s state=pre-genesis roles=%d running=%d waiting-genesis=%d operational=no" % (
        app, candidate, len(roster), running, waiting))
elif failures == 0:
    lines.append("pass roster app=%s candidate=%s roles=%d running=%d operational=yes" % (app, candidate, len(roster), running))
print("\n".join(lines))
print("verdict %d %d" % (failures, waiting))
' "$1" "$2" "$(kernel_roster)"
}

# Sourced by tools/bringup/ca.sh for the Fly helpers and the CA settings: the
# probe's own dispatch below runs only when this file is executed.
[ "${BASH_SOURCE[0]}" = "$0" ] || return 0

# check_kernel_app: the kernel app of human/wallet/deploy/human.toml runs
# exactly two machines, both started, each with its own volume mounted at
# /data, from one candidate image whose identity every line records; the init of
# docker/kernel/init.sh runs as root as the entrypoint; and the roles the init
# records under /run/layerx/init are exactly the kernel_roster, each with its
# process and material boundary (kernel_app_verdict). One line per check.
# Exits 0 when every role runs, 3 when the only roles not running are roster
# roles permitted to wait on the genesis (the pre-genesis bootstrap state,
# never reported as operational), and 1 on any failure.
check_kernel_app() {
	local app answer n_machines n_started n_data candidate verdict failures waiting
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
def identity(m):
    ref = m.get("image_ref") or {}
    if ref.get("digest"):
        return "%s@%s" % (ref.get("repository") or "image", ref["digest"])
    return (m.get("config") or {}).get("image") or "none"
ids = sorted({identity(m) for m in started})
print(len(ms), len(started), len(data), ids[0] if len(ids) == 1 and " " not in ids[0] else "none")
' 2>/dev/null)" || answer=""
	read -r n_machines n_started n_data candidate <<<"${answer:-none none none none}"
	if [ "$n_machines" = 2 ] && [ "$n_started" = 2 ] && [ "$n_data" = 2 ] && [ "$candidate" != none ]; then
		echo "pass machines app=$app machines=2 started=2 volume=/data candidate=$candidate"
	else
		echo "fail machines app=$app machines=$n_machines started=$n_started volume-at-data=$n_data candidate=$candidate"
		finish 1
	fi
	answer="$(printf '%s' "$kernel_app_probe" | fly_ssh "$app" - "python3 - /run/layerx/init $(kernel_roster_paths)")" || answer=""
	verdict="$(kernel_app_verdict "$app" "$candidate" <<<"$answer")" || verdict=""
	read -r _ failures waiting <<<"$(grep '^verdict ' <<<"$verdict" || true)" || true
	if ! [[ "${failures:-}" =~ ^[0-9]+$ && "${waiting:-}" =~ ^[0-9]+$ ]]; then
		echo "fail roster app=$app candidate=$candidate verdict=unreadable"
		finish 1
	fi
	sed '/^verdict /d' <<<"$verdict"
	if [ "$failures" -eq 0 ] && [ "$waiting" -gt 0 ]; then
		echo "check-live: pre-genesis bootstrap, $waiting role(s) waiting on the genesis; not operational"
		exit 3
	fi
	finish "$failures"
}

# check_mirrors: inside the kernel machine of human/wallet/deploy/human.toml
# the mirror publisher's status listener, CHECK_LIVE_MIRRORS_STATUS
# (host:port, default 127.0.0.1:9456, the status_listen docker/kernel/init.sh
# runs it on), answers GET /readyz 200 ready; the verifier config
# CHECK_LIVE_MIRRORS_VERIFY_CONFIG names the Ethereum and the Solana mirror
# as sources, with their RPC endpoints, CA and bearer files; and
# layerx-mirror-verify (CHECK_LIVE_MIRRORS_VERIFY_BIN, default the one on
# PATH) verifies the receipt of CHECK_LIVE_MIRRORS_REQUEST from the mirrors
# alone, with LAYERX_NODE_URL, LAYERX_GATEWAY_URL and
# LAYERX_EXPLORER_API_ORIGIN removed from its environment, bounded by
# CHECK_LIVE_MIRRORS_VERIFY_TIMEOUT seconds (default 300). One line per check.
check_mirrors() {
	local status="${CHECK_LIVE_MIRRORS_STATUS:-127.0.0.1:9456}"
	local verify="${CHECK_LIVE_MIRRORS_VERIFY_BIN:-layerx-mirror-verify}"
	local limit="${CHECK_LIVE_MIRRORS_VERIFY_TIMEOUT:-300}"
	local name app reply rc code body answer eth sol failures=0
	# shellcheck disable=SC2016 # the script expands on the machine
	local script='rc=0
body=$(curl -sS -m "$limit" -w "\n%{http_code}" "http://$status/readyz" 2>/dev/null) || rc=$?
echo "@@readyz $rc $(printf "%s" "$body" | tail -n 1) $(printf "%s" "$body" | head -n 1 | tr -d " " | cut -c1-120)"'
	for name in CHECK_LIVE_MIRRORS_VERIFY_CONFIG CHECK_LIVE_MIRRORS_REQUEST; do
		if [ -z "${!name:-}" ]; then
			echo "check-live: $name is unset" >&2
			exit 2
		elif [ ! -r "${!name}" ]; then
			echo "check-live: $name does not name a readable file" >&2
			exit 2
		fi
	done
	for name in flyctl "$verify"; do
		if ! command -v "$name" >/dev/null 2>&1; then
			echo "check-live: $name is required" >&2
			exit 2
		fi
	done
	if ! app="$(fly_app human/wallet/deploy/human.toml)"; then
		echo "fail mirrors toml=absent"
		finish 1
	fi

	reply="$(printf '%s\n' "$script" | fly_ssh "$app" - "status=$status limit=$timeout sh -s")" || reply=""
	read -r rc code body <<<"$(sed -n 's/^@@readyz //p' <<<"$reply" | head -n 1)"
	if [ "${rc:-}" = 0 ] && [ "${code:-}" = 200 ] && [[ "${body:-}" == *'"ready":true'* ]]; then
		echo "pass mirror-readyz app=$app listen=$status http=200 ready=true"
	else
		echo "fail mirror-readyz app=$app listen=$status curl=${rc:-none} http=${code:-none}"
		failures=$((failures + 1))
	fi

	answer="$(python3 -c '
import json, sys
kinds = [s.get("kind") for s in json.load(open(sys.argv[1])).get("sources", [])]
print(kinds.count("ethereum"), kinds.count("solana"))
' "$CHECK_LIVE_MIRRORS_VERIFY_CONFIG" 2>/dev/null)" || answer=""
	read -r eth sol <<<"${answer:-none none}"
	if [ "$eth" != none ] && [ "$eth" -ge 1 ] && [ "$sol" -ge 1 ]; then
		echo "pass mirror-sources ethereum=$eth solana=$sol"
	else
		echo "fail mirror-sources ethereum=$eth solana=$sol"
		failures=$((failures + 1))
	fi

	answer="$(env -u LAYERX_NODE_URL -u LAYERX_GATEWAY_URL -u LAYERX_EXPLORER_API_ORIGIN \
		timeout "$limit" "$verify" "$CHECK_LIVE_MIRRORS_VERIFY_CONFIG" <"$CHECK_LIVE_MIRRORS_REQUEST" 2>/dev/null)" || true
	answer="$(python3 -c '
import json, sys
try:
    r = json.loads(sys.stdin.read())
except ValueError:
    print("fail error=unreadable")
    sys.exit(0)
v = r.get("verification") or {}
if r.get("ok") is True and v.get("provenance") == "Canonical" and v.get("sourceId"):
    print("pass source=%s batch=%s provenance=Canonical level=%s" % (v["sourceId"], v.get("batchNumber"), v.get("level")))
else:
    print("fail error=%s provenance=%s" % (r.get("error", "none"), v.get("provenance", "none")))
' <<<"$answer")"
	echo "${answer%% *} mirror-verify ${answer#* }"
	[ "${answer%% *}" = pass ] || failures=$((failures + 1))
	finish "$failures"
}

# check_relay: the relay archive of platform/relay_archive/fly.toml against
# its origin, the relay-archive service the kernel app of
# human/wallet/deploy/human.toml runs beside the sequencer. Reads the origin
# inside the kernel machine over its internal-CA TLS listener (head, pins,
# readiness, the genesis.manifest digest and the bytes of the batch one below
# its head), then the public route CHECK_LIVE_RELAY_ORIGIN, then every
# started relay machine through flyctl ssh console --machine, then the origin
# again: the app runs at least two started machines in two regions, and each
# replica and the public route answer ready and fresh, carry the origin's
# network, genesis and sequencer pins with the genesis equal to the kernel's
# manifest, hold a head no more than one batch behind the first origin read
# and no further than the second, and serve the origin's exact batch bytes.
# The original signed activity CHECK_LIVE_RELAY_ACTIVITY is forwarded to the
# router with the key in CHECK_LIVE_RELAY_KEY_FILE and must be answered with
# CHECK_LIVE_RELAY_ACTIVITY_ID; the same bytes with the last byte flipped must
# be refused with a 4xx. One line per check.
relay_listen=127.0.0.1:8080
relay_origin_listen=127.0.0.1:9457
relay_probe='import hashlib, json, ssl, sys, urllib.error, urllib.request
base, cafile, manifest, target, limit = sys.argv[1:6]
context = None
if base.startswith("https:"):
    context = ssl.create_default_context(cafile=None if cafile == "-" else cafile)
def get(path):
    try:
        with urllib.request.urlopen(base + path, timeout=float(limit), context=context) as answer:
            return answer.status, answer.read(16777216)
    except urllib.error.HTTPError as error:
        return error.code, error.read(1048576)
out = {}
try:
    code, body = get("/readyz")
    ready = json.loads(body)
    out.update(readyz=code, ready=ready.get("ready") is True, freshness=ready.get("freshness"))
    head = json.loads(get("/v1/sync/head")[1])
    out.update(network=head.get("network_id"), genesis=head.get("genesis_sha256"), head=head.get("head_batch"))
    out["sequencer"] = json.loads(get("/v1/sync/readiness")[1]).get("sequencer_public_key")
    if manifest != "-":
        with open(manifest, "rb") as source:
            out["manifest"] = hashlib.sha256(source.read()).hexdigest()
    if target == "-" and out["head"] is not None:
        target = str(max(int(out["head"]) - 1, 1))
    if target != "-":
        code, body = get("/v1/sync/batches/" + target)
        out.update(target=target, batch=hashlib.sha256(body).hexdigest() if code == 200 else None)
except (OSError, ValueError, TypeError, AttributeError) as error:
    out["error"] = type(error).__name__
print("@@relay " + json.dumps(out, sort_keys=True))'

check_relay() {
	local origin="${CHECK_LIVE_RELAY_ORIGIN:-https://archive.paxeer.network}"
	local ca="${CHECK_LIVE_RELAY_CA:--}"
	local name app kernel records ids id region reply target answer code activity refusal failures=0
	for name in CHECK_LIVE_RELAY_ACTIVITY CHECK_LIVE_RELAY_KEY_FILE; do
		if [ -z "${!name:-}" ]; then
			echo "check-live: $name is unset" >&2
			exit 2
		elif [ ! -r "${!name}" ] || [ ! -s "${!name}" ]; then
			echo "check-live: $name does not name a readable file" >&2
			exit 2
		fi
	done
	if ! [[ "${CHECK_LIVE_RELAY_ACTIVITY_ID:-}" =~ ^[A-Za-z0-9_:-]{1,256}$ ]]; then
		echo "check-live: CHECK_LIVE_RELAY_ACTIVITY_ID is unset or malformed" >&2
		exit 2
	fi
	if ! [[ "$origin" =~ ^https://[a-z0-9.-]+(:[0-9]+)?$ || "$origin" =~ ^http://(127\.0\.0\.1|\[::1\]):[0-9]+(/[a-z0-9]+)?$ ]]; then
		echo "check-live: CHECK_LIVE_RELAY_ORIGIN must be an https origin or a loopback http origin" >&2
		exit 2
	fi
	if [ "$ca" != - ] && [ ! -r "$ca" ]; then
		echo "check-live: CHECK_LIVE_RELAY_CA does not name a readable file" >&2
		exit 2
	fi
	if ! app="$(fly_app platform/relay_archive/fly.toml)" || ! kernel="$(fly_app human/wallet/deploy/human.toml)"; then
		echo "fail relay toml=absent"
		finish 1
	fi

	reply="$(printf '%s\n' "$relay_probe" | fly_ssh "$kernel" - "python3 - https://$relay_origin_listen /data/tls/relay-archive/ca.pem /data/layerx/node/genesis/genesis.manifest - $timeout")" || reply=""
	records="origin1 $(sed -n 's/^@@relay //p' <<<"$reply" | head -n 1)"
	target="$(python3 -c 'import json, sys; print(json.loads(sys.argv[1] or "{}").get("target") or "-")' "${records#origin1 }" 2>/dev/null)" || target=-
	reply="$(python3 -c "$relay_probe" "$origin" "$ca" - "$target" "$timeout" 2>/dev/null)" || reply=""
	records+=$'\n'"public $(sed -n 's/^@@relay //p' <<<"$reply" | head -n 1)"

	fly_regions "$app" || failures=$((failures + 1))
	ids="$(timeout "$timeout" flyctl machines list --app "$app" --json 2>/dev/null | python3 -c '
import json, sys
for m in json.load(sys.stdin):
    if m.get("state") == "started":
        print(m.get("id", ""), m.get("region", "") or "none")
' 2>/dev/null)" || ids=""
	while read -r id region; do
		[ -n "$id" ] || continue
		reply="$(printf '%s\n' "$relay_probe" | timeout "$timeout" flyctl ssh console --quiet --app "$app" --machine "$id" \
			--command "sh -c 'python3 - http://$relay_listen - - $target $timeout'" 2>/dev/null)" || reply=""
		records+=$'\n'"replica:$id:$region $(sed -n 's/^@@relay //p' <<<"$reply" | head -n 1)"
	done <<<"$ids"

	reply="$(printf '%s\n' "$relay_probe" | fly_ssh "$kernel" - "python3 - https://$relay_origin_listen /data/tls/relay-archive/ca.pem /data/layerx/node/genesis/genesis.manifest - $timeout")" || reply=""
	records+=$'\n'"origin2 $(sed -n 's/^@@relay //p' <<<"$reply" | head -n 1)"

	answer="$(python3 -c '
import json, sys
origin1, public, origin2, replicas = {}, {}, {}, []
for line in sys.argv[3].splitlines():
    tag, _, doc = line.partition(" ")
    try:
        value = json.loads(doc)
    except ValueError:
        value = {}
    value = value if isinstance(value, dict) else {}
    if tag.startswith("replica:"):
        replicas.append((tag.split(":")[1], tag.split(":")[2], value))
    else:
        {"origin1": origin1, "public": public, "origin2": origin2}[tag].update(value)
def number(value):
    try:
        return int(value)
    except (TypeError, ValueError):
        return None
def fresh(row):
    return row.get("readyz") == 200 and row.get("ready") is True and row.get("freshness") == "fresh"
failures = 0
def emit(ok, text):
    global failures
    failures += 0 if ok else 1
    print(("pass " if ok else "fail ") + text)
first, last, target = number(origin1.get("head")), number(origin2.get("head")), number(origin1.get("target"))
kernel = origin1.get("manifest")
genesis = "unreadable" if not kernel or not origin1.get("genesis") else "match" if origin1.get("genesis") == kernel else "mismatch"
origin_ok = fresh(origin1) and genesis == "match" and first is not None and origin1.get("batch") is not None
emit(origin_ok, "relay-origin app=%s head=%s ready=%s freshness=%s genesis=%s" % (
    sys.argv[1], first if first is not None else "none", str(origin1.get("ready") is True).lower(),
    origin1.get("freshness") or "none", genesis))
def replica(label, row):
    head = number(row.get("head"))
    pins = "match" if (row.get("network") == origin1.get("network") and row.get("genesis") == kernel
                       and row.get("sequencer") == origin1.get("sequencer") and kernel) else "mismatch"
    within = head is not None and target is not None and head >= target and (last is None or head <= last)
    same = row.get("batch") is not None and row.get("target") == origin1.get("target") and row.get("batch") == origin1.get("batch")
    lag = first - head if first is not None and head is not None else "none"
    emit(origin_ok and fresh(row) and pins == "match" and within and same,
         "%s head=%s lag=%s ready=%s freshness=%s pins=%s bytes=%s" % (
             label, head if head is not None else "none", lag, str(row.get("ready") is True).lower(),
             row.get("freshness") or "none", pins, "match" if same else "mismatch"))
replica("relay-public origin=%s" % sys.argv[2], public)
for machine, region, row in replicas:
    replica("relay-replica machine=%s region=%s" % (machine, region), row)
recheck = fresh(origin2) and origin2.get("genesis") == kernel and last is not None and first is not None and last >= first
emit(recheck, "relay-origin-recheck app=%s head=%s ready=%s freshness=%s" % (
    sys.argv[1], last if last is not None else "none", str(origin2.get("ready") is True).lower(),
    origin2.get("freshness") or "none"))
print("@@failures %d" % failures)
' "$kernel" "$origin" "$records")" || answer="fail relay-judge error=unreadable"$'\n'"@@failures 1"
	grep -v '^@@' <<<"$answer"
	failures=$((failures + $(sed -n 's/^@@failures //p' <<<"$answer")))

	for name in original altered; do
		reply="$(python3 - "$origin" "$ca" "$CHECK_LIVE_RELAY_ACTIVITY" "$CHECK_LIVE_RELAY_KEY_FILE" "$name" "$timeout" 2>/dev/null <<'PY'
import json
import ssl
import sys
import urllib.error
import urllib.request

origin, cafile, path, key_file, mode, limit = sys.argv[1:7]
with open(path, "rb") as source:
    body = source.read()
if mode == "altered":
    body = body[:-1] + bytes([body[-1] ^ 1])
with open(key_file, encoding="utf-8") as source:
    key = source.read().strip()
context = None
if origin.startswith("https:"):
    context = ssl.create_default_context(cafile=None if cafile == "-" else cafile)
request = urllib.request.Request(
    origin + "/v1/activities",
    data=body,
    method="POST",
    headers={"Content-Type": "application/octet-stream", "Authorization": "LayerX-Key " + key},
)
try:
    with urllib.request.urlopen(request, timeout=float(limit), context=context) as answer:
        code, raw = answer.status, answer.read(1048576)
except urllib.error.HTTPError as error:
    code, raw = error.code, error.read(1048576)
except OSError:
    print("@@submit 000 none none")
    sys.exit(0)
try:
    value = json.loads(raw)
except ValueError:
    value = {}
value = value if isinstance(value, dict) else {}
result = value["result"] if isinstance(value.get("result"), dict) else value
error = value["error"] if isinstance(value.get("error"), dict) else {}
print("@@submit %03d %s %s" % (code, result.get("activity_id") or "none", error.get("code") or "none"))
PY
		)" || reply=""
		read -r code activity refusal <<<"$(sed -n 's/^@@submit //p' <<<"$reply" | head -n 1)"
		code="${code:-000}"
		if [ "$name" = original ]; then
			if { [ "$code" = 200 ] || [ "$code" = 202 ]; } && [ "${activity:-}" = "$CHECK_LIVE_RELAY_ACTIVITY_ID" ]; then
				echo "pass relay-forward http=$code activity=match"
			else
				echo "fail relay-forward http=$code activity=${activity:-none} error=${refusal:-none}"
				failures=$((failures + 1))
			fi
		elif [ "$code" -ge 400 ] && [ "$code" -lt 500 ] && [ "${activity:-}" != "$CHECK_LIVE_RELAY_ACTIVITY_ID" ]; then
			echo "pass relay-altered http=$code refused=${refusal:-yes}"
		else
			echo "fail relay-altered http=$code activity=${activity:-none} refused=no"
			failures=$((failures + 1))
		fi
	done
	finish "$failures"
}

# check_interop: the interop gateway app of platform/hosted/interop/fly.toml
# runs at least two started machines in at least two regions; /readyz at
# CHECK_LIVE_INTEROP_ORIGIN (default https://interchain.paxeer.network)
# answers ready with every component ready; then one x402 exact-scheme round
# trip over the http transport as the payer: the facilitator's supported
# network, a seller offer answered with its PAYMENT-REQUIRED header, the
# payer's transfer to agent:<CHECK_LIVE_INTEROP_PAYEE_DID>:main of
# CHECK_LIVE_INTEROP_AMOUNT (default 1) base units of the kernel asset of
# platform/hosted/node/bootstrap.sh signed by CHECK_LIVE_INTEROP_ENCODER (the
# built hosted-send example of platform/cli) from the account state the router
# at CHECK_LIVE_INTEROP_RPC (default https://api-mainnet-beta.paxeer.network/rpc)
# reports, the buyer's PAYMENT-SIGNATURE and the settlement whose
# PAYMENT-RESPONSE carries success, the lxp:<activity id> transaction and the
# receipt. CHECK_LIVE_INTEROP_API_KEY_FILE holds the payer's signer-bound
# router key <id>:<secret> and CHECK_LIVE_INTEROP_PAYER_KEY_FILE its ed25519
# key (PEM or 32 raw bytes); neither is printed or leaves this host except
# the key as the Authorization header. One line per check.
check_interop() {
	local toml=platform/hosted/interop/fly.toml name app answer n_started n_regions regions origin rpc_url amount network_id w status verdict failures=0
	for name in CHECK_LIVE_INTEROP_API_KEY_FILE CHECK_LIVE_INTEROP_PAYER_KEY_FILE CHECK_LIVE_INTEROP_PAYEE_DID CHECK_LIVE_INTEROP_ENCODER; do
		if [ -z "${!name:-}" ]; then
			echo "check-live: $name is unset" >&2
			exit 2
		fi
	done
	amount="${CHECK_LIVE_INTEROP_AMOUNT:-1}"
	if ! [[ "$amount" =~ ^[1-9][0-9]*$ ]]; then
		echo "check-live: CHECK_LIVE_INTEROP_AMOUNT is not a positive integer" >&2
		exit 2
	fi
	origin="${CHECK_LIVE_INTEROP_ORIGIN:-https://interchain.paxeer.network}"
	rpc_url="${CHECK_LIVE_INTEROP_RPC:-https://api-mainnet-beta.paxeer.network/rpc}"
	if ! app="$(fly_app "$toml")"; then
		echo "fail interop toml=absent"
		finish 1
	fi
	answer="$(timeout "$timeout" flyctl machines list --app "$app" --json 2>/dev/null | python3 -c '
import json, sys
started = [m for m in json.load(sys.stdin) if m.get("state") == "started"]
regions = sorted({m.get("region", "") for m in started} - {""})
print(len(started), len(regions), ",".join(regions) or "none")
' 2>/dev/null)" || answer=""
	read -r n_started n_regions regions <<<"${answer:-none none none}"
	if [[ "$n_started" =~ ^[0-9]+$ ]] && [ "$n_started" -ge 2 ] && [ "$n_regions" -ge 2 ]; then
		echo "pass machines app=$app started=$n_started regions=$regions"
	else
		echo "fail machines app=$app started=$n_started regions=$regions"
		failures=$((failures + 1))
	fi

	w="$(mktemp -d)"
	# shellcheck disable=SC2064 # the path expands now, the locals are gone at exit
	trap "rm -rf '$w'" EXIT
	status="$(curl -sS -m "$timeout" -o "$w/readyz" -w '%{http_code}' "$origin/readyz" 2>/dev/null)" || status=""
	answer="$(python3 -c '
import json, sys
try:
    doc = json.load(open(sys.argv[1]))
    components = doc["components"]
except Exception:
    print("body=unreadable")
    sys.exit(1)
ready = [k for k, v in sorted(components.items()) if v == "ready"]
waiting = [k for k, v in sorted(components.items()) if v != "ready"]
print("status=%s components=%d/%d%s" % (doc.get("status"), len(ready), len(components), " not_ready=" + ",".join(waiting) if waiting else ""))
sys.exit(0 if doc.get("status") == "ready" and components and not waiting else 1)
' "$w/readyz" 2>/dev/null)" && verdict=pass || verdict=fail
	[ "$status" = 200 ] || verdict=fail
	echo "$verdict readyz $origin/readyz http=${status:-none} $answer"
	[ "$verdict" = pass ] || failures=$((failures + 1))

	network_id="$(fly_ssh "$app" - "printenv LAYERX_INTEROP_PROTOCOL_NETWORK_ID" </dev/null | tr -d '\r\n')" || network_id=""
	if ! [[ "$network_id" =~ ^[1-9][0-9]*$ ]]; then
		echo "fail network app=$app protocol_network_id=${network_id:-unreadable}"
		finish $((failures + 1))
	fi
	python3 - "$origin" "$rpc_url" "$CHECK_LIVE_INTEROP_API_KEY_FILE" "$CHECK_LIVE_INTEROP_PAYER_KEY_FILE" \
		"$CHECK_LIVE_INTEROP_PAYEE_DID" "$CHECK_LIVE_INTEROP_ENCODER" "$amount" "$network_id" "$timeout" \
		"$repo_root/platform/hosted/node/bootstrap.sh" <<'PY' || failures=$((failures + $?))
import json, os, re, subprocess, sys, time, urllib.error, urllib.request

origin, rpc_url, key_file, payer_file, payee, encoder, amount, network_id, limit, bootstrap = sys.argv[1:]
limit = int(limit)


def stop(line):
    print("fail " + line)
    sys.exit(1)


try:
    facts = dict(re.findall(r'^(ASSET_ID|ASSET_CURRENCY)="?([0-9A-Za-z]+)"?$', open(bootstrap).read(), re.M))
    asset, currency = facts["ASSET_ID"].lower(), facts["ASSET_CURRENCY"]
except Exception:
    stop("asset bootstrap=unreadable")
try:
    authorization = "LayerX-Key " + open(key_file).read().strip()
    raw = open(payer_file, "rb").read()
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    from cryptography.hazmat.primitives.serialization import Encoding, NoEncryption, PrivateFormat, load_pem_private_key
    signer = Ed25519PrivateKey.from_private_bytes(raw) if len(raw) == 32 else load_pem_private_key(raw, password=None)
    seed = signer.private_bytes(Encoding.Raw, PrivateFormat.Raw, NoEncryption()).hex()
    payer = "did:layerx:" + signer.public_key().public_bytes_raw().hex()
except Exception:
    stop("payer key=unreadable")


def call(method, url, body=None, idempotency=None):
    headers = {"authorization": authorization}
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        headers["content-type"] = "application/json"
    if idempotency:
        headers["idempotency-key"] = idempotency
    request = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=limit) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        try:
            return error.code, json.load(error)
        except Exception:
            return error.code, None
    except Exception:
        return None, None


def rpc(method, params):
    status, doc = call("POST", rpc_url, {"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
    if status != 200 or not isinstance(doc, dict) or "result" not in doc:
        stop("rpc %s http=%s error=%s" % (method, status, (doc or {}).get("error", {}).get("code", "none") if isinstance(doc, dict) else "none"))
    return doc["result"]


def main_account(did):
    names = ["agent:%s:main" % did]
    for record in rpc("lx_getBalances", [did]).get("accounts", []):
        if record.get("asset_id") == asset and record.get("name") in names:
            return record
    stop("account did=%s asset=%s main=absent" % (did, asset))


status, doc = call("GET", origin + "/v1/http/x402/supported")
kinds = [k for k in ((doc or {}).get("result") or {}).get("supported", {}).get("kinds", []) if k.get("scheme") == "exact"]
if status != 200 or not kinds:
    stop("supported %s/v1/http/x402/supported http=%s exact=absent" % (origin, status))
network = kinds[0]["network"]
print("pass supported %s/v1/http/x402/supported http=200 scheme=exact network=%s" % (origin, network))

payee_record = main_account(payee)
source = main_account(payer)
requirements = {"scheme": "exact", "network": network, "amount": amount, "asset": asset,
                "payTo": payee_record["account_id"], "maxTimeoutSeconds": 120,
                "extra": {"layerx": {"commitment": "executed", "account": payee_record["name"], "currency": currency}}}
offer = {"x402Version": 2, "resource": {"url": origin + "/readyz"}, "accepts": [requirements]}
status, doc = call("POST", origin + "/v1/http/x402/seller/offer", offer, os.urandom(16).hex())
result = (doc or {}).get("result") or {}
if status != 200 or result.get("status") != 402 or not result.get("payment_required_header"):
    stop("offer %s/v1/http/x402/seller/offer http=%s status=%s PAYMENT-REQUIRED=%s" % (
        origin, status, result.get("status", "none"), "present" if result.get("payment_required_header") else "absent"))
print("pass offer %s/v1/http/x402/seller/offer http=200 status=402 PAYMENT-REQUIRED=present" % origin)

account = rpc("lx_getAccount", [source["account_id"]])
identity = rpc("lx_getSequence", [payer, "identity"])
now = time.time_ns() // 1_000_000
idempotency = os.urandom(32).hex()
signing = {"seed": seed, "actor": payer, "from": source["account_id"], "to": payee_record["account_id"],
           "from_name": source["name"], "to_name": payee_record["name"], "asset": asset, "amount": amount,
           "source_sequence": int(account["next_sequence"]), "identity_sequence": int(identity["next_sequence"]),
           "network_id": int(network_id), "not_before": now - 1000, "not_after": now + 120000,
           "idempotency_key": idempotency}
signed = subprocess.run([encoder], input=json.dumps(signing).encode(), stdout=subprocess.PIPE,
                        stderr=subprocess.DEVNULL, check=False)
try:
    signed = json.loads(signed.stdout)
    canonical, activity = signed["canonical"], signed["activity_id"]
except Exception:
    stop("sign encoder=refused")

build = {"payment_required": result["payment_required"],
         "scheme_payload": {"layerxActivity": canonical, "layerxIdempotencyKey": idempotency}}
status, doc = call("POST", origin + "/v1/http/x402/buyer/build", build, os.urandom(16).hex())
built = (doc or {}).get("result") or {}
if status != 200 or not built.get("payment_header") or not isinstance(built.get("payment_payload"), dict):
    stop("build %s/v1/http/x402/buyer/build http=%s error=%s" % (origin, status, ((doc or {}).get("error") or {}).get("code", "none")))
print("pass build %s/v1/http/x402/buyer/build http=200 PAYMENT-SIGNATURE=present" % origin)

settle = {"x402Version": 2, "paymentPayload": built["payment_payload"], "paymentRequirements": requirements}
status, doc = call("POST", origin + "/v1/http/x402/settle", settle, os.urandom(16).hex())
deadline = time.monotonic() + 120
while status == 202 and (doc or {}).get("operation") and time.monotonic() < deadline:
    time.sleep(2)
    status, doc = call("GET", origin + "/v1/operations/" + doc["operation"])
settled = (doc or {}).get("result") or {}
receipt = ((settled.get("extensions") or {}).get("layerx") or {}).get("receipt")
line = "settle %s/v1/http/x402/settle http=%s success=%s transaction=%s receipt=%s" % (
    origin, status, str(settled.get("success", "none")).lower(), settled.get("transaction", "none"),
    "present" if receipt else "absent")
if status != 200 or settled.get("success") is not True or settled.get("transaction") != "lxp:" + activity or not receipt:
    stop(line + " want=lxp:" + activity)
print("pass " + line)
PY
	finish "$failures"
}

# check_ramp: the reference ramp app of platform/ramps/fly.toml runs one
# started machine with one volume; ramp.paxeer.network is registered on the
# edge host as an http site of that app, resolves to the edge host and
# answers /readyz 200 under a publicly verified certificate with Fly's
# request id and the body the app's fly.dev name answers; and
# platform/ramps/sandbox-journey.sh against the name records done for one
# on-ramp and one off-ramp order. The journey's inputs are its own variables
# (LAYERX_RAMP_CUSTOMER_TOKEN, LAYERX_RAMP_OPERATOR_URL,
# LAYERX_RAMP_OPERATOR_TOKEN, LAYERX_RAMP_ON_QUOTE_ID,
# LAYERX_RAMP_OFF_QUOTE_ID, LAYERX_RAMP_OFF_GRANT_JSON,
# LAYERX_RAMP_ON_ACCOUNT_SEQUENCE, LAYERX_RAMP_OFF_RECEIVER_SEQUENCE);
# LAYERX_RAMP_CA_PEM defaults to the system CA file. One line per check,
# never printing a token or an address.
check_ramp() {
	local toml=platform/ramps/fly.toml ramp_name=ramp.paxeer.network name app answer n_machines n_started n_mounts
	local reply status=0 manifest line edge_addrs addrs at out headers body code fly_id app_body v failures=0
	for name in LAYERX_RAMP_CUSTOMER_TOKEN LAYERX_RAMP_OPERATOR_URL LAYERX_RAMP_OPERATOR_TOKEN LAYERX_RAMP_ON_QUOTE_ID \
		LAYERX_RAMP_OFF_QUOTE_ID LAYERX_RAMP_OFF_GRANT_JSON LAYERX_RAMP_ON_ACCOUNT_SEQUENCE LAYERX_RAMP_OFF_RECEIVER_SEQUENCE; do
		if [ -z "${!name:-}" ]; then
			echo "check-live: $name is unset" >&2
			exit 2
		fi
	done
	for name in flyctl jq; do
		if ! command -v "$name" >/dev/null 2>&1; then
			echo "check-live: $name is required" >&2
			exit 2
		fi
	done
	if ! app="$(fly_app "$toml")"; then
		echo "fail ramp toml=absent"
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

	reply="$(ssh_read "$EDGE_HOST" "$edge_cmd")" || status=$?
	manifest="$(grep -E '^[a-z0-9.-]+ (http|stream) [a-z0-9-]+ [0-9]+$' <<<"${reply%%@@sites*}" || true)"
	line="$(awk -v n="$ramp_name" '$1 == n' <<<"$manifest" | head -n 1)"
	edge_addrs="$(getent ahosts "${EDGE_HOST#*@}" 2>/dev/null | awk '{print $1}' | sort -u)"
	addrs="$(getent ahosts "$ramp_name" 2>/dev/null | awk '{print $1}' | sort -u)"
	at=no
	if [ -n "$addrs" ] && [ -n "$edge_addrs" ] && [ -z "$(comm -23 <(printf '%s\n' "$addrs") <(printf '%s\n' "$edge_addrs"))" ]; then
		at=yes
	fi
	if [ "$status" -ne 0 ] || [[ "$reply" != *@@sites* ]]; then
		echo "fail edge $ramp_name manifest ssh=$status"
		failures=$((failures + 1))
	elif [ "$line" = "$ramp_name http $app 443" ] && [ "$at" = yes ]; then
		echo "pass edge $ramp_name mode=http app=$app edge=yes"
	else
		read -r _ v name _ <<<"${line:-none none none}"
		echo "fail edge $ramp_name mode=$v app=$name want=$app edge=$at"
		failures=$((failures + 1))
	fi

	status=0
	out="$(curl -sS --max-time "$timeout" -D - "https://$ramp_name/readyz" 2>&1)" || status=$?
	code=none
	fly_id=absent
	body=""
	if [ "$status" -eq 0 ]; then
		headers="${out%%$'\r\n\r\n'*}"
		body="${out#*$'\r\n\r\n'}"
		code="$(sed -n '1s/^HTTP\/[0-9.]* \([0-9]*\).*/\1/p' <<<"$headers")"
		if grep -qiE '^fly-request-id: *[^[:space:]]' <<<"$headers"; then
			fly_id=present
		fi
	fi
	app_body="$(curl -sS --max-time "$timeout" -D - "https://$app.fly.dev/readyz" 2>/dev/null)" || app_body=""
	app_body="${app_body#*$'\r\n\r\n'}"
	v=differ
	if [ -n "$body" ] && [ "$body" = "$app_body" ]; then
		v=match
	fi
	if [ "$status" -ne 0 ]; then
		echo "fail readyz https://$ramp_name/readyz curl=$status"
		failures=$((failures + 1))
	elif [ "$code" = 200 ] && [ "$fly_id" = present ] && [ "$v" = match ]; then
		echo "pass readyz https://$ramp_name/readyz tls=verified http=200 fly-request-id=present body=match"
	else
		echo "fail readyz https://$ramp_name/readyz tls=verified http=${code:-none} fly-request-id=$fly_id body=$v"
		failures=$((failures + 1))
	fi

	status=0
	LAYERX_RAMP_URL="https://$ramp_name" LAYERX_RAMP_CA_PEM="${LAYERX_RAMP_CA_PEM:-/etc/ssl/certs/ca-certificates.crt}" \
		timeout 10m sh "$repo_root/platform/ramps/sandbox-journey.sh" >/dev/null 2>&1 || status=$?
	if [ "$status" -eq 0 ]; then
		echo "pass journey https://$ramp_name on-ramp=done off-ramp=done"
	else
		echo "fail journey https://$ramp_name exit=$status"
		failures=$((failures + 1))
	fi
	finish "$failures"
}

# check_indexer: the indexer app of platform/hosted/indexer/fly.toml has no
# public IP and runs exactly one machine, started, with its volume at /data;
# inside it the layerx-indexer process answers /healthz over TLS at its
# .internal name to the internal CA tools/bringup/ca.sh left on the volume;
# its EVM URL is the archive node's public RPC name (the name rpc_unit reads
# behind ARCHIVE_HOST), its CometBFT URL that name's /comet location and its
# relay URL https://archive.paxeer.network, each pinned to a self-signed ISRG
# Root X1 that verifies the chain the name serves (read at
# CHECK_LIVE_INDEXER_CONNECT, host:port, default the name on 443); the
# location answers a POSTed status and refuses a GET, and the indexer's start
# block is the node's first retained block; the Paxeer backfill cursor sits at
# the cutover the init kept and the live cursor at or past it; and through
# the router px_getUnifiedHistory, lx_getHistory and px_getHistory answer
# items for CHECK_LIVE_INDEXER_ACCOUNT, a 0x EVM address with history on both
# sides. One line per check.
check_indexer() {
	local router_url=https://api-mainnet-beta.paxeer.network/rpc relay_url=https://archive.paxeer.network
	local account="${CHECK_LIVE_INDEXER_ACCOUNT:-}" app answer n_machines n_started n_data unit w reply line side want url pin host
	local subject status code earliest start cutover backfilled live listen method params layerx="" failures=0
	local script
	if ! [[ "$account" =~ ^0x[0-9a-fA-F]{40}$ ]]; then
		echo "check-live: CHECK_LIVE_INDEXER_ACCOUNT must be a 0x EVM address with indexed history" >&2
		exit 2
	fi
	if ! command -v flyctl >/dev/null 2>&1; then
		echo "check-live: flyctl is required" >&2
		exit 2
	fi
	if ! app="$(fly_app platform/hosted/indexer/fly.toml)"; then
		echo "fail indexer toml=absent"
		finish 1
	fi
	answer="$(timeout "$timeout" flyctl ips list --app "$app" --json 2>/dev/null | python3 -c 'import json, sys; print(len(json.load(sys.stdin) or []))' 2>/dev/null)" || answer=none
	if [ "$answer" = 0 ]; then
		echo "pass public-ips app=$app count=0"
	else
		echo "fail public-ips app=$app count=${answer:-none}"
		failures=$((failures + 1))
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
		failures=$((failures + 1))
	fi

	read -r unit _ <<<"$(rpc_unit "$ARCHIVE_HOST" || true)"
	if [[ "${unit:-}" =~ ^api[0-9]+$ ]]; then
		echo "pass archive-name name=$unit.$rpc_domain"
	else
		echo "fail archive-name role=ARCHIVE_HOST name=none"
		failures=$((failures + 1))
		unit=""
	fi

	# shellcheck disable=SC2016 # the script expands on the machine
	script='pid=
for d in /proc/[0-9]*; do
	case "$(readlink "$d/exe" 2>/dev/null)" in
	*/layerx-indexer)
		pid=$d
		break
		;;
	esac
done
if [ -z "$pid" ]; then
	echo "@@process none"
	exit 0
fi
env_of() { tr "\000" "\n" <"$pid/environ" | sed -n "s/^$1=//p" | head -n 1; }
for side in EVM COMET RELAY; do
	echo "@@$side $(env_of "LAYERX_INDEXER_${side}_URL") $(base64 <"$(env_of "LAYERX_INDEXER_${side}_CA_DER")" 2>/dev/null | tr -d "\n")"
done
db=$(env_of LAYERX_INDEXER_DB)
listen=$(env_of LAYERX_INDEXER_LISTEN)
echo "@@start $(env_of LAYERX_INDEXER_START_BLOCK)"
echo "@@listen $listen"
status=0
answer=$(curl -sS -m "$limit" --cacert "$tls/ca.pem" -w "\n%{http_code}" "https://$app.internal:${listen##*:}/healthz" 2>&1) || status=$?
echo "@@health $status $(printf "%s" "$answer" | tail -n 1) $(printf "%s" "$answer" | head -n 1 | tr -d " " | cut -c1-120)"
echo "@@cutover $(cat "$(dirname "$db")/cutover-height" 2>/dev/null)"
echo "@@cursors $(sqlite3 -readonly -separator " " "$db" "SELECT (SELECT position FROM backfill_cursors WHERE chain = '\''paxeer'\''), (SELECT position FROM cursors WHERE chain = '\''paxeer'\'');" 2>/dev/null)"'
	reply="$(printf '%s\n' "$script" | fly_ssh "$app" - "tls=$fly_tls_dir/indexer app=$app limit=$timeout sh -s")" || reply=""
	if [ -z "$reply" ] || grep -q '^@@process none' <<<"$reply"; then
		[ -n "$reply" ] && answer=none || answer=unreachable
		echo "fail indexer app=$app process=$answer"
		finish $((failures + 1))
	fi

	listen="$(sed -n 's/^@@listen //p' <<<"$reply" | head -n 1)"
	read -r status code answer <<<"$(sed -n 's/^@@health //p' <<<"$reply" | head -n 1)"
	if [[ "$listen" == "[::]:"* ]] && [ "$status" = 0 ] && [ "$code" = 200 ] && [[ "$answer" == *'"status":"ok"'* ]]; then
		echo "pass listener url=https://$app.internal:${listen##*:}/healthz listen=$listen tls=internal-ca http=200"
	else
		echo "fail listener app=$app listen=${listen:-none} curl=${status:-none} http=${code:-none}"
		failures=$((failures + 1))
	fi

	w="$(mktemp -d)"
	# shellcheck disable=SC2064
	trap "rm -rf '$w'" EXIT
	for side in EVM COMET RELAY; do
		case "$side" in
		EVM) want="https://$unit.$rpc_domain" ;;
		COMET) want="https://$unit.$rpc_domain/comet" ;;
		RELAY) want="$relay_url" ;;
		esac
		read -r url pin <<<"$(sed -n "s/^@@$side //p" <<<"$reply" | head -n 1)"
		host="${url#https://}"
		host="${host%%/*}"
		if [ -n "$host" ] && [ ! -e "$w/$host-1.pem" ]; then
			timeout "$timeout" openssl s_client -connect "${CHECK_LIVE_INDEXER_CONNECT:-$host:443}" -servername "$host" -showcerts </dev/null 2>/dev/null |
				awk -v out="$w/$host" '/-BEGIN CERTIFICATE-/ { n++; in_cert = 1 } in_cert { print >(out "-" n ".pem") } /-END CERTIFICATE-/ { in_cert = 0; close(out "-" n ".pem") }' || true
			cat "$w/$host"-[2-9].pem >"$w/$host-untrusted.pem" 2>/dev/null || : >"$w/$host-untrusted.pem"
		fi
		subject=unreadable
		status=1
		if [ -n "$pin" ] && base64 -d <<<"$pin" 2>/dev/null | openssl x509 -inform DER -out "$w/pin-$side.pem" 2>/dev/null; then
			subject="$(openssl x509 -in "$w/pin-$side.pem" -noout -subject -nameopt sep_multiline,utf8 | sed -n 's/^ *CN=//p')"
			if [ "$subject" = "ISRG Root X1" ] &&
				[ "$subject" = "$(openssl x509 -in "$w/pin-$side.pem" -noout -issuer -nameopt sep_multiline,utf8 | sed -n 's/^ *CN=//p')" ] &&
				[ -s "$w/$host-1.pem" ] &&
				openssl verify -CAfile "$w/pin-$side.pem" -untrusted "$w/$host-untrusted.pem" -verify_hostname "$host" "$w/$host-1.pem" >/dev/null 2>&1; then
				status=0
			fi
		fi
		if [ -n "$unit" ] && [ "$url" = "$want" ] && [ "$status" -eq 0 ]; then
			echo "pass upstream-ca side=${side,,} url=$url root=X1 verifies=yes"
		else
			echo "fail upstream-ca side=${side,,} url=${url:-none} want=$want pin=${subject// /-} verifies=no"
			failures=$((failures + 1))
		fi
	done

	earliest=""
	if [ -n "$unit" ]; then
		code="$(curl -sS -m "$timeout" -o "$w/comet.json" -w '%{http_code}' -H 'content-type: application/json' \
			--data '{"jsonrpc":"2.0","id":1,"method":"status","params":{}}' "https://$unit.$rpc_domain/comet" 2>/dev/null)" || code=none
		earliest="$(python3 -c '
import json, sys
doc = json.load(open(sys.argv[1]))
value = str(((doc.get("result") or doc).get("sync_info") or {}).get("earliest_block_height", ""))
print(value if value.isdigit() else "")
' "$w/comet.json" 2>/dev/null)" || earliest=""
		status="$(curl -sS -m "$timeout" -o /dev/null -w '%{http_code}' "https://$unit.$rpc_domain/comet" 2>/dev/null)" || status=none
		if [ "$code" = 200 ] && [ -n "$earliest" ] && [ "$status" = 403 ]; then
			echo "pass comet-location url=https://$unit.$rpc_domain/comet post=200 get=403 earliest=$earliest"
		else
			echo "fail comet-location url=https://$unit.$rpc_domain/comet post=$code get=$status earliest=${earliest:-none}"
			failures=$((failures + 1))
		fi
	fi
	start="$(sed -n 's/^@@start //p' <<<"$reply" | head -n 1)"
	if [ -n "$earliest" ] && [ "$start" = "$earliest" ]; then
		echo "pass start-block block=$start earliest=$earliest"
	else
		echo "fail start-block block=${start:-none} earliest=${earliest:-none}"
		failures=$((failures + 1))
	fi

	cutover="$(sed -n 's/^@@cutover //p' <<<"$reply" | head -n 1)"
	read -r backfilled live <<<"$(sed -n 's/^@@cursors //p' <<<"$reply" | head -n 1)"
	if [[ "$cutover" =~ ^[0-9]+$ ]] && [ "${backfilled:-}" = "$cutover" ] && [[ "${live:-}" =~ ^[0-9]+$ ]] && [ "$live" -ge "$cutover" ]; then
		echo "pass backfill chain=paxeer cutover=$cutover backfill=$backfilled live=$live"
	else
		echo "fail backfill chain=paxeer cutover=${cutover:-none} backfill=${backfilled:-none} live=${live:-none}"
		failures=$((failures + 1))
	fi

	for method in px_getUnifiedHistory lx_getHistory px_getHistory; do
		params="[\"$account\"]"
		[ "$method" != lx_getHistory ] || params="[\"$layerx\"]"
		if [ "$method" = lx_getHistory ] && [ -z "$layerx" ]; then
			echo "fail history method=$method account=none via=px_getUnifiedHistory"
			failures=$((failures + 1))
			continue
		fi
		code="$(curl -sS -m "$timeout" -o "$w/history.json" -w '%{http_code}' -H 'content-type: application/json' \
			--data "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"$method\",\"params\":$params}" "$router_url" 2>/dev/null)" || code=none
		read -r answer line <<<"$(python3 -c '
import json, sys
doc = json.load(open(sys.argv[1]))
result = doc.get("result")
if not isinstance(result, dict):
    error = doc.get("error") or {}
    print("error", ((error.get("data") or {}).get("code") or error.get("message") or "no-result").replace(" ", "-"))
    sys.exit(0)
sides = [a.get("account") for a in result.get("accounts") or [] if a.get("side") == "layerx"]
print(len(result.get("items") or []), sides[0] if sides else "-")
' "$w/history.json" 2>/dev/null)"
		if [ "$code" = 200 ] && [[ "${answer:-}" =~ ^[0-9]+$ ]] && [ "$answer" -gt 0 ]; then
			echo "pass history method=$method account=${params:2:-2} items=$answer"
		else
			echo "fail history method=$method account=${params:2:-2} http=$code answer=${answer:-none}${line:+ $line}"
			failures=$((failures + 1))
		fi
		if [ "$method" = px_getUnifiedHistory ] && [[ "${answer:-}" =~ ^[0-9]+$ ]] && [ "${line:--}" != - ]; then
			layerx="$line"
		fi
	done
	finish "$failures"
}

# check_developers: the three developer apps each run started machines in ams
# and fra, per process group for the webhooks app; hooks, api-dev and dev
# answer ready over the edge; a subscription for CHECK_LIVE_DEVELOPERS_RECEIVER
# registers under the session of CHECK_LIVE_DEVELOPERS_TOKEN_FILE and lists
# back; and one producer event is delivered to it within
# CHECK_LIVE_DEVELOPERS_DELIVERY_ATTEMPTS reads five seconds apart. The token
# travels only in a curl config file and is never printed. One line per check.
check_developers() {
	local hooks=https://hooks.paxeer.network api=https://api-dev.paxeer.network web=https://dev.paxeer.network
	local token_file="${CHECK_LIVE_DEVELOPERS_TOKEN_FILE:-}" receiver="${CHECK_LIVE_DEVELOPERS_RECEIVER:-}"
	local attempts="${CHECK_LIVE_DEVELOPERS_DELIVERY_ATTEMPTS:-36}"
	local toml app groups answer group started regions url want code body w endpoint="" n failures=0
	if ! command -v flyctl >/dev/null 2>&1; then
		echo "check-live: flyctl is required" >&2
		exit 2
	fi
	if [ -z "$token_file" ] || [ ! -r "$token_file" ]; then
		echo "check-live: CHECK_LIVE_DEVELOPERS_TOKEN_FILE does not name a readable file" >&2
		exit 2
	fi
	if [[ "$receiver" != https://?* ]]; then
		echo "check-live: CHECK_LIVE_DEVELOPERS_RECEIVER is not an https URL" >&2
		exit 2
	fi
	if ! [[ "$attempts" =~ ^[1-9][0-9]{0,2}$ ]]; then
		echo "check-live: CHECK_LIVE_DEVELOPERS_DELIVERY_ATTEMPTS is not a count from 1 to 999" >&2
		exit 2
	fi

	for toml in platform/hosted/webhooks/fly.toml platform/hosted/dashboard/fly.toml platform/hosted/dashboard/web/fly.toml; do
		if ! app="$(fly_app "$toml")"; then
			echo "fail machines toml=$toml status=absent"
			failures=$((failures + 1))
			continue
		fi
		groups=app
		[ "$toml" != platform/hosted/webhooks/fly.toml ] || groups="public ingress"
		# shellcheck disable=SC2086 # groups is a word list
		answer="$(timeout "$timeout" flyctl machines list --app "$app" --json 2>/dev/null | python3 -c '
import json, sys
ms = json.load(sys.stdin) or []
for group in sys.argv[1:]:
    regions = [m.get("region") or "-" for m in ms if m.get("state") == "started" and ((m.get("config") or {}).get("metadata") or {}).get("fly_process_group", "app") == group]
    print(group, len(regions), ",".join(sorted(set(regions))) or "-")
' $groups 2>/dev/null)" || answer=""
		for group in $groups; do
			read -r started regions <<<"$(sed -n "s/^$group //p" <<<"$answer" | head -n 1)"
			if [ "${started:-0}" -ge 2 ] && [[ ",$regions," == *,ams,* ]] && [[ ",$regions," == *,fra,* ]]; then
				echo "pass machines app=$app group=$group started=$started regions=$regions"
			else
				echo "fail machines app=$app group=$group started=${started:-none} regions=${regions:-none}"
				failures=$((failures + 1))
			fi
		done
	done

	w="$(mktemp -d)"
	# shellcheck disable=SC2064
	trap "rm -rf '$w'" EXIT
	for url in "$hooks/healthz" "$api/healthz" "$web/"; do
		code="$(curl -sS --max-time "$timeout" --output "$w/body" --write-out '%{http_code}' "$url" 2>/dev/null)" || code="${code:-none}"
		body="$(cat "$w/body" 2>/dev/null || true)"
		want=200
		if [ "$code" = "$want" ] && { [ "$url" = "$web/" ] || [[ "$body" == *'"ready":true'* ]]; }; then
			echo "pass readiness url=$url http=200"
		else
			echo "fail readiness url=$url http=$code"
			failures=$((failures + 1))
		fi
	done

	(
		umask 077
		printf 'header = "Authorization: Bearer %s"\n' "$(tr -d '\r\n' <"$token_file")" >"$w/auth"
	)
	python3 -c 'import json, sys; print(json.dumps({"url": sys.argv[1], "kinds": ["journey", "payment", "approval", "program"], "minimum_verification": "unverified"}))' "$receiver" >"$w/register"
	code="$(curl -sS --max-time "$timeout" --config "$w/auth" --header 'Content-Type: application/json' --header 'Idempotency-Key: check-live-developers' \
		--data-binary "@$w/register" --output "$w/body" --write-out '%{http_code}' "$hooks/v1/webhooks/endpoints" 2>/dev/null)" || code="${code:-none}"
	[ "$code" != 201 ] || endpoint="$(python3 -c 'import json, sys; print(json.load(open(sys.argv[1]))["endpoint"])' "$w/body" 2>/dev/null)" || endpoint=""
	if [ -z "$endpoint" ]; then
		echo "fail subscription url=$hooks/v1/webhooks/endpoints http=$code"
		failures=$((failures + 1))
		finish "$failures"
	fi
	code="$(curl -sS --max-time "$timeout" --config "$w/auth" --output "$w/body" --write-out '%{http_code}' "$hooks/v1/webhooks/endpoints" 2>/dev/null)" || code="${code:-none}"
	if [ "$code" = 200 ] && python3 -c 'import json, sys; sys.exit(0 if any(e.get("endpoint") == sys.argv[2] for e in json.load(open(sys.argv[1]))) else 1)' "$w/body" "$endpoint" 2>/dev/null; then
		echo "pass subscription endpoint=$endpoint registered=201 listed=yes"
	else
		echo "fail subscription endpoint=$endpoint registered=201 listed=no http=$code"
		failures=$((failures + 1))
	fi

	answer=""
	for n in $(seq "$attempts"); do
		[ "$n" -eq 1 ] || sleep 5
		code="$(curl -sS --max-time "$timeout" --config "$w/auth" --output "$w/body" --write-out '%{http_code}' "$hooks/v1/webhooks/deliveries" 2>/dev/null)" || code="${code:-none}"
		[ "$code" = 200 ] || continue
		answer="$(python3 -c '
import json, sys
for d in json.load(open(sys.argv[1])):
    if d.get("endpoint") == sys.argv[2] and (d.get("state") or {}).get("state") == "delivered":
        print(d.get("kind", "-"), d.get("delivery", "-"))
        break
' "$w/body" "$endpoint" 2>/dev/null)" || answer=""
		[ -z "$answer" ] || break
	done
	if [ -n "$answer" ]; then
		read -r group n <<<"$answer"
		echo "pass delivery endpoint=$endpoint kind=$group delivery=$n state=delivered"
	else
		echo "fail delivery endpoint=$endpoint state=none-delivered attempts=$attempts http=$code"
		failures=$((failures + 1))
	fi
	finish "$failures"
}

# check_search: the serving x-websearch sidecars behind the serving RPC names
# that tools/bringup/search-front.sh names lists. /xweb/health answers 200
# with status ok on every serving RPC name, and an unpaid /search answers 402
# with a PAYMENT-REQUIRED metered offer in PAX to the payer
# CHECK_LIVE_SEARCH_PAYER_DID on the first serving RPC name, on the second
# (the client's redundant path) when there is one and through
# search.paxeer.network; the offer's amount is the approved price, 0.001 US
# dollars at 13.44 US dollars per PAX, exactly 1/13440 PAX rounded up to a
# whole base unit of the decimals the router's lx_getAsset reports for the
# offer's asset, which must be the registered, unpaused PAX record; the retry
# carrying a PAYMENT-SIGNATURE with the payer's grant for that offer, signed
# with the Ed25519 PEM key of CHECK_LIVE_SEARCH_PAYER_KEY_FILE (never
# printed), answers 200 with a PAYMENT-RESPONSE from the backend whose
# x-search-node answered the challenge. The router is CHECK_LIVE_ROUTER_URL,
# default https://api-mainnet-beta.paxeer.network. One line per check.
search_offer_py='
import base64
import json
import sys

code, header = sys.argv[1], sys.argv[2]
try:
    doc = json.loads(base64.b64decode(header, validate=True))
    offers = [o for o in doc.get("accepts") or [] if ((o.get("extra") or {}).get("layerx") or {}).get("currency") == "PAX"]
except (ValueError, TypeError, AttributeError):
    doc, offers = {}, []
ok = code == "402" and doc.get("x402Version") == 2 and bool(offers)
schemes = ",".join(sorted({str(o.get("scheme")) for o in offers})) or "none"
amount = offers[0].get("amount", "none") if offers else "none"
print(("pass" if ok else "fail") + " http=%s pax=%s amount=%s" % (code or "none", schemes, amount))
metered = [o for o in offers if o.get("scheme") == "metered"]
if ok and metered:
    print(base64.b64encode(json.dumps(metered[0], separators=(",", ":")).encode()).decode())
'
# search_price_py <offer b64> <lx_getAsset reply>: checks the offer's amount
# against the approved 1/13440 PAX, rounded up to a whole base unit of the
# registered decimals of the offer's asset, and prints one result line.
search_price_py='
import base64
import json
import re
import sys
from fractions import Fraction

APPROVED = Fraction(1, 1000) / Fraction(1344, 100)
assert APPROVED == Fraction(1, 13440)
offer = json.loads(base64.b64decode(sys.argv[1]))
try:
    asset = json.loads(sys.argv[2])["result"]["asset"]
    decimals = asset["decimals"]
    registered = (asset["asset_id"] == offer["asset"] and asset["symbol"] == "PAX" and asset["paused"] is False
                  and type(decimals) is int and 0 <= decimals <= 38)
except (ValueError, KeyError, TypeError):
    registered, decimals = False, None
if not registered:
    print("fail asset=%s registered=no" % offer.get("asset", "none"))
    sys.exit()
units = APPROVED * 10 ** decimals
want = max(1, -(-units.numerator // units.denominator))
amount = str(offer.get("amount", ""))
ok = re.fullmatch(r"[1-9][0-9]*", amount) is not None and int(amount) == want
print(("pass" if ok else "fail") + " asset=PAX decimals=%d amount=%s approved=%d" % (decimals, amount or "none", want))
'
# search_grant_py <offer b64> <public key hex> <sig hex|->: with - prints the
# grant id to sign (the digest of the grant fields under the authority-hash
# and LXP:GRANT:v1 domains, as layerx-crypto verifies it); with a signature
# prints the PAYMENT-SIGNATURE header value carrying the canonical grant.
search_grant_py='
import base64
import hashlib
import json
import os
import sys
import time

offer = json.loads(base64.b64decode(sys.argv[1]))
terms = offer["extra"]["layerx"]
amount = int(offer["amount"])
state = os.environ["search_grant_state"]
if sys.argv[3] == "-":
    fields = b"".join((
        bytes.fromhex(terms["payer"]), bytes.fromhex(offer["payTo"]), bytes.fromhex(offer["asset"]),
        amount.to_bytes(16, "big"), amount.to_bytes(16, "big"), b"\0", (0).to_bytes(8, "big"),
        (int(time.time()) + 300).to_bytes(8, "big"), bytes.fromhex(terms["purposeHash"]), b"\0", bytes(32),
        (0).to_bytes(8, "big"), bytes.fromhex(sys.argv[2]),
    ))
    grant_id = hashlib.sha256(b"LXP/v1/authority-hash\0" + b"LXP:GRANT:v1" + fields).digest()
    with open(state, "wb") as handle:
        handle.write(grant_id + fields)
    sys.stdout.buffer.write(grant_id)
    sys.exit(0)
with open(state, "rb") as handle:
    grant = handle.read() + bytes.fromhex(sys.argv[3])
header = {"x402Version": 2, "accepted": offer, "payload": {"grant": grant.hex(), "idempotencyKey": os.urandom(32).hex()}}
print(base64.b64encode(json.dumps(header, separators=(",", ":")).encode()).decode())
'
check_search() {
	local listing name code body headers first="" second="" line reply offer public signature payment work v node asset failures=0
	local router="${CHECK_LIVE_ROUTER_URL:-https://api-mainnet-beta.paxeer.network}"
	local -a names=() paths=()
	for v in CHECK_LIVE_SEARCH_PAYER_KEY_FILE CHECK_LIVE_SEARCH_PAYER_DID; do
		if [ -z "${!v:-}" ]; then
			echo "check-live: $v is unset" >&2
			exit 2
		fi
	done
	if ! listing="$("$(dirname "${BASH_SOURCE[0]}")/search-front.sh" names 2>&1)"; then
		echo "fail names names=unreadable $(printf '%s' "$listing" | tr '\n' ' ' | cut -c1-160)"
		finish 1
	fi
	mapfile -t names < <(sed -n 's/^serve //p' <<<"$listing")
	if [ "${#names[@]}" -eq 0 ]; then
		echo "fail names serving=0"
		finish 1
	fi
	for name in "${names[@]}"; do
		if [ -z "$first" ]; then
			first="$name"
		elif [ -z "$second" ]; then
			second="$name"
		fi
		body="$(curl -sS --max-time "$timeout" -w '\n%{http_code}' "https://$name/xweb/health" 2>/dev/null)" || body=""
		code="${body##*$'\n'}"
		body="${body%$'\n'*}"
		if [ "$code" = 200 ] && [[ "$body" == *'"status":"ok"'* ]]; then
			echo "pass health https://$name/xweb/health http=200 status=ok"
		else
			echo "fail health https://$name/xweb/health http=${code:-none}"
			failures=$((failures + 1))
		fi
	done
	paths=("$first")
	[ -z "$second" ] || paths+=("$second")
	paths+=(search.paxeer.network)
	work="$(mktemp -d)"
	# shellcheck disable=SC2064
	trap "rm -rf '$work'" EXIT
	public="$(openssl pkey -in "$CHECK_LIVE_SEARCH_PAYER_KEY_FILE" -pubout -outform DER 2>/dev/null | tail -c 32 | od -An -tx1 | tr -d ' \n')" || public=""
	for name in "${paths[@]}"; do
		headers="$(curl -sS --max-time "$timeout" -H "LAYERX-PAYER-DID: $CHECK_LIVE_SEARCH_PAYER_DID" -D - -o /dev/null "https://$name/search?q=paxeer" 2>/dev/null)" || headers=""
		code="$(sed -n '1s/^HTTP\/[0-9.]* \([0-9]*\).*/\1/p' <<<"$headers")"
		node="$(sed -n 's/^x-search-node:[[:space:]]*//Ip' <<<"$headers" | tr -d '\r' | head -n 1)"
		reply="$(python3 -c "$search_offer_py" "$code" "$(sed -n 's/^payment-required:[[:space:]]*//Ip' <<<"$headers" | tr -d '\r' | head -n 1)")"
		line="$(head -n 1 <<<"$reply")"
		offer="$(sed -n 2p <<<"$reply")"
		echo "${line%% *} offer https://$name/search ${line#* }"
		if [ "${line%% *}" != pass ] || [ -z "$offer" ]; then
			[ "${line%% *}" != pass ] || echo "fail paid https://$name/search metered=none"
			failures=$((failures + 1))
			continue
		fi
		asset="$(python3 -c 'import base64, json, re, sys; a = str(json.loads(base64.b64decode(sys.argv[1])).get("asset", "")); print(a if re.fullmatch(r"[0-9a-f]{64}", a) else "")' "$offer")"
		reply="$(curl -sS --max-time "$timeout" -H "content-type: application/json" \
			-d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"lx_getAsset\",\"params\":[\"$asset\"]}" "$router/rpc" 2>/dev/null)" || reply=""
		line="$(python3 -c "$search_price_py" "$offer" "$reply")"
		echo "${line%% *} price https://$name/search ${line#* }"
		if [ "${line%% *}" != pass ]; then
			failures=$((failures + 1))
			continue
		fi
		signature=""
		if [ "${#public}" -eq 64 ] && search_grant_state="$work/grant" python3 -c "$search_grant_py" "$offer" "$public" - >"$work/digest" &&
			openssl pkeyutl -sign -rawin -inkey "$CHECK_LIVE_SEARCH_PAYER_KEY_FILE" -in "$work/digest" -out "$work/signature" 2>/dev/null; then
			signature="$(od -An -tx1 "$work/signature" | tr -d ' \n')"
		fi
		if [ "${#signature}" -ne 128 ]; then
			echo "fail paid https://$name/search signature=unavailable"
			failures=$((failures + 1))
			continue
		fi
		payment="$(search_grant_state="$work/grant" python3 -c "$search_grant_py" "$offer" "$public" "$signature")"
		headers="$(curl -sS --max-time "$timeout" -H "LAYERX-PAYER-DID: $CHECK_LIVE_SEARCH_PAYER_DID" -H "PAYMENT-SIGNATURE: $payment" -D - -o /dev/null "https://$name/search?q=paxeer" 2>/dev/null)" || headers=""
		code="$(sed -n '1s/^HTTP\/[0-9.]* \([0-9]*\).*/\1/p' <<<"$headers")"
		v="$(sed -n 's/^x-search-node:[[:space:]]*//Ip' <<<"$headers" | tr -d '\r' | head -n 1)"
		if [ "$v" != "$node" ]; then
			echo "fail paid https://$name/search node=${v:-none} challenge-node=${node:-none}"
			failures=$((failures + 1))
		elif [ "$code" = 200 ] && grep -qi '^payment-response:' <<<"$headers"; then
			echo "pass paid https://$name/search http=200 currency=PAX payment-response=present node=${node:-direct}"
		else
			echo "fail paid https://$name/search http=${code:-none} payment-response=$(grep -qi '^payment-response:' <<<"$headers" && echo present || echo absent)"
			failures=$((failures + 1))
		fi
	done
	finish "$failures"
}

# check_registry: the app of platform/hosted/registry/fly.toml runs one
# started machine with its volume and holds a dedicated IPv4 for the edge's
# passthrough; from the kernel machine of human/wallet/deploy/human.toml,
# https://index.paxeer.network/healthz refuses a request without a client
# certificate and answers 200 ready to one with the kernel's internal-CA
# client identity; the router's lx_getProgramEvents returns, on the reference
# escrow's event topic, an event of the program CHECK_LIVE_REGISTRY_PROGRAM_ID
# names (the deploy step records it); and the router's /readyz reports
# program_registry ready. The router is CHECK_LIVE_ROUTER_URL, default
# https://api-mainnet-beta.paxeer.network. One line per check.
check_registry() {
	local toml=platform/hosted/registry/fly.toml url ingress
	local router="${CHECK_LIVE_ROUTER_URL:-https://api-mainnet-beta.paxeer.network}"
	local topic=6c782e7265662e657363726f772e637573746f6479 program="${CHECK_LIVE_REGISTRY_PROGRAM_ID:-}"
	local app kernel answer reply line status code body anonymous from page failures=0
	# shellcheck disable=SC2016
	local script='status=0
answer=$(curl -sS -m "$limit" --cacert "$tls/ca.pem" --cert "$tls/cert.pem" --key "$tls/key.pem" -w "\n%{http_code}" "$url" 2>&1) || status=$?
echo "@@healthz $status $(printf "%s" "$answer" | tail -n 1) $(printf "%s" "$answer" | head -n 1 | tr -d " " | cut -c1-120)"
status=0
curl -sS -m "$limit" --cacert "$tls/ca.pem" -o /dev/null "$url" 2>/dev/null || status=$?
echo "@@anonymous $status"'
	if ! command -v flyctl >/dev/null 2>&1; then
		echo "check-live: flyctl is required" >&2
		exit 2
	fi
	if ! app="$(fly_app "$toml")" || ! kernel="$(fly_app human/wallet/deploy/human.toml)"; then
		echo "fail registry toml=absent"
		finish 1
	fi

	answer="$(timeout "$timeout" flyctl machines list --app "$app" --json 2>/dev/null | python3 -c '
import json, sys
ms = json.load(sys.stdin)
started = [m for m in ms if m.get("state") == "started"]
mounts = [x for m in ms for x in (m.get("config") or {}).get("mounts") or [] if x.get("path") == "/data"]
print(len(ms), len(started), len(mounts))
' 2>/dev/null)" || answer=""
	read -r status code body <<<"${answer:-none none none}"
	if [ "$status" = 1 ] && [ "$code" = 1 ] && [ "$body" = 1 ]; then
		echo "pass machines app=$app machines=1 started=1 volumes=1"
	else
		echo "fail machines app=$app machines=$status started=$code volumes=$body"
		failures=$((failures + 1))
	fi

	if ! answer="$(python3 - "$repo_root/$toml" <<'PYCONFIG'
import sys, tomllib
with open(sys.argv[1], 'rb') as source:
    config = tomllib.load(source)
port = int(config['env']['LAYERX_REGISTRY_LISTEN'].rsplit(':', 1)[1])
assert 1 <= port <= 65535
private = not config.get('http_service') and not any(row.get('ports') for row in config.get('services', []))
print('https://' + config['app'] + '.internal:' + str(port) + '/healthz', 'private' if private else 'public')
PYCONFIG
)"; then
		echo "fail private-ingress app=$app configuration=invalid"
		finish 1
	fi
	read -r url ingress <<<"$answer"
	if [ "$ingress" = private ]; then
		echo "pass private-ingress app=$app url=$url"
	else
		echo "fail private-ingress app=$app public-service=present"
		failures=$((failures + 1))
	fi

	reply="$(printf '%s\n' "$script" | fly_ssh "$kernel" - "tls=$fly_tls_dir/human-event-client url=$url limit=$timeout sh -s")" || reply=""
	line="$(sed -n 's/^@@healthz //p' <<<"$reply" | head -n 1)"
	anonymous="$(sed -n 's/^@@anonymous //p' <<<"$reply" | head -n 1)"
	read -r status code body <<<"${line:-none none}"
	if [ "$status" = 0 ] && [ "$code" = 200 ] && [[ "$body" == *'"status":"ready"'* ]] && [ -n "$anonymous" ] && [ "$anonymous" != 0 ]; then
		echo "pass healthz url=$url from=$kernel http=200 status=ready anonymous=refused"
	else
		[ "${anonymous:-none}" != 0 ] || anonymous=admitted
		echo "fail healthz url=$url from=$kernel curl=${status:-none} http=${code:-none} anonymous=${anonymous:-none}"
		failures=$((failures + 1))
	fi

	# Serving readiness is the Fly check "serving" of the health listener, which
	# answers the verdict of the mTLS /healthz route; the service TCP check of
	# the mTLS port is the separate transport liveness. Both must pass on the
	# started machine.
	body="${url%/healthz}"
	answer="$(timeout "$timeout" flyctl machines list --app "$app" --json 2>/dev/null | python3 -c '
import json, re, sys
port = sys.argv[1]
started = [m for m in json.load(sys.stdin) if m.get("state") == "started"]
checks = {c.get("name"): c.get("status") for m in started[:1] for c in m.get("checks") or []}
transport = [s for n, s in checks.items() if re.fullmatch(r"servicecheck-[0-9]+-tcp-" + port, n or "")]
print(checks.get("serving") or "absent", transport[0] if len(transport) == 1 else "absent")
' "${body##*:}" 2>/dev/null)" || answer=""
	read -r status code <<<"${answer:-none none}"
	if [ "$status" = passing ] && [ "$code" = passing ]; then
		echo "pass serving app=$app serving=passing transport=passing"
	else
		echo "fail serving app=$app serving=$status transport=$code"
		failures=$((failures + 1))
	fi

	if [ "${registry_stage:-full}" = bootstrap ]; then
		finish "$failures"
	fi

	if [[ ! "$program" =~ ^[0-9a-f]{64}$ ]]; then
		echo "fail program-events program=unset want=CHECK_LIVE_REGISTRY_PROGRAM_ID"
		failures=$((failures + 1))
	else
		from=0
		answer=""
		for page in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16; do
			answer="$(curl -sS -m "$timeout" -H "content-type: application/json" \
				-d "{\"jsonrpc\":\"2.0\",\"id\":$page,\"method\":\"lx_getProgramEvents\",\"params\":[{\"topic\":\"$topic\",\"from_sequence\":$from,\"limit\":256}]}" \
				"$router/rpc" 2>/dev/null | python3 -c '
import json, sys
reply = json.load(sys.stdin)
if "result" not in reply:
    print("error", (reply.get("error") or {}).get("code", "none"))
    sys.exit()
result = reply["result"]
found = [e["sequence"] for e in result["events"] if e["program_id"] == sys.argv[1]]
if found:
    print("found", found[0])
elif result["events"] and result["next_sequence"] > int(sys.argv[2]):
    print("next", result["next_sequence"])
else:
    print("end", result["next_sequence"])
' "$program" "$from" 2>/dev/null)" || answer=""
			[ "${answer%% *}" = next ] || break
			from="${answer#next }"
		done
		case "${answer:-none}" in
		found\ *) echo "pass program-events program=$program sequence=${answer#found }" ;;
		end\ *)
			echo "fail program-events program=$program events=none next_sequence=${answer#end }"
			failures=$((failures + 1))
			;;
		*)
			echo "fail program-events program=$program answer=${answer:-none}"
			failures=$((failures + 1))
			;;
		esac
	fi

	answer="$(curl -sS -m "$timeout" -w "\n%{http_code}" "$router/readyz" 2>/dev/null)" || answer=""
	code="$(tail -n 1 <<<"$answer")"
	body="$(python3 -c 'import json, sys; print(json.loads(sys.argv[1])["components"]["program_registry"])' "$(head -n 1 <<<"$answer")" 2>/dev/null)" || body=none
	if [ "$body" = ready ]; then
		echo "pass router-readyz url=$router/readyz http=$code program_registry=ready"
	else
		echo "fail router-readyz url=$router/readyz http=${code:-none} program_registry=${body:-none}"
		failures=$((failures + 1))
	fi
	finish "$failures"
}

# check_registry_bootstrap: the registry checks that need no router gate.
check_registry_bootstrap() {
	registry_stage=bootstrap
	check_registry
}

# check_registry_plan: prints the four stages of the registry/router bring-up
# in order, each with the stage it requires, its prerequisites and the
# producer of each: material is issued once (ca.sh rows and Fly secrets) and
# reused by every later stage; the registry bootstrap needs only material and
# no router gate; router activation needs the registry bootstrap; the routed
# proof reads a signed registry receipt through the public unified interface.
# Reads only the repository's tomls; no network, no flyctl.
check_registry_plan() {
	local registry=platform/hosted/registry/fly.toml endpoint=human/wallet/deploy/endpoint.toml path
	for path in "$registry" "$endpoint"; do
		if ! grep -qE '^app = "[a-z0-9-]+"$' "$repo_root/$path" 2>/dev/null; then
			echo "fail registry-plan toml=absent $path"
			exit 1
		fi
	done
	if ! grep -q '^  LAYERX_REGISTRY_REQUEST_TOKEN_FILE = ' "$repo_root/$registry" ||
		! grep -q '^  LAYERX_REGISTRY_PUBLICATION_TOKEN_FILE = ' "$repo_root/$registry"; then
		echo "fail registry-plan toml=absent $registry"
		exit 1
	fi
	if ! grep -q '^  LAYERX_GATEWAY_PROGRAM_REGISTRY_URL = ' "$repo_root/$endpoint" ||
		! grep -q '^  LAYERX_GATEWAY_PROGRAM_REGISTRY_TOKEN_FILE = ' "$repo_root/$endpoint"; then
		echo "fail registry-plan toml=absent $endpoint"
		exit 1
	fi
	echo "stage 1 material requires=- needs=request-token,publication-token,gateway-client,registry,registry-event-client,REGISTRY_IDENTITY_TOKEN,REGISTRY_PROGRAM_EVENTS_TOKEN,REGISTRY_WEBHOOKS_EVENTS_TOKEN,LAYERX_REGISTRY_NODE_AUTHORIZATION,LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION,builder-rootfs,environment-tree-digest,replica-id,trust-history producers=request-token:init.sh:--prepare-material,publication-token:init.sh:--prepare-material,gateway-client:ca.sh:gateway-client,registry:ca.sh:registry,registry-event-client:ca.sh:registry-event-client,REGISTRY_IDENTITY_TOKEN:fly-secret:REGISTRY_IDENTITY_TOKEN,REGISTRY_PROGRAM_EVENTS_TOKEN:fly-secret:REGISTRY_PROGRAM_EVENTS_TOKEN,REGISTRY_WEBHOOKS_EVENTS_TOKEN:fly-secret:REGISTRY_WEBHOOKS_EVENTS_TOKEN,LAYERX_REGISTRY_NODE_AUTHORIZATION:fly-secret:LAYERX_REGISTRY_NODE_AUTHORIZATION,LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION:fly-secret:LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION,builder-rootfs:deploy:builder-environment,environment-tree-digest:deploy:builder-environment,replica-id:deploy:kernel-material,trust-history:deploy:kernel-material"
	echo "stage 2 registry-bootstrap requires=material needs=material-record,request-token,publication-token producers=material-record:stage:material,request-token:init.sh:--prepare-material,publication-token:init.sh:--prepare-material"
	echo "stage 3 router-activation requires=registry-bootstrap needs=registry-bootstrap,program-registry-token,client-identity,client-password producers=registry-bootstrap:stage:registry-bootstrap,program-registry-token:fly-secret:ENDPOINT_PROGRAM_REGISTRY_TOKEN,client-identity:fly-secret:ENDPOINT_CLIENT_P12,client-password:fly-secret:ENDPOINT_CLIENT_PASSWORD"
	echo "stage 4 routed-proof requires=router-activation needs=router-activation,registry-receipt producers=router-activation:stage:router-activation,registry-receipt:deploy:routed-proof"
	exit 0
}

# router_prerequisite: router activation requires a passed registry bootstrap,
# protected material and bootstrap records bound to the selected revision,
# image and material generation; refusal precedes every router request.
router_prerequisite() {
	if [ -z "${CHECK_LIVE_STAGE_DIR:-}" ]; then
		echo "check-live: CHECK_LIVE_STAGE_DIR is unset" >&2
		exit 2
	fi
	if ! python3 "$repo_root/tools/qualification/paxeer-x/registry-router-bootstrap.py" \
		--check-stage registry-bootstrap --stage-dir "$CHECK_LIVE_STAGE_DIR" 2>/dev/null; then
		echo "fail router-activation missing=registry-bootstrap producer=stage:registry-bootstrap"
		exit 1
	fi
}

# check_interop_adapters: three conformance legs of the AP2, Visa TAP and fiat
# adapters of the interop gateway app of platform/hosted/interop/fly.toml,
# each the make target of the tree followed by the adapter's entry of
# GET <CHECK_LIVE_INTEROP_ORIGIN>/v1/adapters (default
# https://interchain.paxeer.network), whose conformance suite, vector count
# and digest equal the suite interop/deploy/gateway/render.py derives from this
# checkout and whose four readiness fields are ready: mandates
# (interop-test-mandates, ap2), visa-tap (interop-test-visa-tap, visa-tap) and
# ramps-sandbox (interop-test-ramps-sandbox, fiat). The sandbox journey reads
# the LAYERX_RAMP_* inputs of platform/ramps/sandbox-journey.sh from the
# environment and its two bearer tokens from the files named by
# CHECK_LIVE_INTEROP_ADAPTERS_CUSTOMER_TOKEN_FILE and
# CHECK_LIVE_INTEROP_ADAPTERS_OPERATOR_TOKEN_FILE; no value is printed. One
# line per leg.
check_interop_adapters() {
	local toml=platform/hosted/interop/fly.toml name app origin w status http target exits=()
	for name in LAYERX_RAMP_URL LAYERX_RAMP_CA_PEM LAYERX_RAMP_OPERATOR_URL LAYERX_RAMP_ON_QUOTE_ID \
		LAYERX_RAMP_OFF_QUOTE_ID LAYERX_RAMP_OFF_GRANT_JSON LAYERX_RAMP_ON_ACCOUNT_SEQUENCE \
		LAYERX_RAMP_OFF_RECEIVER_SEQUENCE CHECK_LIVE_INTEROP_ADAPTERS_CUSTOMER_TOKEN_FILE \
		CHECK_LIVE_INTEROP_ADAPTERS_OPERATOR_TOKEN_FILE; do
		if [ -z "${!name:-}" ]; then
			echo "check-live: $name is unset" >&2
			exit 2
		fi
	done
	origin="${CHECK_LIVE_INTEROP_ORIGIN:-https://interchain.paxeer.network}"
	if ! app="$(fly_app "$toml")"; then
		echo "fail interop-adapters toml=absent"
		finish 1
	fi
	w="$(mktemp -d)"
	# shellcheck disable=SC2064 # the path expands now, the locals are gone at exit
	trap "rm -rf '$w'" EXIT

	for target in interop-test-mandates interop-test-visa-tap interop-test-ramps-sandbox; do
		status=0
		(
			if [ "$target" = interop-test-ramps-sandbox ]; then
				LAYERX_RAMP_CUSTOMER_TOKEN="$(<"$CHECK_LIVE_INTEROP_ADAPTERS_CUSTOMER_TOKEN_FILE")"
				LAYERX_RAMP_OPERATOR_TOKEN="$(<"$CHECK_LIVE_INTEROP_ADAPTERS_OPERATOR_TOKEN_FILE")"
				export LAYERX_RAMP_CUSTOMER_TOKEN LAYERX_RAMP_OPERATOR_TOKEN
			fi
			make -C "$repo_root" --no-print-directory "$target"
		) >"$w/$target.log" 2>&1 </dev/null || status=$?
		exits+=("$status")
	done
	http="$(curl -sS -m "$timeout" -o "$w/adapters" -w '%{http_code}' "$origin/v1/adapters" 2>/dev/null)" || http=""
	status=0
	python3 - "$repo_root" "$app" "$w/adapters" "${http:-none}" "${exits[@]}" <<'PY' || status=$?
import importlib.util, json, pathlib, sys

root, app, path, http = sys.argv[1:5]
exits = sys.argv[5:]
spec = importlib.util.spec_from_file_location("render", root + "/interop/deploy/gateway/render.py")
render = importlib.util.module_from_spec(spec)
spec.loader.exec_module(render)
try:
    served = {entry["id"]: entry for entry in json.load(open(path))["adapters"]}
except Exception:
    served = {}
failed = 0
for (leg, target, adapter), code in zip((("mandates", "interop-test-mandates", "ap2"),
                                         ("visa-tap", "interop-test-visa-tap", "visa-tap"),
                                         ("ramps-sandbox", "interop-test-ramps-sandbox", "fiat")), exits):
    entry = served.get(adapter)
    suite = render.first_party_suite(pathlib.Path(root), adapter)
    if entry is None:
        pins, ready = "absent", 0
    else:
        pins = "match" if suite is not None and (entry.get("conformance_suite"), entry.get("conformance_vectors"),
                                                 entry.get("conformance_sha256")) == suite else "differ"
        ready = sum(1 for value in (entry.get("readiness") or {}).values() if value == "ready")
    ok = code == "0" and http == "200" and pins == "match" and ready == 4
    failed += not ok
    print("%s %s app=%s make=%s exit=%s adapter=%s http=%s pins=%s readiness=%d/4" % (
        "pass" if ok else "fail", leg, app, target, code, adapter, http, pins, ready))
sys.exit(failed)
PY
	finish "$status"
}

check_kernel_node() {
	local app answer key value genesis="" network="" authority="" archive="" public="" core="" status="" lni="" head="" failures=0 probe
	local data="${CHECK_LIVE_KERNEL_DATA_DIR:-/data/layerx/node}" run="${CHECK_LIVE_KERNEL_RUN_DIR:-/run/layerx/node}"
	local init="${CHECK_LIVE_KERNEL_INIT_DIR:-/run/layerx/init}" origin="${CHECK_LIVE_KERNEL_ARCHIVE_ORIGIN:-}"
	local ca="${CHECK_LIVE_KERNEL_ARCHIVE_CA:-}" ctl="${CHECK_LIVE_KERNEL_CTL:-/usr/local/bin/layerxctl}"
	local -A clocks=()
	app="${CHECK_LIVE_KERNEL_APP:-}"
	if [ -z "$app" ]; then app="$(fly_app human/wallet/deploy/human.toml)" || app=""; fi
	if [ -z "$app" ]; then echo "fail kernel-node toml=absent"; finish 1; fi
	probe=$(cat <<'PY'
import hashlib, json, os, re, socket, ssl, subprocess, sys, urllib.request
from pathlib import Path
from urllib.parse import urlsplit

data, run, init = map(Path, sys.argv[1:4])
origin, ca, ctl = sys.argv[4:7]

def emit(name, value):
    print(name, json.dumps(value, separators=(',', ':')) if isinstance(value, dict) else value, flush=True)

def environment(path):
    raw = path.read_bytes()
    if len(raw) > 1048576:
        raise ValueError('environment bound')
    result = {}
    for line in raw.decode().splitlines():
        if not line: continue
        key, value = line.split('=', 1)
        if not re.fullmatch('LAYERX_[A-Z0-9_]+', key) or key in result or any(ord(c) < 32 for c in value):
            raise ValueError('environment syntax')
        result[key] = value
    return result

def read_json(url, authorization=None):
    parsed = urlsplit(url)
    if parsed.username or parsed.password or parsed.fragment or parsed.query:
        raise ValueError('origin syntax')
    if parsed.scheme != 'https' and not (parsed.scheme == 'http' and parsed.hostname in ('127.0.0.1', '::1')):
        raise ValueError('origin requires TLS or loopback')
    context = ssl.create_default_context(cafile=ca or None) if parsed.scheme == 'https' else None
    request = urllib.request.Request(url, headers={'Accept': 'application/json'})
    if authorization is not None: request.add_header('Authorization', 'Bearer ' + authorization)
    with urllib.request.urlopen(request, timeout=5, context=context) as response:
        if response.status != 200 or response.headers.get_content_type() != 'application/json':
            raise ValueError('HTTP contract')
        raw = response.read(1048577)
    if len(raw) > 1048576: raise ValueError('response bound')
    value = json.loads(raw)
    if not isinstance(value, dict): raise ValueError('response object')
    return value

node = replica = sequencer = {}
for name, path in (('node', data / 'node.env'), ('replica', data / 'replica.env'), ('sequencer', data / 'sequencer.env')):
    try:
        value = environment(path)
        if name == 'node': node = value
        elif name == 'replica': replica = value
        else: sequencer = value
    except (OSError, ValueError, UnicodeError): pass
try:
    manifest = data / 'genesis/genesis.manifest'
    if manifest.stat().st_size > 16777216: raise ValueError('manifest bound')
    emit('genesis', hashlib.sha256(manifest.read_bytes()).hexdigest())
except (OSError, ValueError): pass
public = node.get('LAYERX_NODE_SEQUENCER_PUBLIC_KEY') or sequencer.get('LAYERX_NODE_SEQUENCER_PUBLIC_KEY')
if public: emit('public', public)
try: emit('core', environment(run / 'core.env')['LAYERX_CORE_SEQUENCER_ID'])
except (OSError, ValueError, KeyError, UnicodeError): pass
try:
    port = int(replica['LAYERX_AUTHORITY_PORT'])
    if not 1 <= port <= 65535 or replica.get('LAYERX_AUTHORITY_ADDRESS') != '127.0.0.1': raise ValueError('authority address')
    authority_origin = 'http://127.0.0.1:' + str(port)
    value = read_json(authority_origin + '/v1/receipt-authority/status', replica['LAYERX_AUTHORITY_BEARER_TOKEN'])
    if value.get('authority_replica_id') != replica['LAYERX_AUTHORITY_REPLICA_ID'] or value.get('network_id') != int(node['LAYERX_NODE_NETWORK_ID']):
        raise ValueError('configured authority identity')
    emit('authority', value)
except (OSError, ValueError, KeyError): authority_origin = None
try:
    parsed = urlsplit(origin)
    if not origin or parsed.path not in ('', '/') or parsed.query or parsed.fragment: raise ValueError('archive origin')
    if parsed.hostname in ('127.0.0.1', '::1') and parsed.port == int(replica.get('LAYERX_AUTHORITY_PORT', '0')):
        raise ValueError('archive and authority origins must be distinct')
    for name, route in (('network', 'network'), ('head', 'head'), ('archive', 'readiness')):
        emit(name, read_json(origin.rstrip('/') + '/v1/sync/' + route))
except (OSError, ValueError, KeyError): pass
try:
    with socket.socket(socket.AF_UNIX) as connection:
        connection.settimeout(5)
        connection.connect(str(run / 'supervisor.sock'))
        connection.sendall(b'status\n')
        raw = bytearray()
        while b'\n' not in raw and len(raw) <= 65536:
            block = connection.recv(4096)
            if not block: break
            raw.extend(block)
        if len(raw) > 65536: raise ValueError('supervisor response bound')
        response = json.loads(raw)
        if not isinstance(response, dict): raise ValueError('supervisor object')
        emit('status', response)
except (OSError, ValueError): pass
try:
    uid = int(sequencer['LAYERX_NODE_LNI_ALLOWED_UID'])
    gid = int(sequencer['LAYERX_NODE_LNI_ALLOWED_GID'])
    command = [ctl, 'read-state', '--socket', str(run / 'layerxd.lni.sock'), '--network-id', node['LAYERX_NODE_NETWORK_ID'],
               '--actor', node['LAYERX_NODE_TREASURY_DID']]
    if os.geteuid() != uid:
        if os.geteuid() != 0: raise ValueError('LNI caller uid')
        command = ['setpriv', '--reuid=' + str(uid), '--regid=' + str(gid), '--clear-groups', *command]
    completed = subprocess.run(command, check=True, timeout=12, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    if len(completed.stdout) > 65536: raise ValueError('LNI response bound')
    value = json.loads(completed.stdout)
    if value.get('network_id') != int(node['LAYERX_NODE_NETWORK_ID']): raise ValueError('LNI network')
    emit('lni', value)
except (OSError, ValueError, KeyError, subprocess.SubprocessError): pass
for service in ('treasury-signer', 'layerxd', 'layerxd-authority', 'guarantor-1', 'guarantor-2'):
    try:
        uid, state, pid = (init / service).read_text().split()
        if state != 'running' or not pid.isascii() or not pid.isdecimal(): raise ValueError('service state')
        process = Path('/proc') / pid
        executable = Path(os.readlink(process / 'exe')).name
        argv = (process / 'cmdline').read_bytes().split(b'\0', 1)[0].decode()
        if executable != 'layerx-runtime-clock':
            emit('clock', service + ' ' + executable)
        elif Path(argv).name != 'layerx-runtime-clock':
            emit('clock', service + ' invalid-clock-argv')
        else:
            emit('clock', service + ' ' + argv)
    except (OSError, ValueError, UnicodeError): emit('clock', service + ' absent')
PY
)
	if [ "${CHECK_LIVE_KERNEL_LOCAL:-0}" = 1 ]; then
		answer="$(timeout "$timeout" python3 -I - "$data" "$run" "$init" "$origin" "$ca" "$ctl" <<<"$probe" 2>/dev/null)" || answer=""
	else
		command -v flyctl >/dev/null 2>&1 || { echo "check-live: flyctl is required" >&2; exit 2; }
		for value in "$data" "$run" "$init" "$origin" "$ca" "$ctl"; do
			[[ $value != *[!a-zA-Z0-9_./:\[\]-]* ]] || { echo "fail kernel-node unsafe-path-or-origin"; finish 1; }
		done
		answer="$(fly_ssh "$app" - "python3 -I - \"$data\" \"$run\" \"$init\" \"$origin\" \"$ca\" \"$ctl\"" <<<"$probe" 2>/dev/null)" || answer=""
	fi
	while read -r key value; do
		case "$key" in
		genesis) genesis=$value ;; network) network=$value ;; authority) authority=$value ;; archive) archive=$value ;;
		public) public=$value ;; core) core=$value ;; status) status=$value ;; lni) lni=$value ;; head) head=$value ;;
		clock) key=${value%% *}; value=${value#"$key"}; clocks[$key]=${value# } ;;
		esac
	done <<<"$answer"
	if python3 -I - "$genesis" "$network" "$authority" "$public" <<'PY'
import json, re, sys
try:
    digest, network, authority, public = sys.argv[1:]
    network, authority = json.loads(network), json.loads(authority)
    assert re.fullmatch('[0-9a-f]{64}', digest)
    assert network['version'] == authority['version'] == 1
    assert type(network['network_id']) is int and network['network_id'] > 0
    assert network['network_id'] == authority['network_id']
    assert network['genesis_sha256'] == authority['genesis_sha256'] == digest
    assert network['sequencer_public_key'] == authority['sequencer_public_key'] == public
except (ValueError, KeyError, TypeError, AssertionError): sys.exit(1)
PY
	then echo "pass genesis app=$app sha256=$genesis replica=match"; else
		echo "fail genesis app=$app sha256=${genesis:-absent} replica=none"; failures=$((failures + 1)); fi
	value="$(printf 'layerx-sequencer:%s' "$public" | sha256sum | cut -d' ' -f1)"
	if [[ $public =~ ^[0-9a-f]{64}$ ]] && [ "$value" = "$core" ]; then echo "pass sequencer public=$public core=match"; else
		echo "fail sequencer public=${public:-absent} core=${core:-absent}"; failures=$((failures + 1)); fi
	if [ "$(python3 -I -c 'import json,sys; print(json.loads(sys.argv[1]).get("state"))' "$status" 2>/dev/null)" = running ]; then
		echo "pass supervisor state=running"; else echo "fail supervisor status=${status:-absent}"; failures=$((failures + 1)); fi
	if python3 -I - "$lni" "$authority" <<'PY'
import json, re, sys
try:
    lni, authority = map(json.loads, sys.argv[1:])
    assert lni['evidence'] == 'authenticated_node_snapshot' and lni['protocol_version'] == 3
    assert lni['network_id'] == authority['network_id']
    assert type(lni['global_sequence']) is int and lni['global_sequence'] >= 0
    assert re.fullmatch('[0-9a-f]{64}', lni['state_root'])
except (ValueError, KeyError, TypeError, AssertionError): sys.exit(1)
PY
	then echo "pass lni socket=present"; else echo "fail lni socket=absent"; failures=$((failures + 1)); fi
	local head_failed=0
	if value="$(python3 -I - "$authority" "$lni" "$genesis" "$public" <<'PY'
import json, re, sys
try:
    authority, lni = map(json.loads, sys.argv[1:3])
    digest, public = sys.argv[3:]
    assert authority['version'] == 1 and authority['ready'] is True
    assert authority['genesis_sha256'] == digest and authority['sequencer_public_key'] == public
    assert authority['network_id'] == lni['network_id']
    for name in ('authority_replica_id', 'receipt_digest', 'head_batch_id'):
        assert re.fullmatch('[0-9a-f]{64}', authority[name])
    for name in ('head_batch', 'last_global_sequence'):
        assert re.fullmatch('0|[1-9][0-9]*', authority[name]) and int(authority[name]) <= 18446744073709551615
    assert lni['global_sequence'] == int(authority['last_global_sequence'])
    print(json.dumps({'head': int(authority['head_batch'])}, separators=(',', ':')))
except (ValueError, KeyError, TypeError, AssertionError): sys.exit(1)
PY
)"; then echo "pass replica head=$value"; else
		echo "fail replica head=${authority:-absent}"; head_failed=1; fi
	if python3 -I - "$head" "$archive" "$authority" "$genesis" "$public" <<'PY'
import json, re, sys
try:
    head, archive, authority = map(json.loads, sys.argv[1:4])
    digest, public = sys.argv[4:]
    assert archive['ready'] is True and archive['recovered_current_process'] is True and archive['freshness'] == 'fresh'
    for field in ('version', 'network_id', 'genesis_sha256', 'head_batch', 'head_batch_id'):
        assert head[field] == archive[field] == authority[field]
    assert head['genesis_sha256'] == digest and archive['sequencer_public_key'] == authority['sequencer_public_key'] == public
    assert re.fullmatch('[0-9a-f]{64}', head['head_batch_id'])
    assert re.fullmatch('[0-9a-f]{64}', head['head_raw_sha256'])
    assert head['head_raw_sha256'] == archive['head_raw_sha256']
except (ValueError, KeyError, TypeError, AssertionError): sys.exit(1)
PY
	then echo "pass archive head=$head"; else echo "fail archive head=${head:-absent}"; head_failed=1; fi
	failures=$((failures + head_failed))

	for key in treasury-signer layerxd layerxd-authority guarantor-1 guarantor-2; do
		value=${clocks[$key]:-absent}
		if [ "${value##*/}" = layerx-runtime-clock ]; then echo "pass clock $key"; else
			echo "fail clock $key exec=$value"; failures=$((failures + 1)); fi
	done
	finish "$failures"
}

# check_xweb_attestors: each of the four apps of
# interop/deploy/x-websearch/attestor-<N>.toml runs one started machine with
# its volume and holds no public IP; inside it, through flyctl ssh console,
# the attestor answers /health on 8480 and every other attestor answers
# /health through its loopback hop 849<M>, and its volume holds the three
# key roles of the toml (/data/keys/attestor.key the web signer,
# submitter.key the EVM submitter, receiver.key the kernel receiver and
# submitter DID) as regular files readable by the owner only, no two of the
# same digest (compared on the machine, never printed); no VALIDATOR_HOSTS
# destination runs an x-websearch unit. One line per check:
#   "pass machines app=<app> machines=1 started=1 volumes=1"
#   "pass public-ips app=<app> count=0"
#   "pass health app=<app> http=200"
#   "pass hop app=<app> peer=<peer app> port=849<M> http=200"
#   "pass keys app=<app> roles=attestor,submitter,receiver private=yes distinct=yes"
#   "pass VALIDATOR_HOSTS[k] x-websearch-units=0"
# with fail and the observed values (ssh=<exit> when a host did not answer).
check_xweb_attestors() {
	local -a apps dests
	local n m answer n_machines n_started n_mounts code status failures=0
	for n in 1 2 3 4; do
		apps[n]="$(fly_app "interop/deploy/x-websearch/attestor-$n.toml")" || apps[n]=""
	done
	for n in 1 2 3 4; do
		if [ -z "${apps[n]}" ]; then
			echo "fail attestor-$n toml=absent"
			failures=$((failures + 1))
			continue
		fi
		answer="$(timeout "$timeout" flyctl machines list --app "${apps[n]}" --json 2>/dev/null | python3 -c '
import json, sys
ms = json.load(sys.stdin)
started = [m for m in ms if m.get("state") == "started"]
mounts = [x.get("volume", "") for m in ms for x in (m.get("config") or {}).get("mounts") or []]
print(len(ms), len(started), len(mounts))
' 2>/dev/null)" || answer=""
		read -r n_machines n_started n_mounts <<<"${answer:-none none none}"
		if [ "$n_machines" = 1 ] && [ "$n_started" = 1 ] && [ "$n_mounts" = 1 ]; then
			echo "pass machines app=${apps[n]} machines=1 started=1 volumes=1"
		else
			echo "fail machines app=${apps[n]} machines=$n_machines started=$n_started volumes=$n_mounts"
			failures=$((failures + 1))
		fi
		answer="$(timeout "$timeout" flyctl ips list --app "${apps[n]}" --json 2>/dev/null | python3 -c 'import json, sys; print(len(json.load(sys.stdin) or []))' 2>/dev/null)" || answer=none
		if [ "$answer" = 0 ]; then
			echo "pass public-ips app=${apps[n]} count=0"
		else
			echo "fail public-ips app=${apps[n]} count=${answer:-none}"
			failures=$((failures + 1))
		fi
		# The image carries socat, not curl: one HTTP/1.0 request per port,
		# the status code of the answer's first line.
		answer="$(fly_ssh "${apps[n]}" - "for p in 8480 8491 8492 8493 8494; do printf \"%s \" \$p; printf \"GET /health HTTP/1.0\\r\\nHost: 127.0.0.1\\r\\n\\r\\n\" | timeout $timeout socat -t $timeout - TCP:127.0.0.1:\$p 2>/dev/null | head -n 1 | cut -d\" \" -f2; echo; done")" || answer=""
		code="$(awk '$1 == 8480 { print $2 }' <<<"$answer")"
		if [ "$code" = 200 ]; then
			echo "pass health app=${apps[n]} http=200"
		else
			echo "fail health app=${apps[n]} http=${code:-none}"
			failures=$((failures + 1))
		fi
		for m in 1 2 3 4; do
			[ "$m" -ne "$n" ] || continue
			code="$(awk -v p=$((8490 + m)) '$1 == p { print $2 }' <<<"$answer")"
			if [ "$code" = 200 ]; then
				echo "pass hop app=${apps[n]} peer=${apps[m]:-attestor-$m} port=$((8490 + m)) http=200"
			else
				echo "fail hop app=${apps[n]} peer=${apps[m]:-attestor-$m} port=$((8490 + m)) http=${code:-none}"
				failures=$((failures + 1))
			fi
		done
		answer="$(fly_ssh "${apps[n]}" - "for r in attestor submitter receiver; do f=/data/keys/\$r.key; if [ -f \$f ] && [ ! -L \$f ]; then echo \$r \$(stat -c %a \$f) \$(sha256sum <\$f | cut -c1-64); else echo \$r absent; fi; done")" || answer=""
		answer="$(python3 -c '
import sys
rows = {}
for line in sys.argv[1].splitlines():
    parts = line.split()
    if parts and parts[0] in ("attestor", "submitter", "receiver"):
        rows[parts[0]] = parts[1:]
roles = ("attestor", "submitter", "receiver")
missing = [r for r in roles if len(rows.get(r, [])) != 2]
if missing:
    print("fail roles=%s absent=%s" % (",".join(roles), ",".join(missing)))
    sys.exit()
open_ = [r for r in roles if rows[r][0] not in ("600", "400")]
distinct = len({rows[r][1] for r in roles}) == 3
ok = not open_ and distinct
print("%s roles=%s private=%s distinct=%s" % ("pass" if ok else "fail", ",".join(roles),
      "yes" if not open_ else "no:" + ",".join(open_), "yes" if distinct else "no"))
' "$answer")"
		echo "${answer%% *} keys app=${apps[n]} ${answer#* }"
		[ "${answer%% *}" = pass ] || failures=$((failures + 1))
	done
	read -r -a dests <<<"$VALIDATOR_HOSTS"
	for n in "${!dests[@]}"; do
		status=0
		answer="$(ssh_read "${dests[$n]}" 'systemctl list-units --no-legend --plain --state=active,activating,reloading "x-websearch*" | wc -l')" || status=$?
		if [ "$status" -ne 0 ]; then
			echo "fail VALIDATOR_HOSTS[$n] x-websearch-units ssh=$status"
			failures=$((failures + 1))
		elif [ "$answer" = 0 ]; then
			echo "pass VALIDATOR_HOSTS[$n] x-websearch-units=0"
		else
			echo "fail VALIDATOR_HOSTS[$n] x-websearch-units=${answer:-none}"
			failures=$((failures + 1))
		fi
	done
	finish "$failures"
}

check_kernel_value_loop() {
	local app key value answer status=0 failures=0
	local -a arguments=()
	if ! command -v flyctl >/dev/null 2>&1; then
		echo "check-live: flyctl is required" >&2
		exit 2
	fi
	if ! app="$(fly_app human/wallet/deploy/human.toml)"; then
		echo "fail kernel-value-loop toml=absent"
		finish 1
	fi
	[ -z "${CHECK_LIVE_VALUE_LOOP_DEPOSIT_TX:-}" ] || arguments+=("DEPOSIT_TX=$CHECK_LIVE_VALUE_LOOP_DEPOSIT_TX")
	[ -z "${CHECK_LIVE_VALUE_LOOP_CHECKPOINT_SECONDS:-}" ] || arguments+=("CHECKPOINT_SECONDS=$CHECK_LIVE_VALUE_LOOP_CHECKPOINT_SECONDS")
	if [ -n "${CHECK_LIVE_VALUE_LOOP_ASSET:-}" ]; then
		case "$CHECK_LIVE_VALUE_LOOP_ASSET" in
		PAX|SID|USDC|USDL) arguments+=("ASSET=$CHECK_LIVE_VALUE_LOOP_ASSET") ;;
		*) echo "fail kernel-value-loop asset=invalid"; finish 1 ;;
		esac
	fi
	answer="$(timeout "${CHECK_LIVE_VALUE_LOOP_TIMEOUT:-840}" flyctl ssh console --quiet --app "$app" \
		--command "sh -c 'LAYERX_KERNEL_DATA=/data/layerx/ LAYERX_KERNEL_RUN=/run/layerx/ LAYERX_KERNEL_TLS=/data/tls/ bash -s -- ${arguments[*]}'" \
		<"$repo_root/tools/bringup/value-loop.sh" 2>/dev/null)" || status=$?
	while read -r key value; do
		case "$key" in
		precondition)
			echo "fail precondition $value"
			finish 1
			;;
		asset | account | credit | activity | balance | batch | checkpoint)
			if [ "$status" -ne 1 ] || [ "$key $value" != "$(tail -n 1 <<<"$answer")" ]; then
				echo "pass $key $value"
			else
				echo "fail $key $value"
				failures=$((failures + 1))
			fi
			;;
		esac
	done <<<"$answer"
	if [ "$status" -ne 0 ] && [ "$failures" -eq 0 ]; then
		echo "fail kernel-value-loop app=$app exit=$status"
		failures=1
	fi
	if [ "$status" -eq 0 ] && ! grep -q '^checkpoint ' <<<"$answer"; then
		echo "fail checkpoint status=absent"
		failures=1
	fi
	finish "$failures"
}

# check_events_upstream: the kernel app of human/wallet/deploy/human.toml
# lists the three event-token names its [[files]] entries mount; the journeys
# and approvals machines of the app of platform/hosted/internal/fly.toml each
# hold a non-empty /data/run/producer-token, read as a byte count; the apps of
# platform/hosted/webhooks/fly.toml and platform/hosted/registry/fly.toml
# exist; and each webhooks trigger consumer lists its trigger name
# (platform/hosted/webhooks/fly.toml, human/wallet/deploy/endpoint.toml,
# human/wallet/deploy/human.toml, docker/platform-registry/init.sh). Prints
# secret names and byte counts only, never a value. One line per check.
check_events_upstream() {
	local kernel internal webhooks endpoint registry group bytes apps name app listed row rest failures=0
	local -a names=()
	if ! command -v flyctl >/dev/null 2>&1; then
		echo "check-live: flyctl is required" >&2
		exit 2
	fi
	if ! kernel="$(fly_app human/wallet/deploy/human.toml)" || ! internal="$(fly_app platform/hosted/internal/fly.toml)" ||
		! webhooks="$(fly_app platform/hosted/webhooks/fly.toml)" || ! endpoint="$(fly_app human/wallet/deploy/endpoint.toml)" ||
		! registry="$(fly_app platform/hosted/registry/fly.toml)"; then
		echo "fail events-upstream toml=absent"
		finish 1
	fi

	for group in journeys approvals; do
		bytes="$(fly_ssh "$internal" "$group" 'wc -c </data/run/producer-token 2>/dev/null || echo absent' </dev/null | tail -n 1 | tr -d ' ')" || bytes=""
		if [[ "$bytes" =~ ^[0-9]+$ ]] && [ "$bytes" -gt 0 ]; then
			echo "pass producer-token app=$internal group=$group bytes=$bytes"
		else
			echo "fail producer-token app=$internal group=$group bytes=${bytes:-unreachable}"
			failures=$((failures + 1))
		fi
	done

	apps="$(timeout "$timeout" flyctl apps list --json 2>/dev/null | python3 -c 'import json, sys; print(" ".join(a.get("Name") or a.get("name") or "" for a in json.load(sys.stdin) or []))' 2>/dev/null)" || apps=unreadable
	for name in "$webhooks" "$registry"; do
		if [ "$apps" = unreadable ]; then
			echo "fail app app=$name exists=unreadable"
			failures=$((failures + 1))
		elif [[ " $apps " == *" $name "* ]]; then
			echo "pass app app=$name exists=yes"
		else
			echo "fail app app=$name exists=no"
			failures=$((failures + 1))
		fi
	done

	for row in "$kernel HUMAN_EVENTS_JOURNEY_TOKEN HUMAN_EVENTS_APPROVAL_TOKEN HUMAN_EVENTS_WEBHOOKS_TOKEN" \
		"$webhooks WEBHOOKS_SOURCE_TRIGGER_TOKEN" "$endpoint ENDPOINT_EVENTS_WEBHOOKS_TOKEN" "$registry REGISTRY_WEBHOOKS_EVENTS_TOKEN"; do
		read -r app rest <<<"$row"
		read -r -a names <<<"$rest"
		listed="$(timeout "$timeout" flyctl secrets list --app "$app" --json 2>/dev/null | python3 -c 'import json, sys; print(" ".join(s.get("Name") or s.get("name") or "" for s in json.load(sys.stdin) or []))' 2>/dev/null)" || listed=unreadable
		for name in "${names[@]}"; do
			if [ "$listed" = unreadable ]; then
				echo "fail secret app=$app name=$name listed=unreadable"
				failures=$((failures + 1))
			elif [[ " $listed " == *" $name "* ]]; then
				echo "pass secret app=$app name=$name listed=yes"
			else
				echo "fail secret app=$app name=$name listed=no"
				failures=$((failures + 1))
			fi
		done
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
kernel-value-loop) ;;
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
mirrors) ;;
relay) ;;
interop) ;;
ramp) ;;
indexer) ;;
developers) ;;
router) ;;
search) ;;
registry) ;;
registry-bootstrap) ;;
registry-plan) ;;
interop-adapters) ;;
xweb-attestors) ;;
kernel-boundaries) ;;
events-upstream) ;;
human) ;;
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
[ "$mode" != relay ] || tools+=(flyctl)
[ "$mode" != interop-adapters ] || tools+=(make)
[ "$mode" != registry-plan ] || tools=(grep)
if [ "$mode" = kernel-node ] && [ "${CHECK_LIVE_KERNEL_LOCAL:-0}" = 1 ]; then tools=(timeout python3 sha256sum); fi
for tool in "${tools[@]}"; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "check-live: $tool is required" >&2
		exit 2
	fi
done

case "$mode" in
explorer | registry-plan) ;;
relay) ;;
kernel-node) [ "${CHECK_LIVE_KERNEL_LOCAL:-0}" = 1 ] || load_hosts ;;
*) load_hosts ;;
esac
"check_${mode//-/_}"
