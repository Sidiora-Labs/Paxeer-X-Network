#!/usr/bin/env bash

human_kms_prepare() (
    set -euo pipefail
    umask 077
    local root="$SECRETS_DIR/human" input="$WORK_DIR/human-evidence-input"
    local current="$WORK_DIR/human-kms-native-state.json" manifest="$WORK_DIR/human-kms-bootstrap.json"
    local funding="${LAYERX_BETA_HUMAN_INITIAL_FUNDING:-1000000000000}" tenant
    tenant=$(python3 - "$WORK_DIR/identity/source-binding.json" <<'PY'
import json, sys
value = json.load(open(sys.argv[1]))
assert set(value) == {'tenant', 'principal'}
assert isinstance(value['tenant'], str) and value['tenant']
print(value['tenant'])
PY
)
    python3 - "$REPO_ROOT/platform/hosted/human" "$SECRETS_DIR/module-registry.json" "$root/kms/registry.json" <<'PY'
import sys
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from provision import protected_json, write_json, require
registry = protected_json(sys.argv[2])
require(registry['schema_version'] == 2, sys.argv[2], 'canonical module registry')
modules = [dict(module_id=module['module'], activity_types=[(module['module'] << 16) | ordinal
    for ordinal in module['ordinals']]) for module in registry['modules']]
write_json(Path(sys.argv[3]), dict(network_id=registry['network_id'], protocol_version=3, modules=modules))
PY
    install -m 0600 "$input/recovery-policy.json" "$root/identity/recovery-policy.json"
    apply_secret "$TESTNET_NAMESPACE" layerx-human-component-material --from-file="$root/components"
    apply_secret "$TESTNET_NAMESPACE" layerx-human-kms-material --from-file="$root/kms"
    apply_secret "$TESTNET_NAMESPACE" layerx-human-identity-material --from-file="$root/identity"
    apply_secret "$TESTNET_NAMESPACE" layerx-human-onboarding-input \
        --from-file=owner-request.json="$input/owner-request.json" \
        --from-file=recovery-policy.json="$input/recovery-policy.json" \
        --from-file=module-registry.json="$SECRETS_DIR/module-registry.json"
    kube -n "$TESTNET_NAMESPACE" get statefulset layerx-node -o json > "$current"
    python3 "$REPO_ROOT/platform/hosted/human/onboarding_manifest.py" "$current" \
        "$REPO_ROOT/platform/hosted/node/deployment.yaml" "$manifest" "$(image_ref layerx-human)" \
        "$NODE_NETWORK_ID" "$PAXEER_CHAIN_ID" "$tenant" "$funding" "$NODE_ASSET_ID"
    kube apply -f "$manifest" > /dev/null
    kube -n "$TESTNET_NAMESPACE" rollout status statefulset/layerx-node --timeout=300s > /dev/null
    local name config="$SECRETS_DIR/human-onboarding-config"
    mkdir -m 0700 "$config"
    for name in TENANCY_DIGEST AUTH_INDEX_KEY STREAM_CURSOR_KEY; do
        kube -n "$TESTNET_NAMESPACE" exec layerx-node-0 -c human-onboarding -- \
            cat "/var/lib/layerx/human/onboarding-config/LAYERX_HUMAN_$name" \
            > "$config/LAYERX_HUMAN_$name"
    done
    for name in owner-kms.json owner-admission.json owner-admission.txt; do
        kube -n "$TESTNET_NAMESPACE" exec layerx-node-0 -c human-onboarding -- \
            cat "/var/lib/layerx/human/onboarding/human-evidence-input/$name" > "$input/$name"
    done
    python3 - "$REPO_ROOT/platform/hosted/human" "$WORK_DIR" "$config" "$funding" <<'PY'
import sys
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from provision import protected_json, require, write_json
root, config = Path(sys.argv[2]), Path(sys.argv[3])
owner = protected_json(root / 'human-evidence-input/owner-kms.json')
previous = protected_json(root / 'human-owner-result.json')
require(previous == {key: owner[key] for key in previous}, root, 'same actual IdentityProvider principal and recovery')
write_json(root / 'human-evidence-input/onboarding-configuration.json', dict(directory=str(config),
    sponsor_principal=owner['principal'], initial_funding=int(sys.argv[4])))
PY
)


human_browser_provision() (
    set -euo pipefail
    umask 077
    local request="$WORK_DIR/human-browser-request.json" result="$WORK_DIR/human-browser-result.json"
    local credential="$SECRETS_DIR/human-browser.credential" origin
    origin=$(cat "$SECRETS_DIR/human/config/LAYERX_HUMAN_ORIGIN")
    local -a mode=()
    if [ "${LAYERX_BETA_RETAIN_MATERIAL:-0}" = 1 ]; then
        mode=(--resume)
    else
        python3 - "$REPO_ROOT/platform/hosted/human" "$WORK_DIR/human-evidence-input/onboarding-configuration.json" "$request" <<'PY'
import secrets, sys
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from provision import protected_json, write_json
configuration = protected_json(sys.argv[2])
identity = secrets.token_hex(16)
write_json(Path(sys.argv[3]), dict(email='beta-' + identity + '@layerx.test',
    display_name='LayerX beta owner', idempotency_key='owner-' + identity,
    sponsor_principal=configuration['sponsor_principal'], initial_funding=configuration['initial_funding']))
PY
    fi
    python3 "$REPO_ROOT/platform/hosted/human/browser_onboarding.py" \
        --url "$HUMAN_URL" --ca "$CA_DIR/ca.crt" --origin "$origin" --request "$request" \
        --authenticator "$REPO_ROOT/human/apps/web/e2e/software-authenticator.ts" \
        --credential "$credential" --result "$result" "${mode[@]}"
    human_recipient_check
    local identity="$WORK_DIR/human-enrollment-identity.json"
    python3 - "$REPO_ROOT/platform/hosted/human" "$HUMAN_URL" "$CA_DIR/ca.crt" "$origin" "$credential" "$result" "$identity" <<'PYENROLLED'
import sys
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from browser_onboarding import Api
from provision import protected_bytes, protected_json, require, write_json
api = Api(sys.argv[2], sys.argv[3], sys.argv[4])
api.cookies = {'__Host-layerx_access': protected_bytes(sys.argv[5], 4096).decode()}
issued = protected_json(sys.argv[6])
identity = api.result('GET', '/internal/v1/principal', 200)
require(identity.get('active') is True and identity.get('sub') == issued['principal']
        and identity.get('session_id') == issued['session_id']
        and type(identity.get('tenant_id')) is str and identity['tenant_id'],
        'Human enrollment', 'actual principal, tenant and issued session')
path = Path(sys.argv[7])
if path.exists():
    require(protected_json(path) == identity, path, 'retained authenticated enrollment identity')
else:
    write_json(path, identity)
PYENROLLED
    local principal tenant session_id headers="$WORK_DIR/human-enrollment-headers"
    principal=$(jq -er .sub "$identity")
    tenant=$(jq -er .tenant_id "$identity")
    session_id=$(jq -er .session_id "$identity")
    printf 'Authorization: Bearer %s\n' "$(cat "$SECRETS_DIR/human-event-producer.token")" > "$headers"
    local service child status deadline body="$WORK_DIR/human-enrollment-readiness.json"
    for service in journeys approvals; do
        "$TOOLS_DIR/kubectl" --kubeconfig "$KUBECONFIG_FILE" -n "$INTERNAL_NAMESPACE" \
            port-forward --address 127.0.0.1 "service/$service" 19465:443 > "$WORK_DIR/$service-enrollment-forward.log" 2>&1 &
        child=$!
        trap 'kill "$child" 2>/dev/null || true; wait "$child" 2>/dev/null || true' EXIT
        deadline=$((SECONDS + 120))
        while :; do
            kill -0 "$child" 2>/dev/null || fail "$service enrollment readiness transport stopped"
            status=$(curl --silent --show-error --max-time 10 --cacert "$CA_DIR/ca.crt" \
                --cert "$CA_DIR/human-event-client/cert.pem" --key "$CA_DIR/human-event-client/key.pem" \
                --connect-to "$service.$INTERNAL_NAMESPACE.svc:443:127.0.0.1:19465" \
                --header "@$headers" --output "$body" --write-out '%{http_code}' \
                "https://$service.$INTERNAL_NAMESPACE.svc/internal/v1/principals/$principal/issued-enrollment") || status=000
            if [ "$status" = 200 ] && jq -e --arg principal "$principal" --arg tenant "$tenant" --arg session_id "$session_id" \
                '.principal == $principal and .tenant == $tenant and .session_id == $session_id and .bound == true and (.revision | type == "number" and . > 0) and (.generation | type == "number" and . > 0)' "$body" > /dev/null; then
                break
            fi
            [ "$SECONDS" -lt "$deadline" ] || fail "$service has not adopted the issuer-enrolled Human session (http=$status)"
            sleep 1
        done
        kill "$child" 2>/dev/null || true
        wait "$child" 2>/dev/null || true
        trap - EXIT
    done
    rm -f "$headers"

)

human_recipient_check() (
    set -euo pipefail
    umask 077
    local input="$WORK_DIR/human-recipient-public.json"
    python3 - "$REPO_ROOT/platform/hosted/human" "$WORK_DIR" "$NODE_NETWORK_ID" "$NODE_ASSET_ID" "$input" <<'PY'
import json, sys
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from provision import protected_json, require
from onboarding_material import write
root = Path(sys.argv[2])
owner = protected_json(root / 'human-evidence-input/owner-kms.json')
registration = protected_json(root / 'human-evidence-input/owner-registration.json')
browser = protected_json(root / 'human-browser-result.json')
require(registration['identity']['did'] == owner['did']
        and registration['authority'] == bytes(owner['public_key']).hex(), root, 'original native sponsor authority')
value = dict(principal=owner['principal'], network_id=int(sys.argv[3]),
    checkpoint=browser['balance']['freshness']['checkpoint'], account=registration['owner_account'],
    asset=sys.argv[4], authority=registration['authority'])
write(Path(sys.argv[5]), (json.dumps(value, separators=(',', ':')) + '\n').encode())
PY
    kube -n "$TESTNET_NAMESPACE" exec -i layerx-node-0 -c human-owner -- \
        python3 /usr/local/lib/layerx-human/recipient_check.py authorized < "$input"
    kube -n "$TESTNET_NAMESPACE" exec -i layerx-node-0 -c components -- \
        python3 /usr/local/lib/layerx-human/recipient_check.py wrong-peer < "$input"
)
