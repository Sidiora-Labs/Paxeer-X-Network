#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
exec timeout 15m python3 tools/qualification/paxeer-x/router-authority-schema.py
