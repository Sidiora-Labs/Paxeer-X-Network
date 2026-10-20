// Package pnl computes realised profit and loss, the daily account profit and
// loss, the leaderboard and the account history of the perps data services
// (contracts/data-services.md section 18). All money arithmetic is exact
// math/big on canonical decimal strings, never float. Position, Trade, Fold and
// TotalPnl originate in Launch-x core/pnl-tracker/internal/pnlmath and keep the
// average-cost fold for spot swaps; perpetual profit and loss is taken from the
// realizedPnl of PositionClosed and the components of TradeSettled.
package pnl

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/bigint"
)

var (
	// ErrZeroExitPrice reports a close whose exit price is 0, which has no
	// closed base size.
	ErrZeroExitPrice = errors.New("pnl: zero exit price")
	// ErrUnknownKind reports a history kind outside the six of History.
	ErrUnknownKind = errors.New("pnl: unknown history kind")
	// ErrRange reports a history range below block 0 or with no row in it.
	ErrRange = errors.New("pnl: invalid block range")
	// ErrLimit reports a history limit outside 1 to MaxHistoryRows.
	ErrLimit = errors.New("pnl: invalid history limit")
)

// Querier reads stored facts; *sql.DB and *sql.Tx satisfy it.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// wad is the 1e18 fixed-point scale shared by price and cost basis.
var wad = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

var canonical = regexp.MustCompile(`^(0|-?[1-9][0-9]*)$`)

// Position is the accumulated spot state of an (account, token) pair. Money
// fields are decimal strings (RealizedPnlUsdl is signed; the rest are
// non-negative).
type Position struct {
	TotalUsdlSpent    string
	TotalTokensBought string
	TotalUsdlReceived string
	TotalTokensSold   string
	AvgCostBasis      string // WAD
	CurrentHoldings   string
	RealizedPnlUsdl   string // signed
	FirstBuyTs        *int64
	LastTradeTs       int64
	TradeCount        int
}

// Trade is one folded swap leg, normalised to stablecoin and token amounts (a
// buy spends stablecoin for tokens; a sell receives stablecoin for tokens).
type Trade struct {
	IsBuy       bool
	UsdlAmount  string
	TokenAmount string
	Ts          int64
}

// parse turns a decimal string into a *big.Int, treating "" as 0. A leading '-'
// is accepted (realized PnL is signed); an empty position field is the zero
// value, so freshly-seen positions fold cleanly.
func parse(name, s string) (*big.Int, error) {
	if s == "" {
		return new(big.Int), nil
	}
	if len(s) > 157 || !canonical.MatchString(s) {
		return nil, fmt.Errorf("pnl: noncanonical %s", name)
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("pnl: %s %q is not a valid integer string", name, s)
	}
	return v, nil
}

// Fold returns the spot position after applying trade to prior, with realised
// profit at average cost. It is pure (no I/O).
func Fold(prior Position, t Trade) (Position, error) {
	spent, err := parse("totalUsdlSpent", prior.TotalUsdlSpent)
	if err != nil {
		return Position{}, err
	}
	bought, err := parse("totalTokensBought", prior.TotalTokensBought)
	if err != nil {
		return Position{}, err
	}
	received, err := parse("totalUsdlReceived", prior.TotalUsdlReceived)
	if err != nil {
		return Position{}, err
	}
	sold, err := parse("totalTokensSold", prior.TotalTokensSold)
	if err != nil {
		return Position{}, err
	}
	priorAverage, err := parse("avgCostBasis", prior.AvgCostBasis)
	if err != nil {
		return Position{}, err
	}
	holdings, err := parse("currentHoldings", prior.CurrentHoldings)
	if err != nil {
		return Position{}, err
	}
	realized, err := parse("realizedPnlUsdl", prior.RealizedPnlUsdl)
	if err != nil {
		return Position{}, err
	}

	usdl, err := parse("usdlAmount", t.UsdlAmount)
	if err != nil {
		return Position{}, err
	}
	tok, err := parse("tokenAmount", t.TokenAmount)
	if err != nil {
		return Position{}, err
	}
	if spent.Sign() < 0 || bought.Sign() < 0 || received.Sign() < 0 || sold.Sign() < 0 || holdings.Sign() < 0 || priorAverage.Sign() < 0 || holdings.BitLen() > 256 || prior.TradeCount < 0 {
		return Position{}, fmt.Errorf("pnl: negative unsigned prior")
	}
	if usdl.Sign() < 0 || tok.Sign() <= 0 || usdl.BitLen() > 256 || tok.BitLen() > 256 {
		return Position{}, fmt.Errorf("pnl: trade amounts must be non-negative")
	}

	// Remaining cost is spend minus previously consumed cost, which survives a
	// complete disposal and rebuy.
	remainingCost := new(big.Int).Sub(spent, new(big.Int).Sub(received, realized))
	if remainingCost.Sign() < 0 || holdings.Sign() == 0 && remainingCost.Sign() != 0 {
		return Position{}, fmt.Errorf("pnl: corrupt remaining basis; rebuild required")
	}
	avgCost := new(big.Int)
	firstBuy := prior.FirstBuyTs

	if t.IsBuy {
		spent.Add(spent, usdl)
		bought.Add(bought, tok)
		holdings.Add(holdings, tok)
		remainingCost.Add(remainingCost, usdl)
		if firstBuy == nil {
			ts := t.Ts
			firstBuy = &ts
		}
	} else {
		if tok.Cmp(holdings) > 0 {
			return Position{}, fmt.Errorf("pnl: oversell attribution anomaly; reconcile unknown inventory")
		}
		// costOfSold = remainingCost * tokenAmount / holdings (stablecoin raw).
		costOfSold := new(big.Int)
		if holdings.Sign() > 0 {
			costOfSold.Mul(remainingCost, tok)
			costOfSold.Quo(costOfSold, holdings)
		}
		remainingCost.Sub(remainingCost, costOfSold)
		realized.Add(realized, new(big.Int).Sub(usdl, costOfSold))

		received.Add(received, usdl)
		sold.Add(sold, tok)
		holdings.Sub(holdings, tok)
	}

	if holdings.BitLen() > 256 {
		return Position{}, fmt.Errorf("pnl: inventory quantity overflow")
	}
	if holdings.Sign() > 0 {
		avgCost.Mul(remainingCost, wad)
		avgCost.Quo(avgCost, holdings)
	}
	lastTrade := prior.LastTradeTs
	if t.Ts > lastTrade {
		lastTrade = t.Ts
	}

	return Position{
		TotalUsdlSpent:    spent.String(),
		TotalTokensBought: bought.String(),
		TotalUsdlReceived: received.String(),
		TotalTokensSold:   sold.String(),
		AvgCostBasis:      avgCost.String(),
		CurrentHoldings:   holdings.String(),
		RealizedPnlUsdl:   realized.String(),
		FirstBuyTs:        firstBuy,
		LastTradeTs:       lastTrade,
		TradeCount:        prior.TradeCount + 1,
	}, nil
}

// TotalPnl returns the signed total spot profit in stablecoin raw:
// realizedPnlUsdl + (priceWad*holdings/WAD - avgCostBasis*holdings/WAD). An
// empty price is unavailable and refused for a non-empty inventory; an explicit
// zero is a verified zero mark.
func TotalPnl(realizedPnlUsdl, avgCostBasis, holdings, priceWad string) (string, error) {
	realized, err := parse("realizedPnlUsdl", realizedPnlUsdl)
	if err != nil {
		return "", err
	}
	avg, err := parse("avgCostBasis", avgCostBasis)
	if err != nil {
		return "", err
	}
	h, err := parse("currentHoldings", holdings)
	if err != nil {
		return "", err
	}
	price, err := parse("priceWad", priceWad)
	if err != nil {
		return "", err
	}

	if avg.Sign() < 0 || h.Sign() < 0 || price.Sign() < 0 {
		return "", fmt.Errorf("pnl: negative unsigned valuation")
	}
	if priceWad == "" {
		if h.Sign() == 0 {
			return realized.String(), nil
		}
		return "", fmt.Errorf("pnl: mark unavailable")
	}
	unrealized := new(big.Int)
	if price.Sign() != 0 && h.Sign() != 0 {
		unrealized.Mul(price, h)
		unrealized.Quo(unrealized, wad)
	}
	remainingCost := new(big.Int)
	if avg.Sign() != 0 && h.Sign() != 0 {
		remainingCost.Mul(avg, h)
		remainingCost.Quo(remainingCost, wad)
	}

	total := new(big.Int).Add(realized, new(big.Int).Sub(unrealized, remainingCost))
	return total.String(), nil
}

// spotSwapsQuery reads the stablecoin legs of an account in one token in
// (block, tx_index, log_index, sub) order: MarketOrderExecuted rows and
// SimpleSwap rows whose other side is the stablecoin.
// ponytail: NativeSwap legs are not folded, they carry no stablecoin side.
const spotSwapsQuery = `SELECT is_buy, usdl, tok, extract(epoch FROM block_time)::bigint FROM (
    SELECT block, tx_index, log_index, sub, block_time, is_buy,
        trunc(stablecoin_amount)::text AS usdl, trunc(token_amount)::text AS tok
    FROM perps.spot_swaps
    WHERE kind = 'market_order' AND "user" = $1 AND token = $2 AND stablecoin = $3
    UNION ALL
    SELECT block, tx_index, log_index, sub, block_time, token_out = $2,
        trunc(CASE WHEN token_out = $2 THEN amount_in ELSE amount_out END)::text,
        trunc(CASE WHEN token_out = $2 THEN amount_out ELSE amount_in END)::text
    FROM perps.spot_swaps
    WHERE kind = 'simple_swap' AND "user" = $1
        AND ((token_in = $3 AND token_out = $2) OR (token_in = $2 AND token_out = $3))
) legs ORDER BY block, tx_index, log_index, sub`

// SpotPosition folds every stored spot swap of account in token against
// stablecoin at average cost and returns the resulting position.
func SpotPosition(ctx context.Context, q Querier, account, token, stablecoin common.Address) (Position, error) {
	rows, err := q.QueryContext(ctx, spotSwapsQuery, account.Bytes(), token.Bytes(), stablecoin.Bytes())
	if err != nil {
		return Position{}, fmt.Errorf("pnl: read spot swaps: %w", err)
	}
	defer rows.Close()
	var pos Position
	for rows.Next() {
		var t Trade
		if err := rows.Scan(&t.IsBuy, &t.UsdlAmount, &t.TokenAmount, &t.Ts); err != nil {
			return Position{}, fmt.Errorf("pnl: scan spot swap: %w", err)
		}
		if pos, err = Fold(pos, t); err != nil {
			return Position{}, err
		}
	}
	if err := rows.Err(); err != nil {
		return Position{}, fmt.Errorf("pnl: read spot swaps: %w", err)
	}
	return pos, nil
}

// RealizedOnClose returns the realised profit of closing closedSizeUsd of a
// position at exitPrice against entryPrice, in the raw decimals of the event:
// closed base size (closedSizeUsd / exitPrice) x (exit - entry) x side,
// truncated toward zero. It equals the realizedPnl of PositionClosed
// (contracts/perps.md section 8).
func RealizedOnClose(isLong bool, entryPrice, exitPrice, closedSizeUsd string) (string, error) {
	entry, err := unsigned("entryPrice", entryPrice)
	if err != nil {
		return "", err
	}
	exit, err := unsigned("exitPrice", exitPrice)
	if err != nil {
		return "", err
	}
	size, err := unsigned("closedSizeUsd", closedSizeUsd)
	if err != nil {
		return "", err
	}
	if exit.Sign() == 0 {
		return "", ErrZeroExitPrice
	}
	v := new(big.Int).Mul(size, new(big.Int).Sub(exit, entry))
	v.Quo(v, exit)
	if !isLong {
		v.Neg(v)
	}
	return signedResult("realizedPnl", v)
}

// Settlement is the profit and loss part of one TradeSettled event.
type Settlement struct {
	SizeUsd        string
	GrossPnl       string // signed
	TotalFeesUsd   string
	FundingPaidUsd string // signed
}

// Daily is one user_pnl_daily row: the trading, fee and funding components
// kept apart, RealizedPnl = TradingPnl - Fees + Funding, Volume the sum of
// size_usd and Trades the count of settlements of the day.
type Daily struct {
	TradingPnl  string // signed
	Fees        string
	Funding     string // signed
	RealizedPnl string // signed
	Volume      string
	Trades      int64
}

// FoldSettlement returns the daily row after applying s to prior. Each sum is
// bounded to its 256-bit column range.
func FoldSettlement(prior Daily, s Settlement) (Daily, error) {
	if prior.Trades < 0 {
		return Daily{}, fmt.Errorf("pnl: negative trades")
	}
	trading, err := signedSum("tradingPnl", prior.TradingPnl, "grossPnl", s.GrossPnl)
	if err != nil {
		return Daily{}, err
	}
	funding, err := signedSum("funding", prior.Funding, "fundingPaidUsd", s.FundingPaidUsd)
	if err != nil {
		return Daily{}, err
	}
	fees, err := unsignedSum(prior.Fees, s.TotalFeesUsd)
	if err != nil {
		return Daily{}, fmt.Errorf("pnl: fees: %w", err)
	}
	volume, err := unsignedSum(prior.Volume, s.SizeUsd)
	if err != nil {
		return Daily{}, fmt.Errorf("pnl: volume: %w", err)
	}
	t, _ := new(big.Int).SetString(trading, 10)
	f, _ := new(big.Int).SetString(fees, 10)
	n, _ := new(big.Int).SetString(funding, 10)
	realized, err := signedResult("realizedPnl", t.Add(t.Sub(t, f), n))
	if err != nil {
		return Daily{}, err
	}
	return Daily{TradingPnl: trading, Fees: fees, Funding: funding, RealizedPnl: realized, Volume: volume, Trades: prior.Trades + 1}, nil
}

const (
	dailySettlementsQuery = `SELECT trunc(size_usd)::text, trunc(gross_pnl)::text, trunc(total_fees_usd)::text, trunc(funding_paid_usd)::text
    FROM perps.trade_settlements
    WHERE account = $1 AND block_time >= $2 AND block_time < $3
    ORDER BY block, tx_index, log_index, sub`
	dailyDeleteQuery = `DELETE FROM perps.user_pnl_daily WHERE account = $1 AND day = $2::date`
	dailyUpsertQuery = `INSERT INTO perps.user_pnl_daily (account, day, trading_pnl, fees, funding, realized_pnl, volume, trades)
    VALUES ($1, $2::date, $3, $4, $5, $6, $7, $8)
    ON CONFLICT (account, day) DO UPDATE SET trading_pnl = EXCLUDED.trading_pnl, fees = EXCLUDED.fees,
        funding = EXCLUDED.funding, realized_pnl = EXCLUDED.realized_pnl, volume = EXCLUDED.volume, trades = EXCLUDED.trades`
)

// RebuildDaily recomputes the user_pnl_daily row of account for the UTC day
// that contains day from its TradeSettled facts, and removes the row when the
// day has none.
func RebuildDaily(ctx context.Context, tx *sql.Tx, account common.Address, day time.Time) (Daily, error) {
	if tx == nil {
		return Daily{}, errors.New("pnl: transaction required")
	}
	start := day.UTC().Truncate(24 * time.Hour)
	date := start.Format(time.DateOnly)
	rows, err := tx.QueryContext(ctx, dailySettlementsQuery, account.Bytes(), start, start.Add(24*time.Hour))
	if err != nil {
		return Daily{}, fmt.Errorf("pnl: read settlements: %w", err)
	}
	d := Daily{TradingPnl: "0", Fees: "0", Funding: "0", RealizedPnl: "0", Volume: "0"}
	for rows.Next() {
		var s Settlement
		if err := rows.Scan(&s.SizeUsd, &s.GrossPnl, &s.TotalFeesUsd, &s.FundingPaidUsd); err != nil {
			return Daily{}, errors.Join(fmt.Errorf("pnl: scan settlement: %w", err), rows.Close())
		}
		if d, err = FoldSettlement(d, s); err != nil {
			return Daily{}, errors.Join(err, rows.Close())
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return Daily{}, fmt.Errorf("pnl: read settlements: %w", err)
	}
	if d.Trades == 0 {
		if _, err := tx.ExecContext(ctx, dailyDeleteQuery, account.Bytes(), date); err != nil {
			return Daily{}, fmt.Errorf("pnl: delete daily row: %w", err)
		}
		return d, nil
	}
	if _, err := tx.ExecContext(ctx, dailyUpsertQuery, account.Bytes(), date, d.TradingPnl, d.Fees, d.Funding, d.RealizedPnl, d.Volume, d.Trades); err != nil {
		return Daily{}, fmt.Errorf("pnl: write daily row: %w", err)
	}
	return d, nil
}

// Window is a leaderboard window.
type Window string

// The leaderboard windows of contracts/data-services.md section 18.
const (
	WindowAll Window = "all"
	Window30d Window = "30d"
	Window7d  Window = "7d"
)

var windows = []struct {
	name Window
	span time.Duration
}{{WindowAll, 0}, {Window30d, 30 * 24 * time.Hour}, {Window7d, 7 * 24 * time.Hour}}

// Leaderboard rebuilds perps.leaderboard, the source of the LeaderboardEntry
// fields rank, userAddress, totalRealizedPnl, totalTrades, winRate,
// avgHoldTime, openPositions and totalVolume.
type Leaderboard struct{}

const (
	leaderboardDeleteQuery = `DELETE FROM perps.leaderboard`
	// $1 window, $2 window start (exclusive, null for all), $3 as-of time.
	leaderboardInsertQuery = `WITH closes AS (
    SELECT c.account, c.realized_pnl, c.is_full_close, c.block_time,
        (SELECT o.block_time FROM perps.position_events o
         WHERE o.kind = 'opened' AND o.position_id = c.position_id
           AND (o.block, o.tx_index, o.log_index, o.sub) < (c.block, c.tx_index, c.log_index, c.sub)
         ORDER BY o.block DESC, o.tx_index DESC, o.log_index DESC, o.sub DESC LIMIT 1) AS opened_time
    FROM perps.position_events c
    WHERE c.kind = 'closed' AND c.block_time <= $3 AND ($2::timestamptz IS NULL OR c.block_time > $2)
), close_stats AS (
    SELECT account, sum(realized_pnl) AS realized_pnl,
        count(*) FILTER (WHERE realized_pnl > 0) AS wins,
        count(*) FILTER (WHERE realized_pnl < 0) AS losses,
        coalesce(floor(avg(extract(epoch FROM block_time - opened_time))
            FILTER (WHERE is_full_close AND opened_time IS NOT NULL)), 0)::bigint AS avg_hold
    FROM closes GROUP BY account
), settle_stats AS (
    SELECT account, sum(size_usd) AS volume, count(*) AS trades
    FROM perps.trade_settlements
    WHERE block_time <= $3 AND ($2::timestamptz IS NULL OR block_time > $2)
    GROUP BY account
), accounts AS (
    SELECT account FROM close_stats UNION SELECT account FROM settle_stats
), open_positions AS (
    SELECT account, count(*) AS open_count FROM perps.positions_current WHERE status = 'open' GROUP BY account
), ranked AS (
    SELECT a.account, coalesce(c.realized_pnl, 0) AS realized_pnl, coalesce(s.volume, 0) AS volume,
        coalesce(s.trades, 0) AS trades,
        CASE WHEN coalesce(c.wins, 0) + coalesce(c.losses, 0) = 0 THEN 0
             ELSE round(c.wins * 100::numeric / (c.wins + c.losses), 4) END AS win_rate,
        coalesce(c.avg_hold, 0) AS avg_hold, coalesce(p.open_count, 0) AS open_count
    FROM accounts a
    LEFT JOIN close_stats c ON c.account = a.account
    LEFT JOIN settle_stats s ON s.account = a.account
    LEFT JOIN open_positions p ON p.account = a.account
)
INSERT INTO perps.leaderboard ("window", rank, account, realized_pnl, volume_usd, trades_count, win_rate, avg_hold_time_seconds, open_positions)
SELECT $1, row_number() OVER (ORDER BY realized_pnl DESC, account), account, realized_pnl, volume, trades, win_rate, avg_hold, open_count
FROM ranked`
)

// Rebuild replaces every window of perps.leaderboard with the ranking as of
// asOf: accounts with a PositionClosed or TradeSettled in the window, ranked by
// the sum of PositionClosed realizedPnl descending, ties by lower account
// bytes. A close with realizedPnl above 0 is a win, below 0 a loss, and
// win_rate = wins / (wins + losses) x 100 rounded to four decimals; the hold
// time averages the full closes of the window from their PositionOpened, and
// openPositions counts the account's open positions.
func (Leaderboard) Rebuild(ctx context.Context, tx *sql.Tx, asOf time.Time) error {
	if tx == nil {
		return errors.New("pnl: transaction required")
	}
	if _, err := tx.ExecContext(ctx, leaderboardDeleteQuery); err != nil {
		return fmt.Errorf("pnl: clear leaderboard: %w", err)
	}
	for _, w := range windows {
		var since sql.NullTime
		if w.span > 0 {
			since = sql.NullTime{Time: asOf.Add(-w.span), Valid: true}
		}
		if _, err := tx.ExecContext(ctx, leaderboardInsertQuery, string(w.name), since, asOf); err != nil {
			return fmt.Errorf("pnl: rebuild leaderboard %s: %w", w.name, err)
		}
	}
	return nil
}

// Kind is a history row kind.
type Kind string

// The history kinds and their fact tables.
const (
	KindPosition    Kind = "position"
	KindOrder       Kind = "order"
	KindFill        Kind = "fill"
	KindFunding     Kind = "funding"
	KindLiquidation Kind = "liquidation"
	KindMargin      Kind = "margin"
)

var historyKinds = []struct {
	kind  Kind
	table string
	match string
}{
	{KindPosition, "position_events", "account = $1"},
	{KindOrder, "order_events", "account = $1"},
	{KindFill, "fills", "(account = $1 OR maker = $1)"},
	{KindFunding, "funding_payments", "account = $1"},
	{KindLiquidation, "liquidations", "account = $1"},
	{KindMargin, "collateral_events", "account = $1"},
}

const historyCommon = `ARRAY['row_id', 'block', 'block_hash', 'tx_index', 'log_index', 'sub', 'tx_hash', 'receipt_kind', 'block_time', 'emitter', 'kind', 'account', 'tx_from']`

// Entry is one account history row: its kind, the fact kind of the event, the
// fact key, the transaction hash (the synthetic receipt hash for end-block
// rows) and the argument columns as a JSON object.
type Entry struct {
	Kind        Kind
	Event       string
	Block       int64
	TxIndex     int
	LogIndex    int
	Sub         int
	TxHash      common.Hash
	ReceiptKind string
	BlockTime   time.Time
	Args        json.RawMessage
}

// MaxHistoryRows is the largest limit one History read takes.
const MaxHistoryRows = 1000

// Key is the (block, tx_index, log_index) of a history row; the rows of one
// key are its sub rows and are never split across History reads.
type Key struct {
	Block    int64
	TxIndex  int
	LogIndex int
}

// Through is the Key just above every row of block, the bound that makes a
// History read include the whole block.
func Through(block int64) Key {
	return Key{Block: block + 1, TxIndex: math.MinInt32, LogIndex: math.MinInt32}
}

func (k Key) less(o Key) bool {
	if k.Block != o.Block {
		return k.Block < o.Block
	}
	if k.TxIndex != o.TxIndex {
		return k.TxIndex < o.TxIndex
	}
	return k.LogIndex < o.LogIndex
}

// History returns the position, order, fill, funding, liquidation and margin
// rows of account from block from up to, excluding, key below, as one list
// ordered by (block, tx_index, log_index, sub) descending. It returns the first
// limit rows, extended by the remaining sub rows of the last one's key so that
// a key is never split: fewer than limit rows means the range is exhausted.
// kinds selects the row kinds; an empty kinds selects all six. A fill belongs
// to both its taker and its maker.
func History(ctx context.Context, q Querier, account common.Address, from int64, below Key, kinds []Kind, limit int) ([]Entry, error) {
	if from < 0 || !Through(from-1).less(below) {
		return nil, fmt.Errorf("%w: %d below %d:%d:%d", ErrRange, from, below.Block, below.TxIndex, below.LogIndex)
	}
	if limit < 1 || limit > MaxHistoryRows {
		return nil, fmt.Errorf("%w: %d", ErrLimit, limit)
	}
	known := map[Kind]bool{}
	for _, h := range historyKinds {
		known[h.kind] = true
	}
	want := map[Kind]bool{}
	for _, k := range kinds {
		if !known[k] {
			return nil, fmt.Errorf("%w: %q", ErrUnknownKind, k)
		}
		want[k] = true
	}
	union := func(where string) string {
		var parts []string
		for _, h := range historyKinds {
			if len(kinds) > 0 && !want[h.kind] {
				continue
			}
			parts = append(parts, fmt.Sprintf(`SELECT '%s' AS hkind, kind, block, tx_index, log_index, sub, tx_hash, receipt_kind, block_time,
        to_jsonb(t) - %s AS args FROM perps.%s t WHERE %s AND %s`, h.kind, historyCommon, h.table, h.match, where))
		}
		return strings.Join(parts, "\nUNION ALL\n")
	}
	out, err := readHistory(ctx, q, union("block >= $2 AND (block, tx_index, log_index) < ($3, $4, $5)")+
		"\nORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT $6",
		account.Bytes(), from, below.Block, below.TxIndex, below.LogIndex, limit)
	if err != nil || len(out) < limit {
		return out, err
	}
	last := out[len(out)-1]
	rest, err := readHistory(ctx, q, union("block = $2 AND tx_index = $3 AND log_index = $4 AND sub < $5")+"\nORDER BY sub DESC",
		account.Bytes(), last.Block, last.TxIndex, last.LogIndex, last.Sub)
	if err != nil {
		return nil, err
	}
	return append(out, rest...), nil
}

func readHistory(ctx context.Context, q Querier, query string, args ...any) ([]Entry, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("pnl: read history: %w", err)
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		var kind string
		var hash []byte
		var args []byte
		if err := rows.Scan(&kind, &e.Event, &e.Block, &e.TxIndex, &e.LogIndex, &e.Sub, &hash, &e.ReceiptKind, &e.BlockTime, &args); err != nil {
			return nil, fmt.Errorf("pnl: scan history: %w", err)
		}
		if len(hash) != common.HashLength {
			return nil, fmt.Errorf("pnl: stored tx hash has %d bytes", len(hash))
		}
		e.Kind = Kind(kind)
		e.TxHash = common.BytesToHash(hash)
		e.Args = args
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pnl: read history: %w", err)
	}
	return out, nil
}

func unsigned(name, s string) (*big.Int, error) {
	if err := bigint.ValidateRaw(s); err != nil {
		return nil, fmt.Errorf("pnl: %s %q: %w", name, s, err)
	}
	v, _ := new(big.Int).SetString(s, 10)
	return v, nil
}

func signed(name, s string) (*big.Int, error) {
	if err := bigint.ValidateRawInt256(s); err != nil {
		return nil, fmt.Errorf("pnl: %s %q: %w", name, s, err)
	}
	v, _ := new(big.Int).SetString(s, 10)
	return v, nil
}

func signedResult(name string, v *big.Int) (string, error) {
	s := v.String()
	if err := bigint.ValidateRawInt256(s); err != nil {
		return "", fmt.Errorf("pnl: %s: %w", name, err)
	}
	return s, nil
}

func signedSum(priorName, prior, addName, add string) (string, error) {
	if prior == "" {
		prior = "0"
	}
	a, err := signed(priorName, prior)
	if err != nil {
		return "", err
	}
	b, err := signed(addName, add)
	if err != nil {
		return "", err
	}
	return signedResult(priorName, a.Add(a, b))
}

func unsignedSum(prior, add string) (string, error) {
	if prior == "" {
		prior = "0"
	}
	return bigint.Add(prior, add)
}
