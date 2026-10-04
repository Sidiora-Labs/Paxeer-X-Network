#!/usr/bin/env bash
set -euo pipefail

profile="${1:-legacy}"
[[ "${profile}" == legacy || "${profile}" == task-104.25.2 ]]

verifier_bin="${LAYERX_MIRROR_VERIFY_BIN:?set LAYERX_MIRROR_VERIFY_BIN}"
config_path="${LAYERX_MIRROR_VERIFY_CONFIG:?set LAYERX_MIRROR_VERIFY_CONFIG}"
canonical_request="${LAYERX_MIRROR_CANONICAL_REQUEST:?set LAYERX_MIRROR_CANONICAL_REQUEST}"
failover_request="${LAYERX_MIRROR_FAILOVER_REQUEST:?set LAYERX_MIRROR_FAILOVER_REQUEST}"
divergence_request="${LAYERX_MIRROR_DIVERGENCE_REQUEST:?set LAYERX_MIRROR_DIVERGENCE_REQUEST}"
tamper_request="${LAYERX_MIRROR_TAMPER_REQUEST:?set LAYERX_MIRROR_TAMPER_REQUEST}"

[[ -x "${verifier_bin}" && -r "${config_path}" && -r "${canonical_request}" && -r "${failover_request}" && -r "${divergence_request}" && -r "${tamper_request}" ]]
[[ -z "${LAYERX_NODE_URL:-}" && -z "${LAYERX_GATEWAY_URL:-}" && -z "${LAYERX_EXPLORER_API_ORIGIN:-}" ]]

verify_ok() {
  local request_path=$1
  "${verifier_bin}" "${config_path}" < "${request_path}" | jq -ce 'select(.ok == true and .verification.provenance == "Canonical" and .verification.sourceId != "")'
}

verify_error() {
  local request_path=$1
  local expected=$2
  "${verifier_bin}" "${config_path}" < "${request_path}" | jq -e --arg expected "${expected}" '.ok == false and .error == $expected'
}

verify_ok "${canonical_request}"
verify_ok "${failover_request}" | jq -e '.verification.failoverCount > 0'
verify_error "${divergence_request}" divergent
verify_error "${tamper_request}" verification

for command_name in LAYERX_MIRROR_TS_CONFORMANCE LAYERX_MIRROR_PYTHON_CONFORMANCE LAYERX_MIRROR_GO_CONFORMANCE LAYERX_MIRROR_JVM_CONFORMANCE LAYERX_MIRROR_SWIFT_CONFORMANCE LAYERX_MIRROR_DOTNET_CONFORMANCE; do
  command_value="${!command_name:?set ${command_name}}"
  env -u LAYERX_NODE_URL -u LAYERX_GATEWAY_URL -u LAYERX_EXPLORER_API_ORIGIN bash -euo pipefail -c "${command_value}"
done

if [[ "${profile}" == task-104.25.2 ]]; then
  state_request="${LAYERX_MIRROR_STATE_REQUEST:?set LAYERX_MIRROR_STATE_REQUEST to genuine archived state inclusion evidence}"
  state_tamper_request="${LAYERX_MIRROR_STATE_TAMPER_REQUEST:?set LAYERX_MIRROR_STATE_TAMPER_REQUEST}"
  explorer_url="${LAYERX_MIRROR_EXPLORER_VERIFY_URL:?set the existing unified explorer verification URL}"
  [[ -r "${state_request}" && -r "${state_tamper_request}" ]]
  jq -e '.evidence.kind == "receipt"' "${canonical_request}" >/dev/null
  jq -e '.evidence.kind == "state"' "${state_request}" >/dev/null
  verify_ok "${state_request}"
  verify_error "${state_tamper_request}" verification
  python3 - "${explorer_url}" "${canonical_request}" "${state_request}" "${tamper_request}" "${state_tamper_request}" <<'PY_BROWSER'
import json, re, sys, urllib.error, urllib.parse, urllib.request
from pathlib import Path
url = sys.argv[1]
parsed = urllib.parse.urlsplit(url)
if (parsed.scheme != 'https' or parsed.hostname != 'api-mainnet-beta.paxeer.network' or parsed.port not in (None, 443) or parsed.username is not None
        or parsed.password is not None or parsed.path != '/api/explorer/verify'
        or parsed.query or parsed.fragment):
    raise SystemExit('mirror browser qualifier requires the existing HTTPS unified explorer verification route')
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, message, headers, target):
        raise RuntimeError('mirror verification redirect refused')
opener = urllib.request.build_opener(NoRedirect())
origin = 'https://api-mainnet-beta.paxeer.network'
with opener.open(origin + '/explorer/verify', timeout=20) as page:
    html = page.read(2_097_153)
    if page.status != 200 or len(html) > 2_097_152:
        raise SystemExit('unified mirror verifier page is unavailable or exceeds its response bound')
    assets = re.findall(rb'(?:src|href)="(/human-ui/_next/static/[^"<>]+)"', html)
    if not assets:
        raise SystemExit('unified mirror verifier page does not expose its confined Human static assets')
    asset = assets[0].decode('ascii')
    if urllib.parse.urlsplit(asset).query or '..' in asset.split('/'):
        raise SystemExit('unified Human asset path is not confined')
with opener.open(origin + asset, timeout=20) as response:
    if response.status != 200 or not response.read(1):
        raise SystemExit('actual unified Human asset mapping is unavailable')
for index, raw_path in enumerate(sys.argv[2:]):
    path = Path(raw_path)
    if path.stat().st_size > 1_050_000:
        raise SystemExit('genuine browser evidence exceeds the unchanged endpoint bound')
    source = json.loads(path.read_text())
    evidence = source['evidence']
    kind = 'receipt' if evidence['kind'] == 'receipt' else 'state-inclusion'
    canonical = {'batch_number': source['batch_number'], 'canonical_hex': evidence['canonical_hex'], 'policy': source['policy']}
    if kind == 'state-inclusion':
        canonical['proof_hex'] = evidence['proof_hex']
    body = json.dumps({'kind': kind, 'evidence': json.dumps(canonical, separators=(',', ':'))}, separators=(',', ':')).encode()
    request = urllib.request.Request(url, data=body, method='POST', headers={'Content-Type': 'application/json', 'Accept': 'application/json', 'Origin': origin, 'Sec-Fetch-Site': 'same-origin'})
    try:
        response = opener.open(request, timeout=20)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        value = json.loads(response.read(1_100_001))
        if index >= 2:
            if response.status != 422 or value != {'status': 'refused'}:
                raise SystemExit('actual unified explorer did not refuse altered evidence')
            continue
        if response.status != 200 or value['kind'] != kind or value['achieved_level'] != ('batch-included' if kind == 'receipt' else 'state-proven'):
            raise SystemExit('actual unified explorer did not verify genuine mirror evidence')
        mirror = value['mirror']
        if (not mirror['source_id'] or not mirror['target'] or not mirror['canonical_position']
                or mirror['provenance'] != 'canonical' or mirror['checkpoint_level'] != 'unavailable'):
            raise SystemExit('actual unified explorer omitted honest pinned mirror provenance')
        lag = mirror['batch_lag']
        if lag['kind'] == 'unknown':
            if mirror['latest_batch'] is not None or mirror['degraded'] is not True:
                raise SystemExit('unknown source head was mislabeled as current')
        elif lag['kind'] == 'known':
            if mirror['latest_batch'] is None or not isinstance(lag['batches'], str):
                raise SystemExit('known freshness omitted its actual coordinates')
        else:
            raise SystemExit('actual unified explorer returned an untyped freshness state')
PY_BROWSER
fi
