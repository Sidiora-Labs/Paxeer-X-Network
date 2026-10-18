#!/usr/bin/env bash
set -euo pipefail
# Run from the candidate workspace; bounded execution is coordinator-owned.
export GOWORK=off GOMAXPROCS=2 GOFLAGS=-mod=readonly
go -C core/trading-charts test -p 1 -count=1 -race -json ./internal/httpapi -run 'Test(Snapshot|Conflict|SlowConsumer|SlowSocket|Socket|RedisPartition|QuotaAtomic|ExplicitPairs|RevisionKnowledge)'
client/apps/web/node_modules/.bin/vitest run src/widgets/trading/chart-datafeed-revision.test.ts src/widgets/trading/chart-datafeed-lifecycle.test.ts --root client/apps/web --config ../../../tools/production/charts-realtime/vitest.config.mts
