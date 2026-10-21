// Package store is the read-only persistence layer for core/ranking-algo. The
// legacy reader performs no schema writes; the separate SnapshotStore owns
// durable ranking artifacts after serial migration integration. This reader reads
// the live stats.pool_stats and indexer.pools tables owned by core/stats-workers
// and core/indexer (strangler: same Postgres as the TS services, invariant i2).
// All money/amount columns are read as text (invariant i1); numeric comparisons
// and ordering are pushed into SQL via CAST(... AS NUMERIC), matching the TS
// drizzle queries (rankers/trending.ts, rankers/new-pools.ts) for parity.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/ranker"
)

// candidateColumns is the shared SELECT list for the heuristic rankers; the only
// difference between them is the WHERE clause (R-1 pre-filtering in DB).
const candidateColumns = `
	pool_address, volume_24h, volume_1h, volume_5m, market_cap, price_change_24h,
	buy_count_24h, sell_count_24h, unique_traders_24h, holder_count`

// Store wraps a pgx pool for read-only ranking queries.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Store from a pgx pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Store satisfies ranker.Source.
var _ ranker.Source = (*Store)(nil)

// TrendingCandidates returns pools with recent activity, at least one unique
// trader, and positive 24h volume (computeTrending R-1 pre-filter).
func (s *Store) TrendingCandidates(ctx context.Context, sinceUpdatedAt int64) ([]ranker.PoolStat, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT`+candidateColumns+`
		FROM stats.pool_stats
		WHERE updated_at >= $1
		  AND unique_traders_24h > 0
		  AND CAST(volume_24h AS NUMERIC) > 0
	`, sinceUpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("store: trending candidates: %w", err)
	}
	defer rows.Close()
	return scanPoolStats(rows)
}

// BreakoutCandidates returns pools with recent activity and positive 1h volume
// (computeBreakout R-1 pre-filter).
func (s *Store) BreakoutCandidates(ctx context.Context, sinceUpdatedAt int64) ([]ranker.PoolStat, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT`+candidateColumns+`
		FROM stats.pool_stats
		WHERE updated_at >= $1
		  AND CAST(volume_1h AS NUMERIC) > 0
	`, sinceUpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("store: breakout candidates: %w", err)
	}
	defer rows.Close()
	return scanPoolStats(rows)
}

// MoversCandidates returns pools with holders, positive 24h volume and positive
// market cap (computeMovers R-1 pre-filter).
func (s *Store) MoversCandidates(ctx context.Context, sinceUpdatedAt int64) ([]ranker.PoolStat, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT`+candidateColumns+`
		FROM stats.pool_stats
		WHERE updated_at >= $1
		  AND holder_count > 0
		  AND CAST(volume_24h AS NUMERIC) > 0
		  AND CAST(market_cap AS NUMERIC) > 0
	`, sinceUpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("store: movers candidates: %w", err)
	}
	defer rows.Close()
	return scanPoolStats(rows)
}

// UnusualCandidates returns pools with at least one trade in the window
// (computeUnusual R-1 pre-filter).
func (s *Store) UnusualCandidates(ctx context.Context, sinceUpdatedAt int64) ([]ranker.PoolStat, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT`+candidateColumns+`
		FROM stats.pool_stats
		WHERE updated_at >= $1
		  AND (buy_count_24h + sell_count_24h) > 0
	`, sinceUpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("store: unusual candidates: %w", err)
	}
	defer rows.Close()
	return scanPoolStats(rows)
}

// TopVolume returns up to limit pools with positive 24h volume, ordered by 24h
// volume descending in the database (computeTopVolume R-1 pre-filter + ORDER BY).
func (s *Store) TopVolume(ctx context.Context, sinceUpdatedAt int64, limit int) ([]ranker.VolumeRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT pool_address, volume_24h
		FROM stats.pool_stats
		WHERE updated_at >= $1
		  AND CAST(volume_24h AS NUMERIC) > 0
		ORDER BY CAST(volume_24h AS NUMERIC) DESC
		LIMIT $2
	`, sinceUpdatedAt, limit)
	if err != nil {
		return nil, fmt.Errorf("store: top volume: %w", err)
	}
	defer rows.Close()

	var out []ranker.VolumeRow
	for rows.Next() {
		var v ranker.VolumeRow
		if err := rows.Scan(&v.Address, &v.Volume24h); err != nil {
			return nil, fmt.Errorf("store: scan top volume: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// NewPools returns the most recently created pools, newest first (computeNew).
func (s *Store) NewPools(ctx context.Context, limit int) ([]ranker.NewPoolRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT pool_address, created_at
		FROM indexer.pools
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: new pools: %w", err)
	}
	defer rows.Close()

	var out []ranker.NewPoolRow
	for rows.Next() {
		var p ranker.NewPoolRow
		if err := rows.Scan(&p.Address, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan new pool: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanPoolStats(rows pgx.Rows) ([]ranker.PoolStat, error) {
	var out []ranker.PoolStat
	for rows.Next() {
		var p ranker.PoolStat
		if err := rows.Scan(
			&p.PoolAddress, &p.Volume24h, &p.Volume1h, &p.Volume5m, &p.MarketCap, &p.PriceChange24h,
			&p.BuyCount24h, &p.SellCount24h, &p.UniqueTraders24h, &p.HolderCount,
		); err != nil {
			return nil, fmt.Errorf("store: scan pool stat: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
