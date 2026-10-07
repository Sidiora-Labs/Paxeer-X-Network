#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
ca=$root/tools/bringup/ca.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }
unset LAYERX_CA_NAMES_FILE BRINGUP_HOSTS_FILE LAYERX_ATTESTOR_CA_DIR
for v in $(env | sed -n 's/^\(LAYERX_EXTRA_SANS_[A-Za-z0-9_]*\)=.*/\1/p'); do unset "$v"; done

grep -q 'flyctl\|LAYERX_FLY_TLS_DIR\|fly_ssh\|fly_app\|\.internal"' "$ca" && fail "ca.sh still names Fly"
"$ca" --help | grep -qi fly && fail "usage still names Fly"
"$ca" --help | grep -q LAYERX_TLS_DIR || fail "usage lacks LAYERX_TLS_DIR"

# services: every row completed from the names file.
"$ca" services >"$work/services"
[ "$(wc -l <"$work/services")" -eq 33 ] || fail "want 33 rows"
awk 'NF != 6 { exit 1 }' "$work/services" || fail "a row is not six fields"
awk '$2 ~ /^box:/ && $3 != "volume" { exit 1 } $2 ~ /^railway:/ && $3 !~ /^[A-Z][A-Z0-9_]*$/ { exit 1 } $2 !~ /^(box|railway):/ { exit 1 }' \
	"$work/services" || fail "a row's target and custody disagree"
tr ' ,' '\n\n' <"$work/services" | grep '\.internal$' | grep -qv '\.railway\.internal$' && fail "a SAN names a non-Railway .internal name"
row() { awk -v s="$1" '$1 == s' "$work/services"; }
san() { row "$1" | awk '{print $6}' | tr ',' '\n' | grep -qxF "$2" || fail "$1 lacks $2"; }
san identity DNS:identity.railway.internal
san identity DNS:localhost
san internal-kms DNS:internal-kms.railway.internal
san internal-redis DNS:redis-internal.railway.internal
san gateway-redis DNS:redis-router.railway.internal
san indexer DNS:indexer.railway.internal
san developer DNS:webhooks-ingress.railway.internal
san developer DNS:webhooks-public.railway.internal
san agentd DNS:kernel.paxeer.network
san agentd DNS:machine.paxeer.network
san human-kms DNS:kernel.paxeer.network
san registry DNS:index.paxeer.network
[ "$(row identity | awk '{print $2, $3}')" = "railway:identity IDENTITY_TLS" ] || fail "identity is not a Railway prefix row"
[ "$(row registry | awk '{print $2, $3}')" = "box:REGISTRY_HOST volume" ] || fail "registry is not a box row"
[ "$(row human | awk '{print $2}')" = "box:KERNEL_HOST" ] || fail "human is not on the kernel box"
[ "$(row agentd-client | awk '{print $6}')" = - ] || fail "a client row gained a SAN"

# Extra SANs reach only their row; unknown rows and bad SANs are refused.
LAYERX_EXTRA_SANS_IDENTITY=DNS:edge-1.proxy.rlwy.net,DNS:edge-2.proxy.rlwy.net "$ca" services >"$work/extra"
grep '^identity ' "$work/extra" | grep -q 'DNS:identity.railway.internal,DNS:edge-1.proxy.rlwy.net,DNS:edge-2.proxy.rlwy.net$' ||
	fail "extra SANs not appended to identity"
[ "$(grep -c proxy.rlwy.net "$work/extra")" -eq 1 ] || fail "extra SANs leaked to other rows"
if LAYERX_EXTRA_SANS_NO_SUCH_ROW=DNS:a.example "$ca" services >/dev/null 2>&1; then fail "an extra SAN for no row was accepted"; fi
if LAYERX_EXTRA_SANS_IDENTITY=DNS:Bad_Name "$ca" services >/dev/null 2>&1; then fail "a malformed extra SAN was accepted"; fi
if LAYERX_EXTRA_SANS_IDENTITY=DNS:identity.railway.internal "$ca" services >/dev/null 2>&1; then fail "a repeated SAN was accepted"; fi

# The names file is loaded by path and must name every row exactly once.
names=$root/tools/bringup/railway-names.env
grep -v '^IDENTITY=' "$names" >"$work/missing.env"
if LAYERX_CA_NAMES_FILE=$work/missing.env "$ca" services >/dev/null 2>&1; then fail "a names file lacking a row was accepted"; fi
{ cat "$names"; echo 'NO_SUCH_ROW=DNS:a.example'; } >"$work/unknown.env"
if LAYERX_CA_NAMES_FILE=$work/unknown.env "$ca" services >/dev/null 2>&1; then fail "a names file naming no row was accepted"; fi
{ cat "$names"; echo 'IDENTITY=DNS:b.example'; } >"$work/twice.env"
if LAYERX_CA_NAMES_FILE=$work/twice.env "$ca" services >/dev/null 2>&1; then fail "a row named twice was accepted"; fi
if LAYERX_CA_NAMES_FILE=$work/absent.env "$ca" services >/dev/null 2>&1; then fail "an absent names file was accepted"; fi
sed 's/^IDENTITY=.*/IDENTITY=DNS:identity.example.net/' "$names" >"$work/other.env"
LAYERX_CA_NAMES_FILE=$work/other.env "$ca" services | grep '^identity ' | grep -q 'DNS:identity.example.net$' ||
	fail "LAYERX_CA_NAMES_FILE was not read"
sed 's/^HUMAN_KMS=.*/HUMAN_KMS=/' "$names" >"$work/server-bare.env"
LAYERX_CA_NAMES_FILE=$work/server-bare.env "$ca" services | grep -q '^human-kms .*DNS:localhost,IP:127.0.0.1$' ||
	fail "a row without network names lost its fixed SANs"

# init and issue-local against a real CA.
export LAYERX_CA_DIR=$work/ca
issue_out=$("$ca" issue identity 2>/dev/null) && fail "issue ran without a CA"
grep -qx "fail material missing=$work/ca/ca.pem producer=tools/bringup/ca.sh init" <<<"$issue_out" || fail "missing CA not reported"
fp=$("$ca" init)
[ "$fp" = "$(openssl x509 -in "$work/ca/ca.pem" -noout -fingerprint -sha256 | cut -d= -f2)" ] || fail "init printed more than the fingerprint"
for f in ca.key ca.pem ca.der; do [ "$(stat -c %a "$work/ca/$f")" = 600 ] || fail "$f is not 0600"; done
if "$ca" init >/dev/null 2>&1; then fail "init replaced a CA"; fi

mkdir -m 0700 "$work/local"
"$ca" issue-local webhook-operator-client --output-dir "$work/local/operator" | grep -q '^issued webhook-operator-client custody=local fingerprint=' ||
	fail "issue-local did not report"
openssl verify -purpose sslclient -CAfile "$work/ca/ca.pem" "$work/local/operator/cert.pem" >/dev/null || fail "local identity does not chain"
[ "$(find "$work/local/operator" -type f -perm 600 | wc -l)" -eq 8 ] || fail "local bundle is not eight 0600 files"
openssl pkcs12 -in "$work/local/operator/identity.p12" -passin "file:$work/local/operator/password" -noout || fail "p12 does not open"

set +e
"$ca" issue no-such-service >/dev/null 2>&1
[ $? -eq 2 ] || fail "an unknown service is not a usage error"
set -e

# A box row needs its alias in the private hosts file.
issue_out=$("$ca" issue registry 2>/dev/null) && fail "issue ran without a hosts file"
grep -qx 'fail material missing=BRINGUP_HOSTS_FILE:REGISTRY_HOST producer=the private hosts file' <<<"$issue_out" ||
	fail "missing hosts file not reported"
printf 'KERNEL_HOST=ca-test-kernel.invalid\n' >"$work/hosts"
issue_out=$(BRINGUP_HOSTS_FILE=$work/hosts "$ca" issue registry 2>/dev/null) && fail "issue ran without the box alias"
grep -qx 'fail material missing=BRINGUP_HOSTS_FILE:REGISTRY_HOST producer=the private hosts file' <<<"$issue_out" ||
	fail "missing box alias not reported"

# An unreachable box is unreadable and nothing is issued beside it.
printf 'REGISTRY_HOST=ca-test-registry.invalid\n' >>"$work/hosts"
inv=$(BRINGUP_HOSTS_FILE=$work/hosts CHECK_LIVE_TIMEOUT=10 "$ca" inventory registry) && fail "an unreadable box passed inventory"
[ "$inv" = "inventory registry target=box:REGISTRY_HOST custody=volume state=unreadable producer=tools/bringup/ca.sh issue registry" ] ||
	fail "unexpected inventory line: $inv"
grep -q invalid <<<"$inv" && fail "inventory printed the host alias"
serial=$(cat "$work/ca/ca.srl")
if BRINGUP_HOSTS_FILE=$work/hosts CHECK_LIVE_TIMEOUT=10 "$ca" issue registry >/dev/null 2>&1; then fail "issue went past an unreadable inventory"; fi
[ "$(cat "$work/ca/ca.srl")" = "$serial" ] || fail "a certificate was signed for an unreadable box"
echo "PASS ca"
