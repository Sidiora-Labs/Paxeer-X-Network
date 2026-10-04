#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
exec timeout 20m python3 tests/ci/websearch-context.py
