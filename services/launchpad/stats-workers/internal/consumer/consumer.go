// Package consumer ports the @analytics_microservices/stats event consumers to
// Go: the swap consumer (transactions, rolling volume buckets, price snapshots +
// changes, pool_stats price/mcap/high-low), the market consumer (pool_stats
// bootstrap), the state consumer (price refresh), the holder tracker (balance
// deltas + debounced enrichment/risk refresh), and the multihop consumer (native
// Router cross-token swaps). All persistence goes through internal/store (real DB, advisory-lock
// serialised); money math uses the shared big.Int helpers (invariant i1).
package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

// statsCacheTTL is the pool-stats Redis cache lifetime (parity with the TS
// `EX 10` on the stats:<pool> key).
const statsCacheTTL = 10 * time.Second

// cacheKey returns the Redis key for a pool's cached stats row.
func cacheKey(poolAddress string) string { return "stats:" + poolAddress }

// cachePoolStats reads the freshest pool_stats row and caches it as JSON for 10s,
// byte-compatible with the TS JSON.stringify(row) payload. Ports
// StatsSwapConsumer.cachePoolStats (a no-op when the row is absent).
func cachePoolStats(ctx context.Context, st *store.Store, rdb *goredis.Client, poolAddress string) error {
	row, err := st.GetPoolStats(ctx, poolAddress)
	if err != nil {
		return err
	}
	if row == nil {
		return nil
	}
	payload, err := json.Marshal(row)
	if err != nil {
		return fmt.Errorf("consumer: marshal pool stats: %w", err)
	}
	if err := rdb.Set(ctx, cacheKey(poolAddress), payload, statsCacheTTL).Err(); err != nil {
		return fmt.Errorf("consumer: cache pool stats: %w", err)
	}
	// Push-first: publish the fresh snapshot on the stats:update channel so the
	// core/api broker fans it out and subscribers update in place — no polling.
	// The payload already carries poolAddress (broker routes on it). Best-effort:
	// a publish failure never fails the cache write (the durable source of truth).
	_ = sharedredis.PublishJSON(ctx, rdb, constants.ChannelStatsUpdate, payload)
	return nil
}
