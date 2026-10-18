#!/usr/bin/env bash
set -euo pipefail
export GOWORK=off GOMAXPROCS=2 GOFLAGS=-mod=readonly
go -C core/trading-charts build ./internal/httpapi
client/apps/web/node_modules/.bin/tsc -p tools/production/charts-realtime/tsconfig.json
