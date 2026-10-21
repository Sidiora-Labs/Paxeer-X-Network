package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"
	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

const (
	// platformCacheKey / platformCacheTTL mirror the TS CACHE_KEY + 30s TTL.
	platformCacheKey = "platform:metrics"
	platformCacheTTL = 30 * time.Second
)

// RegisterPlatform registers GET /stats/platform (always served from cache,
// falling back to a fresh compute). The background precompute + bucket-trader
// cleanup loops are owned by internal/app (see PrecomputePlatformMetrics).
func RegisterPlatform(r chi.Router, st *store.Store, rdb *goredis.Client) {
	r.Get("/stats/platform", platformMetrics(st, rdb))
}

func platformMetrics(st *store.Store, rdb *goredis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if cached, err := rdb.Get(ctx, platformCacheKey).Result(); err == nil && cached != "" {
			sharedhttp.WriteJSON(w, http.StatusOK, json.RawMessage(cached))
			return
		}
		m, err := st.PlatformMetrics(ctx, shareddb.NowSeconds())
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "platform metrics failed")
			return
		}
		if payload, err := json.Marshal(m); err == nil {
			rdb.Set(ctx, platformCacheKey, payload, platformCacheTTL)
		}
		sharedhttp.WriteJSON(w, http.StatusOK, m)
	}
}

// PrecomputePlatformMetrics computes the platform metrics and refreshes the Redis
// cache (parity with the TS background precompute job; called on an interval by
// internal/app).
func PrecomputePlatformMetrics(ctx context.Context, st *store.Store, rdb *goredis.Client) error {
	m, err := st.PlatformMetrics(ctx, shareddb.NowSeconds())
	if err != nil {
		return err
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := rdb.Set(ctx, platformCacheKey, payload, platformCacheTTL).Err(); err != nil {
		return err
	}
	// Push-first: publish the precomputed platform metrics on platform:update (a
	// global event) so the metrics widget updates without polling. Best-effort.
	_ = sharedredis.PublishJSON(ctx, rdb, constants.ChannelPlatformUpdate, payload)
	return nil
}
