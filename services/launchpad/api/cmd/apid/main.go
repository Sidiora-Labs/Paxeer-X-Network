// Command apid is the core/api gateway entrypoint: the public data-plane edge.
// It fans Redis pub/sub events out to clients over WSS/SSE and serves a thin,
// rate-limited REST snapshot surface that reads the shared Postgres + Redis the
// core/* services write. All wiring lives in internal/app so the binary stays
// thin. [SECTION 10]
package main

import (
	"context"
	"log"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/app"
)

func main() {
	if err := app.Run(context.Background()); err != nil {
		log.Fatalf("apid: %v", err)
	}
}
