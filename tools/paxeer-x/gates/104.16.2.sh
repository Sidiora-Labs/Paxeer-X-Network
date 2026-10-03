#!/usr/bin/env bash
set -euo pipefail
if (($#)); then
	printf 'usage: %s (no arguments)\n' "$0" >&2
	exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
: "${LAYERX_CONNECTED_MIDDLEWARE_FIXTURE:?set protected genuine-service acceptance input}"
node platform/middleware/conformance/run.mjs
node platform/middleware/conformance/connected.mjs
