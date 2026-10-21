#!/usr/bin/env bash
# Regenerates the deployment manifest from its reviewed inputs and fails on any byte difference.
set -euo pipefail
here="$(cd "$(dirname "$0")/.." && pwd)"
root="$(cd "$here/../../../../.." && pwd)"
out="$(mktemp)"
trap 'rm -f "$out"' EXIT
python3 "$here/generate.py" \
  --source "$root/contracts/launchpad/deployments/paxeer-x-network-addresses.json" \
  --frozen-client "$root/launchpad/spec/production-system/integration/contracts/v1/chain/client-deployment.json" \
  --output "$out" >/dev/null
diff -u "$here/current.json" "$out"
echo "generate.py reproduces current.json"
