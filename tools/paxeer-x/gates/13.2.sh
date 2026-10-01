#!/usr/bin/env bash
# Focused behavior gate for task 13.2: attestor signatures bound to the approved disclosure.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
PYTHONDONTWRITEBYTECODE=1 timeout 15m python3 tools/qualification/paxeer-x/wallet-attestor-disclosure.py
