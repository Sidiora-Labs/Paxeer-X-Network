#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
cd "$root"
case "${1-}" in
  --code-only)
    [ "$#" -eq 1 ] || exit 2
    bash platform/hosted/tests/topology-check.sh --yaml-parser pyyaml
    printf 'PAXEER_X_CODE_ONLY deployment=UNRUN runtime=UNRUN\n'
    ;;
  "")
    [ "$#" -eq 0 ] || exit 2
    make platform-hosted-topology-check platform-beta-cluster-up platform-beta-cluster-down
    printf 'PAXEER_X_GATE tests=3 skipped=0\n'
    ;;
  *) exit 2 ;;
esac
