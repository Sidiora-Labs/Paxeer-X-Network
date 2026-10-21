// Package store is the persistence layer for core/stats-workers, using pgx
// directly (no sqlc codegen) over the stats schema it owns. All money/amount
// fields are persisted verbatim as text (invariant i1 — no float) and money math
// is done with the shared big.Int helpers. Inserts of immutable rows are
// idempotent (ON CONFLICT DO NOTHING) so webhook redelivery and restart are safe
// (invariant i9). Per-pool mutations that read-modify-write (volume buckets,
// pool_stats price, holder balances) are serialised with transaction-scoped
// Postgres advisory locks (pg_advisory_xact_lock), the connection-safe analogue
// of the TS pg_advisory_lock/unlock pairs — the lock is held on the SAME
// connection as the work and auto-released at commit/rollback.
//
// WithTx binds a Store to a caller-owned transaction so several of these
// writes commit or roll back as one unit (the canonical legacy bridge projects
// a whole fill this way). Each withXactLock then opens a savepoint on that
// transaction instead of a new one; the advisory lock it takes is still
// transaction-scoped, so it is held until the caller's outer commit/rollback.
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/util"
)

// dbtx is the query surface shared by *pgxpool.Pool and pgx.Tx. Begin on a
// pgx.Tx opens a savepoint, which is what lets withXactLock nest inside a
// caller-owned transaction.
type dbtx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Store wraps a pgxpool for stats persistence. db is the pool itself, or the
// caller's transaction for a Store returned by WithTx.
type Store struct {
	pool *pgxpool.Pool
	db   dbtx
}

// New builds a Store from a pgx pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, db: pool}
}

// WithTx returns a Store whose reads and writes all run inside tx, so they see
// each other's uncommitted effects and become durable only when the caller
// commits tx. The caller owns tx (commit/rollback); a nil tx is rejected by
// returning nil so misuse fails loudly rather than silently using the pool.
func (s *Store) WithTx(tx pgx.Tx) *Store {
	if s == nil || tx == nil {
		return nil
	}
	return &Store{pool: s.pool, db: tx}
}

// Pool exposes the underlying pool for health checks and read routes that build
// bespoke aggregation queries.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// PoolStatsRow is a full stats.pool_stats row. JSON tags match the TS drizzle
// property names exactly (camelCase) so the Redis cache payload and the HTTP
// responses are byte-compatible with the TS service (parity). RiskFactors is the
// raw JSON-text column (drizzle returns it as a string, not parsed); creator
// address and risk factors are nullable.
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

// poolStatsColumns is the canonical SELECT list (order matches scanPoolStats).
const poolStatsColumns = `
	pool_address, token_address, price,
	price_change_1m, price_change_5m, price_change_15m, price_change_1h, price_change_24h,
	price_change_dollar_1m, price_change_dollar_5m, price_change_dollar_15m,
	price_change_dollar_1h, price_change_dollar_24h,
	high_24h, low_24h, volume_24h, volume_1h, volume_5m, market_cap,
	buy_count_24h, sell_count_24h, unique_traders_24h, holder_count,
	top10_concentration, creator_holdings_pct, risk_rating, risk_factors,
	creator_address, created_at, updated_at`

// scanPoolStats scans one row in poolStatsColumns order.
func scanPoolStats(row pgx.Row) (*PoolStatsRow, error) {
	var r PoolStatsRow
	err := row.Scan(
		&r.PoolAddress, &r.TokenAddress, &r.Price,
		&r.PriceChange1m, &r.PriceChange5m, &r.PriceChange15m, &r.PriceChange1h, &r.PriceChange24h,
		&r.PriceChangeDollar1m, &r.PriceChangeDollar5m, &r.PriceChangeDollar15m,
		&r.PriceChangeDollar1h, &r.PriceChangeDollar24h,
		&r.High24h, &r.Low24h, &r.Volume24h, &r.Volume1h, &r.Volume5m, &r.MarketCap,
		&r.BuyCount24h, &r.SellCount24h, &r.UniqueTraders24h, &r.HolderCount,
		&r.Top10Concentration, &r.CreatorHoldingsPct, &r.RiskRating, &r.RiskFactors,
		&r.CreatorAddress, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetPoolStats returns the stats row for a pool, or (nil, nil) when absent.
func (s *Store) GetPoolStats(ctx context.Context, poolAddress string) (*PoolStatsRow, error) {
	row := s.db.QueryRow(ctx, `SELECT`+poolStatsColumns+`
		FROM stats.pool_stats WHERE pool_address = $1 LIMIT 1`, poolAddress)
	r, err := scanPoolStats(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get pool stats: %w", err)
	}
	return r, nil
}

// GetPoolStatsBatch returns the stats rows for the given pool addresses (in
// arbitrary order). Missing pools are simply absent from the result.
func (s *Store) GetPoolStatsBatch(ctx context.Context, poolAddresses []string) ([]*PoolStatsRow, error) {
	if len(poolAddresses) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(ctx, `SELECT`+poolStatsColumns+`
		FROM stats.pool_stats WHERE pool_address = ANY($1)`, poolAddresses)
	if err != nil {
		return nil, fmt.Errorf("store: get pool stats batch: %w", err)
	}
	defer rows.Close()

	var out []*PoolStatsRow
	for rows.Next() {
		r, err := scanPoolStats(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan pool stats batch: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InitialPoolStats is the parameter set for InsertInitialPoolStats.
type InitialPoolStats struct {
	PoolAddress    string
	TokenAddress   string
	CreatorAddress *string
	Price          string
	MarketCap      string
	High24h        string
	Low24h         string
	CreatedAt      int64
	UpdatedAt      int64
}

// InsertInitialPoolStats inserts a freshly-created pool's stats row, idempotently
// (ON CONFLICT DO NOTHING). Ports MarketConsumer.processEvent. Returns whether a
// new row was inserted.
func (s *Store) InsertInitialPoolStats(ctx context.Context, p InitialPoolStats) (bool, error) {
	tag, err := s.db.Exec(ctx, `
		INSERT INTO stats.pool_stats (
			pool_address, token_address, creator_address,
			price, market_cap, high_24h, low_24h, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (pool_address) DO NOTHING`,
		p.PoolAddress, p.TokenAddress, p.CreatorAddress,
		p.Price, p.MarketCap, p.High24h, p.Low24h, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return false, fmt.Errorf("store: insert initial pool stats: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// UpdatePoolStatsPrice sets price, market cap and updated_at for an existing
// pool. Ports StateConsumer.processEvent (a no-op when the pool is unknown).
func (s *Store) UpdatePoolStatsPrice(ctx context.Context, poolAddress, price, marketCap string, now int64) error {
	_, err := s.db.Exec(ctx, `
		UPDATE stats.pool_stats
		SET price = $2, market_cap = $3, updated_at = $4
		WHERE pool_address = $1`, poolAddress, price, marketCap, now)
	if err != nil {
		return fmt.Errorf("store: update pool stats price: %w", err)
	}
	return nil
}

// lockID derives the int64 advisory-lock key from a string, byte-identical to
// the TS hashToInt64 used in the pg_advisory_lock calls.
func lockID(key string) int64 { return util.HashToInt64(key) }

// withXactLock runs fn inside a transaction (a savepoint when the Store is
// bound to a caller transaction by WithTx) that first takes the transaction-
// scoped advisory lock keyed by lockKey. The lock is held on the transaction's
// connection and released automatically at commit/rollback — the connection-safe
// equivalent of the TS pg_advisory_lock(id) ... pg_advisory_unlock(id) pair.
func (s *Store) withXactLock(ctx context.Context, lockKey string, fn func(pgx.Tx) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck // no-op after commit; rollback is best-effort cleanup

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, lockID(lockKey)); err != nil {
		return fmt.Errorf("store: advisory lock: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit tx: %w", err)
	}
	return nil
}
