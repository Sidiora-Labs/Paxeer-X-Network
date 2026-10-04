#!/usr/bin/env bash
set -euo pipefail
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
: "${PAXEER_X_SDKGEN_BIN:?set the actual source-bound compiled SDK generator}"
: "${PAXEER_X_SDKGEN_SHA256:?set its sealed artifact SHA256}"
actual_sha=$(sha256sum "$PAXEER_X_SDKGEN_BIN")
actual_sha=${actual_sha%% *}
test "$actual_sha" = "$PAXEER_X_SDKGEN_SHA256"
"$PAXEER_X_SDKGEN_BIN" --check-program-contracts "$repo_root"
