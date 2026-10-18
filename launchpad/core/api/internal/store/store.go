// Package store is the read layer of core/api. Per the gateway design the
// core/* services WRITE to Postgres + Redis; this package only READS from them
// — directly, with no HTTP hop to the owning service. Production reads go
// through AccountingReadModel and the committed-table read models; Store keeps
// the cached-JSON passthrough (platform metrics) and the development-only pool
// stats batch read.
//
// All money/amount fields are read and returned as text (invariant i1 — never
// float). JSON property names match the TS/Go services byte-for-byte so the
// gateway's snapshot responses are parity-compatible with the per-service ones.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

// statsCacheTTL mirrors the TS / stats-workers `EX 10` read-through cache.
const statsCacheTTL = 10 * time.Second

// Store reads from the shared Postgres pool and Redis.
type Store struct {
	pool *pgxpool.Pool
	rdb  *goredis.Client
}

// New builds a Store from a pgx pool and a Redis client.
func New(pool *pgxpool.Pool, rdb *goredis.Client) *Store {
	return &Store{pool: pool, rdb: rdb}
}

func (s *Store) beginLegacyRead(ctx context.Context) (context.Context, context.CancelFunc, pgx.Tx, error) {
	if s == nil || s.pool == nil || ctx == nil {
		return ctx, func() {}, nil, ErrAccountingNotReady
	}
	bounded, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	tx, err := s.pool.BeginTx(bounded, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err == nil {
		_, err = tx.Exec(bounded, "SET LOCAL statement_timeout = '150ms'")
	}
	if err != nil {
		if tx != nil {
			rollbackRead(tx)
		}
		cancel()
		return ctx, func() {}, nil, err
	}
	return bounded, cancel, tx, nil
}

// statsKey is the Redis read-through key for a pool's stats (parity).
func statsKey(pool string) string { return "stats:" + pool }

// ---------------------------------------------------------------------------
// Pool stats (stats schema + Redis read-through) — JSON shape matches the
// stats-workers PoolStatsRow byte-for-byte (parity).
// ---------------------------------------------------------------------------

// PoolStatsRow is a full stats.pool_stats row. JSON tags match the TS drizzle
// property names exactly (camelCase) so the Redis cache payload and the HTTP
// responses are byte-compatible with the stats service.
type PoolStatsRow struct {
	PoolAddress          string  `json:"poolAddress"`
	TokenAddress         string  `json:"tokenAddress"`
	Price                string  `json:"price"`
	PriceChange1m        string  `json:"priceChange1m"`
	PriceChange5m        string  `json:"priceChange5m"`
	PriceChange15m       string  `json:"priceChange15m"`
	PriceChange1h        string  `json:"priceChange1h"`
	PriceChange24h       string  `json:"priceChange24h"`
	PriceChangeDollar1m  string  `json:"priceChangeDollar1m"`
	PriceChangeDollar5m  string  `json:"priceChangeDollar5m"`
	PriceChangeDollar15m string  `json:"priceChangeDollar15m"`
	PriceChangeDollar1h  string  `json:"priceChangeDollar1h"`
	PriceChangeDollar24h string  `json:"priceChangeDollar24h"`
	High24h              string  `json:"high24h"`
	Low24h               string  `json:"low24h"`
	Volume24h            string  `json:"volume24h"`
	Volume1h             string  `json:"volume1h"`
	Volume5m             string  `json:"volume5m"`
	MarketCap            string  `json:"marketCap"`
	BuyCount24h          int     `json:"buyCount24h"`
	SellCount24h         int     `json:"sellCount24h"`
	UniqueTraders24h     int     `json:"uniqueTraders24h"`
	HolderCount          int     `json:"holderCount"`
	Top10Concentration   string  `json:"top10Concentration"`
	CreatorHoldingsPct   string  `json:"creatorHoldingsPct"`
	RiskRating           int     `json:"riskRating"`
	RiskFactors          *string `json:"riskFactors"`
	CreatorAddress       *string `json:"creatorAddress"`
	CreatedAt            int64   `json:"createdAt"`
	UpdatedAt            int64   `json:"updatedAt"`
}

const poolStatsColumns = ` pool_address, token_address, price,
	price_change_1m, price_change_5m, price_change_15m, price_change_1h, price_change_24h,
	price_change_dollar_1m, price_change_dollar_5m, price_change_dollar_15m,
	price_change_dollar_1h, price_change_dollar_24h, high_24h, low_24h,
	volume_24h, volume_1h, volume_5m, market_cap,
	buy_count_24h, sell_count_24h, unique_traders_24h, holder_count,
	top10_concentration, creator_holdings_pct, risk_rating, risk_factors,
	creator_address, created_at, updated_at`

func scanPoolStats(row pgx.Row) (*PoolStatsRow, error) {
	var r PoolStatsRow
	err := row.Scan(
		&r.PoolAddress, &r.TokenAddress, &r.Price,
		&r.PriceChange1m, &r.PriceChange5m, &r.PriceChange15m, &r.PriceChange1h, &r.PriceChange24h,
		&r.PriceChangeDollar1m, &r.PriceChangeDollar5m, &r.PriceChangeDollar15m,
		&r.PriceChangeDollar1h, &r.PriceChangeDollar24h, &r.High24h, &r.Low24h,
		&r.Volume24h, &r.Volume1h, &r.Volume5m, &r.MarketCap,
		&r.BuyCount24h, &r.SellCount24h, &r.UniqueTraders24h, &r.HolderCount,
		&r.Top10Concentration, &r.CreatorHoldingsPct, &r.RiskRating, &r.RiskFactors,
		&r.CreatorAddress, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// StatsBatch returns stats JSON through one byte-bounded Redis script with a DB
// fallback for misses (parity with the stats-workers /stats/batch route). The
// result is keyed by pool address; absent pools are simply omitted.
func (s *Store) StatsBatch(ctx context.Context, pools []string) (map[string]json.RawMessage, error) {
	result := make(map[string]json.RawMessage, len(pools))
	if len(pools) == 0 {
		return result, nil
	}
	if len(pools) > 100 {
		return nil, ErrDataIntegrity
	}
	keys := make([]string, len(pools))
	for i, p := range pools {
		keys[i] = statsKey(p)
	}
	cached, cacheErr := s.publicJSONBatch(ctx, keys)
	if errors.Is(cacheErr, ErrDataIntegrity) {
		return nil, cacheErr
	}
	var missed []string
	total := 2
	for i, p := range pools {
		if cacheErr == nil && cached[i] != "" {
			v := []byte(cached[i])
			if err := ValidatePublicJSON(v); err != nil {
				return nil, err
			}
			key, _ := json.Marshal(p)
			total += len(key) + len(v) + 2
			if total > PublicJSONMaxBytes {
				return nil, ErrDataIntegrity
			}
			result[p] = v
		} else {
			missed = append(missed, p)
		}
	}
	if len(missed) == 0 {
		return result, nil
	}

	ctx, cancel, tx, err := s.beginLegacyRead(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer rollbackRead(tx)
	rows, err := tx.Query(ctx, `SELECT`+poolStatsColumns+`
		FROM stats.pool_stats WHERE pool_address = ANY($1)`, missed)
	if err != nil {
		return nil, fmt.Errorf("store: stats batch: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanPoolStats(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan stats batch: %w", err)
		}
		payload, err := json.Marshal(r)
		if err != nil {
			return nil, fmt.Errorf("store: marshal stats batch: %w", err)
		}
		key, _ := json.Marshal(r.PoolAddress)
		total += len(key) + len(payload) + 2
		if ValidatePublicJSON(payload) != nil || total > PublicJSONMaxBytes {
			return nil, ErrDataIntegrity
		}
		result[r.PoolAddress] = payload
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	if s.rdb != nil {
		cacheCtx, cacheCancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cacheCancel()
		pipe := s.rdb.WithTimeout(75 * time.Millisecond).Pipeline()
		for _, pool := range missed {
			if body, found := result[pool]; found {
				pipe.Set(cacheCtx, statsKey(pool), body, statsCacheTTL)
			}
		}
		_, _ = pipe.Exec(cacheCtx)
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Rankings (Redis ZSETs written by ranking-algo).
// ---------------------------------------------------------------------------

// RankedItem is one entry of a ranked list.
type RankedItem struct {
	PoolAddress string  `json:"poolAddress"`
	Score       float64 `json:"score"`
	Rank        int     `json:"rank"`
}

// ---------------------------------------------------------------------------
// Generic cached-JSON passthrough (platform metrics, pressure, reactions).
// ---------------------------------------------------------------------------

// CachedJSON returns the raw JSON value the services cached at key. found is
// false on a cache miss (the gateway then serves an empty/absent payload).
func (s *Store) CachedJSON(ctx context.Context, key string) (json.RawMessage, bool, error) {
	return s.publicJSON(ctx, key)
}

// Ping verifies the DB and Redis are reachable (readiness probe support).
func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return err
	}
	return s.rdb.Ping(ctx).Err()
}
