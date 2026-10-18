// Command pnld is the core/pnl-tracker service entrypoint: it receives indexer
// webhook Swap events, folds them into per-user realized/unrealized PnL positions
// (bigint cost-basis, no float), serves a rate-limited read API (positions,
// trades, portfolio, cards + OG images, referral attribution, sharer stats,
// status), and runs an idempotent reconciler that backfills missed swaps from
// indexer.swaps. All wiring lives in internal/app so the binary stays thin. Ports
// @analytics_microservices/pnl.
package main

import (
	"context"
	"log"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/app"
)

func main() {
	if err := app.Run(context.Background()); err != nil {
		log.Fatalf("pnld: %v", err)
	}
}
