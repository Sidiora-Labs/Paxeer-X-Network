#!/usr/bin/env bash
# Focused behavior gate for task 8.3: agentd program HTTP time and admission bounds.
# The harness prints the PAXEER_X_GATE summary line and exits with the gate result.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.." || exit 1
exec timeout 30m python3 tools/qualification/paxeer-x/agent_http_bounds.py
