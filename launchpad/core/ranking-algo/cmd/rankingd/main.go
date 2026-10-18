// Command rankingd is the core/ranking-algo service entrypoint: it computes pool
// rankings (trending, breakout, top-volume, movers, unusual, new-pools) on a
// schedule, publishes them to Redis sorted sets, and serves a rate-limited read
// API. All wiring lives in internal/app so the binary stays thin.
package main

import (
	"context"
	"log"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/app"
)

func main() {
	if err := app.Run(context.Background()); err != nil {
		log.Fatalf("rankingd: %v", err)
	}
}
