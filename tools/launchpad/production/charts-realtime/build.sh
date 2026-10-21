#!/usr/bin/env bash
set -euo pipefail
export GOWORK=off GOMAXPROCS=2 GOFLAGS=-mod=readonly
go -C services/launchpad/trading-charts build ./internal/httpapi
apps/launchpad/web/node_modules/.bin/tsc -p tools/launchpad/production/charts-realtime/tsconfig.json
