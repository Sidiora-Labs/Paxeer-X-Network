// Command sociald is the media/social service entrypoint: pool chat rooms +
// threaded comments + DMs over a realtime WebSocket hub (fronted by
// media/gateway), the followers graph, and moderation. Writes are sign-free —
// identity comes from the gateway-injected X-Actor-Wallet header. It delegates
// all wiring and lifecycle to internal/app. [SECTION 13]
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/app"
)

func main() {
	if err := app.Run(context.Background()); err != nil {
		slog.Error("social service exited with error", slog.String("err", err.Error()))
		os.Exit(1)
	}
}
