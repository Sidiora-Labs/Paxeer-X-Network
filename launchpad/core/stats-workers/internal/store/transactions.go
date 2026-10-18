package store

import (
	"context"
	"fmt"
)

// TransactionRow is a stats.pool_transactions row. JSON tags match the TS drizzle
// property names (camelCase) for response parity.
type TransactionRow struct {
	ID             string `json:"id"`
	PoolAddress    string `json:"poolAddress"`
	Sender         string `json:"sender"`
	IsBuy          bool   `json:"isBuy"`
	AmountIn       string `json:"amountIn"`
	AmountOut      string `json:"amountOut"`
	Price          string `json:"price"`
	Fee            string `json:"fee"`
	BlockTimestamp int64  `json:"blockTimestamp"`
	TxHash         string `json:"txHash"`
}

// CrossTokenSwapRow is a stats.cross_token_swaps row (camelCase JSON parity).
type CrossTokenSwapRow struct {
	ID               string `json:"id"`
	Sender           string `json:"sender"`
	TokenIn          string `json:"tokenIn"`
	TokenOut         string `json:"tokenOut"`
	PoolIn           string `json:"poolIn"`
	PoolOut          string `json:"poolOut"`
	AmountIn         string `json:"amountIn"`
	IntermediateUsdl string `json:"intermediateUsdl"`
	AmountOut        string `json:"amountOut"`
	FeeIn            string `json:"feeIn"`
	FeeOut           string `json:"feeOut"`
	BlockTimestamp   int64  `json:"blockTimestamp"`
	TxHash           string `json:"txHash"`
}

// InsertTransaction records a swap in pool_transactions, idempotently keyed on id
// (txHash-logIndex), so webhook redelivery is safe (invariant i9). Ports step 1
// of StatsSwapConsumer.processEvent.
func (s *Store) InsertTransaction(ctx context.Context, t TransactionRow) error {
	_, err := s.RecordTransaction(ctx, t)
	return err
}

// RecordTransaction is InsertTransaction reporting whether the row was newly
// inserted (false when the id already existed). On a WithTx store the row is
// visible to other sessions only once the caller commits.
func (s *Store) RecordTransaction(ctx context.Context, t TransactionRow) (bool, error) {
	tag, err := s.db.Exec(ctx, `
		INSERT INTO stats.pool_transactions
			(id, pool_address, sender, is_buy, amount_in, amount_out, price, fee, block_timestamp, tx_hash)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (id) DO NOTHING`,
		t.ID, t.PoolAddress, t.Sender, t.IsBuy, t.AmountIn, t.AmountOut, t.Price, t.Fee, t.BlockTimestamp, t.TxHash)
	if err != nil {
		return false, fmt.Errorf("store: insert transaction: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// ListTransactions returns a page of a pool's transactions, newest first,
// optionally filtered by side ("buy"/"sell"; anything else means all). Ports the
// GET /stats/:pool/transactions query.
func (s *Store) ListTransactions(ctx context.Context, poolAddress string, limit, offset int, txType string) ([]TransactionRow, error) {
	query := `
		SELECT id, pool_address, sender, is_buy, amount_in, amount_out, price, fee, block_timestamp, tx_hash
		FROM stats.pool_transactions
		WHERE pool_address = $1`
	args := []any{poolAddress}
	switch txType {
	case "buy":
		query += ` AND is_buy = true`
	case "sell":
		query += ` AND is_buy = false`
	}
	query += ` ORDER BY block_timestamp DESC LIMIT $2 OFFSET $3`
	args = append(args, limit, offset)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list transactions: %w", err)
	}
	defer rows.Close()
	return scanTransactions(rows)
}

// CreatorTransactions returns all transactions sent by the creator address
// (lower-cased by the caller), newest first. Ports the creator-activity query.
func (s *Store) CreatorTransactions(ctx context.Context, poolAddress, creatorAddressLower string) ([]TransactionRow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, pool_address, sender, is_buy, amount_in, amount_out, price, fee, block_timestamp, tx_hash
		FROM stats.pool_transactions
		WHERE pool_address = $1 AND sender = $2
		ORDER BY block_timestamp DESC`, poolAddress, creatorAddressLower)
	if err != nil {
		return nil, fmt.Errorf("store: creator transactions: %w", err)
	}
	defer rows.Close()
	return scanTransactions(rows)
}

// InsertCrossTokenSwap records a native multihop (token-for-token) swap,
// idempotently. Ports MultihopConsumer.processEvent.
func (s *Store) InsertCrossTokenSwap(ctx context.Context, c CrossTokenSwapRow) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO stats.cross_token_swaps
			(id, sender, token_in, token_out, pool_in, pool_out, amount_in, intermediate_usdl, amount_out, fee_in, fee_out, block_timestamp, tx_hash)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (id) DO NOTHING`,
		c.ID, c.Sender, c.TokenIn, c.TokenOut, c.PoolIn, c.PoolOut, c.AmountIn,
		c.IntermediateUsdl, c.AmountOut, c.FeeIn, c.FeeOut, c.BlockTimestamp, c.TxHash)
	if err != nil {
		return fmt.Errorf("store: insert cross-token swap: %w", err)
	}
	return nil
}

// ListCrossTokenSwapsByWallet returns a wallet's multihop swap history, newest
// first (wallet lower-cased by the caller).
func (s *Store) ListCrossTokenSwapsByWallet(ctx context.Context, walletLower string, limit, offset int) ([]CrossTokenSwapRow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, sender, token_in, token_out, pool_in, pool_out, amount_in, intermediate_usdl, amount_out, fee_in, fee_out, block_timestamp, tx_hash
		FROM stats.cross_token_swaps
		WHERE sender = $1
		ORDER BY block_timestamp DESC
		LIMIT $2 OFFSET $3`, walletLower, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("store: cross-token swaps by wallet: %w", err)
	}
	defer rows.Close()
	return scanCrossTokenSwaps(rows)
}

// ListCrossTokenSwapsByToken returns all multihop swaps involving a token as
// input or output, newest first (token lower-cased by the caller).
func (s *Store) ListCrossTokenSwapsByToken(ctx context.Context, tokenLower string, limit, offset int) ([]CrossTokenSwapRow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, sender, token_in, token_out, pool_in, pool_out, amount_in, intermediate_usdl, amount_out, fee_in, fee_out, block_timestamp, tx_hash
		FROM stats.cross_token_swaps
		WHERE token_in = $1 OR token_out = $1
		ORDER BY block_timestamp DESC
		LIMIT $2 OFFSET $3`, tokenLower, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("store: cross-token swaps by token: %w", err)
	}
	defer rows.Close()
	return scanCrossTokenSwaps(rows)
}

func scanTransactions(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]TransactionRow, error) {
	out := []TransactionRow{}
	for rows.Next() {
		var t TransactionRow
		if err := rows.Scan(&t.ID, &t.PoolAddress, &t.Sender, &t.IsBuy, &t.AmountIn, &t.AmountOut,
			&t.Price, &t.Fee, &t.BlockTimestamp, &t.TxHash); err != nil {
			return nil, fmt.Errorf("store: scan transaction: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func scanCrossTokenSwaps(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]CrossTokenSwapRow, error) {
	out := []CrossTokenSwapRow{}
	for rows.Next() {
		var c CrossTokenSwapRow
		if err := rows.Scan(&c.ID, &c.Sender, &c.TokenIn, &c.TokenOut, &c.PoolIn, &c.PoolOut,
			&c.AmountIn, &c.IntermediateUsdl, &c.AmountOut, &c.FeeIn, &c.FeeOut, &c.BlockTimestamp, &c.TxHash); err != nil {
			return nil, fmt.Errorf("store: scan cross-token swap: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
