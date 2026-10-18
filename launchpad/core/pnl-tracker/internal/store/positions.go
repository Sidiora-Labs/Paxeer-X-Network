package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/pnlmath"
)

// PositionRow is a full pnl.user_positions row. JSON tags match the TS client
// property names exactly (camelCase) for response parity (pnl.ts UserPosition).
// realizedPnlUsdl is signed; firstBuyTs is nullable.
type PositionRow struct {
	UserAddress       string `json:"userAddress"`
	PoolAddress       string `json:"poolAddress"`
	TokenAddress      string `json:"tokenAddress"`
	TotalUsdlSpent    string `json:"totalUsdlSpent"`
	TotalTokensBought string `json:"totalTokensBought"`
	TotalUsdlReceived string `json:"totalUsdlReceived"`
	TotalTokensSold   string `json:"totalTokensSold"`
	AvgCostBasis      string `json:"avgCostBasis"`
	CurrentHoldings   string `json:"currentHoldings"`
	RealizedPnlUsdl   string `json:"realizedPnlUsdl"`
	FirstBuyTs        *int64 `json:"firstBuyTs"`
	LastTradeTs       int64  `json:"lastTradeTs"`
	TradeCount        int    `json:"tradeCount"`
}

// TradeRow is a full pnl.user_trades row (pnl.ts UserTrade).
type TradeRow struct {
	ID             string `json:"id"`
	UserAddress    string `json:"userAddress"`
	PoolAddress    string `json:"poolAddress"`
	TokenAddress   string `json:"tokenAddress"`
	IsBuy          bool   `json:"isBuy"`
	UsdlAmount     string `json:"usdlAmount"`
	TokenAmount    string `json:"tokenAmount"`
	Price          string `json:"price"`
	Fee            string `json:"fee"`
	BlockNumber    int64  `json:"blockNumber"`
	BlockTimestamp int64  `json:"blockTimestamp"`
	TxHash         string `json:"txHash"`
}

// TradeInput is the normalised swap leg the consumer/reconciler folds. The id is
// txHash-logIndex (idempotency key). UsdlAmount/TokenAmount are the USDL and
// token legs of the swap (a buy spends USDL for tokens; a sell receives USDL).
type TradeInput struct {
	ID             string
	UserAddress    string
	PoolAddress    string
	TokenAddress   string
	IsBuy          bool
	UsdlAmount     string
	TokenAmount    string
	Price          string
	Fee            string
	BlockNumber    int64
	BlockTimestamp int64
	TxHash         string
}

// FoldTrade records a trade and folds it into the (user, pool) position, all in
// one advisory-lock-guarded transaction. It returns whether the trade was newly
// recorded (false on redelivery — the position is left untouched, so the fold is
// exactly-once). Ports the TS PositionService swap fold.
func (s *Store) FoldTrade(ctx context.Context, in TradeInput) (bool, error) {
	user := strings.ToLower(in.UserAddress)
	pool := strings.ToLower(in.PoolAddress)
	token := strings.ToLower(in.TokenAddress)

	var inserted bool
	err := s.withXactLock(ctx, user+"|"+pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO pnl.user_trades (
				id, user_address, pool_address, token_address, is_buy,
				usdl_amount, token_amount, price, fee,
				block_number, block_timestamp, tx_hash
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			ON CONFLICT (id) DO NOTHING`,
			in.ID, user, pool, token, in.IsBuy,
			in.UsdlAmount, in.TokenAmount, in.Price, in.Fee,
			in.BlockNumber, in.BlockTimestamp, in.TxHash)
		if err != nil {
			return fmt.Errorf("store: insert trade: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return nil // already folded — exactly-once
		}
		inserted = true

		prior, priorToken, err := loadPosition(ctx, tx, user, pool)
		if err != nil {
			return err
		}
		next, err := pnlmath.Fold(prior, pnlmath.Trade{
			IsBuy:       in.IsBuy,
			UsdlAmount:  in.UsdlAmount,
			TokenAmount: in.TokenAmount,
			Ts:          in.BlockTimestamp,
		})
		if err != nil {
			return err
		}
		// Keep a previously-resolved token address if this trade couldn't resolve one.
		storeToken := token
		if storeToken == "" {
			storeToken = priorToken
		}
		return upsertPosition(ctx, tx, user, pool, storeToken, next)
	})
	return inserted, err
}

// loadPosition reads the current position aggregates inside the fold tx, or the
// zero position when none exists yet. priorToken carries the stored token so the
// fold can preserve it when a later trade can't resolve one.
func loadPosition(ctx context.Context, tx pgx.Tx, user, pool string) (pnlmath.Position, string, error) {
	var p pnlmath.Position
	var token string
	err := tx.QueryRow(ctx, `
		SELECT token_address, total_usdl_spent, total_tokens_bought, total_usdl_received,
		       total_tokens_sold, avg_cost_basis, current_holdings, realized_pnl_usdl,
		       first_buy_ts, last_trade_ts, trade_count
		FROM pnl.user_positions WHERE user_address = $1 AND pool_address = $2`, user, pool).
		Scan(&token, &p.TotalUsdlSpent, &p.TotalTokensBought, &p.TotalUsdlReceived,
			&p.TotalTokensSold, &p.AvgCostBasis, &p.CurrentHoldings, &p.RealizedPnlUsdl,
			&p.FirstBuyTs, &p.LastTradeTs, &p.TradeCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return pnlmath.Position{}, "", nil
	}
	if err != nil {
		return pnlmath.Position{}, "", fmt.Errorf("store: load position: %w", err)
	}
	return p, token, nil
}

// upsertPosition writes the folded aggregates back, inserting on first sight.
func upsertPosition(ctx context.Context, tx pgx.Tx, user, pool, token string, p pnlmath.Position) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO pnl.user_positions (
			user_address, pool_address, token_address,
			total_usdl_spent, total_tokens_bought, total_usdl_received, total_tokens_sold,
			avg_cost_basis, current_holdings, realized_pnl_usdl,
			first_buy_ts, last_trade_ts, trade_count
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (user_address, pool_address) DO UPDATE SET
			token_address       = EXCLUDED.token_address,
			total_usdl_spent    = EXCLUDED.total_usdl_spent,
			total_tokens_bought = EXCLUDED.total_tokens_bought,
			total_usdl_received = EXCLUDED.total_usdl_received,
			total_tokens_sold   = EXCLUDED.total_tokens_sold,
			avg_cost_basis      = EXCLUDED.avg_cost_basis,
			current_holdings    = EXCLUDED.current_holdings,
			realized_pnl_usdl   = EXCLUDED.realized_pnl_usdl,
			first_buy_ts        = EXCLUDED.first_buy_ts,
			last_trade_ts       = EXCLUDED.last_trade_ts,
			trade_count         = EXCLUDED.trade_count`,
		user, pool, token,
		p.TotalUsdlSpent, p.TotalTokensBought, p.TotalUsdlReceived, p.TotalTokensSold,
		p.AvgCostBasis, p.CurrentHoldings, p.RealizedPnlUsdl,
		p.FirstBuyTs, p.LastTradeTs, p.TradeCount)
	if err != nil {
		return fmt.Errorf("store: upsert position: %w", err)
	}
	return nil
}

const positionColumns = `
	user_address, pool_address, token_address,
	total_usdl_spent, total_tokens_bought, total_usdl_received, total_tokens_sold,
	avg_cost_basis, current_holdings, realized_pnl_usdl,
	first_buy_ts, last_trade_ts, trade_count`

func scanPosition(row pgx.Row) (*PositionRow, error) {
	var p PositionRow
	err := row.Scan(&p.UserAddress, &p.PoolAddress, &p.TokenAddress,
		&p.TotalUsdlSpent, &p.TotalTokensBought, &p.TotalUsdlReceived, &p.TotalTokensSold,
		&p.AvgCostBasis, &p.CurrentHoldings, &p.RealizedPnlUsdl,
		&p.FirstBuyTs, &p.LastTradeTs, &p.TradeCount)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetPosition returns a single (user, pool) position, or (nil, nil) when absent.
func (s *Store) GetPosition(ctx context.Context, user, pool string) (*PositionRow, error) {
	row := s.pool.QueryRow(ctx, `SELECT`+positionColumns+`
		FROM pnl.user_positions WHERE user_address = $1 AND pool_address = $2`,
		strings.ToLower(user), strings.ToLower(pool))
	p, err := scanPosition(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get position: %w", err)
	}
	return p, nil
}

// ListPositions returns every position for a user, most-recently-traded first.
func (s *Store) ListPositions(ctx context.Context, user string) ([]*PositionRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT`+positionColumns+`
		FROM pnl.user_positions WHERE user_address = $1
		ORDER BY last_trade_ts DESC, pool_address ASC`, strings.ToLower(user))
	if err != nil {
		return nil, fmt.Errorf("store: list positions: %w", err)
	}
	defer rows.Close()

	var out []*PositionRow
	for rows.Next() {
		p, err := scanPosition(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan position: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// TradeFilter narrows a user's trade history (all fields optional).
type TradeFilter struct {
	Pool   string
	From   *int64
	To     *int64
	Limit  int
	Offset int
}

// ListTrades returns a user's folded trade history, newest first, filtered by
// the optional pool/time window and paginated.
func (s *Store) ListTrades(ctx context.Context, user string, f TradeFilter) ([]TradeRow, error) {
	conds := []string{"user_address = $1"}
	args := []any{strings.ToLower(user)}
	if f.Pool != "" {
		args = append(args, strings.ToLower(f.Pool))
		conds = append(conds, fmt.Sprintf("pool_address = $%d", len(args)))
	}
	if f.From != nil {
		args = append(args, *f.From)
		conds = append(conds, fmt.Sprintf("block_timestamp >= $%d", len(args)))
	}
	if f.To != nil {
		args = append(args, *f.To)
		conds = append(conds, fmt.Sprintf("block_timestamp <= $%d", len(args)))
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	args = append(args, f.Limit)
	limitPos := len(args)
	args = append(args, f.Offset)
	offsetPos := len(args)

	q := fmt.Sprintf(`
		SELECT id, user_address, pool_address, token_address, is_buy,
		       usdl_amount, token_amount, price, fee, block_number, block_timestamp, tx_hash
		FROM pnl.user_trades WHERE %s
		ORDER BY block_timestamp DESC, id DESC
		LIMIT $%d OFFSET $%d`, strings.Join(conds, " AND "), limitPos, offsetPos)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list trades: %w", err)
	}
	defer rows.Close()

	out := make([]TradeRow, 0, f.Limit)
	for rows.Next() {
		var t TradeRow
		if err := rows.Scan(&t.ID, &t.UserAddress, &t.PoolAddress, &t.TokenAddress, &t.IsBuy,
			&t.UsdlAmount, &t.TokenAmount, &t.Price, &t.Fee, &t.BlockNumber, &t.BlockTimestamp, &t.TxHash); err != nil {
			return nil, fmt.Errorf("store: scan trade: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
