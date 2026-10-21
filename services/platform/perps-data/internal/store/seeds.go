package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/decode"
)

const (
	seedOrderSQL = `INSERT INTO perps.projection_seeds (kind, id, block, height, path, account, market_id, is_long, size_usd, order_type,
    time_in_force, order_status, reduce_only, limit_price, trigger_price, acceptable_price, size_base, filled_base, remaining_size_usd,
    status, deadline, cloid)
VALUES ('order', $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
ON CONFLICT (kind, id) DO NOTHING`
	seedPositionSQL = `INSERT INTO perps.projection_seeds (kind, id, block, height, path, account, market_id, is_long, size_usd,
    collateral_usd, collateral_amount, collateral_token, entry_price, leverage, take_profit_price, stop_loss_price)
VALUES ('position', $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
ON CONFLICT (kind, id) DO NOTHING`
	// The projection row of a seed: every column the getters do not return
	// (placed_block; opened_block and realized_pnl) is NULL.
	projectSeededOrderSQL = `INSERT INTO perps.orders (order_id, account, market_id, order_type, is_long, trigger_price, size_usd,
    limit_price, acceptable_price, time_in_force, reduce_only, deadline, cloid, remaining_size_usd, status, projected_from, projected_height)
SELECT id, account, market_id, order_type, is_long, trigger_price, size_usd, limit_price, acceptable_price, time_in_force,
    reduce_only, deadline, cloid, remaining_size_usd, status, 'state', height
FROM perps.projection_seeds WHERE kind = 'order' AND id = $1
ON CONFLICT (order_id) DO NOTHING`
	projectSeededPositionSQL = `INSERT INTO perps.positions_current (position_id, account, market_id, is_long, size_usd, collateral_usd,
    collateral_amount, collateral_token, entry_price, leverage, status, take_profit_price, stop_loss_price, projected_from, projected_height)
SELECT id, account, market_id, is_long, size_usd, collateral_usd, collateral_amount, collateral_token, entry_price, leverage, 'open',
    take_profit_price, stop_loss_price, 'state', height
FROM perps.projection_seeds WHERE kind = 'position' AND id = $1
ON CONFLICT (position_id) DO NOTHING`
)

// writeSeeds stores the projection seeds of one block; a seed of a stored
// (kind, id) is left as it is.
func writeSeeds(ctx context.Context, tx *sql.Tx, block Block, seeds []decode.Seed) error {
	for _, s := range seeds {
		if s.Block != block.Number {
			return fmt.Errorf("%w: seed of %s %s in block %d, want %d", ErrBlockMismatch, s.Kind, s.ID, s.Block, block.Number)
		}
		at, err := blockArg(s.Block)
		if err != nil {
			return err
		}
		height, err := blockArg(s.Height)
		if err != nil {
			return err
		}
		switch {
		case s.Kind == "order" && s.Order != nil:
			o := s.Order
			deadline, err := blockArg(o.Deadline)
			if err != nil {
				return fmt.Errorf("store: seed of order %s deadline: %w", s.ID, err)
			}
			_, err = tx.ExecContext(ctx, seedOrderSQL, s.ID, at, height, s.Path, s.Account.Bytes(), s.MarketID, s.IsLong, s.SizeUSD,
				int16(o.OrderType), int16(o.TimeInForce), int16(o.OrderStatus), o.ReduceOnly, o.LimitPrice, o.TriggerPrice,
				o.AcceptablePrice, o.SizeBase, o.FilledBase, o.RemainingSizeUSD, o.Status, deadline, o.Cloid[:])
			if err != nil {
				return fmt.Errorf("store: seed of order %s: %w", s.ID, err)
			}
		case s.Kind == "position" && s.Position != nil:
			p := s.Position
			_, err := tx.ExecContext(ctx, seedPositionSQL, s.ID, at, height, s.Path, s.Account.Bytes(), s.MarketID, s.IsLong, s.SizeUSD,
				p.CollateralUSD, p.CollateralAmount, p.CollateralToken.Bytes(), p.EntryPrice, p.Leverage, p.TakeProfitPrice, p.StopLossPrice)
			if err != nil {
				return fmt.Errorf("store: seed of position %s: %w", s.ID, err)
			}
		default:
			return fmt.Errorf("store: seed of %s %s carries no %s record", s.Kind, s.ID, s.Kind)
		}
	}
	return nil
}

// projectSeed inserts the projection row of the stored seed of what ("order
// <id>" or "position <id>") and reports whether it did; any other entity, or
// an id without a seed, has none.
func projectSeed(ctx context.Context, tx *sql.Tx, what string) (bool, error) {
	kind, id, _ := strings.Cut(what, " ")
	var q string
	switch kind {
	case "order":
		q = projectSeededOrderSQL
	case "position":
		q = projectSeededPositionSQL
	default:
		return false, nil
	}
	res, err := tx.ExecContext(ctx, q, id)
	if err != nil {
		return false, fmt.Errorf("project %s from its seed: %w", what, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("project %s from its seed: %w", what, err)
	}
	return n == 1, nil
}
