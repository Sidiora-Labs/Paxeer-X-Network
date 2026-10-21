// Command userd is the media/user service entrypoint: user profiles
// (display name / bio / socials), avatar + banner image storage on Cloudflare
// R2 with virus scanning, and per-wallet watchlists. It delegates all wiring and
// lifecycle to internal/app. [SECTION 12]
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/app"
)

func main() {
	if err := app.Run(context.Background()); err != nil {
		slog.Error("user service exited with error", slog.String("err", err.Error()))
		os.Exit(1)
	}
}
