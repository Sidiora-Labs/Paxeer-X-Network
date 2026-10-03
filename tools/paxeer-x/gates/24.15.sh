#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
: "${PAXEER_X_REGISTRY_ARTIFACT_MANIFEST:?source-bound registry artifact manifest is required}"
exec timeout 10m python3 tools/bringup/tests/registry-artifacts.py --manifest "$PAXEER_X_REGISTRY_ARTIFACT_MANIFEST"
