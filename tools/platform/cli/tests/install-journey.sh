#!/usr/bin/env bash
set -euo pipefail

: "${LAYERX_BIN:?set LAYERX_BIN to the built layerx executable}"
: "${LAYERX_GATEWAY_URL:?set the hosted gateway URL}"
: "${LAYERX_NETWORK_ID:?set the hosted protocol network id}"
: "${LAYERX_IDENTITY_TOKEN:?set a short-lived identity session}"
: "${LAYERX_SIGNING_SEED:?set the funded source Ed25519 seed}"
: "${LAYERX_SOURCE_ACCOUNT:?set the funded 64-hex source account}"
: "${LAYERX_PAYMENT_ASSET:?set the 64-hex payment asset}"
: "${LAYERX_PAYMENT_DESTINATION:?set the 64-hex destination account}"
: "${LAYERX_A2A_SEQUENCE:?set the next source sequence for the A2A payment}"
: "${LAYERX_SEQUENCER_PUBLIC_KEY:?set the independently pinned 64-hex sequencer public key}"
: "${LAYERX_MCP_DAEMON_BINDING:?set the absolute binding document written by genuine full-mode agent-daemon enrolment}"
: "${LAYERX_MCP_PREPARE_REQUEST_FILE:?set the private activity.prepare request issued for the enrolled daemon session}"
: "${LAYERX_MCP_SIGNER_SEED_FILE:?set the private 32-byte Ed25519 seed of the enrolled activity signer}"

hex32='^[0-9a-f]{64}$'
for value in "$LAYERX_SOURCE_ACCOUNT" "$LAYERX_PAYMENT_ASSET" "$LAYERX_PAYMENT_DESTINATION" "$LAYERX_SEQUENCER_PUBLIC_KEY"; do
  [[ "$value" =~ $hex32 ]] || { echo "journey identities must be lowercase 64-hex values" >&2; exit 1; }
done
case "$LAYERX_MCP_DAEMON_BINDING" in
  /*) ;;
  *) echo "LAYERX_MCP_DAEMON_BINDING must be absolute" >&2; exit 1 ;;
esac
for private_input in "$LAYERX_MCP_DAEMON_BINDING" "$LAYERX_MCP_PREPARE_REQUEST_FILE" "$LAYERX_MCP_SIGNER_SEED_FILE"; do
  if [ ! -f "$private_input" ] || [ -L "$private_input" ] \
    || [ "$(stat -c '%u' "$private_input")" != "$(id -u)" ] \
    || [ $((8#$(stat -c '%a' "$private_input") & 8#077)) -ne 0 ]; then
    echo "journey input $private_input must be a private regular file owned by the caller" >&2
    exit 1
  fi
done
test "$(stat -c '%s' "$LAYERX_MCP_SIGNER_SEED_FILE")" -eq 32

journey_root=$(mktemp -d)
cleanup() {
  LAYERX_CONFIG="$journey_root/config.json" LAYERX_INSTALL_ROOT="$journey_root" \
    "$LAYERX_BIN" a2a stop >/dev/null 2>&1 || true
  rm -rf "$journey_root"
}
trap cleanup EXIT INT TERM
umask 077

export LAYERX_CONFIG="$journey_root/config.json"
export LAYERX_INSTALL_ROOT="$journey_root"

"$LAYERX_BIN" --json environment use beta \
  --endpoint "$LAYERX_GATEWAY_URL" --network-id "$LAYERX_NETWORK_ID" >/dev/null
printf '%s\n' "$LAYERX_SIGNING_SEED" | \
  "$LAYERX_BIN" --json key import agent-runtime >/dev/null

mcp_refusal="$journey_root/mcp-install.json"
if "$LAYERX_BIN" --json install mcp --host layerx >"$mcp_refusal" 2>&1; then
  echo "install mcp registered a server without an agent-daemon binding" >&2
  exit 1
fi
grep -q 'binding.json' "$mcp_refusal"
grep -q 'agent-daemon enrolment' "$mcp_refusal"
test ! -e "$journey_root/mcp.json"

mcp_install="$journey_root/mcp-install-bound.json"
"$LAYERX_BIN" --json install mcp --host layerx --daemon-binding "$LAYERX_MCP_DAEMON_BINDING" >"$mcp_install"
jq -e --arg binding "$LAYERX_MCP_DAEMON_BINDING" '
  .data.component == "mcp" and .data.transport == "stdio"
  and .data.authorization == "agent-daemon" and .data.deployment_mode == "full"
  and .data.daemon_binding.path == $binding and .data.daemon_binding.declared_mode == "full"
  and .data.server.args == ["mcp", "serve", "--daemon-binding", $binding]
  and .data.server.env == {}
  and ([.data.tools[].name] | contains(["activity.prepare", "activity.disclose", "activity.sign", "activity.submit", "activity.track"]))
  and (.data.registrations | length) == 1 and .data.registrations[0].host == "layerx"
  and .data.changed == true' "$mcp_install" >/dev/null
jq -e --arg binding "$LAYERX_MCP_DAEMON_BINDING" '
  .mcpServers.layerx.args == ["mcp", "serve", "--daemon-binding", $binding]' "$journey_root/mcp.json" >/dev/null
"$LAYERX_BIN" --json install mcp --host layerx --daemon-binding "$LAYERX_MCP_DAEMON_BINDING" \
  | jq -e '.data.changed == false and .data.idempotent == true' >/dev/null
for secret in "$LAYERX_IDENTITY_TOKEN" "$LAYERX_SIGNING_SEED"; do
  if grep -qF -- "$secret" "$journey_root/mcp.json" "$mcp_install"; then
    echo "MCP installation published a credential outside credential storage" >&2
    exit 1
  fi
done

mcp_payment="$journey_root/mcp-payment.json"
python3 - "$journey_root/mcp.json" "$LAYERX_MCP_PREPARE_REQUEST_FILE" "$LAYERX_MCP_SIGNER_SEED_FILE" \
  >"$mcp_payment" <<'MCP_JOURNEY'
import json
import os
import re
import select
import subprocess
import sys
import time
from pathlib import Path

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat

LEVELS = ('StateProven', 'CheckpointFinalised', 'SettlementAnchored')
PENDING = ('Prepared', 'Signed', 'Queued', 'Submitted', 'Acknowledged', 'Unknown')
PAYMENT = {'activity.prepare', 'activity.disclose', 'activity.sign', 'activity.submit', 'activity.track'}


def require(condition, reason):
    if not condition:
        raise SystemExit('mcp journey: ' + reason)


entry = json.loads(Path(sys.argv[1]).read_text())['mcpServers']['layerx']
request = json.loads(Path(sys.argv[2]).read_text())
signer = Ed25519PrivateKey.from_private_bytes(Path(sys.argv[3]).read_bytes())
public = signer.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw).hex()
key = request.get('idempotency_key', '') if isinstance(request, dict) else ''
require(re.fullmatch('[0-9a-f]{64}', key) is not None, 'prepare request carries no idempotency key')
environment = {name: value for name, value in os.environ.items() if not name.startswith('LAYERX_')}
environment.update(entry.get('env', {}))
server = subprocess.Popen([entry['command'], *entry['args']], stdin=subprocess.PIPE,
                          stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, env=environment)
sequence = 0


def message(method, params):
    global sequence
    sequence += 1
    line = json.dumps({'jsonrpc': '2.0', 'id': sequence, 'method': method, 'params': params},
                      separators=(',', ':'))
    server.stdin.write(line.encode() + b'\n')
    server.stdin.flush()
    require(bool(select.select([server.stdout], [], [], 60)[0]), method + ' response deadline')
    raw = server.stdout.readline(1_048_577)
    require(raw.endswith(b'\n') and len(raw) <= 1_048_576, 'unbounded MCP response line')
    response = json.loads(raw)
    require(response.get('jsonrpc') == '2.0' and response.get('id') == sequence,
            'MCP response lost its invocation identity')
    require('error' not in response, method + ' refused: ' + json.dumps(response.get('error')))
    return response['result']


def call(tool, arguments):
    result = message('tools/call', {'name': tool, 'arguments': arguments,
                                    '_meta': {'layerx/idempotency_key': key}})
    require(isinstance(result, dict) and len(result.get('content', [])) == 1
            and result['content'][0].get('type') == 'text', tool + ' result framing')
    value = json.loads(result['content'][0]['text'])
    require(result.get('structuredContent') == value, tool + ' structured result differs')
    require(result.get('isError') is False and value.get('tool') == tool and 'result' in value,
            tool + ' refused: ' + json.dumps(value.get('refusal')))
    return value['result']


try:
    message('initialize', {})
    listed = {tool['name'] for tool in message('tools/list', {})['tools']}
    require(PAYMENT <= listed, 'installed server does not serve the payment journey')
    prepared = call('activity.prepare', request)
    require(re.fullmatch('[0-9a-f]{64}', prepared.get('preparation_id', '')) is not None
            and re.fullmatch('[0-9a-f]{64}', prepared.get('signing_preimage', '')) is not None
            and prepared.get('approval_required') is False, 'incomplete native preparation')
    disclosed = call('activity.disclose', {'canonical_bytes': prepared['canonical_bytes']})
    require(disclosed.get('preparation_id') == prepared['preparation_id']
            and disclosed.get('canonical_bytes') == prepared['canonical_bytes'],
            'disclosure differs from the immutable preparation')
    signature = signer.sign(bytes.fromhex(prepared['signing_preimage'])).hex()
    call('activity.sign', {'variant': 'external_signature_v1',
                           'preparation_ref': prepared['preparation_id'], 'signature': signature})
    observed = call('activity.submit', {'preparation_ref': prepared['preparation_id'],
                                        'signature': signature, 'signer_public_key': public})
    reference = observed['submission']['submission_ref']
    activity = observed['activity_id']
    deadline = time.monotonic() + 180
    while observed['submission'].get('state') != 'Executed':
        require(observed['submission'].get('state') in PENDING and observed.get('receipt') is None,
                'payment reached ' + str(observed['submission'].get('state')) + ' without a receipt')
        require(time.monotonic() < deadline, 'payment did not execute within the bounded wait')
        time.sleep(1)
        observed = call('activity.track', {'submission_ref': reference})
        require(observed['activity_id'] == activity
                and observed['submission']['submission_ref'] == reference,
                'tracking changed the submitted payment identity')
    receipt = observed.get('receipt') or {}
    require(observed['submission'].get('verification_level') in LEVELS
            and receipt.get('verification_level') in LEVELS,
            'executed payment carries no verified receipt')
    canonical = bytes.fromhex(receipt.get('canonical_bytes', ''))
    require(len(canonical) > 166 and canonical[6:10] == b'\0\0\0\x20'
            and canonical[10:42].hex() == activity, 'receipt belongs to another activity')
    print(json.dumps({'activity_id': activity, 'submission_ref': reference,
                      'verification_level': receipt['verification_level']}))
finally:
    server.stdin.close()
    server.terminate()
    server.wait(timeout=10)
MCP_JOURNEY
jq -e '.activity_id | test("^[0-9a-f]{64}$")' "$mcp_payment" >/dev/null

a2a_port="${LAYERX_A2A_PORT:-19433}"
printf '%s\n' "$LAYERX_IDENTITY_TOKEN" | \
  "$LAYERX_BIN" --json install a2a --environment beta --key agent-runtime \
    --token-stdin --source-account "$LAYERX_SOURCE_ACCOUNT" \
    --asset "$LAYERX_PAYMENT_ASSET" --listen "127.0.0.1:$a2a_port" \
    >"$journey_root/a2a-install.json"
jq -e '.data.lifecycle.state == "running"' "$journey_root/a2a-install.json" >/dev/null
a2a_authorization_file=$(jq -er '.data.authorization.credential_file' "$journey_root/a2a-install.json")
test "$(stat -c '%a' "$a2a_authorization_file")" = 600
for secret in "$LAYERX_IDENTITY_TOKEN" "$LAYERX_SIGNING_SEED"; do
  if grep -qF -- "$secret" "$journey_root/a2a/runtime.json" "$journey_root/a2a/agent-card.json" \
    "$journey_root/a2a-install.json"; then
    echo "A2A installation published a credential outside credential storage" >&2
    exit 1
  fi
done
a2a_authorization=$(tr -d '\r\n' <"$a2a_authorization_file")
"$LAYERX_BIN" --json a2a stop | jq -e '.data.state == "stopped"' >/dev/null
"$LAYERX_BIN" --json a2a start | jq -e '.data.state == "running"' >/dev/null

ready=false
for _ in $(seq 1 50); do
  if curl --silent --fail "http://127.0.0.1:$a2a_port/.well-known/agent-card.json" \
    | jq -e '.name == "LayerX"' >/dev/null; then
    ready=true
    break
  fi
  sleep 0.1
done
test "$ready" = true

now_ms=$(($(date +%s) * 1000))
expires_ms=$((now_ms + 120000))
a2a_idempotency=$(openssl rand -hex 32)
a2a_request=$(jq -nc \
  --arg destination "$LAYERX_PAYMENT_DESTINATION" \
  --arg sequence "$LAYERX_A2A_SEQUENCE" \
  --arg not_before "$now_ms" \
  --arg expires "$expires_ms" \
  --arg idempotency "$a2a_idempotency" \
  '{jsonrpc:"2.0",id:1,method:"message/send",params:{message:{kind:"message",role:"user",messageId:"install-journey",parts:[{kind:"data",data:{skill:"activity.submit",arguments:{destination:$destination,amount:"1",account_sequence:$sequence,not_before_ms:$not_before,expires_at_ms:$expires,fee_limit:"1000",idempotency_key:$idempotency}}}]}}}')
a2a_response=$(curl --fail-with-body --silent --show-error \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $a2a_authorization" \
  --data "$a2a_request" "http://127.0.0.1:$a2a_port/")
a2a_activity=$(printf '%s' "$a2a_response" | \
  jq -er '.result.artifacts[0].parts[0].data.result.gateway.result.activity_id')
[[ "$a2a_activity" =~ $hex32 ]]

receipt_request=$(jq -nc --arg activity "$a2a_activity" \
  '{jsonrpc:"2.0",id:2,method:"message/send",params:{message:{kind:"message",role:"user",messageId:"install-journey-receipt",parts:[{kind:"data",data:{skill:"receipt.get",arguments:{activity_id:$activity}}}]}}}')
receipt_document=
for _ in $(seq 1 90); do
  receipt_response=$(curl --fail-with-body --silent --show-error \
    -H 'Content-Type: application/json' -H "Authorization: Bearer $a2a_authorization" \
    --data "$receipt_request" "http://127.0.0.1:$a2a_port/")
  if printf '%s' "$receipt_response" | jq -e '.result.status.state == "completed"' >/dev/null; then
    receipt_document=$(printf '%s' "$receipt_response" | jq -ec '.result.artifacts[0].parts[0].data.result.result')
    break
  fi
  sleep 2
done
test -n "$receipt_document"
printf '%s' "$receipt_document" | jq -e \
  --arg activity "$a2a_activity" --arg asset "$LAYERX_PAYMENT_ASSET" \
  --arg sequencer "$LAYERX_SEQUENCER_PUBLIC_KEY" '
  .activity_id == $activity and .authority.asset == $asset
  and .authority.sequencer_public_key == $sequencer and (.receipt | test("^[0-9a-f]+$"))' >/dev/null
printf '%s' "$receipt_document" | jq -er '.receipt' >"$journey_root/a2a-receipt.hex"
"$LAYERX_BIN" --json receipt verify --receipt "$journey_root/a2a-receipt.hex" \
  --batch-id "$(printf '%s' "$receipt_document" | jq -er '.authority.batch_id')" \
  --asset "$LAYERX_PAYMENT_ASSET" \
  --previous-state-root "$(printf '%s' "$receipt_document" | jq -er '.authority.previous_state_root')" \
  --resulting-state-root "$(printf '%s' "$receipt_document" | jq -er '.authority.resulting_state_root')" \
  --sequencer-public-key "$LAYERX_SEQUENCER_PUBLIC_KEY" \
  | jq -e --arg activity "$a2a_activity" \
    '.data.verified == true and .data.activity_id == $activity and .data.result_code == 0' >/dev/null

"$LAYERX_BIN" --json a2a status | jq -e '.data.state == "running"' >/dev/null
"$LAYERX_BIN" --json a2a stop | jq -e '.data.state == "stopped"' >/dev/null

jq -nrc --slurpfile mcp "$mcp_payment" --arg a2a "$a2a_activity" \
  '"INSTALL_JOURNEY_RESULT " + ({tests: 7, skipped: 0, mcp_activity: $mcp[0].activity_id, a2a_activity: $a2a} | tojson)'
