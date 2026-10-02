#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
exec timeout 30m python3 tools/qualification/paxeer-x/program_fee_governance.py
