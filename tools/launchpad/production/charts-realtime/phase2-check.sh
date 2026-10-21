#!/usr/bin/env bash
set -euo pipefail
export GOWORK=off GOMAXPROCS=2 GOFLAGS=-mod=readonly
go -C services/launchpad/trading-charts test -p 1 -count=1 -json ./internal/httpapi -run 'TestReplay(GatewayOriginAdmission|ProxyStripsForgedAuthorityHeaders)|TestSocket(AdversarialFloodAndRelease|RejectsMalformedOriginAndBinaryFrames)'
apps/launchpad/web/node_modules/.bin/vitest run src/widgets/trading/chart-datafeed-recovery.test.ts src/widgets/trading/chart-datafeed-lifecycle.test.ts src/widgets/trading/chart-datafeed-revision.test.ts --root apps/launchpad/web --config ../../../tools/launchpad/production/charts-realtime/vitest.config.mts
