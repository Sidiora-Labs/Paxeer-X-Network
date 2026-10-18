// Package store is the persistence layer for core/trading-charts, using pgx
// directly (no sqlc codegen) over the candles schema. All money/amount fields
// are persisted verbatim as text (invariant i1 — no float). All inserts are
// idempotent (ON CONFLICT DO NOTHING) so redelivery and restart are safe
// (invariant i9).
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CandleRow is a row in candles.candles.
type CandleRow struct {
	PoolAddress     string
	Timeframe       string
	CandleStart     int64
	Open            string
	High            string
	Low             string
	Close           string
	VolumeUsdl      string
	VolumeToken     string
	BuyVolumeUsdl   string
	SellVolumeUsdl  string
	TradeCount      int
	UniqueTraders   int
	LargeTradeCount int
	LastTradeTs     int64
	SequenceNum     int64
	McapOpen        string
	McapHigh        string
	McapLow         string
	McapClose       string
}

// CursorRow is a row in candles.candle_cursors.
type CursorRow struct {
	PoolAddress     string
	Timeframe       string
	LastClose       string
	LastCandleStart int64
	LastSequenceNum int64
}

// Store wraps a pgxpool for candle persistence.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Store from a pgx pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// GetCandle fetches a single candle by PK, or returns nil if not found.
func (s *Store) GetCandle(ctx context.Context, poolAddr, timeframe string, candleStart int64) (*CandleRow, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT pool_address, timeframe, candle_start, open, high, low, close,
		       volume_usdl, volume_token, buy_volume_usdl, sell_volume_usdl,
		       trade_count, unique_traders, large_trade_count, last_trade_ts,
		       sequence_num, mcap_open, mcap_high, mcap_low, mcap_close
		FROM candles.candles
		WHERE pool_address = $1 AND timeframe = $2 AND candle_start = $3
	`, poolAddr, timeframe, candleStart)

	var c CandleRow
	err := row.Scan(
		&c.PoolAddress, &c.Timeframe, &c.CandleStart, &c.Open, &c.High, &c.Low, &c.Close,
		&c.VolumeUsdl, &c.VolumeToken, &c.BuyVolumeUsdl, &c.SellVolumeUsdl,
		&c.TradeCount, &c.UniqueTraders, &c.LargeTradeCount, &c.LastTradeTs,
		&c.SequenceNum, &c.McapOpen, &c.McapHigh, &c.McapLow, &c.McapClose,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get candle: %w", err)
	}
	return &c, nil
}

// InsertCandle inserts a new candle. ON CONFLICT DO NOTHING for idempotency.
func (s *Store) InsertCandle(ctx context.Context, c CandleRow) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO candles.candles (
			pool_address, timeframe, candle_start, open, high, low, close,
			volume_usdl, volume_token, buy_volume_usdl, sell_volume_usdl,
			trade_count, unique_traders, large_trade_count, last_trade_ts,
			sequence_num, mcap_open, mcap_high, mcap_low, mcap_close
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
		ON CONFLICT DO NOTHING
	`,
		c.PoolAddress, c.Timeframe, c.CandleStart, c.Open, c.High, c.Low, c.Close,
		c.VolumeUsdl, c.VolumeToken, c.BuyVolumeUsdl, c.SellVolumeUsdl,
		c.TradeCount, c.UniqueTraders, c.LargeTradeCount, c.LastTradeTs,
		c.SequenceNum, c.McapOpen, c.McapHigh, c.McapLow, c.McapClose,
	)
	if err != nil {
		return fmt.Errorf("store: insert candle: %w", err)
	}
	return nil
}

// UpdateCandle updates an existing candle by PK.
func (s *Store) UpdateCandle(ctx context.Context, c CandleRow) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE candles.candles SET
			high = $4, low = $5, close = $6,
			volume_usdl = $7, volume_token = $8, buy_volume_usdl = $9, sell_volume_usdl = $10,
			trade_count = $11, unique_traders = $12, large_trade_count = $13,
			last_trade_ts = $14, mcap_high = $15, mcap_low = $16, mcap_close = $17
		WHERE pool_address = $1 AND timeframe = $2 AND candle_start = $3
	`,
		c.PoolAddress, c.Timeframe, c.CandleStart,
		c.High, c.Low, c.Close,
		c.VolumeUsdl, c.VolumeToken, c.BuyVolumeUsdl, c.SellVolumeUsdl,
		c.TradeCount, c.UniqueTraders, c.LargeTradeCount,
		c.LastTradeTs, c.McapHigh, c.McapLow, c.McapClose,
	)
	if err != nil {
		return fmt.Errorf("store: update candle: %w", err)
	}
	return nil
}

// UpdateCursorClose updates last_close on the cursor when candleStart <= lastCandleStart.
func (s *Store) UpdateCursorClose(ctx context.Context, poolAddr, timeframe, closePrice string, candleStart int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE candles.candle_cursors SET last_close = $3
		WHERE pool_address = $1 AND timeframe = $2 AND last_candle_start <= $4
	`, poolAddr, timeframe, closePrice, candleStart)
	if err != nil {
		return fmt.Errorf("store: update cursor close: %w", err)
	}
	return nil
}

// GetCursor returns the cursor for a pool+timeframe, or nil if not found.
func (s *Store) GetCursor(ctx context.Context, poolAddr, timeframe string) (*CursorRow, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT pool_address, timeframe, last_close, last_candle_start, last_sequence_num
		FROM candles.candle_cursors
		WHERE pool_address = $1 AND timeframe = $2
	`, poolAddr, timeframe)

	var c CursorRow
	err := row.Scan(&c.PoolAddress, &c.Timeframe, &c.LastClose, &c.LastCandleStart, &c.LastSequenceNum)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get cursor: %w", err)
	}
	return &c, nil
}

// UpsertCursor inserts or updates a cursor row.
func (s *Store) UpsertCursor(ctx context.Context, c CursorRow) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO candles.candle_cursors (pool_address, timeframe, last_close, last_candle_start, last_sequence_num)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (pool_address, timeframe) DO UPDATE SET
			last_close = EXCLUDED.last_close,
			last_candle_start = EXCLUDED.last_candle_start,
			last_sequence_num = EXCLUDED.last_sequence_num
	`, c.PoolAddress, c.Timeframe, c.LastClose, c.LastCandleStart, c.LastSequenceNum)
	if err != nil {
		return fmt.Errorf("store: upsert cursor: %w", err)
	}
	return nil
}

// AllCursors returns every cursor row (for gap detection).
func (s *Store) AllCursors(ctx context.Context) ([]CursorRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT pool_address, timeframe, last_close, last_candle_start, last_sequence_num
		FROM candles.candle_cursors
	`)
	if err != nil {
		return nil, fmt.Errorf("store: all cursors: %w", err)
	}
	defer rows.Close()

	var out []CursorRow
	for rows.Next() {
		var c CursorRow
		if err := rows.Scan(&c.PoolAddress, &c.Timeframe, &c.LastClose, &c.LastCandleStart, &c.LastSequenceNum); err != nil {
			return nil, fmt.Errorf("store: scan cursor: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// HistoryRange returns candles ordered by sequence_num ascending within [from, to].
func (s *Store) HistoryRange(ctx context.Context, poolAddr, timeframe string, from, to int64) ([]CandleRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT pool_address, timeframe, candle_start, open, high, low, close,
		       volume_usdl, volume_token, buy_volume_usdl, sell_volume_usdl,
		       trade_count, unique_traders, large_trade_count, last_trade_ts,
		       sequence_num, mcap_open, mcap_high, mcap_low, mcap_close
		FROM candles.candles
		WHERE pool_address = $1 AND timeframe = $2 AND candle_start >= $3 AND candle_start <= $4
		ORDER BY sequence_num ASC
	`, poolAddr, timeframe, from, to)
	if err != nil {
		return nil, fmt.Errorf("store: history range: %w", err)
	}
	defer rows.Close()
	return scanCandles(rows)
}

// HistoryCountback returns the last `limit` candles up to `to`, ordered by
// sequence_num DESC, then reversed to ascending order (parity with TS countback).
func (s *Store) HistoryCountback(ctx context.Context, poolAddr, timeframe string, to int64, limit int) ([]CandleRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT pool_address, timeframe, candle_start, open, high, low, close,
		       volume_usdl, volume_token, buy_volume_usdl, sell_volume_usdl,
		       trade_count, unique_traders, large_trade_count, last_trade_ts,
		       sequence_num, mcap_open, mcap_high, mcap_low, mcap_close
		FROM candles.candles
		WHERE pool_address = $1 AND timeframe = $2 AND candle_start <= $3
		ORDER BY sequence_num DESC
		LIMIT $4
	`, poolAddr, timeframe, to, limit)
	if err != nil {
		return nil, fmt.Errorf("store: history countback: %w", err)
	}
	defer rows.Close()

	result, err := scanCandles(rows)
	if err != nil {
		return nil, err
	}
	// Reverse to ascending order.
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result, nil
}

func scanCandles(rows pgx.Rows) ([]CandleRow, error) {
	var out []CandleRow
	for rows.Next() {
		var c CandleRow
		if err := rows.Scan(
			&c.PoolAddress, &c.Timeframe, &c.CandleStart, &c.Open, &c.High, &c.Low, &c.Close,
			&c.VolumeUsdl, &c.VolumeToken, &c.BuyVolumeUsdl, &c.SellVolumeUsdl,
			&c.TradeCount, &c.UniqueTraders, &c.LargeTradeCount, &c.LastTradeTs,
			&c.SequenceNum, &c.McapOpen, &c.McapHigh, &c.McapLow, &c.McapClose,
		); err != nil {
			return nil, fmt.Errorf("store: scan candle: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
