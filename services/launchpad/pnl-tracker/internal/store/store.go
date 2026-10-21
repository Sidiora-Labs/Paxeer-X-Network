// Package store is the persistence layer for core/pnl-tracker, using pgx directly
// (no sqlc codegen) over the pnl schema it owns plus schema-qualified reads of the
// indexer/stats/metadata schemas it shares the database with (invariant i2, L3).
// All money/amount fields are persisted verbatim as text (invariant i1 — no
// float) and PnL fold math goes through internal/pnlmath (math/big). Trade
// inserts are idempotent (ON CONFLICT DO NOTHING) so webhook redelivery and the
// reconciler never double-count (invariant i9); the read-modify-write of a
// position is serialised with a transaction-scoped advisory lock keyed by
// (user, pool), held on the same connection and auto-released at commit/rollback.
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/util"
)

// Store wraps a pgxpool for pnl persistence.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Store from a pgx pool.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Pool exposes the underlying pool for health checks and tests.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// lockID derives the int64 advisory-lock key from a string, byte-identical to
// the TS hashToInt64 used in the pg_advisory_lock calls.
func lockID(key string) int64 { return util.HashToInt64(key) }

// withXactLock runs fn inside a transaction that first takes the transaction-
// scoped advisory lock keyed by lockKey. The lock is held on the transaction's
// connection and released automatically at commit/rollback.
func (s *Store) withXactLock(ctx context.Context, lockKey string, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
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

// MarketRow is the cross-schema market context for a pool, read from
// stats.pool_stats (owned by core/stats-workers in the same database). All
// numeric fields are decimal strings (i1).
type MarketRow struct {
	PriceWad          string
	MarketCapUsdl     string
	PriceChange24hBps string
}

// GetMarket returns the stats.pool_stats market context for a pool, or
// (nil, nil) when the pool has no stats row yet.
func (s *Store) GetMarket(ctx context.Context, poolAddress string) (*MarketRow, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT price, market_cap, price_change_24h
		FROM stats.pool_stats WHERE pool_address = $1 LIMIT 1`, poolAddress)
	var m MarketRow
	err := row.Scan(&m.PriceWad, &m.MarketCapUsdl, &m.PriceChange24hBps)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get market: %w", err)
	}
	return &m, nil
}

// GetPoolToken resolves a pool's token address via the shared indexer.pools
// table (the Swap webhook args carry only poolAddress). Returns "" when the pool
// is not yet known to the indexer.
func (s *Store) GetPoolToken(ctx context.Context, poolAddress string) (string, error) {
	var token string
	err := s.pool.QueryRow(ctx, `
		SELECT token_address FROM indexer.pools WHERE pool_address = $1 LIMIT 1`, poolAddress).Scan(&token)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("store: get pool token: %w", err)
	}
	return token, nil
}

// TokenMeta is the cross-schema token metadata (name/symbol) read from
// metadata.token_metadata (owned by media/metadata in the same DB — i2/L3).
type TokenMeta struct {
	Symbol string
	Name   string
}

// GetTokenMeta returns a token's metadata, or (nil, nil) when none exists. NULL
// symbol/name columns are normalised to "".
func (s *Store) GetTokenMeta(ctx context.Context, tokenAddress string) (*TokenMeta, error) {
	var symbol, name *string
	err := s.pool.QueryRow(ctx, `
		SELECT symbol, name FROM metadata.token_metadata WHERE token_address = $1 LIMIT 1`, tokenAddress).
		Scan(&symbol, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get token meta: %w", err)
	}
	m := &TokenMeta{}
	if symbol != nil {
		m.Symbol = *symbol
	}
	if name != nil {
		m.Name = *name
	}
	return m, nil
}
