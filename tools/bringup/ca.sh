#!/usr/bin/env bash
set -euo pipefail

# The CA settings and the certificate arithmetic are the probe's.
# shellcheck source=tools/bringup/check-live.sh
. "$(dirname "${BASH_SOURCE[0]}")/check-live.sh"

usage() {
	cat <<'EOF'
usage: tools/bringup/ca.sh init | inventory [<service>] | issue <service> | services
       tools/bringup/ca.sh issue-local <local service> --output-dir <dir> | local-services
       tools/bringup/ca.sh request-server paxeer-comet-boundary-api4 --output-dir <dir>
       tools/bringup/ca.sh sign-server paxeer-comet-boundary-api4 --csr <file> --output-dir <dir>
       tools/bringup/ca.sh install-server paxeer-comet-boundary-api4 --input-dir <signed-dir> --output-dir <request-dir> --ca-file <operator-ca.pem>

The internal CA of the Paxeer X Network bring-up. Runs on the CA host, the
operator host that holds the CA key, the Railway CLI login and ssh access to
the boxes.

Every row names a target: box:<VARIABLE>, a box whose ssh host alias is the
variable of that name in the private hosts file BRINGUP_HOSTS_FILE, or
railway:<service>[,<service>...], one or more services of the Railway
project in environment LAYERX_RAILWAY_ENVIRONMENT. A box row keeps its
identity under LAYERX_TLS_DIR/<service> on the box (custody volume); a
Railway row keeps it as the eight service variables PREFIX_CERT, PREFIX_KEY,
PREFIX_CA, PREFIX_CERT_DER, PREFIX_KEY_DER, PREFIX_CA_DER, PREFIX_P12 and
PREFIX_PASSWORD, base64 without line breaks, on every service of the row
(custody secrets). A row's SAN list is its fixed SANs, then its line of the
names file LAYERX_CA_NAMES_FILE, then LAYERX_EXTRA_SANS_<ROW>, the row's
name in upper case with - read as _ (for example
LAYERX_EXTRA_SANS_IDENTITY=DNS:<name>.proxy.rlwy.net for a Railway TCP
proxy name). The names file holds one ROW=<SAN list> line per row and no
other row.

init      generates the CA key and certificate under LAYERX_CA_DIR with mode
          0600 and prints nothing but the certificate's SHA-256 fingerprint.
          Refuses to touch a directory that already holds a CA.

inventory [<service>]
          inventories the identity material already retained for every row
          (or the one service) before any deployment action, and changes
          nothing: for a box row the presence of cert.pem, key.der,
          cert.der, ca.der, identity.p12 and password under
          LAYERX_TLS_DIR/<service> on the box over ssh, and whether the
          retained certificate chains to the row's CA; for a Railway row the
          presence of the eight PREFIX_* variables on each service of the row
          and verification of the deployed public PREFIX_CERT against the
          same CA.
          Only names, fingerprints and day counts are printed; no key,
          password or secret value is printed. One line per row:
          "inventory <service> target=<target> custody=volume|secrets
          state=present|absent|partial|incompatible|foreign|unreadable
          [fingerprint=<sha256> expires_in=<days>d]
          producer=tools/bringup/ca.sh issue <service>".
          A row whose CA directory is not readable here (an attestor row
          without LAYERX_ATTESTOR_CA_DIR) or whose box or service cannot be
          read is unreadable. Exits 1 when any row is unreadable or foreign.

issue <service>
          inventories the service first. Retained material that is present,
          chains to the row's CA and expires in more than
          LAYERX_CA_RENEW_DAYS days is reused and never regenerated: prints
          "reused <service> target=<target> custody=volume|secrets
          fingerprint=<sha256> expires_in=<days>d". A retained
          same-CA certificate missing its required role or service name is
          renewed under that CA. A certificate that does not chain to the
          row's CA, or material that cannot be inventoried, is refused, so no
          second, incompatible authority is ever issued beside it. Otherwise
          it issues the certificate of one service with the row's SAN list.
          For a box row, the box generates the key and signing request under
          LAYERX_TLS_DIR/<service> through ssh <alias> sh -c; only the
          request comes back, the CA signs it here and the certificate goes
          back the same way with the CA certificate as a tar stream on
          standard input, and the box derives key.der, cert.der, ca.der and
          identity.p12 locked by a password file it generates. For a Railway
          row the key and request are generated in a directory on the
          /dev/shm tmpfs of this host, the same files are derived there and
          each is set base64-encoded on standard input through railway
          variable set --stdin --skip-deploys on every service of the row,
          and the directory is removed. No key is printed. Prints one line:
          "issued <service> target=<target> custody=volume|secrets
          fingerprint=<sha256> expires_in=<days>d".
          A missing CA or box host is also reported on stdout as
          "fail material missing=<prerequisite> producer=<producer>".

services  prints the service list, one per line: service, target, custody
          ("volume" or the variable prefix), common name, extended key
          usage, SAN list as issued ("-" for a client identity).

issue-local <local service> --output-dir <dir>
          issues one fixed local identity of local-services on this host,
          never on a box or Railway: the identity, role, extended key usage
          and SAN list are the row's and nothing else is accepted. <dir> is
          an absolute path that must not exist, whose components are no
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
          "-" for the target and "local" for custody.

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
  CHECK_LIVE_TIMEOUT   seconds per ssh or railway call, default 30
  LAYERX_CA_DIR        the CA directory, default /etc/layerx/ca
  LAYERX_ATTESTOR_CA_DIR
                       the directory holding ca.key and ca.pem of the
                       attestors' gateway CA, the client authority that
                       ATTESTOR_TLS_CA of paxeer-attestor-1 to 5 bundles with
                       the node CA; issue signs the rows of attestor_services
                       under it and never under the internal CA
  LAYERX_TLS_DIR       the certificate directory root on a box, default
                       /data/tls
  LAYERX_CA_NAMES_FILE the names file, default
                       tools/bringup/railway-names.env
  LAYERX_EXTRA_SANS_<ROW>
                       extra SANs of one row, comma separated
  LAYERX_RAILWAY_ENVIRONMENT
                       the Railway environment, default beta
  LAYERX_RAILWAY_BIN   the railway CLI, default ~/.railway/bin/railway
  BRINGUP_HOSTS_FILE   the private hosts file naming each box row's ssh alias
  LAYERX_CA_RENEW_DAYS days of remaining validity below which issue renews
                       a retained certificate under the same CA, default 30

Exits 1 when the CA is missing, already present on init, a box host is
unknown, retained material is foreign or cannot be inventoried, an ssh or
railway step fails, or a row of the service tables or of the names file is
malformed, missing or listed twice; 2 on a usage error or an unknown service.
EOF
}

subject_org="Paxeer X Network"
ca_days=3650
cert_days=397
renew_days="${LAYERX_CA_RENEW_DAYS:-30}"
tls_dir="${LAYERX_TLS_DIR:-/data/tls}"
names_file="${LAYERX_CA_NAMES_FILE:-$repo_root/tools/bringup/railway-names.env}"
railway_env="${LAYERX_RAILWAY_ENVIRONMENT:-beta}"
railway_bin="${LAYERX_RAILWAY_BIN:-$HOME/.railway/bin/railway}"

# ca_services: every certificate the bring-up issues, after the issue_cert
# calls of platform/hosted/tests/beta-cluster.sh: service, target, custody,
# common name, extended key usage, fixed SAN list. The network names of a row
# (<service>.railway.internal, the box names) come from the names file; a
# service reached on loopback carries localhost and 127.0.0.1.
ca_services() {
	cat <<'EOF'
pending-core box:KERNEL_HOST volume layerx-pending-core serverAuth DNS:layerx-pending-core,DNS:localhost,IP:127.0.0.1
pending-core-admin box:KERNEL_HOST volume layerx-pending-core-admin serverAuth DNS:layerx-pending-core-admin
receipt-authority box:KERNEL_HOST volume layerx-receipt-authority serverAuth DNS:layerx-receipt-authority,DNS:authority,DNS:localhost,IP:127.0.0.1
agent-boundary box:KERNEL_HOST volume layerx-agent-boundary serverAuth DNS:layerx-agent-boundary,DNS:component,DNS:localhost,IP:127.0.0.1
agentd box:KERNEL_HOST volume layerx-agentd serverAuth DNS:layerx-agentd,DNS:localhost,IP:127.0.0.1
agentd-client box:KERNEL_HOST volume layerx-agentd-client clientAuth -
paxeer-boundary-loopback box:KERNEL_HOST volume paxeer-boundary serverAuth DNS:paxeer-boundary,DNS:paxeer-boundary-loopback,DNS:localhost,IP:127.0.0.1
paxeer-boundary-public box:KERNEL_HOST volume paxeer-observer-boundary serverAuth DNS:paxeer-observer-boundary,DNS:paxeer-boundary-public,DNS:localhost,IP:127.0.0.1
guarantor box:KERNEL_HOST volume layerx-guarantor serverAuth,clientAuth DNS:localhost,IP:127.0.0.1
human box:KERNEL_HOST volume layerx-human serverAuth DNS:layerx-human,DNS:localhost,IP:127.0.0.1
human-event-client box:KERNEL_HOST volume layerx-human-events clientAuth URI:urn:layerx:webhooks:role:producer
human-attestor-client box:KERNEL_HOST volume layerx-human-components clientAuth -
human-kms box:KERNEL_HOST volume layerx-human-kms serverAuth DNS:layerx-human-kms,DNS:localhost,IP:127.0.0.1
human-kms-client box:KERNEL_HOST volume layerx-human-components clientAuth -
human-kms-executor box:KERNEL_HOST volume layerx-human-movement clientAuth -
relay-archive box:KERNEL_HOST volume layerx-relay-archive serverAuth DNS:layerx-relay-archive,DNS:localhost,IP:127.0.0.1
gateway-redis railway:redis-router REDIS_TLS layerx-gateway-redis serverAuth DNS:layerx-gateway-redis,DNS:localhost,IP:127.0.0.1
gateway-client railway:router ENDPOINT_CLIENT layerx-gateway clientAuth URI:urn:layerx:webhooks:role:producer
identity railway:identity IDENTITY_TLS layerx-identity serverAuth DNS:layerx-identity,DNS:identity,DNS:localhost,IP:127.0.0.1
internal-kms railway:internal-kms INTERNAL_TLS kms serverAuth DNS:kms,DNS:localhost,IP:127.0.0.1
internal-journeys railway:internal-journeys INTERNAL_TLS journeys serverAuth DNS:journeys,DNS:localhost,IP:127.0.0.1
internal-payments railway:internal-payments INTERNAL_TLS payments serverAuth DNS:payments,DNS:localhost,IP:127.0.0.1
internal-approvals railway:internal-approvals INTERNAL_TLS approvals serverAuth DNS:approvals,DNS:localhost,IP:127.0.0.1
internal-programs railway:internal-programs INTERNAL_TLS programs serverAuth DNS:programs,DNS:localhost,IP:127.0.0.1
internal-redis railway:redis-internal REDIS_TLS redis serverAuth DNS:redis,DNS:localhost,IP:127.0.0.1
registry box:REGISTRY_HOST volume layerx-program-registry serverAuth DNS:layerx-program-registry,DNS:localhost,IP:127.0.0.1
registry-event-client box:REGISTRY_HOST volume layerx-registry-events clientAuth URI:urn:layerx:webhooks:role:producer
indexer railway:indexer INDEXER_TLS layerx-indexer serverAuth DNS:layerx-indexer,DNS:localhost,IP:127.0.0.1
interop-client railway:interop INTEROP_CLIENT layerx-interop-gateway clientAuth -
developer railway:webhooks-public,webhooks-ingress WEBHOOKS_INGRESS_TLS layerx-developer serverAuth DNS:layerx-webhooks,DNS:localhost,IP:127.0.0.1
developer-client railway:webhooks-public,webhooks-ingress WEBHOOKS_CLIENT layerx-developer clientAuth -
dashboard-client railway:dashboard DASHBOARD_CLIENT layerx-dashboard clientAuth -
ramp-client railway:ramp RAMP_CLIENT layerx-reference-ramp clientAuth -
EOF
}

# local_services: identities issue-local makes on the operator host for an
# operator holding them by hand; they are never on a box or Railway, so they
# stay out of ca_services and the names file.
local_services() {
	cat <<'EOF'
webhook-operator-client - local layerx-webhooks-operator clientAuth URI:urn:layerx:webhooks:role:operator
EOF
}

# attestor_services: the rows the attestors' gateway CA signs, because the
# attestors admit keys.generate and sign only from a client chaining to it.
attestor_services="human-attestor-client"

# ca_rows: ca_services with each fixed SAN list completed by the row's line of
# the names file and LAYERX_EXTRA_SANS_<ROW>; status 1 naming the problem when
# the names file is unreadable, lacks a row, names an unknown row or a line
# twice, or an extra SAN variable names no row.
ca_rows() {
	if [ ! -r "$names_file" ]; then
		echo "ca: the names file $names_file is not readable" >&2
		return 1
	fi
	ca_services | awk -v names="$names_file" -v extras=<(env | sed -n 's/^LAYERX_EXTRA_SANS_\([A-Za-z0-9_]*=.*\)$/\1/p') '
	function bad(why) {
		printf "ca: %s\n", why >"/dev/stderr"
		failed = 1
	}
	function add(list, more) {
		if (more == "") return list
		return list == "" ? more : list "," more
	}
	BEGIN {
		while ((getline line <names) > 0) {
			n++
			if (line ~ /^[[:space:]]*(#|$)/) continue
			if (line !~ /^[A-Z0-9_]+=[^[:space:]]*$/) {
				bad(names " line " n " is malformed")
				continue
			}
			key = substr(line, 1, index(line, "=") - 1)
			if (key in name) bad(names " names " key " twice")
			name[key] = substr(line, index(line, "=") + 1)
		}
		while ((getline line <extras) > 0) {
			key = substr(line, 1, index(line, "=") - 1)
			extra[key] = substr(line, index(line, "=") + 1)
		}
	}
	{
		if (NF != 6) {
			bad("ca_services row " NR " is malformed: want 6 fields, got " NF)
			next
		}
		key = toupper($1)
		gsub(/-/, "_", key)
		seen[key] = 1
		if (!(key in name)) {
			bad(names " has no line " key " for row " $1)
			next
		}
		sans = ($6 == "-") ? "" : $6
		sans = add(add(sans, name[key]), extra[key])
		print $1, $2, $3, $4, $5, (sans == "" ? "-" : sans)
	}
	END {
		for (key in name) if (!(key in seen)) bad(names " line " key " names no row")
		for (key in extra) if (!(key in seen)) bad("LAYERX_EXTRA_SANS_" key " names no row")
		exit failed
	}'
}

# table_check <table>: refuses the rows of the table on stdin unless each is
# exactly one identity: six fields, a service named once, a box target with
# volume custody or a Railway target with a variable prefix (one prefix per
# Railway service), or none and local custody for local_services, a known
# extended key usage, a SAN list of DNS, IP and URN entries named once, and a
# SAN list on every server identity.
table_check() {
	awk -v table="$1" '
	function bad(why) {
		printf "ca: %s row %d is malformed: %s\n", table, NR, why >"/dev/stderr"
		failed = 1
	}
	{
		if (NF != 6) {
			bad("want 6 fields, got " NF)
			next
		}
		if ($1 !~ /^[a-z0-9][a-z0-9-]*$/) bad("service " $1)
		else if ($1 in seen) bad("service " $1 " listed twice")
		seen[$1] = 1
		if (table == "local_services") {
			if ($2 != "-" || $3 != "local") bad("a local identity names no target and has local custody")
		} else if ($2 ~ /^box:[A-Z][A-Z0-9_]*$/) {
			if ($3 != "volume") bad("a box row has volume custody")
		} else if ($2 ~ /^railway:[a-z0-9][a-z0-9-]*(,[a-z0-9][a-z0-9-]*)*$/) {
			if ($3 !~ /^[A-Z][A-Z0-9_]*$/) bad("a railway row has a variable prefix, not " $3)
			k = split(substr($2, 9), services, ",")
			for (i = 1; i <= k; i++) {
				if ((services[i] " " $3) in prefixes) bad("prefix " $3 " of railway service " services[i] " listed twice")
				prefixes[services[i] " " $3] = 1
			}
		} else bad("target " $2)
		if ($4 !~ /^[a-z][a-z0-9-]*$/) bad("common name " $4)
		if ($5 != "serverAuth" && $5 != "clientAuth" && $5 != "serverAuth,clientAuth") bad("extended key usage " $5)
		if ($6 == "-") {
			if ($5 ~ /serverAuth/) bad("a server identity without a SAN list")
		} else {
			n = split($6, sans, ",")
			delete names
			for (i = 1; i <= n; i++) {
				if (sans[i] !~ /^(DNS:[a-z0-9.-]+|IP:[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+|URI:urn:[a-z0-9:._-]+)$/) bad("SAN " sans[i])
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

# The files every issued identity consists of, as <file>:<variable suffix>.
# derive_cmd turns key.pem, cert.pem and ca.pem into the rest; it carries no
# single quote so it runs the same through box_ssh and here.
identity_files="cert.pem:CERT key.pem:KEY ca.pem:CA cert.der:CERT_DER key.der:KEY_DER ca.der:CA_DER identity.p12:P12 password:PASSWORD"
derive_cmd() {
	printf '%s' "openssl x509 -in cert.pem -outform DER -out cert.der && openssl pkcs8 -topk8 -nocrypt -in key.pem -outform DER -out key.der && openssl x509 -in ca.pem -outform DER -out ca.der && openssl rand -hex 32 >password && openssl pkcs12 -export -inkey key.pem -in cert.pem -certfile ca.pem -name $1 -passout file:password -out identity.p12"
}

# box_host <variable>: prints the ssh host alias the private hosts file
# assigns to the variable; status 1 when the file or the variable is missing.
# The alias is never printed elsewhere.
box_host() {
	local host
	{ [ -n "${BRINGUP_HOSTS_FILE:-}" ] && [ -r "$BRINGUP_HOSTS_FILE" ]; } || return 1
	# shellcheck disable=SC1090
	host="$(. "$BRINGUP_HOSTS_FILE" >/dev/null 2>&1 && printf '%s' "${!1:-}")" || return 1
	[ -n "$host" ] && printf '%s' "$host"
}

# box_ssh <host> <command>: runs the command under sh on the box without
# prompting, bounded by CHECK_LIVE_TIMEOUT. stdin passes through and stdout is
# the result; stderr is dropped. The command carries no single quote.
box_ssh() {
	timeout "$timeout" ssh -o BatchMode=yes -- "$1" "sh -c '$2'" 2>/dev/null
}

# railway_set <service> <name> <file>: sets the service variable to the file's
# base64 on standard input, never on a command line, without a deploy.
railway_set() {
	base64 -w 0 "$3" | timeout "$timeout" "$railway_bin" variable set "$2" --stdin --service "$1" \
		--environment "$railway_env" --skip-deploys >/dev/null 2>&1
}

# railway_read <service> <prefix>: prints the variable names of the service on
# one line, then the PEM of its PREFIX_CERT when set. No other value is
# printed.
railway_read() {
	timeout "$timeout" "$railway_bin" variable list --service "$1" --environment "$railway_env" --json </dev/null 2>/dev/null |
		python3 -c '
import base64, json, sys
doc = json.load(sys.stdin)
if isinstance(doc, list):
    doc = {v.get("name"): v.get("value") for v in doc}
print(" ".join(sorted(k for k in doc if k)))
cert = doc.get(sys.argv[1] + "_CERT") or ""
if cert:
    sys.stdout.write(base64.b64decode(cert, validate=True).decode("ascii"))
' "$2" 2>/dev/null
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

# issue_box <service> <host> <cn> <eku> <sans>: the key and request are made
# on the box under LAYERX_TLS_DIR/<service> and the key stays there; the
# certificate and the CA certificate return as a tar stream.
issue_box() {
	local service="$1" host="$2" cn="$3" dir="$tls_dir/$1"
	box_ssh "$host" "umask 077 && mkdir -p $dir && chmod 0700 $dir && cd $dir && openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out key.pem.new 2>/dev/null && openssl req -new -key key.pem.new -subj \"/O=$subject_org/CN=$cn\"" </dev/null >"$work/csr.pem" || {
		echo "ca: $service: the box did not produce a signing request" >&2
		exit 1
	}
	sign "$service" "$4" "$5"
	cp "$ca_dir/ca.pem" "$work/ca.pem"
	tar -C "$work" -cf - cert.pem ca.pem | box_ssh "$host" "umask 077 && cd $dir && rm -rf incoming.new && mkdir incoming.new && tar -x -o -f - -C incoming.new && openssl verify -CAfile incoming.new/ca.pem incoming.new/cert.pem >/dev/null && mv incoming.new/ca.pem ca.pem && mv key.pem.new key.pem && mv incoming.new/cert.pem cert.pem && rmdir incoming.new && $(derive_cmd "$cn")" >/dev/null || {
		echo "ca: $service: the box did not accept the certificate" >&2
		exit 1
	}
}

# issue_railway <service> <services> <prefix> <cn> <eku> <sans>: the key and
# request are made in the tmpfs work directory and every identity file is set
# as PREFIX_* on each Railway service of the row.
issue_railway() {
	local service="$1" prefix="$3" cn="$4" file svc
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
	for svc in ${2//,/ }; do
		for file in $identity_files; do
			railway_set "$svc" "${prefix}_${file#*:}" "$work/${file%%:*}" || {
				echo "ca: $service on railway service $svc: railway variable set ${prefix}_${file#*:} failed" >&2
				exit 1
			}
		done
	done
}

# service_row <service>: the completed row of the service; exits 2 when there
# is none.
service_row() {
	local line
	line="$(awk -v s="$1" '$1 == s' <<<"$table")"
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
	local service="$1" cert="$2" row_ca="$3" roles cn eku sans want_cn want_eku want_sans host
	case "$service" in
	human-kms | human-kms-client | human-kms-executor)
		# Each Human KMS identity is exactly its own row: the server, the
		# components' service client and movement's restricted executor never
		# stand in for one another.
		read -r _ _ _ want_cn want_eku want_sans <<<"$(service_row "$service")"
		cn="$(openssl x509 -noout -subject -nameopt multiline <<<"$cert" 2>/dev/null | sed -n 's/^ *commonName *= //p')"
		eku="$(openssl x509 -noout -ext extendedKeyUsage <<<"$cert" 2>/dev/null | tail -n +2 | sed 's/^ *//')"
		sans="$(openssl x509 -noout -ext subjectAltName <<<"$cert" 2>/dev/null | tail -n +2 | sed 's/^ *//')"
		[ "$cn" = "$want_cn" ] || return 1
		case "$want_eku" in
		serverAuth)
			[ "$eku" = "TLS Web Server Authentication" ] &&
				[[ ", $sans, " == *", DNS:layerx-human-kms, "* ]] &&
				[ "$sans" = "$(printf '%s' "$want_sans" | sed 's/,/, /g; s/IP:/IP Address:/g')" ] &&
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
		host="$(service_row developer | awk '{print $6}' | tr ',' '\n' | sed -n 's/^DNS:\(.*\.railway\.internal\)$/\1/p' | head -n 1)"
		[ -n "$host" ] && openssl verify -purpose sslserver -verify_hostname "$host" \
			-CAfile "$row_ca/ca.pem" <<<"$cert" >/dev/null 2>&1
		;;
	*) return 0 ;;
	esac
}

# cert_state <service> <row ca dir> <missing> <cert>: prints "<state>
# <fingerprint> <days>" for a retained public certificate, <missing> the
# count of its absent companion files.
cert_state() {
	local fingerprint days
	if [ -z "$2" ] || [ ! -r "$2/ca.pem" ] ||
		! fingerprint="$(openssl x509 -noout -fingerprint -sha256 <<<"$4" 2>/dev/null | cut -d= -f2)" ||
		! days="$(days_left <<<"$4" 2>/dev/null)"; then
		echo "unreadable - -"
	elif ! openssl verify -CAfile "$2/ca.pem" <<<"$4" >/dev/null 2>&1; then
		echo "foreign $fingerprint $days"
	elif ! certificate_usage_matches "$1" "$4" "$2"; then
		echo "incompatible $fingerprint $days"
	elif [ "$3" -gt 0 ]; then
		echo "partial $fingerprint $days"
	else
		echo "present $fingerprint $days"
	fi
}

# inventory_box <service> <host variable> <row ca dir>: the state of the
# identity under LAYERX_TLS_DIR/<service> on the box.
inventory_box() {
	local service="$1" dir="$tls_dir/$1" host answer missing cert
	if ! host="$(box_host "$2")"; then
		echo "unreadable - -"
		return
	fi
	answer="$(box_ssh "$host" "if [ -d $dir ]; then cd $dir && for f in key.der cert.der ca.der identity.p12 password; do [ -s \$f ] || echo missing=\$f; done && if [ -s cert.pem ]; then cat cert.pem; else echo missing=cert.pem; fi; else echo directory=absent; fi; echo inventory=done" </dev/null)" || answer=""
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
	cert_state "$service" "$3" "$missing" "$cert"
}

# inventory_railway <service> <services> <prefix> <row ca dir>: the state of
# the PREFIX_* variables on every Railway service of the row; services that
# disagree make the row partial unless one is unreadable or foreign.
inventory_railway() {
	local service="$1" prefix="$3" svc answer listed cert file have want state result="" states=""
	for svc in ${2//,/ }; do
		if ! answer="$(railway_read "$svc" "$prefix")"; then
			state="unreadable - -"
		else
			listed="$(head -n 1 <<<"$answer")"
			cert="$(tail -n +2 <<<"$answer")"
			have=0
			want=0
			for file in $identity_files; do
				want=$((want + 1))
				[[ " $listed " != *" ${prefix}_${file#*:} "* ]] || have=$((have + 1))
			done
			if [ "$have" -eq 0 ]; then
				state="absent - -"
			elif [ -z "$cert" ]; then
				state="partial - -"
			else
				state="$(cert_state "$service" "$4" $((want - have)) "$cert")"
			fi
		fi
		states="$states${state%% *} "
		if [ -z "$result" ]; then
			result="$state"
		elif [ "$result" != "$state" ]; then
			result="partial - -"
		fi
	done
	case " $states" in
	*" unreadable "*) echo "unreadable - -" ;;
	*" foreign "*) [[ "$result" == foreign* ]] && echo "$result" || echo "foreign - -" ;;
	*) echo "$result" ;;
	esac
}

# inventory_row <service> <target> <custody> <row ca dir>: prints "<state>
# <fingerprint> <days>" for the material the service already has, state one
# of present, absent, partial, incompatible, foreign or unreadable. It reads
# file and variable names and the public certificate only.
inventory_row() {
	case "$2" in
	box:*) inventory_box "$1" "${2#box:}" "$4" ;;
	railway:*) inventory_railway "$1" "${2#railway:}" "$3" "$4" ;;
	esac
}

# ca_inventory [<service>]: one inventory line per row; exits 1 when any row
# is unreadable or foreign, so a deployment action never starts on material
# nobody has accounted for.
ca_inventory() {
	local service target custody row_ca state fingerprint days failures=0 rows
	if [ -n "${1:-}" ]; then
		rows="$(service_row "$1")"
	else
		rows="$table"
	fi
	while read -r service target custody _; do
		row_ca="$(row_ca_dir "$service")" || row_ca=""
		read -r state fingerprint days <<<"$(inventory_row "$service" "$target" "$custody" "$row_ca")"
		[ "$custody" = volume ] || custody=secrets
		if [ "$fingerprint" != - ]; then
			echo "inventory $service target=$target custody=$custody state=$state fingerprint=$fingerprint expires_in=${days}d producer=tools/bringup/ca.sh issue $service"
		else
			echo "inventory $service target=$target custody=$custody state=$state producer=tools/bringup/ca.sh issue $service"
		fi
		case "$state" in unreadable | foreign) failures=$((failures + 1)) ;; esac
	done <<<"$rows"
	[ "$failures" -eq 0 ] || exit 1
}

ca_issue() {
	local service="$1" line target custody cn eku sans host="" state fingerprint days
	line="$(service_row "$service")"
	read -r _ target custody cn eku sans <<<"$line"
	if [[ " $attestor_services " == *" $service "* ]]; then
		ca_dir="$(row_ca_dir "$service")" || {
			echo "fail material missing=LAYERX_ATTESTOR_CA_DIR producer=the attestors' gateway CA"
			echo "ca: $service is issued under the attestors' gateway CA, an authority apart from the internal CA; set LAYERX_ATTESTOR_CA_DIR to it" >&2
			exit 1
		}
	fi
	if [ ! -r "$ca_dir/ca.key" ] || [ ! -r "$ca_dir/ca.pem" ]; then
		echo "fail material missing=$ca_dir/ca.pem producer=tools/bringup/ca.sh init"
		echo "ca: no CA under $ca_dir; run tools/bringup/ca.sh init on the CA host" >&2
		exit 1
	fi
	if [[ "$target" == box:* ]] && ! host="$(box_host "${target#box:}")"; then
		echo "fail material missing=BRINGUP_HOSTS_FILE:${target#box:} producer=the private hosts file"
		echo "ca: $service: BRINGUP_HOSTS_FILE does not assign ${target#box:}" >&2
		exit 1
	fi
	umask 077
	exec {ca_issue_lock_fd}>"$ca_dir/issue.lock"
	flock -x -w "$timeout" "$ca_issue_lock_fd" || {
		echo "ca: the established authority issuer is busy" >&2
		exit 1
	}
	read -r state fingerprint days <<<"$(inventory_row "$service" "$target" "$custody" "$ca_dir")"
	case "$state" in
	unreadable)
		echo "ca: $service on $target: the retained material cannot be inventoried; nothing is issued before it is" >&2
		exit 1
		;;
	foreign)
		echo "ca: $service on $target: the retained certificate $fingerprint does not chain to $ca_dir/ca.pem; issuing beside it would make a second, incompatible authority, so remove it deliberately first" >&2
		exit 1
		;;
	partial)
		if [ "$custody" != volume ]; then
			echo "ca: $service on $target: retained variable material is partial; restore the original identity before issuing" >&2
			exit 1
		fi
		;;
	present)
		if [ "$days" -gt "$renew_days" ]; then
			[ "$custody" = volume ] || custody=secrets
			echo "reused $service target=$target custody=$custody fingerprint=$fingerprint expires_in=${days}d"
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
	[ "$sans" != - ] || sans=""
	if [ "$custody" = volume ]; then
		issue_box "$service" "$host" "$cn" "$eku" "$sans"
	else
		issue_railway "$service" "${target#railway:}" "$custody" "$cn" "$eku" "$sans"
		custody=secrets
	fi
	echo "issued $service target=$target custody=$custody fingerprint=$(openssl x509 -in "$work/cert.pem" -noout -fingerprint -sha256 | cut -d= -f2) expires_in=$(days_left <"$work/cert.pem")d"
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
	read -r _ _ _ cn eku sans <<<"$line"
	if [ ! -r "$ca_dir/ca.key" ] || [ ! -r "$ca_dir/ca.pem" ]; then
		echo "ca: no CA under $ca_dir; run tools/bringup/ca.sh init on the CA host" >&2
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
[ "$mode" != issue ] || tools+=(timeout ssh tar base64 python3 flock realpath "$railway_bin")
[ "$mode" != inventory ] || tools+=(timeout ssh base64 python3 realpath "$railway_bin")
for tool in "${tools[@]}"; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "ca: $tool is required" >&2
		exit 2
	fi
done

case "$mode" in
services | inventory | issue)
	table="$(ca_rows)" || exit 1
	table_check ca_services <<<"$table" || exit 1
	;;
local-services | issue-local) local_services | table_check local_services || exit 1 ;;
esac

case "$mode" in
request-server) ca_request_server "$2" "$4" ;;
sign-server) ca_sign_server "$2" "$4" "$6" ;;
install-server) ca_install_server "$2" "$4" "$6" "$8" ;;
init) ca_init ;;
services) printf '%s\n' "$table" ;;
local-services) local_services ;;
inventory) ca_inventory "${2:-}" ;;
issue) ca_issue "$2" ;;
issue-local) ca_issue_local "$2" "$4" ;;
esac
