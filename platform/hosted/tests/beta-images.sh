#!/usr/bin/env bash

IMAGE_NAMES=(layerx-testnet-control layerx-gateway layerx-faucet layerx-program-registry layerx-webhooks layerx-dashboard layerx-dashboard-web
    layerx-internal layerx-human layerx-human-web layerx-node layerx-core-boundary layerx-receipt-authority layerx-agent-boundary layerx-identity layerx-paxeer-boundary
    layerx-mirror layerx-relay-archive layerx-interop-gateway layerx-reference-ramp
    paxd-node paxd
    bridge-relayer explorer-backend explorer-elixir-builder explorer-frontend explorer-sig-provider explorer-smart-contract-verifier flyci-controller flyci-runner
    gas-station hpx-registry kernel layerx localnode platform-indexer platform-registry-builder rpcnode
    search-front wallet-attestor wallet-gateway wallet-pwa x-websearch intent-ingester redis feeder)

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
        bridge-relayer) printf 'ghcr.io/sidiora-labs/bridge-relayer:0.1.0 docker/bridge-relayer/Dockerfile' ;;
        explorer-backend) printf 'ghcr.io/sidiora-labs/explorer-backend:0.1.0 docker/explorer-backend/Dockerfile' ;;
        explorer-elixir-builder) printf 'ghcr.io/sidiora-labs/explorer-elixir-builder:0.1.0 docker/explorer-elixir-builder/Dockerfile' ;;
        explorer-frontend) printf 'ghcr.io/sidiora-labs/explorer-frontend:0.1.0 docker/explorer-frontend/Dockerfile' ;;
        explorer-sig-provider) printf 'ghcr.io/sidiora-labs/explorer-sig-provider:0.1.0 docker/explorer-sig-provider/Dockerfile' ;;
        explorer-smart-contract-verifier) printf 'ghcr.io/sidiora-labs/explorer-smart-contract-verifier:0.1.0 docker/explorer-smart-contract-verifier/Dockerfile' ;;
        flyci-controller) printf 'ghcr.io/sidiora-labs/flyci-controller:0.1.0 docker/flyci-controller/Dockerfile' ;;
        flyci-runner) printf 'ghcr.io/sidiora-labs/flyci-runner:0.1.0 docker/flyci-runner/Dockerfile' ;;
        gas-station) printf 'ghcr.io/sidiora-labs/gas-station:0.1.0 docker/gas-station/Dockerfile' ;;
        hpx-registry) printf 'ghcr.io/sidiora-labs/hpx-registry:0.1.0 docker/hpx-registry/Dockerfile' ;;
        kernel) printf 'ghcr.io/sidiora-labs/kernel:0.1.0 docker/kernel/Dockerfile' ;;
        layerx) printf 'ghcr.io/sidiora-labs/layerx:0.1.0 docker/layerx/Dockerfile' ;;
        localnode) printf 'ghcr.io/sidiora-labs/localnode:0.1.0 docker/localnode/Dockerfile' ;;
        platform-indexer) printf 'ghcr.io/sidiora-labs/platform-indexer:0.1.0 docker/platform-indexer/Dockerfile' ;;
        platform-registry-builder) printf 'ghcr.io/sidiora-labs/platform-registry-builder:0.1.0 docker/platform-registry-builder/Dockerfile' ;;
        rpcnode) printf 'ghcr.io/sidiora-labs/rpcnode:0.1.0 docker/rpcnode/Dockerfile' ;;
        search-front) printf 'ghcr.io/sidiora-labs/search-front:0.1.0 docker/search-front/Dockerfile' ;;
        wallet-attestor) printf 'ghcr.io/sidiora-labs/wallet-attestor:0.1.0 docker/wallet-attestor/Dockerfile' ;;
        wallet-gateway) printf 'ghcr.io/sidiora-labs/wallet-gateway:0.1.0 docker/wallet-gateway/Dockerfile' ;;
        wallet-pwa) printf 'ghcr.io/sidiora-labs/wallet-pwa:0.1.0 docker/wallet-pwa/Dockerfile' ;;
        x-websearch) printf 'ghcr.io/sidiora-labs/x-websearch:0.1.0 docker/x-websearch/Dockerfile' ;;
        intent-ingester) printf 'ghcr.io/sidiora-labs/intent-ingester:0.1.0 docker/intent-ingester/Dockerfile' ;;
        redis) printf 'ghcr.io/sidiora-labs/redis:0.1.0 docker/redis/Dockerfile' ;;
        feeder) printf 'ghcr.io/sidiora-labs/feeder:0.1.0 platform/hosted/feeder/Dockerfile' ;;
        *) fail "unknown image $1" ;;
    esac
}


image_build_args() {
    case "$1" in
        layerx-node|layerx-relay-archive) printf -- '--build-arg LXP_REVISION=%s' "$REVISION" ;;
        kernel) printf -- '--build-arg LXP_REVISION=%s' "$(git -C "${REPO_ROOT:-.}" rev-parse HEAD)" ;;
        flyci-controller) printf -- '--build-arg SOURCE_REVISION=%s' "$(git -C "${REPO_ROOT:-.}" rev-parse HEAD)" ;;
        wallet-gateway) printf -- '--build-arg SOURCE_REVISION=%s --build-arg SOURCE_TREE=%s' \
            "$(git -C "${REPO_ROOT:-.}" rev-parse HEAD)" "$(git -C "${REPO_ROOT:-.}" rev-parse 'HEAD^{tree}')" ;;
        paxd-node) printf -- '--build-arg PAX_CHAIN_REF=%s' "$REVISION" ;;
        paxd) printf -- '--build-arg PAXD_IMAGE=%s' "$(image_ref paxd-node)" ;;
        *) ;;
    esac
}

image_target() {
    # image_target NAME: the name of the last (runtime) stage of a multi-stage Dockerfile, empty otherwise
    local canonical dockerfile
    read -r canonical dockerfile <<<"$(image_source "$1")"
    dockerfile="${REPO_ROOT:-.}/$dockerfile"
    [ -f "$dockerfile" ] || return 0
    [ "$(grep -cE '^FROM[[:space:]]' "$dockerfile")" -gt 1 ] || return 0
    grep -E '^FROM[[:space:]]' "$dockerfile" | tail -n 1 \
        | sed -nE 's/^FROM[[:space:]].*[[:space:]][Aa][Ss][[:space:]]+([^[:space:]]+)[[:space:]]*$/\1/p'
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
