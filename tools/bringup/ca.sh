#!/usr/bin/env bash
set -euo pipefail

# The Fly helpers, the CA settings and the certificate arithmetic are the
# probe's.
# shellcheck source=tools/bringup/check-live.sh
. "$(dirname "${BASH_SOURCE[0]}")/check-live.sh"

usage() {
	cat <<'EOF'
usage: tools/bringup/ca.sh init | inventory [<service>] | issue <service> | services
       tools/bringup/ca.sh issue-local <local service> --output-dir <dir> | local-services
       tools/bringup/ca.sh request-server paxeer-comet-boundary-api4 --output-dir <dir>
       tools/bringup/ca.sh sign-server paxeer-comet-boundary-api4 --csr <file> --output-dir <dir>
       tools/bringup/ca.sh install-server paxeer-comet-boundary-api4 --input-dir <signed-dir> --output-dir <request-dir> --ca-file <operator-ca.pem>

The internal CA of the Paxeer X Network bring-up. Runs on the edge host, the
operator host that holds the CA key and the Fly login.

init      generates the CA key and certificate under LAYERX_CA_DIR with mode
          0600 and prints nothing but the certificate's SHA-256 fingerprint.
          Refuses to touch a directory that already holds a CA.

inventory [<service>]
          inventories the identity material already retained for every row
          (or the one service) before any deployment action, and changes
          nothing: for a volume row the presence of cert.pem, key.der,
          cert.der, ca.der, identity.p12 and password under
          LAYERX_FLY_TLS_DIR/<service> on a machine of the app, and whether
          the retained certificate chains to the row's CA; for a secrets row
          the presence of the eight PREFIX_* names in flyctl secrets list
          and verification of the deployed public PREFIX_CERT against the
          same CA through its declared [[files]] guest path.
          Only names, fingerprints and day counts are read; no key, password
          or secret value is read or printed. One line per row:
          "inventory <service> app=<app> custody=volume|secrets
          state=present|absent|partial|incompatible|foreign|unreadable
          [fingerprint=<sha256> expires_in=<days>d]
          producer=tools/bringup/ca.sh issue <service>".
          A volume row whose CA directory is not readable here (an
          attestor row without LAYERX_ATTESTOR_CA_DIR) is unreadable.
          Exits 1 when any row is unreadable or foreign.

issue <service>
          inventories the service first. Retained material that is present,
          chains to the row's CA and expires in more than
          LAYERX_CA_RENEW_DAYS days is reused and never regenerated: prints
          "reused <service> app=<app> custody=volume|secrets
          fingerprint=<sha256> expires_in=<days>d". A retained
          same-CA certificate missing its required role or service name is renewed
          under that CA. A certificate that does not chain to the row's CA, or material that
          cannot be inventoried, is refused, so no second, incompatible
          authority is ever issued beside it. Otherwise it issues the certificate of one service for the Fly app whose toml
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
          A missing CA or app is also reported on stdout as
          "fail material missing=<prerequisite> producer=<producer>".

services  prints the service list, one per line: service, Fly app toml,
          process group ("-" for the whole app), custody ("volume" or the
          secret prefix), common name, extended key usage, SAN list ("-" for
          a client identity).

issue-local <local service> --output-dir <dir>
          issues one fixed local identity of local-services on this host,
          never through Fly: the identity, role, extended key usage and SAN
          list are the row's and nothing else is accepted. <dir> is an
          absolute path that must not exist, whose components are no
          symbolic links, whose parent is a directory owned by the caller
          or root and writable by neither group nor others, and which
          neither lies in LAYERX_CA_DIR nor contains it. The key, the
          request and the derived files are made in a mode 0700 staging
          directory beside <dir>, verified against the CA, and the staging
          directory is renamed to <dir> without replacing anything, with
          mode 0700 and every file 0600. No key is printed. Prints one line:
          "issued <service> custody=local fingerprint=<sha256>
          expires_in=<days>d".

local-services
          prints the local identity list in the columns of services, with
          "-" for the toml and the process group and "local" for custody.

request-server creates a new protected directory holding only api4-key.der
          (PKCS8) and api4-request.pem. Run on the server; transfer only CSR.
sign-server uses the established CA under its issuer lock and writes only
          api4-cert.pem, api4-cert.der and api4-trust.pem to a new directory.
install-server requires the retained request directory and explicit expected
          operator CA, verifies the complete fixed server identity and key
          pairing, then atomically installs the public files. It preserves
          the local private key and refuses existing or partial certificates.
          All material directories require 0700 and files 0600, owned by the
          caller; no symlink components or arbitrary server roles are accepted.

Environment:
  CHECK_LIVE_TIMEOUT   seconds per flyctl call, default 30
  LAYERX_CA_DIR        the CA directory, default /etc/layerx/ca
  LAYERX_ATTESTOR_CA_DIR
                       the directory holding ca.key and ca.pem of the
                       attestors' gateway CA, the client authority that
                       ATTESTOR_TLS_CA of paxeer-attestor-1 to 5 bundles with
                       the node CA; issue signs the rows of attestor_services
                       under it and never under the internal CA
  LAYERX_FLY_TLS_DIR   the certificate directory root on the volume of a Fly
                       app, default /data/tls
  LAYERX_CA_RENEW_DAYS days of remaining validity below which issue renews
                       a retained certificate under the same CA, default 30

Exits 1 when the CA is missing, already present on init, a toml names no
app, retained material is foreign or cannot be inventoried, a Fly step
fails, or a row of the service tables is malformed or listed twice; 2 on a
usage error or an unknown service.
EOF
}

subject_org="Paxeer X Network"
ca_days=3650
cert_days=397
renew_days="${LAYERX_CA_RENEW_DAYS:-30}"

# ca_services: every certificate the bring-up issues, after the issue_cert
# calls of platform/hosted/tests/beta-cluster.sh: service, Fly app toml,
# process group, custody, common name, extended key usage, SAN list. Every
# server certificate carries its app's .internal name, or its process
# group's <group>.process.<app>.internal name; a service reached on loopback
# carries localhost and 127.0.0.1; a public name only on the two TCP
# passthrough surfaces.
ca_services() {
	cat <<'EOF'
pending-core platform/hosted/node/fly.toml - volume layerx-pending-core serverAuth DNS:layerx-pending-core,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
pending-core-admin platform/hosted/node/fly.toml - volume layerx-pending-core-admin serverAuth DNS:layerx-pending-core-admin,DNS:<app>.internal
receipt-authority platform/hosted/node/fly.toml - volume layerx-receipt-authority serverAuth DNS:layerx-receipt-authority,DNS:authority,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
agent-boundary platform/hosted/node/fly.toml - volume layerx-agent-boundary serverAuth DNS:layerx-agent-boundary,DNS:component,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
agentd human/wallet/deploy/human.toml - volume layerx-agentd serverAuth DNS:layerx-agentd,DNS:machine.paxeer.network,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
agentd-client human/wallet/deploy/human.toml - volume layerx-agentd-client clientAuth -
paxeer-boundary-loopback human/wallet/deploy/human.toml - volume paxeer-boundary serverAuth DNS:paxeer-boundary,DNS:paxeer-boundary-loopback,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
paxeer-boundary-public human/wallet/deploy/human.toml - volume paxeer-observer-boundary serverAuth DNS:paxeer-observer-boundary,DNS:paxeer-boundary-public,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
guarantor human/wallet/deploy/human.toml - volume layerx-guarantor serverAuth,clientAuth DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
human platform/hosted/node/fly.toml - volume layerx-human serverAuth DNS:layerx-human,DNS:<app>.internal,DNS:paxeer-human-service.internal,DNS:localhost,IP:127.0.0.1
human-event-client human/wallet/deploy/human.toml - volume layerx-human-events clientAuth URI:urn:layerx:webhooks:role:producer
human-attestor-client human/wallet/deploy/human.toml - volume layerx-human-components clientAuth -
human-kms human/wallet/deploy/human.toml - volume layerx-human-kms serverAuth DNS:layerx-human-kms,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
human-kms-client human/wallet/deploy/human.toml - volume layerx-human-components clientAuth -
human-kms-executor human/wallet/deploy/human.toml - volume layerx-human-movement clientAuth -
relay-archive human/wallet/deploy/human.toml - volume layerx-relay-archive serverAuth DNS:layerx-relay-archive,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
gateway-redis human/wallet/deploy/redis.toml - REDIS_TLS layerx-gateway-redis serverAuth DNS:layerx-gateway-redis,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
gateway-client human/wallet/deploy/endpoint.toml - ENDPOINT_CLIENT layerx-gateway clientAuth URI:urn:layerx:webhooks:role:producer
identity platform/hosted/identity/fly.toml - volume layerx-identity serverAuth DNS:layerx-identity,DNS:identity,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
internal-kms platform/hosted/internal/fly.toml kms volume kms serverAuth DNS:kms,DNS:kms.process.<app>.internal,DNS:localhost,IP:127.0.0.1
internal-journeys platform/hosted/internal/fly.toml journeys volume journeys serverAuth DNS:journeys,DNS:journeys.process.<app>.internal,DNS:localhost,IP:127.0.0.1
internal-payments platform/hosted/internal/fly.toml payments volume payments serverAuth DNS:payments,DNS:payments.process.<app>.internal,DNS:localhost,IP:127.0.0.1
internal-approvals platform/hosted/internal/fly.toml approvals volume approvals serverAuth DNS:approvals,DNS:approvals.process.<app>.internal,DNS:localhost,IP:127.0.0.1
internal-programs platform/hosted/internal/fly.toml programs volume programs serverAuth DNS:programs,DNS:programs.process.<app>.internal,DNS:localhost,IP:127.0.0.1
internal-redis platform/hosted/internal/redis.toml - REDIS_TLS redis serverAuth DNS:redis,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
registry platform/hosted/registry/fly.toml - volume layerx-program-registry serverAuth DNS:layerx-program-registry,DNS:index.paxeer.network,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
registry-event-client platform/hosted/registry/fly.toml - volume layerx-registry-events clientAuth URI:urn:layerx:webhooks:role:producer
indexer platform/hosted/indexer/fly.toml - volume layerx-indexer serverAuth DNS:layerx-indexer,DNS:<app>.internal,DNS:localhost,IP:127.0.0.1
interop-client platform/hosted/interop/fly.toml - INTEROP_CLIENT layerx-interop-gateway clientAuth -
developer platform/hosted/webhooks/fly.toml ingress WEBHOOKS_INGRESS_TLS layerx-developer serverAuth DNS:layerx-webhooks,DNS:ingress.process.<app>.internal,DNS:public.process.<app>.internal,DNS:localhost,IP:127.0.0.1
developer-client platform/hosted/webhooks/fly.toml - WEBHOOKS_CLIENT layerx-developer clientAuth -
dashboard-client platform/hosted/dashboard/fly.toml - DASHBOARD_CLIENT layerx-dashboard clientAuth -
ramp-client platform/ramps/fly.toml - RAMP_CLIENT layerx-reference-ramp clientAuth DNS:<app>.internal
EOF
}

# local_services: identities issue-local makes on the operator host for an
# operator holding them by hand; they are never part of a Fly app, so they
# stay out of ca_services and every Fly mount.
local_services() {
	cat <<'EOF'
webhook-operator-client - - local layerx-webhooks-operator clientAuth URI:urn:layerx:webhooks:role:operator
EOF
}

# attestor_services: the rows the attestors' gateway CA signs, because the
# attestors admit keys.generate and sign only from a client chaining to it.
attestor_services="human-attestor-client"

# table_check <table>: refuses the rows of the table on stdin unless each is
# exactly one identity: seven fields, a service named once, a Fly toml,
# process group and custody (or none and local custody for local_services),
# one secret prefix per toml, a known extended key usage, a SAN list of
# DNS, IP and URN entries named once, and a SAN list on every server identity.
table_check() {
	awk -v table="$1" '
	function bad(why) {
		printf "ca: %s row %d is malformed: %s\n", table, NR, why >"/dev/stderr"
		failed = 1
	}
	{
		if (NF != 7) {
			bad("want 7 fields, got " NF)
			next
		}
		if ($1 !~ /^[a-z0-9][a-z0-9-]*$/) bad("service " $1)
		else if ($1 in seen) bad("service " $1 " listed twice")
		seen[$1] = 1
		if (table == "local_services") {
			if ($2 != "-" || $3 != "-" || $4 != "local") bad("a local identity names no toml or process group and has local custody")
		} else {
			if ($2 !~ /^[a-z0-9][a-z0-9_\/.-]*\.toml$/) bad("toml " $2)
			if ($3 != "-" && $3 !~ /^[a-z][a-z0-9-]*$/) bad("process group " $3)
			if ($4 != "volume" && $4 !~ /^[A-Z][A-Z0-9_]*$/) bad("custody " $4)
			else if ($4 != "volume" && (($2 " " $4) in prefixes)) bad("secret prefix " $4 " of " $2 " listed twice")
			prefixes[$2 " " $4] = 1
		}
		if ($5 !~ /^[a-z][a-z0-9-]*$/) bad("common name " $5)
		if ($6 != "serverAuth" && $6 != "clientAuth" && $6 != "serverAuth,clientAuth") bad("extended key usage " $6)
		if ($7 == "-") {
			if ($6 ~ /serverAuth/) bad("a server identity without a SAN list")
		} else {
			n = split($7, sans, ",")
			delete names
			for (i = 1; i <= n; i++) {
				if (sans[i] !~ /^(DNS:[a-z0-9<>.-]+|IP:[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+|URI:urn:[a-z0-9:._-]+)$/) bad("SAN " sans[i])
				else if (sans[i] in names) bad("SAN " sans[i] " listed twice")
				names[sans[i]] = 1
			}
		}
	}
	END {
		if (NR == 0) bad("no rows")
		exit failed
	}'
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

# service_row <service>: the row of the service; exits 2 when there is none.
service_row() {
	local line
	line="$(ca_services | awk -v s="$1" '$1 == s')"
	if [ -z "$line" ]; then
		echo "ca: unknown service $1; see tools/bringup/ca.sh services" >&2
		exit 2
	fi
	printf '%s' "$line"
}

# row_ca_dir <service>: prints the directory of the CA that signs the
# service; status 1 when it is the attestors' gateway CA and
# LAYERX_ATTESTOR_CA_DIR is unset or holds the internal CA's key: the
# attestor client shares the components' common name, so one authority for
# both would turn xweb attestor authority into Human KMS authority.
row_ca_dir() {
	if [[ " $attestor_services " == *" $1 "* ]]; then
		[ -n "${LAYERX_ATTESTOR_CA_DIR:-}" ] || return 1
		[ "$(realpath -m -- "$LAYERX_ATTESTOR_CA_DIR")" != "$(realpath -m -- "$ca_dir")" ] || return 1
		[ ! -r "$ca_dir/ca.pem" ] ||
			[ "$(openssl x509 -in "$LAYERX_ATTESTOR_CA_DIR/ca.pem" -noout -pubkey 2>/dev/null)" != \
				"$(openssl x509 -in "$ca_dir/ca.pem" -noout -pubkey 2>/dev/null)" ] || return 1
		printf '%s' "$LAYERX_ATTESTOR_CA_DIR"
	else
		printf '%s' "$ca_dir"
	fi
}

certificate_usage_matches() {
	local service="$1" cert="$2" row_ca="$3" app="$4" roles cn eku sans want_cn want_eku want_sans
	case "$service" in
	human-kms | human-kms-client | human-kms-executor)
		# Each Human KMS identity is exactly its own row: the server, the
		# components' service client and movement's restricted executor never
		# stand in for one another.
		read -r _ _ _ _ want_cn want_eku want_sans <<<"$(service_row "$service")"
		cn="$(openssl x509 -noout -subject -nameopt multiline <<<"$cert" 2>/dev/null | sed -n 's/^ *commonName *= //p')"
		eku="$(openssl x509 -noout -ext extendedKeyUsage <<<"$cert" 2>/dev/null | tail -n +2 | sed 's/^ *//')"
		sans="$(openssl x509 -noout -ext subjectAltName <<<"$cert" 2>/dev/null | tail -n +2 | sed 's/^ *//')"
		[ "$cn" = "$want_cn" ] || return 1
		case "$want_eku" in
		serverAuth)
			[ "$eku" = "TLS Web Server Authentication" ] &&
				[[ ", $sans, " == *", DNS:layerx-human-kms, "* ]] &&
				[ "$sans" = "$(printf '%s' "${want_sans//<app>/$app}" | sed 's/,/, /g; s/IP:/IP Address:/g')" ] &&
				openssl verify -purpose sslserver -verify_hostname layerx-human-kms -verify_ip 127.0.0.1 \
					-CAfile "$row_ca/ca.pem" <<<"$cert" >/dev/null 2>&1
			;;
		clientAuth)
			[ "$eku" = "TLS Web Client Authentication" ] && [ -z "$sans" ] &&
				openssl verify -purpose sslclient -CAfile "$row_ca/ca.pem" <<<"$cert" >/dev/null 2>&1
			;;
		*) return 1 ;;
		esac
		;;
	human-event-client | gateway-client | registry-event-client)
		roles="$(openssl x509 -noout -ext subjectAltName <<<"$cert" 2>/dev/null |
			tr ',' '\n' | sed 's/^[[:space:]]*//' | grep '^URI:urn:layerx:webhooks:role:' || true)"
		[ "$roles" = URI:urn:layerx:webhooks:role:producer ] &&
			openssl verify -purpose sslclient -CAfile "$row_ca/ca.pem" <<<"$cert" >/dev/null 2>&1
		;;
	developer)
		openssl verify -purpose sslserver -verify_hostname "public.process.$app.internal" \
			-CAfile "$row_ca/ca.pem" <<<"$cert" >/dev/null 2>&1
		;;
	*) return 0 ;;
	esac
}

# inventory_row <service> <app> <group> <custody> <row ca dir>: prints
# "<state> <fingerprint> <days>" for the material the service already has,
# state one of present, absent, partial, incompatible, foreign or unreadable. It reads
# file names and the public certificate only.
inventory_row() {
	local service="$1" app="$2" group="$3" custody="$4" row_ca="$5" dir="$fly_tls_dir/$1" answer missing cert listed file name toml path fingerprint days have=0 want=0
	if [ "$custody" != volume ]; then
		listed="$(timeout "$timeout" flyctl secrets list --app "$app" --json </dev/null 2>/dev/null | python3 -c 'import json, sys; print(" ".join(s.get("Name") or s.get("name") or "" for s in json.load(sys.stdin) or []))' 2>/dev/null)" || {
			echo "unreadable - -"
			return
		}
		for file in $identity_files; do
			name="${custody}_${file#*:}"
			want=$((want + 1))
			[[ " $listed " != *" $name "* ]] || have=$((have + 1))
		done
		if [ "$have" -eq "$want" ]; then
			read -r _ toml _ <<<"$(service_row "$service")"
			if ! path="$(fly_guest_path "$toml" "${custody}_CERT")" ||
				! cert="$(fly_ssh "$app" "$group" "cat $path" </dev/null)" ||
				[ -z "$row_ca" ] || [ ! -r "$row_ca/ca.pem" ]; then
				echo "unreadable - -"
				return
			fi
			if ! fingerprint="$(openssl x509 -noout -fingerprint -sha256 <<<"$cert" 2>/dev/null | cut -d= -f2)" ||
				! days="$(days_left <<<"$cert" 2>/dev/null)"; then
				echo "unreadable - -"
				return
			fi
			if openssl verify -CAfile "$row_ca/ca.pem" <<<"$cert" >/dev/null 2>&1; then
				if certificate_usage_matches "$service" "$cert" "$row_ca" "$app"; then
					echo "present $fingerprint $days"
				else
					echo "incompatible $fingerprint $days"
				fi
			else
				echo "foreign $fingerprint $days"
			fi
		elif [ "$have" -eq 0 ]; then
			echo "absent - -"
		else
			echo "partial - -"
		fi
		return
	fi
	answer="$(fly_ssh "$app" "$group" "if [ -d $dir ]; then cd $dir && for f in key.der cert.der ca.der identity.p12 password; do [ -s \$f ] || echo missing=\$f; done && if [ -s cert.pem ]; then cat cert.pem; else echo missing=cert.pem; fi; else echo directory=absent; fi; echo inventory=done" </dev/null)" || answer=""
	if [[ "$answer" != *inventory=done* ]]; then
		echo "unreadable - -"
		return
	fi
	if [[ "$answer" == *directory=absent* ]]; then
		echo "absent - -"
		return
	fi
	missing="$(grep -c '^missing=' <<<"$answer" || true)"
	cert="$(sed -n '/-----BEGIN CERTIFICATE-----/,/-----END CERTIFICATE-----/p' <<<"$answer")"
	if [ -z "$cert" ]; then
		[ "$missing" -ge 6 ] && echo "absent - -" || echo "partial - -"
		return
	fi
	if [ -z "$row_ca" ] || [ ! -r "$row_ca/ca.pem" ]; then
		echo "unreadable - -"
		return
	fi
	if ! openssl verify -CAfile "$row_ca/ca.pem" <(printf '%s\n' "$cert") >/dev/null 2>&1; then
		echo "foreign $(openssl x509 -noout -fingerprint -sha256 <<<"$cert" | cut -d= -f2) $(days_left <<<"$cert")"
		return
	fi
	if ! certificate_usage_matches "$service" "$cert" "$row_ca" "$app"; then
		echo "incompatible $(openssl x509 -noout -fingerprint -sha256 <<<"$cert" | cut -d= -f2) $(days_left <<<"$cert")"
	elif [ "$missing" -gt 0 ]; then
		echo "partial $(openssl x509 -noout -fingerprint -sha256 <<<"$cert" | cut -d= -f2) $(days_left <<<"$cert")"
	else
		echo "present $(openssl x509 -noout -fingerprint -sha256 <<<"$cert" | cut -d= -f2) $(days_left <<<"$cert")"
	fi
}

# ca_inventory [<service>]: one inventory line per row; exits 1 when any row
# is unreadable or foreign, so a deployment action never starts on material
# nobody has accounted for.
ca_inventory() {
	local service line toml group custody app row_ca state fingerprint days failures=0 rows
	if [ -n "${1:-}" ]; then
		rows="$(service_row "$1")"
	else
		rows="$(ca_services)"
	fi
	while read -r service toml group custody _; do
		if ! app="$(fly_app "$toml")"; then
			echo "fail material missing=$toml producer=the app line of $toml"
			failures=$((failures + 1))
			continue
		fi
		row_ca="$(row_ca_dir "$service")" || row_ca=""
		read -r state fingerprint days <<<"$(inventory_row "$service" "$app" "$group" "$custody" "$row_ca")"
		[ "$custody" = volume ] || custody=secrets
		if [ "$fingerprint" != - ]; then
			days="${days}d"
			echo "inventory $service app=$app custody=$custody state=$state fingerprint=$fingerprint expires_in=$days producer=tools/bringup/ca.sh issue $service"
		else
			echo "inventory $service app=$app custody=$custody state=$state producer=tools/bringup/ca.sh issue $service"
		fi
		case "$state" in unreadable | foreign) failures=$((failures + 1)) ;; esac
	done <<<"$rows"
	[ "$failures" -eq 0 ] || exit 1
}

ca_issue() {
	local service="$1" line toml group custody cn eku sans app state fingerprint days
	line="$(service_row "$service")"
	read -r _ toml group custody cn eku sans <<<"$line"
	if [[ " $attestor_services " == *" $service "* ]]; then
		ca_dir="$(row_ca_dir "$service")" || {
			echo "fail material missing=LAYERX_ATTESTOR_CA_DIR producer=the attestors' gateway CA"
			echo "ca: $service is issued under the attestors' gateway CA, an authority apart from the internal CA; set LAYERX_ATTESTOR_CA_DIR to it" >&2
			exit 1
		}
	fi
	if [ ! -r "$ca_dir/ca.key" ] || [ ! -r "$ca_dir/ca.pem" ]; then
		echo "fail material missing=$ca_dir/ca.pem producer=tools/bringup/ca.sh init"
		echo "ca: no CA under $ca_dir; run tools/bringup/ca.sh init on the edge host" >&2
		exit 1
	fi
	if ! app="$(fly_app "$toml")"; then
		echo "fail material missing=$toml producer=the app line of $toml"
		echo "ca: $service: $toml names no app" >&2
		exit 1
	fi
	umask 077
	exec {ca_issue_lock_fd}>"$ca_dir/issue.lock"
	flock -x -w "$timeout" "$ca_issue_lock_fd" || {
		echo "ca: the established authority issuer is busy" >&2
		exit 1
	}
	read -r state fingerprint days <<<"$(inventory_row "$service" "$app" "$group" "$custody" "$ca_dir")"
	case "$state" in
	unreadable)
		echo "ca: $service on $app: the retained material cannot be inventoried; nothing is issued before it is" >&2
		exit 1
		;;
	foreign)
		echo "ca: $service on $app: the retained certificate $fingerprint does not chain to $ca_dir/ca.pem; issuing beside it would make a second, incompatible authority, so remove it deliberately first" >&2
		exit 1
		;;
	partial)
		if [ "$custody" != volume ]; then
			echo "ca: $service on $app: retained secret material is partial; restore the original identity before issuing" >&2
			exit 1
		fi
		;;
	present)
		if [ "$days" -gt "$renew_days" ]; then
			[ "$custody" = volume ] || custody=secrets
			days="${days}d"
			echo "reused $service app=$app custody=$custody fingerprint=$fingerprint expires_in=$days"
			return
		fi
		;;
	esac
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

# local_refuse <message>: refuses an issue-local destination or bundle.
local_refuse() {
	echo "ca: issue-local: $1" >&2
	exit 1
}

# local_destination <dir>: checks the issue-local destination before anything
# is generated and prints its parent.
local_destination() {
	local dir="$1" component prefix="" parent mode owner ca_real components
	case "$dir" in
	/*) ;;
	*) local_refuse "the output directory must be an absolute path" ;;
	esac
	IFS=/ read -r -a components <<<"${dir#/}"
	[ "${#components[@]}" -gt 0 ] || local_refuse "the output directory must not be /"
	for component in "${components[@]}"; do
		case "$component" in
		"" | . | ..) local_refuse "the output directory must be a normalized path" ;;
		esac
		prefix="$prefix/$component"
		if [ -L "$prefix" ]; then
			local_refuse "$prefix is a symbolic link"
		fi
	done
	[ "$prefix" = "$dir" ] || local_refuse "the output directory must be a normalized path"
	if [ -e "$dir" ]; then
		local_refuse "$dir already exists"
	fi
	parent="${dir%/*}"
	parent="${parent:-/}"
	[ -d "$parent" ] || local_refuse "the parent $parent is not a directory"
	owner="$(stat -c %u "$parent")"
	mode="$(stat -c %a "$parent")"
	if [ "$owner" != 0 ] && [ "$owner" != "$(id -u)" ]; then
		local_refuse "the parent $parent is owned by uid $owner"
	fi
	if (((8#$mode & 8#022) != 0)); then
		local_refuse "the parent $parent has mode $mode, writable by group or others"
	fi
	ca_real="$(realpath -m -- "$ca_dir")"
	case "$dir/" in
	"$ca_real"/*) local_refuse "$dir lies in the CA directory" ;;
	esac
	case "$ca_real/" in
	"$dir"/*) local_refuse "$dir contains the CA directory" ;;
	esac
	printf '%s' "$parent"
}

ca_issue_local() {
	local service="$1" dir="$2" line cn eku sans parent file count
	line="$(local_services | awk -v s="$service" '$1 == s')"
	if [ -z "$line" ]; then
		echo "ca: unknown local service $service; see tools/bringup/ca.sh local-services" >&2
		exit 2
	fi
	read -r _ _ _ _ cn eku sans <<<"$line"
	if [ ! -r "$ca_dir/ca.key" ] || [ ! -r "$ca_dir/ca.pem" ]; then
		echo "ca: no CA under $ca_dir; run tools/bringup/ca.sh init on the edge host" >&2
		exit 1
	fi
	umask 077
	exec {ca_issue_lock_fd}>"$ca_dir/issue.lock"
	flock -x -w "$timeout" "$ca_issue_lock_fd" || {
		echo "ca: the established authority issuer is busy" >&2
		exit 1
	}
	parent="$(local_destination "$dir")"
	work="$(umask 077 && mktemp -d -p "$parent" ".ca-issue-local.XXXXXXXX")"
	trap 'rm -rf "$work"' EXIT
	chmod 0700 "$work"
	(
		umask 077
		cd "$work"
		openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out key.pem 2>/dev/null
		openssl req -new -key key.pem -subj "/O=$subject_org/CN=$cn" -out csr.pem
	)
	sign "$service" "$eku" "$sans"
	(
		umask 077
		cd "$work"
		cp "$ca_dir/ca.pem" ca.pem
		sh -c "$(derive_cmd "$cn")"
		rm -f csr.pem ext.cnf
	)
	openssl verify -purpose sslclient -CAfile "$work/ca.pem" "$work/cert.pem" >/dev/null 2>&1 ||
		local_refuse "the issued certificate does not verify against the CA"
	[ "$(openssl x509 -in "$work/cert.pem" -noout -pubkey)" = "$(openssl pkey -in "$work/key.pem" -pubout 2>/dev/null)" ] ||
		local_refuse "the issued certificate does not match its key"
	[ "$(openssl x509 -in "$work/cert.pem" -noout -ext subjectAltName 2>/dev/null | tail -n +2 | sed 's/^ *//')" = "$sans" ] ||
		local_refuse "the issued certificate does not carry exactly $sans"
	count=0
	for file in $identity_files; do
		{ [ -f "$work/${file%%:*}" ] && [ ! -L "$work/${file%%:*}" ]; } ||
			local_refuse "the bundle lacks ${file%%:*}"
		chmod 0600 "$work/${file%%:*}"
		count=$((count + 1))
	done
	[ "$(find "$work" -mindepth 1 | wc -l)" -eq "$count" ] ||
		local_refuse "the bundle holds files beyond the identity"
	chmod 0700 "$work"
	{ [ ! -e "$dir" ] && [ ! -L "$dir" ]; } || local_refuse "$dir appeared during issuance"
	python3 - "$work" "$dir" <<'ATOMIC_LOCAL_BUNDLE' || local_refuse "the bundle could not be published without overwrite"
import ctypes, os, sys
source, destination = sys.argv[1:]
parent = os.path.dirname(destination)
if os.path.dirname(source) != parent:
    raise SystemExit("local bundle staging is outside the destination parent")
fd = os.open(parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
try:
    staging = os.open(os.path.basename(source), os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
    try:
        for name in os.listdir(staging):
            item = os.open(name, os.O_RDONLY | os.O_NOFOLLOW, dir_fd=staging)
            try:
                os.fsync(item)
            finally:
                os.close(item)
        os.fsync(staging)
    finally:
        os.close(staging)
    libc = ctypes.CDLL(None, use_errno=True)
    rename = libc.renameat2
    rename.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
    rename.restype = ctypes.c_int
    if rename(fd, os.fsencode(os.path.basename(source)), fd, os.fsencode(os.path.basename(destination)), 1):
        raise OSError(ctypes.get_errno(), "atomic local bundle publication refused")
    os.fsync(fd)
finally:
    os.close(fd)
ATOMIC_LOCAL_BUNDLE
	trap - EXIT
	echo "issued $service custody=local fingerprint=$(openssl x509 -in "$dir/cert.pem" -noout -fingerprint -sha256 | cut -d= -f2) expires_in=$(days_left <"$dir/cert.pem")d"
}

server_role() {
	[ "$1" = paxeer-comet-boundary-api4 ] || {
		echo "ca: unsupported server role" >&2
		exit 2
	}
}

server_material() {
	python3 - "$@" <<'PY'
import ctypes
import fcntl
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import tempfile

ROLE = b'paxeer-comet-boundary-api4'
DNS = b'api4.mainnet-beta.paxeer.network'
KEY = 'api4-key.der'
CSR = 'api4-request.pem'
PUBLIC = ('api4-cert.pem', 'api4-cert.der', 'api4-trust.pem')


def require(ok, message):
    if not ok:
        raise ValueError(message)


def openssl(*args):
    result = subprocess.run(['openssl', *map(str, args)], capture_output=True)
    require(result.returncode == 0, 'OpenSSL server material validation failed')
    return result.stdout


def path_guard(value):
    path = Path(value)
    require(path.is_absolute() and str(path) == value and not any(x in ('.', '..', '') for x in value.split('/')[1:]), 'path must be absolute and normalized')
    for ancestor in (*reversed(path.parents), path):
        require(not ancestor.is_symlink(), 'symlink component refused')
    return path


def directory(value):
    path = path_guard(value)
    info = path.lstat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o700, 'directory must be owned by caller with mode 0700')
    parent = path.parent.stat()
    require(parent.st_uid in (0, os.geteuid()) and parent.st_mode & 0o022 == 0, 'directory parent is not protected')
    return path


def regular(value):
    path = path_guard(str(value))
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1, 'material must be a private regular file of the caller')
    require(info.st_size > 0, 'empty material refused')
    parent = path.parent.stat()
    require(parent.st_uid in (0, os.geteuid()) and parent.st_mode & 0o022 == 0, 'material parent is not protected')
    return path


def exact_files(path, names):
    require(set(os.listdir(path)) == set(names), 'partial or conflicting server material refused')
    for name in names:
        regular(path / name)


def tlv(data, offset=0):
    start = offset
    require(offset + 2 <= len(data), 'truncated DER')
    tag, length = data[offset:offset + 2]
    offset += 2
    if length & 128:
        count = length & 127
        require(0 < count <= 4 and offset + count <= len(data), 'invalid DER length')
        require(data[offset] != 0, 'noncanonical DER length')
        length = int.from_bytes(data[offset:offset + count], 'big')
        require(length >= 128, 'noncanonical DER length')
        offset += count
    end = offset + length
    require(end <= len(data), 'truncated DER value')
    return (tag, data[offset:end], data[start:end]), end


def children(data):
    values, offset = [], 0
    while offset < len(data):
        value, offset = tlv(data, offset)
        values.append(value)
    return values


def root(data):
    value, end = tlv(data)
    require(value[0] == 48 and end == len(data), 'invalid DER envelope')
    return children(value[1])


def subject(value):
    require(value[0] == 48, 'invalid subject')
    rdns = children(value[1])
    require(len(rdns) == 1 and rdns[0][0] == 49, 'subject must contain only the fixed CN')
    attributes = children(rdns[0][1])
    require(len(attributes) == 1 and attributes[0][0] == 48, 'subject must contain only the fixed CN')
    pair = children(attributes[0][1])
    require(len(pair) == 2 and pair[0][:2] == (6, b'\x55\x04\x03') and pair[1][0] in (12, 19) and pair[1][1] == ROLE, 'wrong server subject')


def p256(value):
    require(value[0] == 48, 'invalid public key')
    fields = children(value[1])
    require(len(fields) == 2 and fields[0][0] == 48, 'invalid public key')
    algorithm = children(fields[0][1])
    require([x[:2] for x in algorithm] == [(6, b'\x2a\x86\x48\xce\x3d\x02\x01'), (6, b'\x2a\x86\x48\xce\x3d\x03\x01\x07')], 'only named P-256 keys are accepted')
    require(fields[1][0] == 3 and len(fields[1][1]) == 66 and fields[1][1][:2] == b'\x00\x04', 'invalid P-256 public point')
    return value[2]


def request(value):
    path = regular(value)
    openssl('req', '-in', path, '-noout', '-verify')
    fields = root(openssl('req', '-in', path, '-outform', 'DER'))
    require(len(fields) == 3 and fields[0][0] == 48, 'invalid CSR')
    info = children(fields[0][1])
    require(len(info) == 4 and info[0][:2] == (2, b'\x00') and info[3][:2] == (160, b''), 'CSR attributes and requested extensions are forbidden')
    subject(info[1])
    return p256(info[2])


def one_pem(path):
    data = regular(path).read_bytes()
    require(re.fullmatch(rb'\s*-----BEGIN CERTIFICATE-----\s+[A-Za-z0-9+/=\r\n]+-----END CERTIFICATE-----\s*', data) is not None, 'expected one public certificate')
    return data


def certificate(path, trust, public):
    one_pem(path)
    one_pem(trust)
    openssl('verify', '-no-CApath', '-no-CAstore', '-purpose', 'sslserver', '-verify_hostname', DNS.decode(), '-CAfile', trust, path)
    der = openssl('x509', '-in', path, '-outform', 'DER')
    fields = root(der)
    require(len(fields) == 3 and fields[0][0] == 48, 'invalid leaf certificate')
    body = children(fields[0][1])
    offset = int(body[0][0] == 160)
    subject(body[offset + 4])
    require(p256(body[offset + 5]) == public, 'certificate and request keys differ')
    wrappers = [x for x in body if x[0] == 163]
    require(len(wrappers) == 1, 'missing or duplicate certificate extensions')
    extensions = root(wrappers[0][1])
    values = {}
    for entry in extensions:
        require(entry[0] == 48, 'invalid certificate extension')
        items = children(entry[1])
        require(len(items) in (2, 3) and items[0][0] == 6 and items[-1][0] == 4, 'invalid certificate extension')
        oid = items[0][1]
        require(oid not in values, 'duplicate certificate extension')
        values[oid] = items[-1][1]
    require(values.get(b'\x55\x1d\x11') == b'\x30' + bytes([len(DNS) + 2]) + b'\x82' + bytes([len(DNS)]) + DNS, 'certificate must contain the single fixed DNS SAN')
    require(values.get(b'\x55\x1d\x25') == b'\x30\x0a\x06\x08\x2b\x06\x01\x05\x05\x07\x03\x01', 'certificate must contain only serverAuth')
    require(values.get(b'\x55\x1d\x13') == b'\x30\x00', 'server certificate must not be a CA')
    return der


def pair(value):
    path = directory(value)
    exact_files(path, (KEY, CSR))
    public = request(path / CSR)
    openssl('pkcs8', '-inform', 'DER', '-in', path / KEY, '-nocrypt', '-out', '/dev/null')
    openssl('pkey', '-inform', 'DER', '-in', path / KEY, '-check', '-noout')
    key = openssl('pkey', '-inform', 'DER', '-in', path / KEY, '-pubout', '-outform', 'DER')
    require(key == public, 'private key and request do not match')
    return path, public


def rename(source, target, flags):
    library = ctypes.CDLL(None, use_errno=True)
    function = getattr(library, 'renameat2', None)
    require(function is not None, 'atomic server material publication is unavailable')
    function.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
    function.restype = ctypes.c_int
    result = function(-100, os.fsencode(source), -100, os.fsencode(target), flags)
    require(result == 0, 'atomic server material publication refused')


try:
    mode, *args = sys.argv[1:]
    if mode == 'csr':
        request(args[0])
    elif mode == 'pair':
        pair(args[0])
    elif mode == 'ca':
        authority = directory(args[0])
        regular(authority / 'ca.key')
        one_pem(authority / 'ca.pem')
    elif mode == 'leaf':
        certificate(Path(args[1]), Path(args[2]), request(args[0]))
    elif mode == 'publish':
        source = directory(args[0])
        target = path_guard(args[1])
        require(not target.exists(), 'output directory already exists')
        rename(source, target, 1)
    elif mode == 'install':
        supplied, target, expected = directory(args[0]), directory(args[1]), regular(args[2])
        descriptor = os.open(target, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
            target, public = pair(str(target))
            exact_files(supplied, PUBLIC)
            require(one_pem(supplied / PUBLIC[2]) == one_pem(expected), 'foreign operator CA refused')
            der = certificate(supplied / PUBLIC[0], expected, public)
            require(der == (supplied / PUBLIC[1]).read_bytes(), 'PEM and DER certificates differ')
            original = {name: (target / name).stat() for name in (KEY, CSR)}
            staged = Path(tempfile.mkdtemp(prefix='.ca-install-server.', dir=target.parent))
            try:
                os.chmod(staged, 0o700)
                for name in (KEY, CSR):
                    os.link(target / name, staged / name, follow_symlinks=False)
                for name in PUBLIC:
                    with open(staged / name, 'xb') as handle:
                        os.chmod(staged / name, 0o600)
                        handle.write((supplied / name).read_bytes())
                        handle.flush()
                        os.fsync(handle.fileno())
                require(set(os.listdir(target)) == {KEY, CSR}, 'request directory changed during installation')
                for name, info in original.items():
                    now = (target / name).lstat()
                    require((now.st_dev, now.st_ino, now.st_mode, now.st_uid, now.st_size, now.st_mtime_ns) == (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_size, info.st_mtime_ns), 'request material changed during installation')
                require(os.fstat(descriptor).st_ino == target.stat().st_ino, 'request directory changed during installation')
                rename(staged, target, 2)
            finally:
                shutil.rmtree(staged)
        finally:
            os.close(descriptor)
    else:
        raise ValueError('unknown server material operation')
except (OSError, ValueError, IndexError, subprocess.SubprocessError) as error:
    print('ca: server material: ' + str(error), file=sys.stderr)
    sys.exit(1)
PY
}

ca_request_server() {
	local service="$1" dir="$2" parent
	server_role "$service"
	parent="$(local_destination "$dir")"
	umask 077
	work="$(mktemp -d -p "$parent" .ca-request-server.XXXXXXXX)"
	trap 'rm -rf "$work"' EXIT
	openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 2>/dev/null |
		openssl pkcs8 -topk8 -nocrypt -outform DER -out "$work/api4-key.der" 2>/dev/null
	openssl req -new -keyform DER -key "$work/api4-key.der" -subj "/CN=$service" -out "$work/api4-request.pem"
	chmod 0600 "$work/api4-key.der" "$work/api4-request.pem"
	server_material pair "$work"
	server_material publish "$work" "$dir"
	trap - EXIT
	echo "requested $service custody=local"
}

ca_sign_server() {
	local service="$1" csr="$2" dir="$3" parent
	server_role "$service"
	server_material csr "$csr"
	server_material ca "$ca_dir"
	parent="$(local_destination "$dir")"
	umask 077
	exec {ca_issue_lock_fd}>"$ca_dir/issue.lock"
	flock -x -w "$timeout" "$ca_issue_lock_fd" || {
		echo "ca: the established authority issuer is busy" >&2
		exit 1
	}
	work="$(mktemp -d -p "$parent" .ca-sign-server.XXXXXXXX)"
	trap 'rm -rf "$work"' EXIT
	cp "$csr" "$work/csr.pem"
	chmod 0600 "$work/csr.pem"
	server_material csr "$work/csr.pem"
	sign "$service" serverAuth DNS:api4.mainnet-beta.paxeer.network
	chmod 0600 "$work/cert.pem"
	server_material leaf "$work/csr.pem" "$work/cert.pem" "$ca_dir/ca.pem"
	mv "$work/cert.pem" "$work/api4-cert.pem"
	openssl x509 -in "$work/api4-cert.pem" -outform DER -out "$work/api4-cert.der"
	cp "$ca_dir/ca.pem" "$work/api4-trust.pem"
	chmod 0600 "$work/api4-cert.pem" "$work/api4-cert.der" "$work/api4-trust.pem"
	rm "$work/csr.pem" "$work/ext.cnf"
	server_material publish "$work" "$dir"
	trap - EXIT
	echo "signed $service custody=public"
}

ca_install_server() {
	server_role "$1"
	server_material install "$2" "$3" "$4"
	echo "installed $1 custody=local"
}

mode="${1:-}"
case "$mode" in
-h | --help)
	usage
	exit 0
	;;
request-server)
	{ [ "$#" -eq 4 ] && [ "$3" = --output-dir ]; } || {
		usage >&2
		exit 2
	}
	;;
sign-server)
	{ [ "$#" -eq 6 ] && [ "$3" = --csr ] && [ "$5" = --output-dir ]; } || {
		usage >&2
		exit 2
	}
	;;
install-server)
	{ [ "$#" -eq 8 ] && [ "$3" = --input-dir ] && [ "$5" = --output-dir ] && [ "$7" = --ca-file ]; } || {
		usage >&2
		exit 2
	}
	;;
inventory)
	[ "$#" -le 2 ] || {
		usage >&2
		exit 2
	}
	;;
init | services | local-services)
	[ "$#" -eq 1 ] || {
		usage >&2
		exit 2
	}
	;;
issue-local)
	{ [ "$#" -eq 4 ] && [ "$3" = --output-dir ]; } || {
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
case "$mode" in
request-server | sign-server | install-server) tools+=(python3 stat realpath mktemp flock) ;;
esac
[ "$mode" != issue-local ] || tools+=(stat realpath mktemp find flock)
[ "$mode" != issue ] || tools+=(timeout flyctl base64 python3 flock realpath)
[ "$mode" != inventory ] || tools+=(timeout flyctl python3 realpath)
for tool in "${tools[@]}"; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "ca: $tool is required" >&2
		exit 2
	fi
done

case "$mode" in
services | inventory | issue) ca_services | table_check ca_services || exit 1 ;;
local-services | issue-local) local_services | table_check local_services || exit 1 ;;
esac

case "$mode" in
request-server) ca_request_server "$2" "$4" ;;
sign-server) ca_sign_server "$2" "$4" "$6" ;;
install-server) ca_install_server "$2" "$4" "$6" "$8" ;;
init) ca_init ;;
services) ca_services ;;
local-services) local_services ;;
inventory) ca_inventory "${2:-}" ;;
issue) ca_issue "$2" ;;
issue-local) ca_issue_local "$2" "$4" ;;
esac
