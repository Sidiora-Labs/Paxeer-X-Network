#!/bin/sh
set -eu
umask 077

usage() {
    cat <<'USAGE'
usage: probe.sh --url URL --ca FILE --client-cert FILE --client-key FILE --bearer-file FILE
                [--journey read|write --rpc-url URL --gateway-key-file FILE --credential-file FILE]

Probes the hosted agentd Service. The Service publishes the agentd loopback
health surface through a mutually authenticated TLS boundary, so a ready
deployment answers only when the caller presents both a client certificate the
internal CA issued and the agent program bearer. The probe asserts all three
properties: the authenticated request reports ready, the same request without
the bearer is refused, and a request without a client certificate never reaches
HTTP at all. While the authenticated request runs, a second authenticated TLS
connection is held open without sending a request, proving that one stalled
client cannot block health behind it.

The default read journey proves transport and verified reads only; health never
proves tenant write admission. An explicit tenant read or write journey requires
the full HTTPS /v1/agent/rpc URL, a separate gateway key file containing
<key_id>:<secret>, and an owned private JSON file containing exactly tenant,
session_id, token_id and generation. The write journey additionally requires the
authenticated tenant readiness owner to admit writes with no recovery reason.
USAGE
}

url=
ca=
client_cert=
client_key=
bearer_file=
journey=read
rpc_url=
gateway_key_file=
credential_file=
while [ $# -gt 0 ]; do
    case "$1" in
        --url) url=${2:?probe.sh: --url needs a value}; shift 2 ;;
        --ca) ca=${2:?probe.sh: --ca needs a value}; shift 2 ;;
        --client-cert) client_cert=${2:?probe.sh: --client-cert needs a value}; shift 2 ;;
        --client-key) client_key=${2:?probe.sh: --client-key needs a value}; shift 2 ;;
        --bearer-file) bearer_file=${2:?probe.sh: --bearer-file needs a value}; shift 2 ;;
        --journey) journey=${2:?probe.sh: --journey needs a value}; shift 2 ;;
        --rpc-url) rpc_url=${2:?probe.sh: --rpc-url needs a value}; shift 2 ;;
        --gateway-key-file) gateway_key_file=${2:?probe.sh: --gateway-key-file needs a value}; shift 2 ;;
        --credential-file) credential_file=${2:?probe.sh: --credential-file needs a value}; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) printf 'probe.sh: unknown argument %s\n' "$1" >&2; usage >&2; exit 2 ;;
    esac
done

for required in url ca client_cert client_key bearer_file; do
    eval "value=\$$required"
    if [ -z "$value" ]; then
        printf 'probe.sh: --%s is required\n' "$(printf '%s' "$required" | tr '_' '-')" >&2
        exit 2
    fi
done
for file in "$ca" "$client_cert" "$client_key" "$bearer_file"; do
    [ -r "$file" ] || { printf 'probe.sh: %s is not readable\n' "$file" >&2; exit 2; }
done
[ -s "$bearer_file" ] || { printf 'probe.sh: the agent program bearer is empty\n' >&2; exit 2; }
case "$journey" in
    read|write) ;;
    *) printf 'probe.sh: --journey must be read or write\n' >&2; exit 2 ;;
esac
tenant_probe=0
if [ "$journey" = write ] || [ -n "$rpc_url$gateway_key_file$credential_file" ]; then
    if [ -z "$rpc_url" ] || [ -z "$gateway_key_file" ] || [ -z "$credential_file" ]; then
        printf 'probe.sh: a tenant journey requires --rpc-url, --gateway-key-file and --credential-file\n' >&2
        exit 2
    fi
    tenant_probe=1
fi

work=$(mktemp -d)
stalled=
cleanup() {
    if [ -n "$stalled" ]; then
        kill "$stalled" 2>/dev/null || true
        wait "$stalled" 2>/dev/null || true
    fi
    rm -rf "${work:?}"
}
trap cleanup EXIT INT TERM
if [ "$tenant_probe" = 1 ]; then
    python3 - "$rpc_url" "$gateway_key_file" "$credential_file" "$bearer_file" "$work" <<'PY'
import json
import os
import pathlib
import re
import stat
import sys
import urllib.parse

def refuse(message):
    raise SystemExit('agentd-probe: ' + message)

def private(path, limit):
    name = pathlib.Path(path)
    if any(part == '.env' or part.startswith('.env.') or part.endswith('.env') for part in name.parts):
        refuse('environment files are not probe credentials')
    try:
        before = os.lstat(path)
        if (not stat.S_ISREG(before.st_mode) or before.st_uid != os.geteuid()
                or before.st_mode & 0o077 or before.st_nlink != 1 or before.st_size > limit):
            refuse('credential files must be owned private regular files with one link')
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(fd, 'rb') as source:
            opened = os.fstat(source.fileno())
            if (opened.st_dev, opened.st_ino) != (before.st_dev, before.st_ino):
                refuse('credential file changed while opening')
            data = source.read(limit + 1)
            after = os.fstat(source.fileno())
            if (after.st_uid != os.geteuid() or after.st_mode & 0o077
                    or after.st_nlink != 1 or len(data) > limit
                    or (after.st_size, after.st_mtime_ns) != (opened.st_size, opened.st_mtime_ns)):
                refuse('credential file changed while reading')
        return data, (opened.st_dev, opened.st_ino)
    except OSError:
        refuse('protected credential file could not be read')

def unique(items):
    value = {}
    for key, item in items:
        if key in value:
            refuse('duplicate credential fields')
        value[key] = item
    return value

try:
    url = urllib.parse.urlsplit(sys.argv[1])
    if (url.scheme != 'https' or not url.hostname or url.username is not None
            or url.password is not None or url.path != '/v1/agent/rpc'
            or url.query or url.fragment or url.port == 0):
        refuse('RPC URL must be the full HTTPS /v1/agent/rpc target')
    key_bytes, key_identity = private(sys.argv[2], 256)
    credential_bytes, credential_identity = private(sys.argv[3], 4096)
    bearer_identity = os.stat(sys.argv[4])
    if key_identity in (credential_identity, (bearer_identity.st_dev, bearer_identity.st_ino)):
        refuse('gateway key must be separate from session credentials and program bearer')
    key = key_bytes.decode('ascii').removesuffix('\n')
    if re.fullmatch(r'[A-Za-z0-9_-]{1,64}:[A-Za-z0-9_-]{1,192}', key) is None:
        refuse('gateway key must contain one canonical key_id:secret line')
    if key_bytes.strip() == pathlib.Path(sys.argv[4]).read_bytes().strip():
        refuse('gateway key must differ from the program reader bearer')
    credential = json.loads(credential_bytes, object_pairs_hook=unique)
    if (not isinstance(credential, dict)
            or set(credential) != {'tenant', 'session_id', 'token_id', 'generation'}
            or not all(isinstance(value, str) for value in credential.values())):
        refuse('session credential must contain exactly four string coordinates')
    tenant = credential['tenant']
    if not 0 < len(tenant.encode('utf-8')) <= 255 or '\0' in tenant:
        refuse('tenant coordinate is outside its bound')
    for field in ('session_id', 'token_id'):
        if re.fullmatch(r'[0-9a-f]{64}', credential[field]) is None:
            refuse('session identifiers must be canonical lowercase hex32')
    generation = credential['generation']
    if re.fullmatch(r'[1-9][0-9]{0,19}', generation) is None or int(generation) >= 2**64:
        refuse('session generation must be a canonical nonzero u64 decimal string')
    work = pathlib.Path(sys.argv[5])
    (work / 'tenant-request.json').write_text(json.dumps({
        'version': 1, 'request_id': '1', 'operation': 'tenant.readiness',
        'request': {}, 'credential': credential,
    }, separators=(',', ':')), encoding='utf-8')
    (work / 'gateway.conf').write_text('header = "Authorization: LayerX-Key ' + key + '"\n', encoding='ascii')
except (ValueError, UnicodeError, OSError):
    refuse('protected credentials or RPC URL are malformed')
PY
fi
python3 - "$url" "$ca" "$client_cert" "$client_key" "$work/stalled-ready" <<'PY' > "$work/stalled.log" 2>&1 &
import pathlib
import socket
import ssl
import sys
import urllib.parse
url = urllib.parse.urlsplit(sys.argv[1])
if url.scheme != 'https' or not url.hostname:
    raise SystemExit('HTTPS URL required')
context = ssl.create_default_context(cafile=sys.argv[2])
context.load_cert_chain(sys.argv[3], sys.argv[4])
with socket.create_connection((url.hostname, url.port or 443), timeout=3) as raw:
    with context.wrap_socket(raw, server_hostname=url.hostname) as connection:
        connection.settimeout(10)
        pathlib.Path(sys.argv[5]).write_text('TLS established\n')
        connection.recv(1)
PY
stalled=$!
ready_attempt=0
while [ ! -f "$work/stalled-ready" ]; do
    if ! kill -0 "$stalled" 2>/dev/null || [ "$ready_attempt" -ge 30 ]; then
        printf 'agentd-probe: could not establish the stalled authenticated TLS peer\n' >&2
        exit 1
    fi
    ready_attempt=$((ready_attempt + 1))
    sleep 0.1
done
printf 'header = "Authorization: Bearer %s"\n' "$(cat "$bearer_file")" > "$work/bearer.conf"

status=0
curl --silent --show-error --connect-timeout 2 --max-time 2 --output "$work/ready.json" --write-out '%{http_code}' \
    --cacert "$ca" --cert "$client_cert" --key "$client_key" --config "$work/bearer.conf" \
    "$url/healthz" > "$work/ready.code" 2> "$work/ready.err" || status=$?
if [ "$status" != 0 ]; then
    printf 'agentd-probe: authenticated health request failed (curl %s)\n' "$status" >&2
    sed -n '1,10p' "$work/ready.err" >&2
    exit 1
fi
if [ "$(cat "$work/ready.code")" != 200 ]; then
    printf 'agentd-probe: health returned HTTP %s\n' "$(cat "$work/ready.code")" >&2
    sed -n '1,10p' "$work/ready.json" >&2
    exit 1
fi
if ! grep -q '"ready":true' "$work/ready.json"; then
    printf 'agentd-probe: health did not report a ready owner\n' >&2
    sed -n '1,10p' "$work/ready.json" >&2
    exit 1
fi

if ! kill -0 "$stalled" 2>/dev/null; then
    printf 'agentd-probe: the stalled TLS peer closed before concurrent health completed\n' >&2
    exit 1
fi

status=0
curl --silent --show-error --max-time 10 --output "$work/anonymous.json" --write-out '%{http_code}' \
    --cacert "$ca" --cert "$client_cert" --key "$client_key" \
    "$url/healthz" > "$work/anonymous.code" 2> "$work/anonymous.err" || status=$?
if [ "$status" != 0 ]; then
    printf 'agentd-probe: unauthenticated health request failed before HTTP (curl %s)\n' "$status" >&2
    sed -n '1,10p' "$work/anonymous.err" >&2
    exit 1
fi
if [ "$(cat "$work/anonymous.code")" != 401 ]; then
    printf 'agentd-probe: health without the agent program bearer returned HTTP %s, not 401\n' \
        "$(cat "$work/anonymous.code")" >&2
    exit 1
fi

status=0
curl --silent --show-error --max-time 10 --output /dev/null \
    --cacert "$ca" --config "$work/bearer.conf" \
    "$url/healthz" > /dev/null 2> "$work/unverified.err" || status=$?
if [ "$status" = 0 ]; then
    printf 'agentd-probe: the boundary accepted a client without a certificate\n' >&2
    exit 1
fi

if [ "$tenant_probe" = 1 ]; then
    status=0
    curl --silent --show-error --connect-timeout 2 --max-time 10 --output "$work/tenant-readiness.json" --write-out '%{http_code}' \
        --cacert "$ca" --cert "$client_cert" --key "$client_key" --config "$work/gateway.conf" \
        --header 'Content-Type: application/json' --data-binary "@$work/tenant-request.json" \
        "$rpc_url" > "$work/tenant-readiness.code" 2> "$work/tenant-readiness.err" || status=$?
    if [ "$status" != 0 ]; then
        printf 'agentd-probe: authenticated tenant readiness request failed (curl %s)\n' "$status" >&2
        exit 1
    fi
    if [ "$(cat "$work/tenant-readiness.code")" != 200 ]; then
        printf 'agentd-probe: tenant readiness returned HTTP %s\n' "$(cat "$work/tenant-readiness.code")" >&2
        exit 1
    fi
    python3 - "$work/tenant-readiness.json" "$journey" <<'PY'
import json
import pathlib
import sys

def refuse(message):
    raise SystemExit('agentd-probe: ' + message)

def unique(items):
    value = {}
    for key, item in items:
        if key in value:
            refuse('tenant readiness response contains duplicate fields')
        value[key] = item
    return value

try:
    path = pathlib.Path(sys.argv[1])
    if path.stat().st_size > 8192:
        refuse('tenant readiness response exceeds its bound')
    response = json.loads(path.read_bytes(), object_pairs_hook=unique)
    if (not isinstance(response, dict) or set(response) != {'request_id', 'value', 'verification_status'}
            or response['request_id'] != '1'):
        refuse('tenant readiness success envelope is malformed')
    verification = response['verification_status']
    if verification != {'state': 'achieved', 'level': 'Unverified'}:
        refuse('tenant readiness verification status is malformed')
    value = response['value']
    fields = {'transport_ready', 'verified_reads_ready', 'writes_admitted', 'recovery_reason'}
    reasons = {'recovery_pending', 'store_unavailable', 'store_refused', 'budget_state_unverified',
               'receipt_evidence_missing', 'durable_recovery_failed', 'spend_unreconciled',
               'transport_unavailable', 'verified_read_unavailable'}
    if not isinstance(value, dict) or set(value) != fields:
        refuse('tenant readiness value fields are malformed')
    if any(type(value[field]) is not bool for field in fields - {'recovery_reason'}):
        refuse('tenant readiness flags must be booleans')
    reason = value['recovery_reason']
    if reason is not None and (not isinstance(reason, str) or reason not in reasons):
        refuse('tenant readiness recovery reason is outside the stable contract')
    if (value['writes_admitted'] != (reason is None)
            or (value['writes_admitted'] and not (value['transport_ready'] and value['verified_reads_ready']))
            or (value['verified_reads_ready'] and not value['transport_ready'])):
        refuse('tenant readiness flags contradict the owner contract')
    if not value['transport_ready'] or not value['verified_reads_ready']:
        refuse('authenticated tenant transport or verified reads are unavailable')
    if sys.argv[2] == 'write' and (not value['writes_admitted'] or reason is not None):
        refuse('tenant writes are not admitted: ' + reason)
except (ValueError, UnicodeError, OSError):
    refuse('tenant readiness response is malformed')
PY
fi

printf 'agentd-probe: %s ready behind a stalled client; bearer and client certificate both enforced\n' "$url"
if [ "$journey" = write ]; then
    printf 'agentd-probe: authenticated tenant transport and verified reads ready; tenant writes admitted\n'
elif [ "$tenant_probe" = 1 ]; then
    printf 'agentd-probe: authenticated tenant transport and verified reads ready; no tenant write claim\n'
else
    printf 'agentd-probe: transport and verified reads only; no tenant write claim\n'
fi
