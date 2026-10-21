// Command livestreamd is the media/livestream service entrypoint: Livepeer-backed
// token livestreams (create/manage streams, playback ids, pool association). It
// delegates all wiring and lifecycle to internal/app. [SECTION 14]
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/app"
)

func main() {
	if err := app.Run(context.Background()); err != nil {
		slog.Error("livestream service exited with error", slog.String("reason", "livestream_runtime_failed"))
		os.Exit(1)
	}
}
