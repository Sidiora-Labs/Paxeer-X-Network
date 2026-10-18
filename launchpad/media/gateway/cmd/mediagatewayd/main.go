// Command mediagatewayd is the media/gateway entrypoint: the public media edge
// that authenticates users once (EIP-191 -> JWT), fronts media/social over a
// REST + WebSocket tunnel injecting the trusted X-Actor-Wallet header, serves
// media bytes from Cloudflare R2 with CDN caching, and guards the token-create
// upload before forwarding it to media/metadata. It delegates all wiring and
// lifecycle to internal/app. [SECTION 15]
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/app"
)

func main() {
	if err := app.Run(context.Background()); err != nil {
		slog.Error("media gateway exited with error", slog.String("err", err.Error()))
		os.Exit(1)
	}
}
