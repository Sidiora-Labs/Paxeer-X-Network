#!/usr/bin/env bash
set -euo pipefail

# The Fly helpers, the CA settings and the certificate arithmetic are the
# probe's.
# shellcheck source=tools/bringup/check-live.sh
. "$(dirname "${BASH_SOURCE[0]}")/check-live.sh"

usage() {
	cat <<'EOF'
usage: tools/bringup/ca.sh init | issue <service> | services

The internal CA of the Paxeer X Network bring-up. Runs on the edge host, the
operator host that holds the CA key and the Fly login.

init      generates the CA key and certificate under LAYERX_CA_DIR with mode
          0600 and prints nothing but the certificate's SHA-256 fingerprint.
          Refuses to touch a directory that already holds a CA.

issue <service>
          issues the certificate of one service for the Fly app whose toml
          the service's row names, with the row's SAN list and <app> read as
          the app name from the toml's app line. For a volume row, a machine
          of the app (of the row's process group when it names one)
          generates the key and signing request under
          LAYERX_FLY_TLS_DIR/<service> on its volume through flyctl ssh
          console; only the request comes back, the CA signs it here and the
          certificate goes back the same way with the CA certificate, and
          the machine derives key.der, cert.der, ca.der and identity.p12
          locked by a password file it generates. For an app with several
          machines, whose row names a secret prefix PREFIX, the key and
          request are generated in a directory on the /dev/shm tmpfs of this
          host, the same files are derived there and piped base64-encoded,
          as [[files]] secrets are read, into flyctl secrets import --stage
          on standard input as PREFIX_CERT, PREFIX_KEY, PREFIX_CA,
          PREFIX_CERT_DER, PREFIX_KEY_DER, PREFIX_CA_DER, PREFIX_P12 and
          PREFIX_PASSWORD, and the directory is removed. No key is printed.
          Prints one line:
          "issued <service> app=<app> custody=volume|secrets
          fingerprint=<sha256> expires_in=<days>d".

services  prints the service list, one per line: service, Fly app toml,
          process group ("-" for the whole app), custody ("volume" or the
          secret prefix), common name, extended key usage, SAN list ("-" for
          a client identity).

Environment:
  CHECK_LIVE_TIMEOUT   seconds per flyctl call, default 30
  LAYERX_CA_DIR        the CA directory, default /etc/layerx/ca
  LAYERX_FLY_TLS_DIR   the certificate directory root on the volume of a Fly
                       app, default /data/tls

Exits 1 when the CA is missing, already present on init, a toml names no
app, or a Fly step fails; 2 on a usage error or an unknown service.
EOF
}

subject_org="Paxeer X Network"
ca_days=3650
cert_days=397

# ca_services: every certificate the bring-up issues, after the issue_cert
# calls of platform/hosted/tests/beta-cluster.sh: service, Fly app toml,
# process group, custody, common name, extended key usage, SAN list. Every
# server certificate carries its app's .internal name, or its process
# group's <group>.process.<app>.internal name; a service reached on loopback
# carries localhost and 127.0.0.1; a public name only on the two TCP
# passthrough surfaces.
ca_services() {
	cat <<'EOF'
pending-core human/wallet/deploy/human.toml - volume layerx-pending-core serverAuth DNS:layerx-pending-core,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
pending-core-admin human/wallet/deploy/human.toml - volume layerx-pending-core-admin serverAuth DNS:layerx-pending-core-admin,DNS:<app>.internal
receipt-authority human/wallet/deploy/human.toml - volume layerx-receipt-authority serverAuth DNS:layerx-receipt-authority,DNS:authority,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
agent-boundary human/wallet/deploy/human.toml - volume layerx-agent-boundary serverAuth DNS:layerx-agent-boundary,DNS:component,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
agentd human/wallet/deploy/human.toml - volume layerx-agentd serverAuth DNS:layerx-agentd,DNS:machine.paxeer.network,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
agentd-client human/wallet/deploy/human.toml - volume layerx-agentd-client clientAuth -
paxeer-boundary-loopback human/wallet/deploy/human.toml - volume paxeer-boundary serverAuth DNS:paxeer-boundary,DNS:paxeer-boundary-loopback,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
paxeer-boundary-public human/wallet/deploy/human.toml - volume paxeer-observer-boundary serverAuth DNS:paxeer-observer-boundary,DNS:paxeer-boundary-public,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
guarantor human/wallet/deploy/human.toml - volume layerx-guarantor serverAuth,clientAuth DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
human human/wallet/deploy/human.toml - volume layerx-human serverAuth DNS:layerx-human,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
human-event-client human/wallet/deploy/human.toml - volume layerx-human-events clientAuth -
relay-archive human/wallet/deploy/human.toml - volume layerx-relay-archive serverAuth DNS:layerx-relay-archive,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
gateway-redis human/wallet/deploy/redis.toml - REDIS_TLS layerx-gateway-redis serverAuth DNS:layerx-gateway-redis,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
gateway-client human/wallet/deploy/endpoint.toml - ENDPOINT_CLIENT layerx-gateway clientAuth -
identity platform/hosted/identity/fly.toml - volume layerx-identity serverAuth DNS:layerx-identity,DNS:identity,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
internal-kms platform/hosted/internal/fly.toml kms volume kms serverAuth DNS:kms,DNS:kms.process.<app>.internal,DNS:localhost,IP:127.0.0.1
internal-journeys platform/hosted/internal/fly.toml journeys volume journeys serverAuth DNS:journeys,DNS:journeys.process.<app>.internal,DNS:localhost,IP:127.0.0.1
internal-payments platform/hosted/internal/fly.toml payments volume payments serverAuth DNS:payments,DNS:payments.process.<app>.internal,DNS:localhost,IP:127.0.0.1
internal-approvals platform/hosted/internal/fly.toml approvals volume approvals serverAuth DNS:approvals,DNS:approvals.process.<app>.internal,DNS:localhost,IP:127.0.0.1
internal-programs platform/hosted/internal/fly.toml programs volume programs serverAuth DNS:programs,DNS:programs.process.<app>.internal,DNS:localhost,IP:127.0.0.1
internal-redis platform/hosted/internal/redis.toml - REDIS_TLS redis serverAuth DNS:redis,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
registry platform/hosted/registry/fly.toml - volume layerx-program-registry serverAuth DNS:layerx-program-registry,DNS:index.paxeer.network,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
registry-event-client platform/hosted/registry/fly.toml - volume layerx-registry-events clientAuth -
indexer platform/hosted/indexer/fly.toml - volume layerx-indexer serverAuth DNS:layerx-indexer,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
interop-client platform/hosted/interop/fly.toml - INTEROP_CLIENT layerx-interop-gateway clientAuth -
developer platform/hosted/webhooks/fly.toml ingress WEBHOOKS_INGRESS_TLS layerx-developer serverAuth DNS:layerx-webhooks,DNS:ingress.process.<app>.internal,DNS:localhost,IP:127.0.0.1
developer-client platform/hosted/webhooks/fly.toml - WEBHOOKS_CLIENT layerx-developer clientAuth -
ramp-client platform/ramps/fly.toml - RAMP_CLIENT layerx-reference-ramp clientAuth DNS:<app>.internal
EOF
}

# The files every issued identity consists of, as <file>:<secret suffix>.
# derive_cmd turns key.pem, cert.pem and ca.pem into the rest; it carries no
# single quote so it runs the same through fly_ssh and here.
identity_files="cert.pem:CERT key.pem:KEY ca.pem:CA cert.der:CERT_DER key.der:KEY_DER ca.der:CA_DER identity.p12:P12 password:PASSWORD"
derive_cmd() {
	printf '%s' "openssl x509 -in cert.pem -outform DER -out cert.der && openssl pkcs8 -topk8 -nocrypt -in key.pem -outform DER -out key.der && openssl x509 -in ca.pem -outform DER -out ca.der && openssl rand -hex 32 >password && openssl pkcs12 -export -inkey key.pem -in cert.pem -certfile ca.pem -name $1 -passout file:password -out identity.p12"
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

# sign <service> <eku> <sans>: signs $work/csr.pem into $work/cert.pem with
# the SAN list, after checking the request's own signature.
sign() {
	openssl req -in "$work/csr.pem" -noout -verify >/dev/null 2>&1 || {
		echo "ca: $1: the signing request does not verify" >&2
		exit 1
	}
	{
		printf 'basicConstraints=CA:FALSE\nkeyUsage=digitalSignature,keyEncipherment\nextendedKeyUsage=%s\n' "$2"
		[ -z "$3" ] || printf 'subjectAltName=%s\n' "$3"
	} >"$work/ext.cnf"
	openssl x509 -req -in "$work/csr.pem" -CA "$ca_dir/ca.pem" -CAkey "$ca_dir/ca.key" -CAcreateserial \
		-days "$cert_days" -sha256 -extfile "$work/ext.cnf" -out "$work/cert.pem" 2>/dev/null
}

# issue_volume <service> <app> <group> <cn> <eku> <sans>: the key and request
# are made on the machine's volume and the key stays there.
issue_volume() {
	local service="$1" app="$2" group="$3" cn="$4" dir="$fly_tls_dir/$1"
	fly_ssh "$app" "$group" "umask 077 && mkdir -p $dir && chmod 0700 $dir && cd $dir && openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out key.pem.new 2>/dev/null && openssl req -new -key key.pem.new -subj \"/O=$subject_org/CN=$cn\"" </dev/null >"$work/csr.pem" || {
		echo "ca: $service on $app: the machine did not produce a signing request" >&2
		exit 1
	}
	sign "$service" "$5" "$6"
	cat "$work/cert.pem" "$ca_dir/ca.pem" | fly_ssh "$app" "$group" "umask 077 && cd $dir && cat >bundle.new && sed -n \"1,/END CERTIFICATE/p\" bundle.new >cert.pem.new && sed \"1,/END CERTIFICATE/d\" bundle.new >ca.pem.new && rm bundle.new && openssl verify -CAfile ca.pem.new cert.pem.new >/dev/null && mv ca.pem.new ca.pem && mv key.pem.new key.pem && mv cert.pem.new cert.pem && $(derive_cmd "$cn")" >/dev/null || {
		echo "ca: $service on $app: the machine did not accept the certificate" >&2
		exit 1
	}
}

# issue_secrets <service> <app> <prefix> <cn> <eku> <sans>: the key and
# request are made in the tmpfs work directory and the identity files are
# piped into flyctl secrets import on standard input.
issue_secrets() {
	local service="$1" app="$2" prefix="$3" cn="$4" file
	(
		umask 077
		cd "$work"
		openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out key.pem 2>/dev/null
		openssl req -new -key key.pem -subj "/O=$subject_org/CN=$cn" -out csr.pem
	)
	sign "$service" "$5" "$6"
	(
		umask 077
		cd "$work"
		cp "$ca_dir/ca.pem" ca.pem
		sh -c "$(derive_cmd "$cn")"
	)
	for file in $identity_files; do
		printf '%s_%s=%s\n' "$prefix" "${file#*:}" "$(base64 -w 0 "$work/${file%%:*}")"
	done | timeout "$timeout" flyctl secrets import --app "$app" --stage >/dev/null 2>&1 || {
		echo "ca: $service on $app: flyctl secrets import failed" >&2
		exit 1
	}
}

ca_issue() {
	local service="$1" line toml group custody cn eku sans app
	line="$(ca_services | awk -v s="$service" '$1 == s')"
	if [ -z "$line" ]; then
		echo "ca: unknown service $service; see tools/bringup/ca.sh services" >&2
		exit 2
	fi
	read -r _ toml group custody cn eku sans <<<"$line"
	if [ ! -r "$ca_dir/ca.key" ] || [ ! -r "$ca_dir/ca.pem" ]; then
		echo "ca: no CA under $ca_dir; run tools/bringup/ca.sh init on the edge host" >&2
		exit 1
	fi
	if ! app="$(fly_app "$toml")"; then
		echo "ca: $service: $toml names no app" >&2
		exit 1
	fi
	if [ "$(stat -f -c %T /dev/shm 2>/dev/null)" != tmpfs ]; then
		echo "ca: /dev/shm is not a tmpfs; signing material is only ever written to memory here" >&2
		exit 1
	fi
	work="$(mktemp -d -p /dev/shm)"
	trap 'rm -rf "$work"' EXIT
	sans="$(app_sans "$sans" "$app")"
	if [ "$custody" = volume ]; then
		issue_volume "$service" "$app" "$group" "$cn" "$eku" "$sans"
	else
		issue_secrets "$service" "$app" "$custody" "$cn" "$eku" "$sans"
		custody=secrets
	fi
	echo "issued $service app=$app custody=$custody fingerprint=$(openssl x509 -in "$work/cert.pem" -noout -fingerprint -sha256 | cut -d= -f2) expires_in=$(days_left <"$work/cert.pem")d"
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
	[ "$#" -eq 2 ] || {
		usage >&2
		exit 2
	}
	;;
*)
	usage >&2
	exit 2
	;;
esac

tools=(openssl awk)
[ "$mode" != issue ] || tools+=(timeout flyctl base64)
for tool in "${tools[@]}"; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "ca: $tool is required" >&2
		exit 2
	fi
done

case "$mode" in
init) ca_init ;;
services) ca_services ;;
issue) ca_issue "$2" ;;
esac
