// Package pnlcache holds the Redis key conventions and invalidation helper shared
// by the read API (read-through caching) and the swap consumer (cache busting on
// fold). Payloads are JSON, identical in shape to the HTTP responses, so a cache
// hit is byte-compatible with a cache miss (parity with the TS service caching).
package pnlcache

import (
	"context"
	"strings"

	goredis "github.com/redis/go-redis/v9"
)

// TTLs for the read-through caches.
const (
	// PositionsTTL / PortfolioTTL bound the freshness of a user's cached reads.
	// Short windows keep PnL near-live while still shedding read load.
	PositionsTTL = 10 // seconds
	PortfolioTTL = 10 // seconds
	// CardTTL is long: a minted card snapshot is immutable.
	CardTTL = 300 // seconds
)

// KeyPositions returns the cache key for a user's positions list.
func KeyPositions(user string) string { return "pnl:positions:" + strings.ToLower(user) }

// KeyPortfolio returns the cache key for a user's enriched portfolio.
func KeyPortfolio(user string) string { return "pnl:portfolio:" + strings.ToLower(user) }

// KeyCard returns the cache key for a minted card by id.
func KeyCard(cardID string) string { return "pnl:card:" + cardID }

// KeyOG returns the cache key for a rendered OG image (PNG bytes) by card id.
func KeyOG(cardID string) string { return "pnl:og:" + cardID }

// OGTTL bounds how long a rendered OG image is cached (the snapshot is immutable).
const OGTTL = 86400 // seconds

// InvalidateUser drops a user's cached positions + portfolio so the next read
// recomputes after a fold. Errors are returned for the caller to log; a stale
// cache is bounded by the TTL regardless.
func InvalidateUser(ctx context.Context, rdb *goredis.Client, user string) error {
	if rdb == nil {
		return nil
	}
	return rdb.Del(ctx, KeyPositions(user), KeyPortfolio(user)).Err()
}
