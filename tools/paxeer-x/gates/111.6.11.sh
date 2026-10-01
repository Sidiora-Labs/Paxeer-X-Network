#!/usr/bin/env bash
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
: "${PAXEER_X_EVIDENCE_DIR:?private evidence directory required}"
export CONTAINERS_CHECK_LOG_DIR="$PAXEER_X_EVIDENCE_DIR/containers-111.6.11"
mkdir -p "$CONTAINERS_CHECK_LOG_DIR" || exit 1
chmod 700 "$CONTAINERS_CHECK_LOG_DIR" || exit 1
output=$(timeout 20m tools/containers/check.sh platform-agent-boundary platform-authority platform-core platform-dashboard platform-dashboard-web platform-faucet platform-gateway platform-identity platform-internal platform-node paxeer platform-registry platform-registry-builder platform-testnet platform-webhooks ramps relay-archive hpx-registry 2>&1)
code=$?
printf '%s\n' "$output"
tests=$(printf '%s\n' "$output" | awk '/^containers-check: docker\/.* check ok$/ { n++ } END { print n+0 }')
printf 'PAXEER_X_GATE tests=%s skipped=0\n' "$tests"
exit "$code"
