#!/usr/bin/env bash
set -euo pipefail

usage() {
    cat <<'EOF'
usage: tools/containers/check.sh [service ...]

Checks the container layout of the repository: every deployable service's
Dockerfile lives under docker/<service>/ and is gone from its old place, no
tracked file still names a moved Dockerfile by its old path, and every
docker/<service>/Dockerfile* passes docker build --check from its build
context. With service names only those services are checked; without them the
whole layout is, and any tracked Dockerfile outside docker/ that is not on the
out-of-scope list embedded below fails the check.

Logs of the docker build --check runs are written under
CONTAINERS_CHECK_LOG_DIR (default .logs/containers-check).

Exit status: 0 when every check passes, 1 when one fails, 2 on usage errors.
EOF
}

REPO_ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$REPO_ROOT"

# service|Dockerfile under docker/|old path|build context
SERVICES='
layerx|docker/layerx/Dockerfile|Dockerfile|.
wallet-pwa|docker/wallet-pwa/Dockerfile|human/apps/wallet/Dockerfile|.
web|docker/web/Dockerfile|human/apps/web/Dockerfile|.
wallet-gateway|docker/wallet-gateway/Dockerfile|human/wallet/gateway/Dockerfile|.
wallet-attestor|docker/wallet-attestor/Dockerfile|human/wallet/attestor/Dockerfile|.
interop-gateway|docker/interop-gateway/Dockerfile|interop/deploy/gateway/Dockerfile|.
interop-mirror|docker/interop-mirror/Dockerfile|interop/deploy/mirror/Dockerfile|.
x-websearch|docker/x-websearch/Dockerfile|interop/deploy/x-websearch/Dockerfile|.
platform-agent-boundary|docker/platform-agent-boundary/Dockerfile|platform/hosted/agent-boundary/Dockerfile|.
platform-authority|docker/platform-authority/Dockerfile|platform/hosted/authority/Dockerfile|.
platform-core|docker/platform-core/Dockerfile|platform/hosted/core/Dockerfile|.
platform-dashboard|docker/platform-dashboard/Dockerfile|platform/hosted/dashboard/Dockerfile|.
platform-dashboard-web|docker/platform-dashboard-web/Dockerfile|platform/hosted/dashboard/web/Dockerfile|.
platform-faucet|docker/platform-faucet/Dockerfile|platform/hosted/faucet/Dockerfile|.
platform-gateway|docker/platform-gateway/Dockerfile|platform/hosted/gateway/Dockerfile|.
platform-identity|docker/platform-identity/Dockerfile|platform/hosted/identity/Dockerfile|.
platform-internal|docker/platform-internal/Dockerfile|platform/hosted/internal/Dockerfile|.
platform-node|docker/platform-node/Dockerfile|platform/hosted/node/Dockerfile|.
paxeer|docker/paxeer/Dockerfile|platform/hosted/paxeer/Dockerfile|.
paxeer|docker/paxeer/Dockerfile.paxd|platform/hosted/paxeer/Dockerfile.paxd|.
paxeer|docker/paxeer/Dockerfile.paxd-node|platform/hosted/paxeer/Dockerfile.paxd-node|.
platform-registry|docker/platform-registry/Dockerfile|platform/hosted/registry/Dockerfile|.
platform-registry-builder|docker/platform-registry-builder/Dockerfile|platform/hosted/registry/builder-environment/Dockerfile|@registry-builder
platform-testnet|docker/platform-testnet/Dockerfile|platform/hosted/testnet/Dockerfile|.
platform-webhooks|docker/platform-webhooks/Dockerfile|platform/hosted/webhooks/Dockerfile|.
ramps|docker/ramps/Dockerfile|platform/ramps/Dockerfile|.
relay-archive|docker/relay-archive/Dockerfile|platform/relay_archive/Dockerfile|.
hpx-registry|docker/hpx-registry/Dockerfile|hpx/registry/Dockerfile|.
flyci-controller|docker/flyci-controller/Dockerfile|tools/flyci/controller/Dockerfile|.
flyci-runner|docker/flyci-runner/Dockerfile|tools/flyci/runner/Dockerfile|.
explorer-frontend|docker/explorer-frontend/Dockerfile|explorer/frontend/Dockerfile|.
explorer-sig-provider|docker/explorer-sig-provider/Dockerfile|explorer/services/sig-provider/Dockerfile|.
explorer-smart-contract-verifier|docker/explorer-smart-contract-verifier/Dockerfile|explorer/services/smart-contract-verifier/Dockerfile|.
explorer-backend|docker/explorer-backend/Dockerfile|explorer/backend/docker/Dockerfile|explorer/backend
explorer-backend|docker/explorer-backend/oldUI.Dockerfile|explorer/backend/docker/oldUI.Dockerfile|explorer/backend
explorer-elixir-builder|docker/explorer-elixir-builder/Dockerfile|explorer/deploy/tools/Dockerfile.elixir-builder|.
localnode|docker/localnode/Dockerfile|docker/localnode/Dockerfile|.
rpcnode|docker/rpcnode/Dockerfile|docker/rpcnode/Dockerfile|.
'

# ignore files that moved with their Dockerfile: service|new path|old path
IGNORES='
layerx|docker/layerx/Dockerfile.dockerignore|Dockerfile.dockerignore
wallet-pwa|docker/wallet-pwa/Dockerfile.dockerignore|human/apps/wallet/.dockerignore
web|docker/web/Dockerfile.dockerignore|human/apps/web/Dockerfile.dockerignore
platform-node|docker/platform-node/Dockerfile.dockerignore|platform/hosted/node/Dockerfile.dockerignore
explorer-frontend|docker/explorer-frontend/Dockerfile.dockerignore|explorer/frontend/.dockerignore
explorer-sig-provider|docker/explorer-sig-provider/Dockerfile.dockerignore|explorer/services/sig-provider/.dockerignore
explorer-smart-contract-verifier|docker/explorer-smart-contract-verifier/Dockerfile.dockerignore|explorer/services/smart-contract-verifier/.dockerignore
'

# other stale wordings of a moved file or of a sub-directory build context: service|fixed string
STALE='
layerx|Dockerfile Dockerfile.dockerignore
localnode|context: docker/localnode
localnode|cd docker && docker build
rpcnode|context: docker/rpcnode
rpcnode|cd docker && docker build
'

# tracked Dockerfiles that stay where they are: another lane, vendored upstream trees, fixtures, devcontainers
OUT_OF_SCOPE='
.devcontainer/Dockerfile
consensus/networks/local/localnode/Dockerfile
consensus/spec/ivy-proofs/Dockerfile
consensus/test/docker/Dockerfile
consensus/test/e2e/docker/Dockerfile
explorer/backend/.devcontainer/Dockerfile
platform/hosted/human/Dockerfile
platform/hosted/human/Dockerfile.dockerignore
sdk/contrib/devtools/dockerfile
sdk/contrib/images/simd-dlv/Dockerfile
sdk/contrib/images/simd-env/Dockerfile
sdk/contrib/rosetta/node/Dockerfile
sdk/contrib/rosetta/rosetta-cli/Dockerfile
storage/db_engine/litt/util/testdata/ssh-test.Dockerfile
wasm-runtime/builders/Dockerfile.alpine
wasm-runtime/builders/Dockerfile.centos7
wasm-runtime/builders/Dockerfile.cross
wasm/contrib/prototools-docker/Dockerfile
'

# files whose mentions of an old path are history or data, not references
REFERENCE_EXEMPT=(
    ':(exclude)spec'
    ':(exclude)CHANGELOG.md'
    ':(exclude)explorer/backend/CHANGELOG.md'
    ':(exclude)GOTCHA.kvx'
    ':(exclude)tools/containers/check.sh'
)
# files where the bare word Dockerfile or an unflagged docker build is not the root image
ROOT_TOKEN_EXEMPT=(
    ':(exclude)*.md'
    ':(exclude).devcontainer'
    ':(exclude)explorer/backend'
    ':(exclude)consensus'
    ':(exclude)sdk/contrib'
    ':(exclude)storage'
    ':(exclude)wasm'
    ':(exclude)wasm-runtime'
    ':(exclude)tools/codebase-map/generate.mjs'
)

DOCKERFILE_NAME='(^|/)([Dd]ockerfile(\.[^/]*)?|[^/]+\.Dockerfile)$'
LOG_DIR=${CONTAINERS_CHECK_LOG_DIR:-.logs/containers-check}

rows() { printf '%s\n' "$1" | sed '/^$/d'; }
tracked() { git ls-files --error-unmatch -- "$1" >/dev/null 2>&1; }
in_list() {
    local needle=$1 item
    shift
    for item in "$@"; do [ "$item" = "$needle" ] && return 0; done
    return 1
}

known_services=()
while IFS='|' read -r service _; do
    in_list "$service" "${known_services[@]+"${known_services[@]}"}" || known_services+=("$service")
done < <(rows "$SERVICES")

selected=()
all=1
for arg in "$@"; do
    case "$arg" in
        -h|--help) usage; exit 0 ;;
        -*) usage >&2; exit 2 ;;
    esac
    if ! in_list "$arg" "${known_services[@]}"; then
        printf 'containers-check: unknown service %s\n' "$arg" >&2
        exit 2
    fi
    selected+=("$arg")
    all=0
done
picked() { [ "$all" = 1 ] || in_list "$1" "${selected[@]}"; }

failures=0
fail() { printf 'containers-check: %s\n' "$1" >&2; failures=$((failures + 1)); }

# (a) placement
while IFS='|' read -r service new old _; do
    picked "$service" || continue
    tracked "$new" || fail "$service: $new is not tracked"
    tracked "$new.dockerignore" || fail "$service: $new.dockerignore is not tracked"
    [ "$old" = "$new" ] || ! tracked "$old" || fail "$service: $old is still tracked beside $new"
done < <(rows "$SERVICES")
while IFS='|' read -r service new old; do
    picked "$service" || continue
    tracked "$new" || fail "$service: $new is not tracked"
    ! tracked "$old" || fail "$service: $old is still tracked beside $new"
done < <(rows "$IGNORES")
if [ "$all" = 1 ]; then
    mapfile -t out_of_scope < <(rows "$OUT_OF_SCOPE")
    mapfile -t placed < <(rows "$SERVICES" | cut -d'|' -f2)
    while IFS= read -r path; do
        case "$path" in
            *.dockerignore) continue ;;
            docker/*) in_list "$path" "${placed[@]}" || fail "$path is under docker/ but not a known service Dockerfile" ;;
            *) in_list "$path" "${out_of_scope[@]}" || fail "$path is a tracked Dockerfile outside docker/ and not on the out-of-scope list" ;;
        esac
    done < <(git ls-files | grep -E "$DOCKERFILE_NAME" || true)
fi

# (b) stale references
stale=()
while IFS='|' read -r service new old _; do
    picked "$service" || continue
    [ "$old" = "$new" ] || stale+=("$old")
done < <(rows "$SERVICES")
while IFS='|' read -r service _ old; do
    picked "$service" && stale+=("$old") || true
done < <(rows "$IGNORES")
while IFS='|' read -r service text; do
    picked "$service" && stale+=("$text") || true
done < <(rows "$STALE")
if [ "${#stale[@]}" -gt 0 ]; then
    grep_args=()
    for text in "${stale[@]}"; do
        case "$text" in
            Dockerfile|Dockerfile.dockerignore) continue ;;
        esac
        grep_args+=(-e "$text")
    done
    if [ "${#grep_args[@]}" -gt 0 ]; then
        hits=$(git grep -n -F "${grep_args[@]}" -- . "${REFERENCE_EXEMPT[@]}" || true)
        if [ -n "$hits" ]; then
            printf '%s\n' "$hits" >&2
            fail "tracked files still name a moved Dockerfile by its old path"
        fi
    fi
fi
if picked layerx; then
    hits=$(git grep -n -E "['\"]Dockerfile(\.dockerignore)?['\"]|(file:|-f|--file=?|dockerfile *=) *Dockerfile(\.dockerignore)?([[:space:]]|$)|(^|[[:space:]])-[[:space:]]+Dockerfile(\.dockerignore)?[[:space:]]*$" -- . "${REFERENCE_EXEMPT[@]}" "${ROOT_TOKEN_EXEMPT[@]}" || true)
    if [ -n "$hits" ]; then
        printf '%s\n' "$hits" >&2
        fail "tracked files still name the root Dockerfile instead of docker/layerx/Dockerfile"
    fi
    hits=$(git grep -l -E 'docker (buildx )?build([[:space:]]|\\|$)' -- . "${REFERENCE_EXEMPT[@]}" "${ROOT_TOKEN_EXEMPT[@]}" 2>/dev/null | while IFS= read -r file; do
        awk -v file="$file" '
            {
                line = $0
                start = NR
                while (line ~ /\\$/ && (getline next_line) > 0) {
                    sub(/\\$/, "", line)
                    line = line " " next_line
                }
                if (line ~ /docker (buildx )?build([[:space:]]|$)/ && line !~ /(^|[[:space:]])(-f|--file)([[:space:]=]|$)/) {
                    print file ":" start ": " line
                }
            }' "$file"
    done || true)
    if [ -n "$hits" ]; then
        printf '%s\n' "$hits" >&2
        fail "docker build invocations without --file rely on a Dockerfile at the context root"
    fi
fi

# (c) docker build --check from the declared context
mkdir -p "$LOG_DIR"
checked=()
context_directory=
cleanup_context() {
    if [ -n "$context_directory" ]; then rm -rf -- "$context_directory"; fi
}
trap cleanup_context EXIT
copy_inputs() {
    python3 - "$1" "$2" <<'PYTHON'
import glob
import json
from pathlib import Path
import re
import shlex
import sys

recipe, context = map(Path, sys.argv[1:])
source = re.sub(r"\\\n[ \t]*", " ", recipe.read_text())
for line in source.splitlines():
    parts = line.strip().split(None, 1)
    if len(parts) != 2 or parts[0].upper() not in ("COPY", "ADD"):
        continue
    rest = parts[1]
    options = []
    while rest.startswith("--"):
        option, rest = rest.split(None, 1)
        options.append(option)
    if any(option.startswith("--from=") for option in options):
        continue
    paths = json.loads(rest) if rest.startswith("[") else shlex.split(rest)
    if len(paths) < 2:
        raise ValueError("COPY/ADD has no source and destination")
    for name in paths[:-1]:
        path = Path(name)
        if path.is_absolute() or ".." in path.parts or "$" in name or "://" in name:
            raise ValueError("COPY/ADD source cannot be resolved in the declared context")
        matches = glob.glob(str(context / path))
        if not matches or any(not Path(match).exists() for match in matches):
            raise ValueError("missing COPY/ADD input: " + name)
print("containers-check: COPY inputs present")
PYTHON
}
check_recipe() {
    local recipe=$1 context=$2
    if [ "$recipe" = docker/paxeer/Dockerfile.paxd ]; then
        source platform/hosted/tests/beta-images.sh
        local REVISION
        REVISION=$(git rev-parse HEAD)
        paxd_check_recipe "$LOG_DIR/paxd-check.Dockerfile" || return
        copy_inputs "$LOG_DIR/paxd-check.Dockerfile" "$context" || return
        docker build --check --build-arg "PAX_CHAIN_REF=$REVISION" -f "$LOG_DIR/paxd-check.Dockerfile" "$context"
    else
        docker build --check -f "$recipe" "$context"
    fi
}
while IFS='|' read -r service new _ context; do
    picked "$service" || continue
    if [ "$context" = @registry-builder ]; then
        context_directory=$(mktemp -d "${TMPDIR:-/tmp}/containers-builder.XXXXXX")
        if ! bash platform/hosted/registry/builder-environment/build-env.sh --prepare-context "$context_directory/prepared" >"$LOG_DIR/registry-builder-context.log" 2>&1; then
            cat "$LOG_DIR/registry-builder-context.log" >&2
            fail "$service: canonical context preparation failed"
            continue
        fi
        context="$context_directory/prepared/context"
        expected_revision=$(git rev-parse HEAD)
        if [ "$(cat "$context_directory/prepared/source-revision")" != "$expected_revision" ]; then
            fail "$service: generated context source revision differs"
            continue
        fi
    fi
    dir=$(dirname -- "$new")
    for dockerfile in "$dir"/Dockerfile*; do
        [ -f "$dockerfile" ] || continue
        case "$dockerfile" in *.dockerignore) continue ;; esac
        in_list "$dockerfile" "${checked[@]+"${checked[@]}"}" && continue
        checked+=("$dockerfile")
        log=$LOG_DIR/$(printf '%s' "$dockerfile" | tr '/' '_').log
        recipe="$dockerfile"
        if [ "$service" = platform-registry-builder ]; then
            recipe="$context/Dockerfile"
            if ! cmp -- "$dockerfile" "$recipe" || ! cmp -- "$dockerfile.dockerignore" "$recipe.dockerignore"; then
                fail "$service: prepared recipe or ignore differs from source"
                continue
            fi
        fi
        if ! copy_inputs "$recipe" "$context" >"$log.inputs" 2>&1; then
            cat "$log.inputs" >&2
            fail "$service: COPY/ADD context inputs are incomplete"
        fi
        if check_recipe "$recipe" "$context" >"$log" 2>&1; then
            printf 'containers-check: %s check ok\n' "$dockerfile"
        else
            tail -n 20 "$log" >&2
            fail "docker build --check failed for $dockerfile (log $log)"
        fi
    done
done < <(rows "$SERVICES")

if [ "$failures" -gt 0 ]; then
    printf 'containers-check: %d failure(s)\n' "$failures" >&2
    exit 1
fi
printf 'containers-check: ok\n'
