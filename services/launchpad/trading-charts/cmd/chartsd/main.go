// Command chartsd is the core/trading-charts service entrypoint: it receives
// swap events (via Redis pub/sub and HMAC webhooks), builds OHLCV candles across
// all timeframes, serves TradingView UDF data, and streams real-time candle
// updates over WebSocket. All wiring lives in internal/app so the binary stays
// thin.
package main

import (
	"context"
	"log"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/app"
)

func main() {
	if err := app.Run(context.Background()); err != nil {
		log.Fatalf("chartsd: %v", err)
	}
}
