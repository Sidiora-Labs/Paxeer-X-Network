#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
exec timeout 10m python3 tools/bringup/tests/paxeer-x-runtime-contract.py --case role-directory-prerequisite
