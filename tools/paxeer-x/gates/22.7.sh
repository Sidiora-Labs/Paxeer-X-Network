#!/usr/bin/env bash
set -euo pipefail
exec timeout 20m python3 tools/qualification/paxeer-x/explorer-receipt-reader.py
