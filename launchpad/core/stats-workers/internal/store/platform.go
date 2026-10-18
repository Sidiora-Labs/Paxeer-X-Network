package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// PlatformMetrics is the aggregated platform-wide metrics payload. JSON tags
// match the TS computePlatformMetrics return object (camelCase) for parity.
type PlatformMetrics struct {
	TotalVolume24h       string `json:"totalVolume24h"`
	TotalVolume1h        string `json:"totalVolume1h"`
	TotalMarketCap       string `json:"totalMarketCap"`
	TotalFees24h         string `json:"totalFees24h"`
	TotalTransactions24h int    `json:"totalTransactions24h"`
	TotalTransactions1h  int    `json:"totalTransactions1h"`
	TotalBuys24h         int    `json:"totalBuys24h"`
	TotalSells24h        int    `json:"totalSells24h"`
	UniqueTraders24h     int    `json:"uniqueTraders24h"`
	TotalTokensLaunched  int    `json:"totalTokensLaunched"`
	NewTokens24h         int    `json:"newTokens24h"`
	CrossTokenSwaps24h   int    `json:"crossTokenSwaps24h"`
	UpdatedAt            int64  `json:"updatedAt"`
}

// PlatformMetrics computes the platform-wide aggregates. Ports
// computePlatformMetrics (the same set of independent aggregate queries). now is
// the unix-second clock; cutoffs are derived as now-86400 / now-3600.
func (s *Store) PlatformMetrics(ctx context.Context, now int64) (PlatformMetrics, error) {
	cutoff24h := now - 86400
	cutoff1h := now - 3600
	var m PlatformMetrics
	m.UpdatedAt = now

	if err := s.db.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(CAST(volume_24h AS NUMERIC)), 0)::TEXT,
			COALESCE(SUM(CAST(volume_1h  AS NUMERIC)), 0)::TEXT,
			COALESCE(SUM(CAST(market_cap AS NUMERIC)), 0)::TEXT,
			COALESCE(SUM(buy_count_24h),  0)::INT,
			COALESCE(SUM(sell_count_24h), 0)::INT,
			COUNT(*)::INT
		FROM stats.pool_stats`).
		Scan(&m.TotalVolume24h, &m.TotalVolume1h, &m.TotalMarketCap,
			&m.TotalBuys24h, &m.TotalSells24h, &m.TotalTokensLaunched); err != nil {
		return PlatformMetrics{}, fmt.Errorf("store: platform pool_stats agg: %w", err)
	}

	if err := s.db.QueryRow(ctx, `
		SELECT COUNT(DISTINCT trader)::INT FROM stats.bucket_traders WHERE bucket_start >= $1`, cutoff24h).
		Scan(&m.UniqueTraders24h); err != nil {
		return PlatformMetrics{}, fmt.Errorf("store: platform unique traders: %w", err)
	}

	if err := s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(CAST(fee AS NUMERIC)), 0)::TEXT, COUNT(*)::INT
		FROM stats.pool_transactions WHERE block_timestamp >= $1`, cutoff24h).
		Scan(&m.TotalFees24h, &m.TotalTransactions24h); err != nil {
		return PlatformMetrics{}, fmt.Errorf("store: platform fees agg: %w", err)
	}

	if err := s.db.QueryRow(ctx, `
		SELECT COUNT(*)::INT FROM stats.pool_stats WHERE created_at >= $1`, cutoff24h).
		Scan(&m.NewTokens24h); err != nil {
		return PlatformMetrics{}, fmt.Errorf("store: platform new tokens: %w", err)
	}

	if err := s.db.QueryRow(ctx, `
		SELECT COUNT(*)::INT FROM stats.cross_token_swaps WHERE block_timestamp >= $1`, cutoff24h).
		Scan(&m.CrossTokenSwaps24h); err != nil {
		return PlatformMetrics{}, fmt.Errorf("store: platform cross swaps: %w", err)
	}

	if err := s.db.QueryRow(ctx, `
		SELECT COUNT(*)::INT FROM stats.pool_transactions WHERE block_timestamp >= $1`, cutoff1h).
		Scan(&m.TotalTransactions1h); err != nil {
		return PlatformMetrics{}, fmt.Errorf("store: platform tx 1h: %w", err)
	}

	return m, nil
}

// PruneBucketTraders deletes bucket_traders rows older than cutoff (parity with
// the hourly cleanup job). Returns the number of rows removed.
func (s *Store) PruneBucketTraders(ctx context.Context, cutoff int64) (int64, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM stats.bucket_traders WHERE bucket_start < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("store: prune bucket traders: %w", err)
	}
	return tag.RowsAffected(), nil
}

// PressureStats holds the per-pool 24h core counters plus the 1h volume/count
// breakdown used by the pressure route.
type PressureStats struct {
	BuyCount24h  int
	SellCount24h int
	Volume24h    string
	Volume1h     string
	BuyVolume1h  string
	SellVolume1h string
	BuyCount1h   int
	SellCount1h  int
}

// PressureStats returns the buy/sell pressure inputs for a pool, or (zero,false)
// when the pool is unknown. Ports the GET /stats/:pool/pressure queries.
func (s *Store) PressureStats(ctx context.Context, poolAddress string, cutoff1h int64) (PressureStats, bool, error) {
	var ps PressureStats
	err := s.db.QueryRow(ctx, `
		SELECT buy_count_24h, sell_count_24h, volume_24h, volume_1h
		FROM stats.pool_stats WHERE pool_address = $1 LIMIT 1`, poolAddress).
		Scan(&ps.BuyCount24h, &ps.SellCount24h, &ps.Volume24h, &ps.Volume1h)
	if errors.Is(err, pgx.ErrNoRows) {
		return PressureStats{}, false, nil
	}
	if err != nil {
		return PressureStats{}, false, fmt.Errorf("store: pressure pool_stats: %w", err)
	}

	if err := s.db.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN is_buy = true  THEN CAST(amount_in AS NUMERIC) ELSE 0 END), 0)::TEXT,
			COALESCE(SUM(CASE WHEN is_buy = false THEN CAST(amount_in AS NUMERIC) ELSE 0 END), 0)::TEXT,
			COALESCE(SUM(CASE WHEN is_buy = true  THEN 1 ELSE 0 END), 0)::INT,
			COALESCE(SUM(CASE WHEN is_buy = false THEN 1 ELSE 0 END), 0)::INT
		FROM stats.pool_transactions
		WHERE pool_address = $1 AND block_timestamp >= $2`, poolAddress, cutoff1h).
		Scan(&ps.BuyVolume1h, &ps.SellVolume1h, &ps.BuyCount1h, &ps.SellCount1h); err != nil {
		return PressureStats{}, false, fmt.Errorf("store: pressure 1h breakdown: %w", err)
	}
	return ps, true, nil
}

// SearchResult is one row of the token search result (camelCase JSON parity).
type SearchResult struct {
	TokenAddress   string  `json:"tokenAddress"`
	PoolAddress    string  `json:"poolAddress"`
	Name           *string `json:"name"`
	Symbol         *string `json:"symbol"`
	Description    *string `json:"description"`
	CreatedBy      string  `json:"createdBy"`
	CreatedAt      int64   `json:"createdAt"`
	Price          string  `json:"price"`
	MarketCap      string  `json:"marketCap"`
	Volume24h      string  `json:"volume24h"`
	PriceChange24h string  `json:"priceChange24h"`
	HolderCount    int     `json:"holderCount"`
}

// Search runs the token search: a metadata.token_metadata INNER JOIN
// stats.pool_stats, matching token/pool address (isAddress) or name/symbol,
// ordered by 24h volume descending. Ports the GET /search query. The
// metadata schema is owned by media/metadata; this read is cross-schema against
// the SAME database (invariant i2). likePattern is the pre-escaped `%term%`.
func (s *Store) Search(ctx context.Context, isAddress bool, likePattern string, limit int) ([]SearchResult, error) {
	where := `(LOWER(m.name) LIKE $1 ESCAPE '\' OR LOWER(m.symbol) LIKE $1 ESCAPE '\')`
	if isAddress {
		where = `(LOWER(m.token_address) LIKE $1 ESCAPE '\' OR LOWER(m.pool_address) LIKE $1 ESCAPE '\')`
	}
	query := `
		SELECT m.token_address, m.pool_address, m.name, m.symbol, m.description,
		       m.created_by, m.created_at,
		       ps.price, ps.market_cap, ps.volume_24h, ps.price_change_24h, ps.holder_count
		FROM metadata.token_metadata m
		INNER JOIN stats.pool_stats ps ON m.pool_address = ps.pool_address
		WHERE ` + where + `
		ORDER BY CAST(ps.volume_24h AS NUMERIC) DESC
		LIMIT $2`

	rows, err := s.db.Query(ctx, query, likePattern, limit)
	if err != nil {
		return nil, fmt.Errorf("store: search: %w", err)
	}
	defer rows.Close()

	out := []SearchResult{}
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.TokenAddress, &r.PoolAddress, &r.Name, &r.Symbol, &r.Description,
			&r.CreatedBy, &r.CreatedAt, &r.Price, &r.MarketCap, &r.Volume24h, &r.PriceChange24h, &r.HolderCount); err != nil {
			return nil, fmt.Errorf("store: scan search result: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
