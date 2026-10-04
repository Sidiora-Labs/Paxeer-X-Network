#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
exec timeout 25m python3 tools/bringup/tests/paxeer-x-runtime-contract.py --case fixture-foundation
