#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)
cd "$root"
[[ $# -eq 0 ]] || { echo '103.6.1 accepts no selector arguments' >&2; exit 2; }
make layerxd platform-test-node
docker build --file docker/platform-node/Dockerfile --build-arg "LXP_REVISION=$(git rev-parse HEAD)" --tag layerx-node:verify .
printf 'PAXEER_X_GATE tests=2 skipped=0\n'
