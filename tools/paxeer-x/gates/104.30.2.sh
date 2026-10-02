#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
exec timeout 20m python3 tools/qualification/paxeer-x/program_transfer_authority.py
