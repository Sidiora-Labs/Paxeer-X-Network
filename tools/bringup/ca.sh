#!/usr/bin/env bash
set -euo pipefail

# The host map loader, the ssh helpers and the SAN rules are the probe's.
# shellcheck source=tools/bringup/check-live.sh
. "$(dirname "${BASH_SOURCE[0]}")/check-live.sh"

usage() {
	cat <<'EOF'
usage: tools/bringup/ca.sh init | issue <service> <role> | services

The internal CA of the Paxeer X Network bring-up. Runs on the edge host, the
only host that holds the CA key.

init      generates the CA key and certificate under LAYERX_CA_DIR with mode
          0600 and prints nothing but the certificate's SHA-256 fingerprint.
          Refuses to touch a directory that already holds a CA.

issue <service> <role>
          issues the certificate of one service on every destination of its
          role in the host map. The host generates its own key and signing
          request under LAYERX_ETC_DIR/<service>/tls and only the request
          comes back; the CA signs it with the service's SAN list plus the
          host's own address; the certificate is delivered with the CA
          certificate, and the host derives key.der, cert.der, ca.der and
          identity.p12 locked by a password file it generates. The key never
          leaves the host. Prints one line per destination:
          "issued <service> <role> fingerprint=<sha256> expires_in=<days>d".

services  prints the service list, one per line: service, host-map role,
          common name, extended key usage, SAN list ("-" for a client
          identity).

Environment:
  BRINGUP_HOSTS_FILE   the operator's private host map, see check-live.sh
  CHECK_LIVE_TIMEOUT   seconds per ssh call, default 30
  LAYERX_CA_DIR        the CA directory, default /etc/layerx/ca
  LAYERX_ETC_DIR       the service directory root on every host, default
                       /etc/layerx

Exits 1 when the CA is missing, already present on init, or a host step
fails; 2 on a usage error or a host map lacking a role.
EOF
}

subject_org="Paxeer X Network"
ca_days=3650
cert_days=397

# ca_services: every certificate the bring-up issues, after the issue_cert
# calls of platform/hosted/tests/beta-cluster.sh: service, host-map role,
# common name, extended key usage, SAN list. A service reached on loopback
# carries localhost and 127.0.0.1; a public name is the one the spec serves.
ca_services() {
	cat <<'EOF'
pending-core KERNEL_HOST layerx-pending-core serverAuth DNS:layerx-pending-core,DNS:localhost,IP:127.0.0.1
pending-core-admin KERNEL_HOST layerx-pending-core-admin serverAuth DNS:layerx-pending-core-admin
receipt-authority KERNEL_HOST layerx-receipt-authority serverAuth DNS:layerx-receipt-authority,DNS:authority,DNS:localhost,IP:127.0.0.1
agent-boundary KERNEL_HOST layerx-agent-boundary serverAuth DNS:layerx-agent-boundary,DNS:component,DNS:localhost,IP:127.0.0.1
agentd KERNEL_HOST layerx-agentd serverAuth DNS:layerx-agentd,DNS:agent.paxeer.network,DNS:localhost,IP:127.0.0.1
agentd-client KERNEL_HOST layerx-agentd-client clientAuth -
paxeer-boundary-loopback KERNEL_HOST paxeer-boundary serverAuth DNS:paxeer-boundary,DNS:paxeer-boundary-loopback,DNS:localhost,IP:127.0.0.1
paxeer-boundary-public KERNEL_HOST paxeer-observer-boundary serverAuth DNS:paxeer-observer-boundary,DNS:paxeer-boundary-public,DNS:localhost,IP:127.0.0.1
guarantor KERNEL_HOST layerx-guarantor serverAuth,clientAuth DNS:localhost,IP:127.0.0.1
human KERNEL_HOST layerx-human serverAuth DNS:layerx-human,DNS:human.paxeer.network,DNS:localhost,IP:127.0.0.1
human-event-client KERNEL_HOST layerx-human-events clientAuth -
gateway PLATFORM_HOST layerx-gateway serverAuth DNS:layerx-gateway,DNS:api.mainnet-beta.router.paxeer.network,DNS:localhost,IP:127.0.0.1
gateway-redis PLATFORM_HOST layerx-gateway-redis serverAuth DNS:layerx-gateway-redis,DNS:localhost,IP:127.0.0.1
gateway-client PLATFORM_HOST layerx-gateway clientAuth -
identity PLATFORM_HOST layerx-identity serverAuth DNS:layerx-identity,DNS:identity,DNS:identity.paxeer.network,DNS:localhost,IP:127.0.0.1
internal-kms PLATFORM_HOST kms serverAuth DNS:kms,DNS:localhost,IP:127.0.0.1
internal-journeys PLATFORM_HOST journeys serverAuth DNS:journeys,DNS:localhost,IP:127.0.0.1
internal-payments PLATFORM_HOST payments serverAuth DNS:payments,DNS:localhost,IP:127.0.0.1
internal-approvals PLATFORM_HOST approvals serverAuth DNS:approvals,DNS:localhost,IP:127.0.0.1
internal-programs PLATFORM_HOST programs serverAuth DNS:programs,DNS:localhost,IP:127.0.0.1
registry PLATFORM_HOST layerx-program-registry serverAuth DNS:layerx-program-registry,DNS:registry.paxeer.network,DNS:localhost,IP:127.0.0.1
registry-event-client PLATFORM_HOST layerx-registry-events clientAuth -
relay-archive PLATFORM_HOST layerx-relay-archive serverAuth DNS:layerx-relay-archive,DNS:archive.paxeer.network,DNS:localhost,IP:127.0.0.1
interop-gateway PLATFORM_HOST layerx-interop-gateway serverAuth DNS:layerx-interop-gateway,DNS:interop.paxeer.network,DNS:localhost,IP:127.0.0.1
interop-client PLATFORM_HOST layerx-interop-gateway clientAuth -
developer PLATFORM_HOST layerx-developer serverAuth DNS:layerx-webhooks,DNS:layerx-dashboard-api,DNS:webhooks.paxeer.network,DNS:api.developers.paxeer.network,DNS:developers.paxeer.network,DNS:localhost,IP:127.0.0.1
developer-client PLATFORM_HOST layerx-developer clientAuth -
ramp PLATFORM_HOST layerx-reference-ramp serverAuth DNS:layerx-reference-ramp,DNS:layerx-reference-ramp-operator,DNS:ramp.paxeer.network,DNS:localhost,IP:127.0.0.1
gas-station PLATFORM_HOST layerx-gas-station serverAuth DNS:layerx-gas-station,DNS:gas.paxeer.network,DNS:localhost,IP:127.0.0.1
x-websearch VALIDATOR_HOSTS layerx-x-websearch serverAuth DNS:layerx-x-websearch,DNS:search.paxeer.network,DNS:localhost,IP:127.0.0.1
EOF
}

# ssh_host <destination> <command>: runs a command on the destination without
# prompting, bounded by CHECK_LIVE_TIMEOUT, with stdin and stderr passed
# through.
ssh_host() {
	timeout "$timeout" ssh -o BatchMode=yes -- "$1" "$2"
}

ca_init() {
	if [ -e "$ca_dir/ca.key" ] || [ -e "$ca_dir/ca.pem" ]; then
		echo "ca: $ca_dir already holds a CA; a new one invalidates every issued certificate, so remove it deliberately first" >&2
		exit 1
	fi
	mkdir -p "$ca_dir"
	chmod 0700 "$ca_dir"
	(
		umask 077
		openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$ca_dir/ca.key" 2>/dev/null
		openssl req -x509 -new -key "$ca_dir/ca.key" -days "$ca_days" -sha256 \
			-subj "/O=$subject_org/CN=LayerX internal CA" \
			-addext 'basicConstraints=critical,CA:TRUE,pathlen:0' -addext 'keyUsage=critical,keyCertSign,cRLSign' \
			-out "$ca_dir/ca.pem" 2>/dev/null
		openssl x509 -in "$ca_dir/ca.pem" -outform DER -out "$ca_dir/ca.der"
	)
	chmod 0600 "$ca_dir/ca.key" "$ca_dir/ca.pem" "$ca_dir/ca.der"
	openssl x509 -in "$ca_dir/ca.pem" -noout -fingerprint -sha256 | cut -d= -f2
}

# issue_one <service> <role> <destination> <cn> <eku> <sans>: one certificate
# on one destination; the key is made there and stays there.
issue_one() {
	local service="$1" role="$2" dest="$3" cn="$4" eku="$5" sans="$6" dir ca_pem
	dir="$etc_dir/$service/tls"
	ssh_host "$dest" "umask 077 && mkdir -p '$dir' && chmod 0700 '$dir' && cd '$dir' && openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out key.pem.new && openssl req -new -key key.pem.new -subj '/O=$subject_org/CN=$cn'" </dev/null >"$work/csr.pem" || {
		echo "ca: $service on $role: the host did not produce a signing request" >&2
		exit 1
	}
	openssl req -in "$work/csr.pem" -noout -verify >/dev/null 2>&1 || {
		echo "ca: $service on $role: the signing request does not verify" >&2
		exit 1
	}
	sans="$(expected_sans "$eku" "$sans" "$dest")"
	{
		printf 'basicConstraints=CA:FALSE\nkeyUsage=digitalSignature,keyEncipherment\nextendedKeyUsage=%s\n' "$eku"
		[ -z "$sans" ] || printf 'subjectAltName=%s\n' "$sans"
	} >"$work/ext.cnf"
	openssl x509 -req -in "$work/csr.pem" -CA "$ca_dir/ca.pem" -CAkey "$ca_dir/ca.key" -CAcreateserial \
		-days "$cert_days" -sha256 -extfile "$work/ext.cnf" -out "$work/cert.pem" 2>/dev/null
	ca_pem="$(cat "$ca_dir/ca.pem")"
	ssh_host "$dest" "umask 077 && cd '$dir' && cat >cert.pem.new && printf '%s\n' '$ca_pem' >ca.pem.new && openssl verify -CAfile ca.pem.new cert.pem.new >/dev/null && mv ca.pem.new ca.pem && mv key.pem.new key.pem && mv cert.pem.new cert.pem && openssl x509 -in cert.pem -outform DER -out cert.der && openssl pkcs8 -topk8 -nocrypt -in key.pem -outform DER -out key.der && openssl x509 -in ca.pem -outform DER -out ca.der && openssl rand -hex 32 >password && openssl pkcs12 -export -inkey key.pem -in cert.pem -certfile ca.pem -name '$cn' -passout file:password -out identity.p12" <"$work/cert.pem" || {
		echo "ca: $service on $role: the host did not accept the certificate" >&2
		exit 1
	}
	echo "issued $service $role fingerprint=$(openssl x509 -in "$work/cert.pem" -noout -fingerprint -sha256 | cut -d= -f2) expires_in=$(days_left <"$work/cert.pem")d"
}

ca_issue() {
	local service="$1" role="$2" line table_role cn eku sans dests dest
	line="$(ca_services | awk -v s="$service" '$1 == s')"
	if [ -z "$line" ]; then
		echo "ca: unknown service $service; see tools/bringup/ca.sh services" >&2
		exit 2
	fi
	read -r _ table_role cn eku sans <<<"$line"
	if [ "$role" != "$table_role" ]; then
		echo "ca: $service lives on $table_role, not $role" >&2
		exit 2
	fi
	if [ ! -r "$ca_dir/ca.key" ] || [ ! -r "$ca_dir/ca.pem" ]; then
		echo "ca: no CA under $ca_dir; run tools/bringup/ca.sh init on the edge host" >&2
		exit 1
	fi
	load_hosts
	work="$(mktemp -d)"
	trap 'rm -rf "$work"' EXIT
	read -r -a dests <<<"${!role}"
	for dest in "${dests[@]}"; do
		issue_one "$service" "$role" "$dest" "$cn" "$eku" "$sans"
	done
}

mode="${1:-}"
case "$mode" in
-h | --help)
	usage
	exit 0
	;;
init | services)
	[ "$#" -eq 1 ] || {
		usage >&2
		exit 2
	}
	;;
issue)
	[ "$#" -eq 3 ] || {
		usage >&2
		exit 2
	}
	;;
*)
	usage >&2
	exit 2
	;;
esac

for tool in ssh timeout openssl; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "ca: $tool is required" >&2
		exit 2
	fi
done

case "$mode" in
init) ca_init ;;
services) ca_services ;;
issue) ca_issue "$2" "$3" ;;
esac
