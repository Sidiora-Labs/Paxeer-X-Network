#!/usr/bin/env bash
# paxeer-x-services: kernel replica-authority core-boundary paxeer-boundaries identity human human-kms wallet-ui wallet-gateway wallet-attestors agentd mcp-a2a programs-runtime program-registry internal-services redis webhooks-dashboard interop mirror archive ramp search-web xweb-attestors gas bridge markets indexer explorer hpx ci private-profile
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
exec timeout 15m python3 tools/paxeer-x/tests/routes_test.py
