#!/usr/bin/env bash
set -euo pipefail
export GOWORK=off GOMAXPROCS=2 GOFLAGS=-mod=readonly
go -C core/trading-charts test -p 1 -count=1 -json ./internal/httpapi -run 'TestReplay(GatewayOriginAdmission|ProxyStripsForgedAuthorityHeaders)|TestSocket(AdversarialFloodAndRelease|RejectsMalformedOriginAndBinaryFrames)'
client/apps/web/node_modules/.bin/vitest run src/widgets/trading/chart-datafeed-recovery.test.ts src/widgets/trading/chart-datafeed-lifecycle.test.ts src/widgets/trading/chart-datafeed-revision.test.ts --root client/apps/web --config ../../../tools/production/charts-realtime/vitest.config.mts
