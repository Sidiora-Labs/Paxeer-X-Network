#!/bin/sh
set -eu
exec timeout 15m python3 tools/qualification/paxeer-x/wallet-injected-fixture.py --verify-baseline
