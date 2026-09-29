#!/usr/bin/env bash
set -euo pipefail

usage() {
	cat <<'EOF'
usage: tools/bringup/check-live.sh hosts | ca

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

Environment:
  BRINGUP_HOSTS_FILE   private env file assigning EDGE_HOST, KERNEL_HOST,
                       PLATFORM_HOST, EXPLORER_HOST, ARCHIVE_HOST,
                       VALIDATOR_HOSTS, RPC_HOSTS and HPX_HOST; each value is
                       one ssh destination or, for the plural roles, a
                       space-separated list of them
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

# Sourced by tools/bringup/ca.sh for the host map and the ssh helpers: the
# probe's own dispatch below runs only when this file is executed.
[ "${BASH_SOURCE[0]}" = "$0" ] || return 0

mode="${1:-}"
case "$mode" in
-h | --help)
	usage
	exit 0
	;;
hosts | ca) ;;
*)
	usage >&2
	exit 2
	;;
esac

if [ "$#" -ne 1 ]; then
	usage >&2
	exit 2
fi

for tool in ssh timeout openssl; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "check-live: $tool is required" >&2
		exit 2
	fi
done

load_hosts
"check_${mode//-/_}"
