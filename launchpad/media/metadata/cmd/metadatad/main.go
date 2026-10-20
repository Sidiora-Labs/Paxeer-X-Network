// Command metadatad is the media/metadata service entrypoint: token metadata
// (name/symbol/decimals/socials/desc) + logo/banner image storage on Cloudflare
// R2 with virus scanning. It delegates all wiring and lifecycle to internal/app.
// [SECTION 11]
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/app"
)

func main() {
	if err := app.Run(context.Background()); err != nil {
		app.LogStartupFailure(slog.Default(), err)
		os.Exit(1)
	}
}
