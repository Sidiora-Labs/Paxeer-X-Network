// Command statsd is the core/stats-workers service entrypoint: it receives
// indexer webhook events (Swap / MarketCreated / PoolStateUpdated), maintains
// per-pool statistics, holder balances and risk ratings, and serves a
// rate-limited read API (pool stats, holders, transactions, analytics, platform
// metrics, search, pressure, reactions). All wiring lives in internal/app so the
// binary stays thin. Ports @analytics_microservices/stats.
package main

import (
	"context"
	"log"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/app"
)

func main() {
	if err := app.Run(context.Background()); err != nil {
		log.Fatalf("statsd: %v", err)
	}
}
