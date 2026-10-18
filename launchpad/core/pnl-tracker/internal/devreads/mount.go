package devreads

import (
	"context"
	"log/slog"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
)

// Mount registers the development read surface from the service config. It is
// a no-op in production, where the journey runtime owns these paths.
//
// Inputs: RPC_URL (block anchors for watermarks), IDENTITY_SERVICE_URL +
// IDENTITY_SERVICE_TOKEN (the gateway's private-network v2 identity
// introspection; /me routes answer 503 without them) and WEBHOOK_HMAC_SECRET
// (keys the page cursors).
func Mount(ctx context.Context, r chi.Router, cfg config.Config, pool *pgxpool.Pool, st *store.Store, logger *slog.Logger) {
	if cfg.NodeEnv == "production" {
		return
	}
	chain, err := DialChain(ctx, cfg.RPCURL)
	if err != nil {
		logger.Warn("development portfolio reads have no chain anchor", slog.Any("err", err))
	}
	var identity auth.RequestIdentityAuthority
	if strings.TrimSpace(cfg.IdentityURL) != "" && len(cfg.IdentityServiceToken) >= 32 {
		identity = auth.HTTPIdentityAuthority{Endpoint: strings.TrimRight(cfg.IdentityURL, "/"), ServiceToken: cfg.IdentityServiceToken, AllowPlainHTTP: true}
	} else {
		logger.Warn("development /me portfolio reads disabled: IDENTITY_SERVICE_URL and IDENTITY_SERVICE_TOKEN (>= 32 chars) are required")
	}
	Register(r, Deps{
		Reader:              Reader{Pool: pool, Chain: chain},
		Store:               st,
		Identity:            identity,
		CursorSecret:        cfg.WebhookHMACSecret,
		RewardPerConversion: cfg.RewardPerConversion,
		Logger:              logger,
	})
}
