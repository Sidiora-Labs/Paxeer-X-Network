#!/usr/bin/env bash
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
exec timeout 10m python3 tools/paxeer-x/tests/durable_release_evidence.py
