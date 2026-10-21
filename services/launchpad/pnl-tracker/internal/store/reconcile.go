package store

import (
	"context"
	"fmt"
)

// Cursor is the reconciler's keyset position over indexer.swaps, ordered by
// (block_number, log_index). The fresh cursor is (0, -1) so the first scan
// includes everything.
type Cursor struct {
	LastBlock    int64
	LastLogIndex int64
	UpdatedAt    int64
}

// IndexerSwap is a row from the shared indexer.swaps table (cross-schema), joined
// to indexer.pools for the token address. It is the reconciler's input.
type IndexerSwap struct {
	ID             string
	PoolAddress    string
	TokenAddress   string
	Sender         string
	IsBuy          bool
	AmountIn       string
	AmountOut      string
	Fee            string
	Price          string
	BlockNumber    int64
	BlockTimestamp int64
	LogIndex       int64
	TxHash         string
}

// GetCursor reads the singleton reconciler cursor.
func (s *Store) GetCursor(ctx context.Context) (Cursor, error) {
	var c Cursor
	err := s.pool.QueryRow(ctx, `
		SELECT last_block, last_log_index, updated_at
		FROM pnl.reconciler_cursor WHERE id = 1`).Scan(&c.LastBlock, &c.LastLogIndex, &c.UpdatedAt)
	if err != nil {
		return Cursor{}, fmt.Errorf("store: get cursor: %w", err)
	}
	return c, nil
}

// AdvanceCursor moves the reconciler cursor forward to (block, logIndex).
func (s *Store) AdvanceCursor(ctx context.Context, block, logIndex int64, lastSwapID string, now int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE pnl.reconciler_cursor
		SET last_block = $1, last_log_index = $2, last_swap_id = $3, updated_at = $4
		WHERE id = 1`, block, logIndex, lastSwapID, now)
	if err != nil {
		return fmt.Errorf("store: advance cursor: %w", err)
	}
	return nil
}

// ListUnreconciledSwaps returns up to limit swaps after the cursor, in
// (block_number, log_index) order, joined to their token address. The keyset
// predicate guarantees forward progress even for blocks larger than one batch.
func (s *Store) ListUnreconciledSwaps(ctx context.Context, c Cursor, limit int) ([]IndexerSwap, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.pool_address, COALESCE(p.token_address, ''), s.sender, s.is_buy,
		       s.amount_in, s.amount_out, s.fee, s.price,
		       s.block_number, s.block_timestamp, s.log_index, s.tx_hash
		FROM indexer.swaps s
		LEFT JOIN indexer.pools p ON p.pool_address = s.pool_address
		WHERE (s.block_number, s.log_index) > ($1::bigint, $2::bigint)
		ORDER BY s.block_number ASC, s.log_index ASC
		LIMIT $3`, c.LastBlock, c.LastLogIndex, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list unreconciled swaps: %w", err)
	}
	defer rows.Close()

	out := make([]IndexerSwap, 0, limit)
	for rows.Next() {
		var sw IndexerSwap
		if err := rows.Scan(&sw.ID, &sw.PoolAddress, &sw.TokenAddress, &sw.Sender, &sw.IsBuy,
			&sw.AmountIn, &sw.AmountOut, &sw.Fee, &sw.Price,
			&sw.BlockNumber, &sw.BlockTimestamp, &sw.LogIndex, &sw.TxHash); err != nil {
			return nil, fmt.Errorf("store: scan swap: %w", err)
		}
		out = append(out, sw)
	}
	return out, rows.Err()
}

// IndexerHead returns the highest block recorded in indexer.swaps (0 if empty).
func (s *Store) IndexerHead(ctx context.Context) (int64, error) {
	var head int64
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(block_number), 0) FROM indexer.swaps`).Scan(&head); err != nil {
		return 0, fmt.Errorf("store: indexer head: %w", err)
	}
	return head, nil
}

// ConsumerBlock returns the highest block already folded into pnl.user_trades
// (the realtime consumer's progress; 0 if empty).
func (s *Store) ConsumerBlock(ctx context.Context) (int64, error) {
	var b int64
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(block_number), 0) FROM pnl.user_trades`).Scan(&b); err != nil {
		return 0, fmt.Errorf("store: consumer block: %w", err)
	}
	return b, nil
}
