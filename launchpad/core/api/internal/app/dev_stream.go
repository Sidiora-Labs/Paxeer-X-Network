package app

import (
	"context"
	"crypto/rand"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
)

// newDevelopmentStream builds the development v2 stream source (outside
// production only): the production broker.PostgresStream over the
// api_stream_dev projection, with stream tickets consumed and sessions
// checked by the gateway identity authority over the private network
// (IDENTITY_SERVICE_URL + IDENTITY_SERVICE_TOKEN, plain http admitted).
//
// A failure leaves the stream nil, so /v2/snapshots, /v2/stream and
// /v2/stream/sse keep answering 503 DEPENDENCY_UNAVAILABLE; the legacy /ws
// and /stream surfaces are unaffected either way.
func newDevelopmentStream(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, st *store.Store, rdb *goredis.Client, logger *slog.Logger) *rest.DevelopmentStream {
	key := []byte(cfg.StreamSigningKey)
	if len(key) < 32 {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			logger.Error("development stream disabled: signing key", slog.Any("err", err))
			return nil
		}
		logger.Warn("STREAM_SIGNING_KEY shorter than 32 bytes: development resume cursors are signed with a per-process key and do not survive a restart")
	}
	if cfg.IdentityURL == "" || len(cfg.IdentityServiceToken) < 32 {
		logger.Warn("IDENTITY_SERVICE_URL/IDENTITY_SERVICE_TOKEN not configured: ticketed v2 streams answer AUTH_REQUIRED; public market topics are served")
	}
	if cfg.NetworkRPCURL == "" {
		logger.Warn("NETWORK_RPC_URL not configured: the development stream projection stays idle")
	}
	authority := sharedauth.HTTPStreamAuthority{Endpoint: cfg.IdentityURL, ServiceToken: cfg.IdentityServiceToken, AllowPlainHTTP: true}
	stream, err := rest.NewDevelopmentStream(ctx, rest.DevelopmentStreamOptions{Pool: pool, Store: st, Redis: rdb, ChainRPCURL: cfg.NetworkRPCURL, SigningKey: key, Authority: authority, Logger: logger})
	if err != nil {
		logger.Error("development stream disabled", slog.Any("err", err))
		return nil
	}
	return stream
}
