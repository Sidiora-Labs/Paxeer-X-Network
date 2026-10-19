package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/bigint"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/pnl"
)

// ErrNotProjected reports a fact row naming an order, position or spot order
// that has no projection row, or a fact row that is not stored.
var ErrNotProjected = errors.New("store: projection row missing")

// USDL is the collateral token of the perps venue (contracts/perps.md
// section 2), the token column of perps.account_margin.
var USDL = common.HexToAddress("0x85FcD13735F4309833A503EE804ea32395851479")

// usdlToUSD converts 6-decimal USDL units to 18-decimal USD
// (contracts/perps.md section 2).
const usdlToUSD = "1000000000000"

// Spot fact tables whose rows feed perps.spot_token_seen and perps.spot_orders.
const (
	spotSwapsTable      = "spot_swaps"
	spotOrderTable      = "spot_order_events"
	spotRegistryTable   = "spot_token_registry"
	endBlockReceiptKind = decode.ReceiptPerpsEndBlock
)

// Spot order kinds and their event kind prefixes, longest first.
var spotKinds = []struct{ prefix, kind string }{
	{"stop_limit_", "stop_limit"},
	{"stop_loss_", "stop_loss"},
	{"limit_", "limit"},
}

// Spot order types of the source enumeration (IPECOROrders.OrderType).
const (
	spotLimitBuy      = 0
	spotLimitSell     = 1
	spotStopLimitBuy  = 3
	spotStopLimitSell = 4
)

// Project folds the decoded fact rows of one block into the projection tables
// of schema perps (contracts/data-services.md section 2). WriteBlock calls it in
// the block transaction after the block row and the fact rows are inserted,
// with only the rows that write inserted, so a block written twice folds once.
// positions_current, orders and spot_orders follow their events in row order;
// account_margin, user_stats, user_pnl_daily and market_stats_rolling are
// recomputed from the stored facts for every key the rows touch, and
// keeper_cycles and spot_token_seen are written from the rows of the block.
func Project(ctx context.Context, tx *sql.Tx, rows []decode.Row) error {
	if len(rows) == 0 {
		return nil
	}
	first := rows[0].Common()
	var (
		margin  set[common.Address]
		stats   set[common.Address]
		daily   set[accountDay]
		markets set[string]
		cycle   keeperCycle
	)
	for _, row := range rows {
		f := row.Common()
		if f.Block != first.Block {
			return fmt.Errorf("store: Project rows span blocks %d and %d", first.Block, f.Block)
		}
		if err := projectPosition(ctx, tx, row); err != nil {
			return err
		}
		if err := projectOrder(ctx, tx, row); err != nil {
			return err
		}
		switch row.Table() {
		case spotOrderTable:
			if err := projectSpotOrder(ctx, tx, f); err != nil {
				return err
			}
			if err := projectSpotTokens(ctx, tx, spotOrderTable, f); err != nil {
				return err
			}
		case spotSwapsTable, spotRegistryTable:
			if err := projectSpotTokens(ctx, tx, row.Table(), f); err != nil {
				return err
			}
		}
		endBlock := f.ReceiptKind == endBlockReceiptKind
		cycle.seen = cycle.seen || endBlock
		switch r := row.(type) {
		case decode.CollateralDepositedRow, decode.CollateralWithdrawnRow, decode.CollateralTransferredRow,
			decode.PositionOpenedRow, decode.PositionModifiedRow, decode.ADLExecutedRow:
			margin.add(f.Account)
		case decode.PositionClosedRow, decode.PositionFundingAppliedRow, decode.FeeCollectedRow:
			margin.add(f.Account)
			stats.add(f.Account)
		case decode.LiquidationRow:
			margin.add(f.Account)
			stats.add(f.Account)
			if endBlock {
				cycle.liquidations++
			}
		case decode.TradeSettledRow:
			stats.add(f.Account)
			daily.add(accountDay{f.Account, f.BlockTime.UTC().Truncate(24 * time.Hour)})
		case decode.FillRow:
			markets.add(r.MarketID)
		case decode.OpenInterestChangedRow:
			markets.add(r.MarketID)
		case decode.MarkPriceUpdatedRow:
			if endBlock {
				cycle.markets++
			}
		case decode.OrderExecutedRow:
			if endBlock {
				cycle.orders++
			}
		}
	}
	for _, a := range margin.list {
		if err := projectMargin(ctx, tx, a, first.Block); err != nil {
			return err
		}
	}
	for _, a := range stats.list {
		if err := projectStats(ctx, tx, a, first.Block); err != nil {
			return err
		}
	}
	for _, d := range daily.list {
		if _, err := pnl.RebuildDaily(ctx, tx, d.account, d.day); err != nil {
			return fmt.Errorf("store: project daily %s %s: %w", d.account.Hex(), d.day.Format(time.DateOnly), err)
		}
	}
	for _, m := range markets.list {
		if err := projectMarketStats(ctx, tx, m, first.Block, first.BlockTime); err != nil {
			return err
		}
	}
	if cycle.seen {
		return projectKeeperCycle(ctx, tx, first.Block, first.BlockTime, cycle)
	}
	return nil
}

// set keeps the distinct keys of a block in first-seen order, so projection
// rows are written in a deterministic order.
type set[T comparable] struct {
	seen map[T]struct{}
	list []T
}

func (s *set[T]) add(v T) {
	if s.seen == nil {
		s.seen = map[T]struct{}{}
	}
	if _, ok := s.seen[v]; ok {
		return
	}
	s.seen[v] = struct{}{}
	s.list = append(s.list, v)
}

type accountDay struct {
	account common.Address
	day     time.Time
}

// keeperCycle counts the logs of the perps_endblock receipt of one block.
type keeperCycle struct {
	seen         bool
	markets      int
	orders       int
	liquidations int
}

// exec1 runs a statement that must change exactly one projection row.
func exec1(ctx context.Context, tx *sql.Tx, what, query string, args ...any) error {
	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("project %s: %w", what, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("project %s: %w", what, err)
	}
	if n != 1 {
		return fmt.Errorf("%w: %s", ErrNotProjected, what)
	}
	return nil
}

func blockArg(b uint64) (int64, error) {
	if b > 1<<63-1 {
		return 0, fmt.Errorf("store: block %d exceeds bigint", b)
	}
	return int64(b), nil
}

const (
	positionOpenSQL = `INSERT INTO perps.positions_current (position_id, account, market_id, is_long, size_usd, collateral_usd,
    collateral_amount, collateral_token, entry_price, leverage, realized_pnl, status, opened_block)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 0, 'open', $11)
ON CONFLICT (position_id) DO UPDATE SET account = EXCLUDED.account, market_id = EXCLUDED.market_id, is_long = EXCLUDED.is_long,
    size_usd = EXCLUDED.size_usd, collateral_usd = EXCLUDED.collateral_usd, collateral_amount = EXCLUDED.collateral_amount,
    collateral_token = EXCLUDED.collateral_token, entry_price = EXCLUDED.entry_price, leverage = EXCLUDED.leverage,
    status = 'open', opened_block = EXCLUDED.opened_block, closed_block = NULL, take_profit_price = NULL, stop_loss_price = NULL`
	positionModifySQL  = `UPDATE perps.positions_current SET size_usd = $2, collateral_usd = $3, collateral_amount = $4 WHERE position_id = $1`
	positionEntrySQL   = `UPDATE perps.positions_current SET entry_price = $2 WHERE position_id = $1`
	positionTpSlSQL    = `UPDATE perps.positions_current SET take_profit_price = $2, stop_loss_price = $3 WHERE position_id = $1`
	positionFundingSQL = `UPDATE perps.positions_current SET collateral_usd = $2, collateral_amount = $3 WHERE position_id = $1`
	positionSizeSQL    = `SELECT size_usd FROM perps.positions_current WHERE position_id = $1 FOR UPDATE`
	positionResizeSQL  = `UPDATE perps.positions_current SET size_usd = $2 WHERE position_id = $1`
	// A full close is a liquidation when a Liquidation of the position is in
	// the same receipt; a partial liquidation slice leaves the position open.
	positionCloseSQL = `UPDATE perps.positions_current SET realized_pnl = realized_pnl + $2::numeric,
    status = CASE WHEN NOT $3 THEN status
        WHEN EXISTS (SELECT 1 FROM perps.liquidations l WHERE l.position_id = $1 AND l.tx_hash = $5) THEN 'liquidated'
        ELSE 'closed' END,
    closed_block = CASE WHEN $3 THEN $4 ELSE closed_block END
WHERE position_id = $1`
	positionLiquidateSQL = `UPDATE perps.positions_current SET status = CASE
        WHEN status = 'closed' AND EXISTS (SELECT 1 FROM perps.position_events c WHERE c.kind = 'closed' AND c.position_id = $1
            AND c.is_full_close AND c.tx_hash = $2) THEN 'liquidated'
        ELSE status END
WHERE position_id = $1`
)

// projectPosition folds the position events of contracts/perps.md section 18
// into perps.positions_current; other rows are ignored.
func projectPosition(ctx context.Context, tx *sql.Tx, row decode.Row) error {
	switch r := row.(type) {
	case decode.PositionOpenedRow:
		collateralUSD, err := bigint.MulDiv(r.CollateralAmount, usdlToUSD, "1")
		if err != nil {
			return fmt.Errorf("project position %s collateral: %w", r.PositionID, err)
		}
		block, err := blockArg(r.Block)
		if err != nil {
			return err
		}
		return exec1(ctx, tx, "position "+r.PositionID, positionOpenSQL, r.PositionID, r.User.Bytes(), r.MarketID, r.IsLong,
			r.SizeUSD, collateralUSD, r.CollateralAmount, r.CollateralToken.Bytes(), r.EntryPrice, r.Leverage, block)
	case decode.PositionModifiedRow:
		return exec1(ctx, tx, "position "+r.PositionID, positionModifySQL, r.PositionID, r.NewSizeUSD, r.NewCollateralUSD, r.NewCollateralAmount)
	case decode.PositionEntryPriceChangedRow:
		return exec1(ctx, tx, "position "+r.PositionID, positionEntrySQL, r.PositionID, r.NewEntryPrice)
	case decode.PositionTpSlSetRow:
		return exec1(ctx, tx, "position "+r.PositionID, positionTpSlSQL, r.PositionID, r.TakeProfitPrice, r.StopLossPrice)
	case decode.PositionFundingAppliedRow:
		return exec1(ctx, tx, "position "+r.PositionID, positionFundingSQL, r.PositionID, r.NewCollateralUSD, r.NewCollateralAmount)
	case decode.ADLExecutedRow:
		var size string
		switch err := tx.QueryRowContext(ctx, positionSizeSQL, r.PositionID).Scan(&size); {
		case errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("%w: position %s", ErrNotProjected, r.PositionID)
		case err != nil:
			return fmt.Errorf("project position %s: %w", r.PositionID, err)
		}
		reduced, err := bigint.Sub(size, r.DeleveragedSizeUSD)
		if err != nil {
			return fmt.Errorf("project position %s adl: %w", r.PositionID, err)
		}
		return exec1(ctx, tx, "position "+r.PositionID, positionResizeSQL, r.PositionID, reduced)
	case decode.PositionClosedRow:
		block, err := blockArg(r.Block)
		if err != nil {
			return err
		}
		return exec1(ctx, tx, "position "+r.PositionID, positionCloseSQL, r.PositionID, r.RealizedPnl, r.IsFullClose, block, r.TxHash.Bytes())
	case decode.LiquidationRow:
		return exec1(ctx, tx, "position "+r.PositionID, positionLiquidateSQL, r.PositionID, r.TxHash.Bytes())
	}
	return nil
}

const (
	orderPlaceSQL = `INSERT INTO perps.orders (order_id, account, market_id, order_type, is_long, trigger_price, size_usd,
    remaining_size_usd, status, placed_block)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7, 'active', $8)`
	orderOptionsSQL = `UPDATE perps.orders SET time_in_force = $2, reduce_only = $3, limit_price = $4, acceptable_price = $5,
    deadline = $6, cloid = $7 WHERE order_id = $1`
	orderTriggerSQL  = `UPDATE perps.orders SET status = CASE WHEN status = 'active' THEN 'triggered' ELSE status END WHERE order_id = $1`
	orderExecutedSQL = `UPDATE perps.orders SET position_id = $2, execution_price = $3 WHERE order_id = $1`
	orderCloseSQL    = `UPDATE perps.orders SET status = CASE WHEN status IN ('active', 'triggered') THEN $2 ELSE status END,
    closed_block = CASE WHEN status IN ('active', 'triggered') THEN $3 ELSE closed_block END
WHERE order_id = $1`
	orderRemainingSQL = `SELECT remaining_size_usd FROM perps.orders WHERE order_id = $1 FOR UPDATE`
	orderFillSQL      = `UPDATE perps.orders SET remaining_size_usd = $2::numeric,
    status = CASE WHEN $2::numeric = 0 AND status IN ('active', 'triggered') THEN 'filled' ELSE status END,
    closed_block = CASE WHEN $2::numeric = 0 AND status IN ('active', 'triggered') THEN $3 ELSE closed_block END
WHERE order_id = $1`
)

// projectOrder folds the order lifecycle of contracts/data-services.md
// section 2 into perps.orders; other rows are ignored.
func projectOrder(ctx context.Context, tx *sql.Tx, row decode.Row) error {
	f := row.Common()
	block, err := blockArg(f.Block)
	if err != nil {
		return err
	}
	switch r := row.(type) {
	case decode.OrderPlacedRow:
		return exec1(ctx, tx, "order "+r.OrderID, orderPlaceSQL, r.OrderID, r.User.Bytes(), r.MarketID, int16(r.OrderType), r.IsLong,
			r.TriggerPrice, r.SizeUSD, block)
	case decode.OrderOptionsRow:
		return exec1(ctx, tx, "order "+r.OrderID, orderOptionsSQL, r.OrderID, int16(r.TimeInForce), r.ReduceOnly, r.LimitPrice,
			r.AcceptablePrice, r.Deadline, r.Cloid[:])
	case decode.OrderTriggeredRow:
		return exec1(ctx, tx, "order "+r.OrderID, orderTriggerSQL, r.OrderID)
	case decode.OrderExecutedRow:
		return exec1(ctx, tx, "order "+r.OrderID, orderExecutedSQL, r.OrderID, r.PositionID, r.ExecutionPrice)
	case decode.OrderCancelledRow:
		return exec1(ctx, tx, "order "+r.OrderID, orderCloseSQL, r.OrderID, "cancelled", block)
	case decode.OrderExpiredRow:
		return exec1(ctx, tx, "order "+r.OrderID, orderCloseSQL, r.OrderID, "expired", block)
	case decode.FillRow:
		for _, id := range []string{r.MakerOrderID, r.TakerOrderID} {
			if id == "0" {
				continue
			}
			if err := fillOrder(ctx, tx, id, r.SizeUSD, block); err != nil {
				return err
			}
		}
	}
	return nil
}

func fillOrder(ctx context.Context, tx *sql.Tx, orderID, size string, block int64) error {
	var remaining string
	switch err := tx.QueryRowContext(ctx, orderRemainingSQL, orderID).Scan(&remaining); {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%w: order %s", ErrNotProjected, orderID)
	case err != nil:
		return fmt.Errorf("project order %s: %w", orderID, err)
	}
	left, err := bigint.Sub(remaining, size)
	if err != nil {
		return fmt.Errorf("project order %s fill: %w", orderID, err)
	}
	return exec1(ctx, tx, "order "+orderID, orderFillSQL, orderID, left, block)
}

const (
	spotEventSQL = `SELECT kind, order_id, "user", order_type, amount, target_price, token, stablecoin, execution_price,
    expires_at, stop_price, limit_price, trigger_price, receipt_kind, block
FROM perps.spot_order_events WHERE tx_hash = $1 AND log_index = $2 AND sub = $3`
	spotPlaceSQL = `INSERT INTO perps.spot_orders (order_id, account, kind, order_type, side, token, stablecoin, amount,
    target_price, stop_price, limit_price, trigger_price, status, placed_block)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 'open', $13)`
	spotModifySQL   = `UPDATE perps.spot_orders SET target_price = $3, expires_at = $4 WHERE kind = $1 AND order_id = $2`
	spotActivateSQL = `UPDATE perps.spot_orders SET status = CASE WHEN status = 'open' THEN 'activated' ELSE status END
WHERE kind = $1 AND order_id = $2`
	spotCloseSQL = `UPDATE perps.spot_orders SET status = CASE WHEN status IN ('open', 'activated') THEN $3 ELSE status END,
    execution_price = CASE WHEN status IN ('open', 'activated') THEN $4 ELSE execution_price END,
    closed_block = CASE WHEN status IN ('open', 'activated') THEN $5 ELSE closed_block END
WHERE kind = $1 AND order_id = $2`
)

// projectSpotOrder folds the stored perps.spot_order_events row of f into
// perps.spot_orders keyed (kind, order_id). A stop-limit or stop-loss
// cancellation in the spot EndBlock receipt is the expiry sweep, the only
// EndBlock path that ends a resting stop order (contracts/spot.md, Expiry).
func projectSpotOrder(ctx context.Context, tx *sql.Tx, f decode.Fact) error {
	var (
		eventKind, orderID, receiptKind                          string
		user, token, stablecoin                                  []byte
		orderType                                                sql.NullInt16
		amount, target, execution, expires, stop, limit, trigger sql.NullString
		block                                                    int64
	)
	err := tx.QueryRowContext(ctx, spotEventSQL, f.TxHash.Bytes(), f.LogIndex, f.Sub).Scan(&eventKind, &orderID, &user, &orderType,
		&amount, &target, &token, &stablecoin, &execution, &expires, &stop, &limit, &trigger, &receiptKind, &block)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%w: spot order event %s/%d/%d", ErrNotProjected, f.TxHash, f.LogIndex, f.Sub)
	case err != nil:
		return fmt.Errorf("project spot order event: %w", err)
	}
	var kind, action string
	for _, k := range spotKinds {
		if rest, ok := strings.CutPrefix(eventKind, k.prefix); ok {
			kind, action = k.kind, rest
			break
		}
	}
	what := "spot order " + kind + " " + orderID
	switch action {
	case "created":
		side, err := spotSide(kind, orderType)
		if err != nil {
			return fmt.Errorf("project %s: %w", what, err)
		}
		return exec1(ctx, tx, what, spotPlaceSQL, orderID, user, kind, orderType, side, token, stablecoin, amount, target, stop,
			limit, trigger, block)
	case "modified":
		return exec1(ctx, tx, what, spotModifySQL, kind, orderID, target, expires)
	case "activated":
		return exec1(ctx, tx, what, spotActivateSQL, kind, orderID)
	case "executed":
		return exec1(ctx, tx, what, spotCloseSQL, kind, orderID, "executed", execution, block)
	case "expired":
		return exec1(ctx, tx, what, spotCloseSQL, kind, orderID, "expired", nil, block)
	case "cancelled":
		status := "cancelled"
		if kind != "limit" && receiptKind == string(decode.ReceiptSpotEndBlock) {
			status = "expired"
		}
		return exec1(ctx, tx, what, spotCloseSQL, kind, orderID, status, nil, block)
	}
	return fmt.Errorf("store: unknown spot order event kind %q", eventKind)
}

// spotSide maps the order type of a created spot order to its side: limit 0
// buy and 1 sell, stop-limit 3 buy and 4 sell, a stop-loss always sells.
func spotSide(kind string, orderType sql.NullInt16) (string, error) {
	if kind == "stop_loss" {
		return "sell", nil
	}
	if !orderType.Valid {
		return "", errors.New("order type missing")
	}
	switch {
	case kind == "limit" && orderType.Int16 == spotLimitBuy, kind == "stop_limit" && orderType.Int16 == spotStopLimitBuy:
		return "buy", nil
	case kind == "limit" && orderType.Int16 == spotLimitSell, kind == "stop_limit" && orderType.Int16 == spotStopLimitSell:
		return "sell", nil
	}
	return "", fmt.Errorf("order type %d is not a %s order", orderType.Int16, kind)
}

func spotTokensSQL(table, columns string) string {
	return `INSERT INTO perps.spot_token_seen (token, first_block)
SELECT v.token, min(s.block) FROM perps.` + table + ` s CROSS JOIN LATERAL (VALUES ` + columns + `) v(token)
WHERE s.tx_hash = $1 AND s.log_index = $2 AND s.sub = $3 AND v.token IS NOT NULL GROUP BY v.token
ON CONFLICT (token) DO UPDATE SET first_block = LEAST(perps.spot_token_seen.first_block, EXCLUDED.first_block)`
}

var spotTokenQueries = map[string]string{
	spotSwapsTable:    spotTokensSQL(spotSwapsTable, `(s.stablecoin), (s.token), (s.token_other), (s.token_in), (s.token_out)`),
	spotOrderTable:    spotTokensSQL(spotOrderTable, `(s.token), (s.stablecoin)`),
	spotRegistryTable: spotTokensSQL(spotRegistryTable, `(s.token), (s.share_token)`),
}

// projectSpotTokens records every token of the stored spot swap, order or
// registry row of f in perps.spot_token_seen.
func projectSpotTokens(ctx context.Context, tx *sql.Tx, table string, f decode.Fact) error {
	res, err := tx.ExecContext(ctx, spotTokenQueries[table], f.TxHash.Bytes(), f.LogIndex, f.Sub)
	if err != nil {
		return fmt.Errorf("project spot tokens of %s: %w", table, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("project spot tokens of %s: %w", table, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %s row %s/%d/%d", ErrNotProjected, table, f.TxHash, f.LogIndex, f.Sub)
	}
	return nil
}

const marginSQL = `INSERT INTO perps.account_margin (account, token, balance, locked, margin_mode, updated_block)
SELECT $1, $2,
    COALESCE(base.new_balance, 0) + COALESCE((SELECT sum(CASE c.kind WHEN 'transferred_in' THEN c.amount ELSE -c.amount END)
        FROM perps.collateral_events c
        WHERE c.account = $1 AND c.kind IN ('transferred_in', 'transferred_out')
            AND (base.block IS NULL OR (c.block, c.tx_index, c.log_index, c.sub) > (base.block, base.tx_index, base.log_index, base.sub))), 0),
    COALESCE((SELECT sum(p.collateral_amount) FROM perps.positions_current p WHERE p.account = $1 AND p.status = 'open'), 0),
    0, $3
FROM (SELECT 1) one
LEFT JOIN LATERAL (SELECT e.new_balance, e.block, e.tx_index, e.log_index, e.sub FROM perps.collateral_events e
    WHERE e.account = $1 AND e.kind IN ('deposited', 'withdrawn')
    ORDER BY e.block DESC, e.tx_index DESC, e.log_index DESC, e.sub DESC LIMIT 1) base ON true
ON CONFLICT (account, token) DO UPDATE SET balance = EXCLUDED.balance, locked = EXCLUDED.locked, updated_block = EXCLUDED.updated_block`

// projectMargin recomputes the perps.account_margin row of account: balance is
// the new_balance of the latest deposit or withdrawal plus or minus every later
// transfer, locked the collateral of the open positions, margin_mode 0.
func projectMargin(ctx context.Context, tx *sql.Tx, account common.Address, block uint64) error {
	b, err := blockArg(block)
	if err != nil {
		return err
	}
	return exec1(ctx, tx, "margin "+account.Hex(), marginSQL, account.Bytes(), USDL.Bytes(), b)
}

const statsSQL = `WITH s AS (SELECT count(*) AS n, COALESCE(sum(size_usd), 0) AS vol FROM perps.trade_settlements WHERE account = $1),
f AS (SELECT GREATEST(COALESCE(sum(fee_usd) FILTER (WHERE fee_type <> 4), 0) - COALESCE(sum(fee_usd) FILTER (WHERE fee_type = 4), 0), 0) AS net
    FROM perps.fees WHERE account = $1),
fu AS (SELECT COALESCE(sum(funding_payment_usd), 0) AS v FROM perps.funding_payments WHERE account = $1),
c AS (SELECT COALESCE(sum(realized_pnl), 0) AS pnl, count(*) FILTER (WHERE realized_pnl > 0) AS w,
    count(*) FILTER (WHERE realized_pnl < 0) AS l FROM perps.position_events WHERE account = $1 AND kind = 'closed'),
q AS (SELECT count(*) AS n FROM perps.liquidations WHERE account = $1)
INSERT INTO perps.user_stats (account, trades_count, volume_usd, fees_usd, funding_usd, realized_pnl, total_pnl, win_count,
    loss_count, liquidations, updated_block)
SELECT $1, s.n, s.vol, f.net, fu.v, c.pnl, c.pnl - f.net + fu.v, c.w, c.l, q.n, $2 FROM s, f, fu, c, q
ON CONFLICT (account) DO UPDATE SET trades_count = EXCLUDED.trades_count, volume_usd = EXCLUDED.volume_usd,
    fees_usd = EXCLUDED.fees_usd, funding_usd = EXCLUDED.funding_usd, realized_pnl = EXCLUDED.realized_pnl,
    total_pnl = EXCLUDED.total_pnl, win_count = EXCLUDED.win_count, loss_count = EXCLUDED.loss_count,
    liquidations = EXCLUDED.liquidations, updated_block = EXCLUDED.updated_block`

// projectStats recomputes the perps.user_stats row of account: trades and
// volume from TradeSettled, fees net of maker rebates, funding from
// PositionFundingApplied, win and loss counts from PositionClosed realized_pnl
// and total_pnl = realized_pnl - fees_usd + funding_usd.
func projectStats(ctx context.Context, tx *sql.Tx, account common.Address, block uint64) error {
	b, err := blockArg(block)
	if err != nil {
		return err
	}
	return exec1(ctx, tx, "stats "+account.Hex(), statsSQL, account.Bytes(), b)
}

const marketStatsSQL = `INSERT INTO perps.market_stats_rolling (market_id, volume_24h, volume_7d, volume_30d, trades_count,
    open_interest_long, open_interest_short, high_24h, low_24h, open_24h, last_price, updated_block)
SELECT $1,
    COALESCE((SELECT sum(size_usd) FROM perps.fills WHERE market_id = $1 AND block_time > $2::timestamptz - interval '24 hours'), 0),
    COALESCE((SELECT sum(size_usd) FROM perps.fills WHERE market_id = $1 AND block_time > $2::timestamptz - interval '7 days'), 0),
    COALESCE((SELECT sum(size_usd) FROM perps.fills WHERE market_id = $1 AND block_time > $2::timestamptz - interval '30 days'), 0),
    (SELECT count(*) FROM perps.fills WHERE market_id = $1),
    COALESCE(oi.long_oi, 0), COALESCE(oi.short_oi, 0),
    (SELECT max(price) FROM perps.fills WHERE market_id = $1 AND block_time > $2::timestamptz - interval '24 hours'),
    (SELECT min(price) FROM perps.fills WHERE market_id = $1 AND block_time > $2::timestamptz - interval '24 hours'),
    (SELECT price FROM perps.fills WHERE market_id = $1 AND block_time > $2::timestamptz - interval '24 hours'
        ORDER BY block, tx_index, log_index, sub LIMIT 1),
    (SELECT price FROM perps.fills WHERE market_id = $1 ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1),
    $3
FROM (SELECT 1) one
LEFT JOIN LATERAL (SELECT long_oi, short_oi FROM perps.open_interest WHERE market_id = $1
    ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1) oi ON true
ON CONFLICT (market_id) DO UPDATE SET volume_24h = EXCLUDED.volume_24h, volume_7d = EXCLUDED.volume_7d,
    volume_30d = EXCLUDED.volume_30d, trades_count = EXCLUDED.trades_count, open_interest_long = EXCLUDED.open_interest_long,
    open_interest_short = EXCLUDED.open_interest_short, high_24h = EXCLUDED.high_24h, low_24h = EXCLUDED.low_24h,
    open_24h = EXCLUDED.open_24h, last_price = EXCLUDED.last_price, updated_block = EXCLUDED.updated_block`

// projectMarketStats recomputes the perps.market_stats_rolling row of market
// from its fills, each Fill counted once per match, over the windows ending at
// the block time, and its latest OpenInterestChanged.
// ponytail: recomputed per touched market from the fills table, so a market
// without fills keeps its windows until its next fill; move to hourly buckets
// refreshed by the Snapshotter if the per-block scan or the staleness matters.
func projectMarketStats(ctx context.Context, tx *sql.Tx, market string, block uint64, blockTime time.Time) error {
	b, err := blockArg(block)
	if err != nil {
		return err
	}
	return exec1(ctx, tx, "market stats "+market, marketStatsSQL, market, blockTime.UTC(), b)
}

const keeperCycleSQL = `INSERT INTO perps.keeper_cycles (block, block_time, markets_updated, orders_executed, liquidations_executed,
    orders_failed, liquidations_failed)
VALUES ($1, $2, $3, $4, $5, 0, 0)
ON CONFLICT (block) DO UPDATE SET block_time = EXCLUDED.block_time, markets_updated = EXCLUDED.markets_updated,
    orders_executed = EXCLUDED.orders_executed, liquidations_executed = EXCLUDED.liquidations_executed`

// projectKeeperCycle writes the perps.keeper_cycles row of the perps_endblock
// receipt of block from its MarkPriceUpdated, OrderExecuted and Liquidation
// counts; both failed counts are 0.
func projectKeeperCycle(ctx context.Context, tx *sql.Tx, block uint64, blockTime time.Time, k keeperCycle) error {
	b, err := blockArg(block)
	if err != nil {
		return err
	}
	return exec1(ctx, tx, "keeper cycle", keeperCycleSQL, b, blockTime.UTC(), k.markets, k.orders, k.liquidations)
}

// Snapshotter writes the hourly perps.market_snapshots and
// perps.protocol_snapshots rows (contracts/data-services.md section 2).
type Snapshotter struct{}

const (
	previousBlockTimeSQL = `SELECT time FROM perps.blocks WHERE number < $1 ORDER BY number DESC LIMIT 1`
	marketSnapshotSQL    = `INSERT INTO perps.market_snapshots (market_id, hour, mark_price, index_price, funding_rate, volume_usd, trades_count)
SELECT m.market_id, $1, mp.mark_price, mp.index_price, fr.new_rate_per_second, COALESCE(v.vol, 0), v.n
FROM perps.markets m
JOIN LATERAL (SELECT mark_price, index_price FROM perps.mark_prices WHERE market_id = m.market_id AND block_time < $1
    ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1) mp ON true
LEFT JOIN LATERAL (SELECT new_rate_per_second FROM perps.funding_rates WHERE market_id = m.market_id AND block_time < $1
    ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1) fr ON true
CROSS JOIN LATERAL (SELECT sum(size_usd) AS vol, count(*) AS n FROM perps.fills WHERE market_id = m.market_id
    AND block_time >= $1::timestamptz - interval '1 hour' AND block_time < $1) v
ON CONFLICT (market_id, hour) DO NOTHING`
	protocolSnapshotSQL = `INSERT INTO perps.protocol_snapshots (hour, total_volume_usd, total_oi_usd, total_fees_usd, tvl_usd,
    insurance_total_usd, accounts, trades_count)
SELECT $1,
    (SELECT COALESCE(sum(size_usd), 0) FROM perps.fills WHERE block_time < $1),
    (SELECT COALESCE(sum(o.long_oi + o.short_oi), 0) FROM (SELECT DISTINCT ON (market_id) long_oi, short_oi FROM perps.open_interest
        WHERE block_time < $1 ORDER BY market_id, block DESC, tx_index DESC, log_index DESC, sub DESC) o),
    (SELECT GREATEST(COALESCE(sum(fee_usd) FILTER (WHERE fee_type <> 4), 0) - COALESCE(sum(fee_usd) FILTER (WHERE fee_type = 4), 0), 0)
        FROM perps.fees WHERE block_time < $1),
    (SELECT COALESCE(sum(CASE kind WHEN 'in' THEN value ELSE -value END), 0) FROM perps.usdl_transfers WHERE block_time < $1),
    COALESCE(d.new_balance, 0) + COALESCE((SELECT sum(c.amount) FROM perps.insurance_events c
        WHERE c.kind = 'contribution' AND c.block_time < $1
            AND (d.block IS NULL OR (c.block, c.tx_index, c.log_index, c.sub) > (d.block, d.tx_index, d.log_index, d.sub))), 0),
    (SELECT count(*) FROM (SELECT maker FROM perps.fills WHERE block_time < $1 UNION SELECT taker FROM perps.fills WHERE block_time < $1) a),
    (SELECT count(*) FROM perps.fills WHERE block_time < $1)
FROM (SELECT 1) one
LEFT JOIN LATERAL (SELECT new_balance, block, tx_index, log_index, sub FROM perps.insurance_events WHERE kind = 'draw' AND block_time < $1
    ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1) d ON true
ON CONFLICT (hour) DO NOTHING`
)

// Snapshot writes, for every UTC hour boundary h with previous block time < h
// <= blockTime, one market_snapshots row per market with a mark price and one
// protocol_snapshots row from the facts before h: tvl_usd is the USDL into
// minus out of the venue custody addresses, insurance_total_usd the balance of
// the latest InsuranceDraw plus the later contributions. WriteBlock calls it
// after inserting the perps.blocks row of block; the first indexed block has
// no previous block and writes nothing.
func (Snapshotter) Snapshot(ctx context.Context, tx *sql.Tx, block uint64, blockTime time.Time) error {
	b, err := blockArg(block)
	if err != nil {
		return err
	}
	var prev time.Time
	switch err := tx.QueryRowContext(ctx, previousBlockTimeSQL, b).Scan(&prev); {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("snapshot: previous block time: %w", err)
	}
	end := blockTime.UTC()
	for h := prev.UTC().Truncate(time.Hour).Add(time.Hour); !h.After(end); h = h.Add(time.Hour) {
		if _, err := tx.ExecContext(ctx, marketSnapshotSQL, h); err != nil {
			return fmt.Errorf("snapshot markets at %s: %w", h.Format(time.RFC3339), err)
		}
		if _, err := tx.ExecContext(ctx, protocolSnapshotSQL, h); err != nil {
			return fmt.Errorf("snapshot protocol at %s: %w", h.Format(time.RFC3339), err)
		}
	}
	return nil
}
