#!/usr/bin/env bash
set -euo pipefail
if (($#)); then
    printf 'usage: %s (no arguments)\n' "$0" >&2
    exit 2
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
: "${PAXEER_X_JVM_BUILD_PROVENANCE:?set private build-input and compiled-artifact record}"
: "${PAXEER_X_SDKGEN_BIN:?set genuine prebuilt platform SDK generator}"
sh platform/sdk/conformance/run-jvm.sh "$PWD" --prebuilt
