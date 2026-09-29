#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'EOF'
usage: tools/bringup/check-live.sh hosts | hpx

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

Environment:
  BRINGUP_HOSTS_FILE   private env file assigning EDGE_HOST, KERNEL_HOST,
                       PLATFORM_HOST, EXPLORER_HOST, ARCHIVE_HOST,
                       VALIDATOR_HOSTS, RPC_HOSTS and HPX_HOST; each value is
                       one ssh destination or, for the plural roles, a
                       space-separated list of them
  CHECK_LIVE_HPX_ORIGIN  origin of the hpx registry, default
                       https://node.hyperpaxeer.com
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
hosts | hpx) ;;
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

case "$mode" in
hosts) tools=(ssh timeout) ;;
hpx) tools=(curl python3 sha256sum) ;;
esac
for tool in "${tools[@]}"; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "check-live: $tool is required" >&2
		exit 2
	fi
done

roles=(EDGE_HOST KERNEL_HOST PLATFORM_HOST EXPLORER_HOST ARCHIVE_HOST VALIDATOR_HOSTS RPC_HOSTS HPX_HOST)

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

load_hosts
"check_${mode//-/_}"
