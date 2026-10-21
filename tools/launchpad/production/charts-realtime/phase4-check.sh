#!/usr/bin/env bash
set -euo pipefail
: "${CHART_MEASUREMENT_CONFIG:?Actual private chart measurement configuration required}"
: "${CHART_MEASUREMENT_CONFIG_SHA256:?Immutable private chart configuration digest required}"
python3 tools/launchpad/production/charts-realtime/phase4-run.py --mode prepare --config "$CHART_MEASUREMENT_CONFIG" --config-sha256 "$CHART_MEASUREMENT_CONFIG_SHA256"
