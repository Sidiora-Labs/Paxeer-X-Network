#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/../../.." && pwd)
cd "$root"
if [ "${1:-}" = "--build" ]; then
    exec timeout 12m python3 tools/qualification/paxeer-x/wallet-injected-fixture.py --build
fi
if [ "$#" -ne 0 ]; then
    exit 2
fi
exec timeout 15m python3 tools/qualification/paxeer-x/wallet-injected-fixture.py --verify-baseline
