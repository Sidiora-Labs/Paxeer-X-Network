#!/usr/bin/env bash

IMAGE_NAMES=(layerx-testnet-control layerx-gateway layerx-faucet layerx-program-registry layerx-webhooks layerx-dashboard layerx-dashboard-web
    layerx-internal layerx-human layerx-human-web layerx-node layerx-core-boundary layerx-receipt-authority layerx-agent-boundary layerx-identity layerx-paxeer-boundary
    layerx-mirror layerx-relay-archive layerx-interop-gateway layerx-reference-ramp
    paxd-node paxd)

image_source() {
    case "$1" in
        layerx-testnet-control) printf 'ghcr.io/sidiora-labs/layerx-testnet-control:0.1.0 docker/platform-testnet/Dockerfile' ;;
        layerx-gateway) printf 'ghcr.io/sidiora-labs/layerx-gateway:0.1.0 docker/platform-gateway/Dockerfile' ;;
        layerx-faucet) printf 'ghcr.io/sidiora-labs/layerx-faucet:0.1.0 docker/platform-faucet/Dockerfile' ;;
        layerx-program-registry) printf 'ghcr.io/sidiora-labs/layerx-program-registry:0.1.0 docker/platform-registry/Dockerfile' ;;
        layerx-webhooks) printf 'ghcr.io/sidiora-labs/layerx-webhooks:0.1.0 docker/platform-webhooks/Dockerfile' ;;
        layerx-dashboard) printf 'ghcr.io/sidiora-labs/layerx-dashboard:0.1.0 docker/platform-dashboard/Dockerfile' ;;
        layerx-dashboard-web) printf 'ghcr.io/sidiora-labs/layerx-dashboard-web:0.1.0 docker/platform-dashboard-web/Dockerfile' ;;
        layerx-internal) printf 'ghcr.io/sidiora-labs/layerx-internal:0.1.0 docker/platform-internal/Dockerfile' ;;
        layerx-human) printf 'ghcr.io/sidiora-labs/layerx-human:0.1.0 docker/human-service/Dockerfile' ;;
        layerx-human-web) printf 'ghcr.io/sidiora-labs/layerx-human-web:0.1.0 docker/web/Dockerfile' ;;
        layerx-node) printf 'ghcr.io/sidiora-labs/layerx-node:0.1.0 docker/platform-node/Dockerfile' ;;
        layerx-core-boundary) printf 'ghcr.io/sidiora-labs/layerx-core-boundary:0.1.0 docker/platform-core/Dockerfile' ;;
        layerx-receipt-authority) printf 'ghcr.io/sidiora-labs/layerx-receipt-authority:0.1.0 docker/platform-authority/Dockerfile' ;;
        layerx-agent-boundary) printf 'ghcr.io/sidiora-labs/layerx-agent-boundary:0.1.0 docker/platform-agent-boundary/Dockerfile' ;;
        layerx-identity) printf 'ghcr.io/sidiora-labs/layerx-identity:0.1.0 docker/platform-identity/Dockerfile' ;;
        layerx-paxeer-boundary) printf 'ghcr.io/sidiora-labs/layerx-paxeer-boundary:0.1.0 docker/paxeer/Dockerfile' ;;
        layerx-mirror) printf 'ghcr.io/sidiora-labs/layerx-mirror:0.1.0 docker/interop-mirror/Dockerfile' ;;
        layerx-relay-archive) printf 'ghcr.io/sidiora-labs/layerx-relay-archive:0.1.0 docker/relay-archive/Dockerfile' ;;
        layerx-interop-gateway) printf 'ghcr.io/sidiora-labs/layerx-interop-gateway:0.1.0 docker/interop-gateway/Dockerfile' ;;
        layerx-reference-ramp) printf 'ghcr.io/sidiora-labs/layerx-reference-ramp:0.1.0 docker/ramps/Dockerfile' ;;
        paxd-node) printf 'ghcr.io/sidiora-labs/paxd-node:0.1.0 docker/paxeer/Dockerfile.paxd-node' ;;
        paxd) printf 'ghcr.io/sidiora-labs/paxd:0.1.0 docker/paxeer/Dockerfile.paxd' ;;
        *) fail "unknown image $1" ;;
    esac
}


image_build_args() {
    case "$1" in
        layerx-node|layerx-relay-archive) printf -- '--build-arg LXP_REVISION=%s' "$REVISION" ;;
        paxd-node) printf -- '--build-arg PAX_CHAIN_REF=%s' "$REVISION" ;;
        paxd) printf -- '--build-arg PAXD_IMAGE=%s' "$(image_ref paxd-node)" ;;
        *) ;;
    esac
}

paxd_build_plan() {
    local node_ref node_recipe paxd_ref paxd_recipe
    read -r node_ref node_recipe < <(image_source paxd-node)
    read -r paxd_ref paxd_recipe < <(image_source paxd)
    python3 - "$node_recipe" "$paxd_recipe" "${REVISION:?source revision required}" <<'PYTHON'
import json
import re
import sys
node, paxd, revision = sys.argv[1:]
if not re.fullmatch(r"[0-9a-f]{40}", revision):
    raise ValueError("immutable source revision required")
print(json.dumps({"target": {
    "paxd-node": {"context": ".", "dockerfile": node,
                  "args": {"PAX_CHAIN_REF": revision}},
    "paxd": {"context": ".", "dockerfile": paxd,
             "contexts": {"paxd-base": "target:paxd-node"},
             "args": {"PAXD_IMAGE": "paxd-base"}}
}}))
PYTHON
}

paxd_check_recipe() {
    local destination=$1
    paxd_build_plan | python3 -c '
import json
from pathlib import Path
import re
import sys

plan = json.load(sys.stdin)["target"]
producer = plan["paxd-node"]
consumer = plan["paxd"]
if producer["context"] != "." or consumer["context"] != "." or consumer["contexts"] != {"paxd-base": "target:paxd-node"} or consumer["args"] != {"PAXD_IMAGE": "paxd-base"}:
    raise ValueError("unexpected canonical Paxeer dependency")
node_path = Path(producer["dockerfile"])
paxd_path = Path(consumer["dockerfile"])
node = node_path.read_text()
paxd = paxd_path.read_text()
header, separator, runtime = paxd.partition("\nFROM ${PAXD_IMAGE}\n")
if not separator or not header.startswith("ARG PAXD_IMAGE=") or "\n" in header:
    raise ValueError("Paxeer dependency declaration changed")
stages = list(re.finditer(r"^FROM ([^\n]+)$", node, re.M))
if not stages or " AS " in stages[-1].group(1).upper():
    raise ValueError("Paxeer producer final stage changed")
last = stages[-1]
node = node[:last.end()] + " AS paxd-base" + node[last.end():]
output = Path(sys.argv[1])
output.write_text(header + "\n" + node.rstrip() + "\n\nFROM paxd-base\n" + runtime)
ignored = Path(str(node_path) + ".dockerignore").read_text()
expected = ["*", "!platform/hosted/paxeer/init-chain.sh", "!platform/hosted/paxeer/contracts/BetaUsdl.runtime.hex"]
actual = Path(str(paxd_path) + ".dockerignore").read_text().splitlines()
if actual != expected:
    raise ValueError("Paxeer runtime input contract changed")
exceptions = ["!platform/", "platform/**", "!platform/hosted/", "platform/hosted/**", "!platform/hosted/paxeer/", "platform/hosted/paxeer/**", "!platform/hosted/paxeer/init-chain.sh", "!platform/hosted/paxeer/contracts/", "platform/hosted/paxeer/contracts/**", "!platform/hosted/paxeer/contracts/BetaUsdl.runtime.hex"]
Path(str(output) + ".dockerignore").write_text(ignored.rstrip() + "\n" + "\n".join(exceptions) + "\n")
' "$destination"
}

registry_manifest_digest() {
    jq -er '
        def image_manifest:
            . == "application/vnd.oci.image.manifest.v1+json"
            or . == "application/vnd.docker.distribution.manifest.v2+json";
        def image_index:
            . == "application/vnd.oci.image.index.v1+json"
            or . == "application/vnd.docker.distribution.manifest.list.v2+json";
        def descriptor:
            type == "object"
            and (.digest | type == "string" and length == 71 and test("^sha256:[0-9a-f]{64}$"))
            and (.size | type == "number" and . > 0 and floor == .)
            and (.mediaType | image_manifest or image_index);
        if descriptor and (
            if .mediaType | image_index then
                .schemaVersion == 2
                and (.manifests | type == "array" and length > 0 and all(.[]; descriptor))
            else
                (has("schemaVersion") | not) and (has("manifests") | not)
            end
        ) then .digest else error("invalid registry root manifest descriptor") end
    '
}

registry_image_digest() {
    local manifest digest
    manifest=$(docker buildx imagetools inspect --format '{{json .Manifest}}' "$1") || return 1
    digest=$(registry_manifest_digest <<<"$manifest") || return 1
    if [[ $1 == *@* ]] && [ "${1##*@}" != "$digest" ]; then
        printf 'registry root digest does not match the requested digest\n' >&2
        return 1
    fi
    printf '%s\n' "$digest"
}
