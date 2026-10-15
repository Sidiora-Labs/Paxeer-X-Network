// Package feeds builds the symbol, markets and chain topics of the shared
// WebSocket (contracts/data-services.md sections 8, 10, 11 and 16): the
// perpetual and spot books, the trade tapes, the stats and market records and
// the chain items, as snapshots for a subscribe and as live frames.
package feeds

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/crc32"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/bigint"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/decode"
)

const (
	// SnapshotLevels bounds each side of a book snapshot.
	SnapshotLevels = 100
	// LiveLevels bounds each side of a live book frame and of the checksum.
	LiveLevels = 20
)

var (
	// usdScale is 10^18: perps prices and USD sizes are 18-decimal fixed
	// point (contracts/perps.md section 2), as are spot target prices.
	usdScale = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	// spotScale is 10^6: USDL, PAX and SID amounts carry 6 decimals
	// (contracts/spot.md, Token model).
	spotScale = big.NewInt(1_000_000)
)

// Level is one book level, written as [price, size].
type Level [2]float64

// rowKey orders fact rows as Perps-Seq does: (block, tx_index, log_index, sub).
type rowKey [4]int64

func keyOf(f decode.Fact) (rowKey, error) {
	if f.Block > 1<<63-1 {
		return rowKey{}, fmt.Errorf("feeds: block %d out of range", f.Block)
	}
	return rowKey{int64(f.Block), f.TxIndex, f.LogIndex, f.Sub}, nil
}

func (k rowKey) compare(o rowKey) int { return slices.Compare(k[:], o[:]) }

type bookOrder struct {
	bid       bool
	limit     *big.Int
	remaining *big.Int
}

// Book is the perpetual book of one market: the perps.orders rows with
// status active, bids from is_long orders and asks from the others. Levels
// count only orders with a non-zero limit_price and remaining_size_usd above
// 0, each level summing remaining_size_usd at its limit_price
// (contracts/data-services.md section 10).
type Book struct {
	market string
	orders map[string]*bookOrder
	// applied is the key of the last fact row folded into the book; rows at
	// or below it are already part of the book.
	applied rowKey
}

// NewBook returns the empty book of market, a decimal market id, holding
// every row after applied.
func NewBook(market string, applied rowKey) *Book {
	return &Book{market: market, orders: map[string]*bookOrder{}, applied: applied}
}

const (
	bookOrdersSQL = `SELECT order_id::text, is_long, limit_price::text, remaining_size_usd::text
	FROM perps.orders WHERE market_id = $1 AND status = 'active'`
	headSQL = `SELECT height FROM perps.cursors WHERE name = 'head'`
)

// LoadBook reads the active orders of market and the head cursor in one
// repeatable-read transaction, so the book holds every fact row up to the
// head and Apply folds only the rows after it.
func LoadBook(ctx context.Context, db *sql.DB, market string) (*Book, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	head := int64(-1)
	if err := tx.QueryRowContext(ctx, headSQL).Scan(&head); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	b := NewBook(market, rowKey{head, 1<<63 - 1, 1<<63 - 1, 1<<63 - 1})
	rows, err := tx.QueryContext(ctx, bookOrdersSQL, market)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id, remaining string
			long          bool
			limit         sql.NullString
		)
		if err := rows.Scan(&id, &long, &limit, &remaining); err != nil {
			return nil, err
		}
		o := &bookOrder{bid: long}
		if o.remaining, err = parseRaw(remaining); err != nil {
			return nil, err
		}
		if limit.Valid {
			if o.limit, err = parseRaw(limit.String); err != nil {
				return nil, err
			}
		}
		b.orders[id] = o
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return b, tx.Commit()
}

// Apply folds one fact row into the book as the perps.orders projection
// does: OrderPlaced adds an active order of the market, OrderOptions sets
// its limit price, Fill lowers the remaining size of its maker and taker
// orders, and OrderCancelled, OrderExpired and OrderTriggered end the active
// order. A row at or below the last applied key is skipped, so a redelivered
// message changes nothing; rows of other tables are ignored.
func (b *Book) Apply(row decode.Row) error {
	key, err := keyOf(row.Common())
	if err != nil {
		return err
	}
	if key.compare(b.applied) <= 0 {
		return nil
	}
	switch r := row.(type) {
	case decode.OrderPlacedRow:
		if r.MarketID == b.market {
			size, err := parseRaw(r.SizeUSD)
			if err != nil {
				return err
			}
			b.orders[r.OrderID] = &bookOrder{bid: r.IsLong, remaining: size}
		}
	case decode.OrderOptionsRow:
		if o, ok := b.orders[r.OrderID]; ok {
			if o.limit, err = parseRaw(r.LimitPrice); err != nil {
				return err
			}
		}
	case decode.FillRow:
		if r.MarketID == b.market {
			for _, id := range []string{r.MakerOrderID, r.TakerOrderID} {
				if err := b.fill(id, r.SizeUSD); err != nil {
					return err
				}
			}
		}
	case decode.OrderCancelledRow:
		delete(b.orders, r.OrderID)
	case decode.OrderExpiredRow:
		delete(b.orders, r.OrderID)
	case decode.OrderTriggeredRow:
		delete(b.orders, r.OrderID)
	}
	b.applied = key
	return nil
}

func (b *Book) fill(id, size string) error {
	o, ok := b.orders[id]
	if !ok {
		return nil
	}
	left, err := bigint.Sub(o.remaining.String(), size)
	if err != nil {
		return fmt.Errorf("feeds: fill of order %s: %w", id, err)
	}
	if left == "0" {
		delete(b.orders, id)
		return nil
	}
	o.remaining, err = parseRaw(left)
	return err
}

// Levels returns up to depth levels per side, asks ascending and bids
// descending. Prices are limit_price over 10^18 and sizes are in contracts,
// the summed remaining_size_usd over the limit price.
func (b *Book) Levels(depth int) (asks, bids []Level) {
	return b.sides(depth, func(price, size *big.Int) float64 {
		f, _ := new(big.Rat).SetFrac(size, price).Float64()
		return f
	})
}

// NotionalLevels returns the levels of Levels with sizes the summed
// remaining_size_usd over 10^18.
func (b *Book) NotionalLevels(depth int) (asks, bids []Level) {
	return b.sides(depth, func(_, size *big.Int) float64 {
		f, _ := new(big.Rat).SetFrac(size, usdScale).Float64()
		return f
	})
}

func (b *Book) sides(depth int, size func(price, sum *big.Int) float64) (asks, bids []Level) {
	sums := map[bool]map[string]*big.Int{true: {}, false: {}}
	prices := map[string]*big.Int{}
	for _, o := range b.orders {
		if o.limit == nil || o.limit.Sign() == 0 || o.remaining.Sign() <= 0 {
			continue
		}
		p := o.limit.String()
		prices[p] = o.limit
		side := sums[o.bid]
		if side[p] == nil {
			side[p] = new(big.Int)
		}
		side[p].Add(side[p], o.remaining)
	}
	asks = levels(sums[false], prices, usdScale, size, false, depth)
	bids = levels(sums[true], prices, usdScale, size, true, depth)
	return asks, bids
}

// Checksum is the CRC32 (IEEE) of the top 20 levels of the book.
func (b *Book) Checksum() uint32 {
	return checksum(b.Levels(LiveLevels))
}

// levels turns the raw size sums at raw prices into levels, best first.
func levels(sums map[string]*big.Int, prices map[string]*big.Int, priceScale *big.Int, size func(price, sum *big.Int) float64, desc bool, depth int) []Level {
	keys := make([]*big.Int, 0, len(sums))
	for p := range sums {
		keys = append(keys, prices[p])
	}
	slices.SortFunc(keys, func(a, b *big.Int) int {
		if desc {
			return b.Cmp(a)
		}
		return a.Cmp(b)
	})
	if len(keys) > depth {
		keys = keys[:depth]
	}
	out := make([]Level, len(keys))
	for i, p := range keys {
		price, _ := new(big.Rat).SetFrac(p, priceScale).Float64()
		out[i] = Level{price, size(p, sums[p.String()])}
	}
	return out
}

// checksum is the CRC32 (IEEE) of the top 20 levels written as price:size
// pairs joined by commas, asks then bids (contracts/data-services.md
// section 10).
func checksum(asks, bids []Level) uint32 {
	pairs := make([]string, 0, 2*LiveLevels)
	for _, side := range [][]Level{asks[:min(len(asks), LiveLevels)], bids[:min(len(bids), LiveLevels)]} {
		for _, l := range side {
			pairs = append(pairs, formatNumber(l[0])+":"+formatNumber(l[1]))
		}
	}
	return crc32.ChecksumIEEE([]byte(strings.Join(pairs, ",")))
}

// formatNumber writes v in its shortest decimal form, as the frame carries it.
func formatNumber(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

const spotBookSQL = `SELECT side = 'buy', target_price::text, sum(amount)::text
	FROM perps.spot_orders WHERE token = $1 AND status = 'open' AND target_price > 0
	GROUP BY side, target_price`

// spotLevels aggregates the open spot_orders of token at target_price, bids
// from buy and asks from sell orders, up to depth levels per side. A sell
// order rests the token amount; a buy order escrows the stablecoin amount,
// so its size is that amount over the price. Amounts carry 6 decimals.
func spotLevels(ctx context.Context, db *sql.DB, token []byte, depth int) (asks, bids []Level, err error) {
	rows, err := db.QueryContext(ctx, spotBookSQL, token)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	sums := map[bool]map[string]*big.Int{true: {}, false: {}}
	prices := map[string]*big.Int{}
	for rows.Next() {
		var (
			buy          bool
			price, total string
		)
		if err := rows.Scan(&buy, &price, &total); err != nil {
			return nil, nil, err
		}
		p, err := parseRaw(price)
		if err != nil {
			return nil, nil, err
		}
		if sums[buy][price], err = parseRaw(total); err != nil {
			return nil, nil, err
		}
		prices[price] = p
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	sell := func(_, sum *big.Int) float64 {
		f, _ := new(big.Rat).SetFrac(sum, spotScale).Float64()
		return f
	}
	buy := func(price, sum *big.Int) float64 {
		num := new(big.Int).Mul(sum, usdScale)
		f, _ := new(big.Rat).SetFrac(num, new(big.Int).Mul(price, spotScale)).Float64()
		return f
	}
	return levels(sums[false], prices, usdScale, sell, false, depth), levels(sums[true], prices, usdScale, buy, true, depth), nil
}

func parseRaw(raw string) (*big.Int, error) {
	if err := bigint.ValidateRaw(raw); err != nil {
		return nil, err
	}
	v, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return nil, fmt.Errorf("feeds: invalid integer %q", raw)
	}
	return v, nil
}

// loadOrderRows reads the order_events and fills rows of market at the
// source position (block, tx_index, log_index) of one stream message as
// decode rows in row order, the input of Book.Apply.
func loadOrderRows(ctx context.Context, db *sql.DB, market string, block, txIndex, logIndex int64) ([]decode.Row, error) {
	var out []decode.Row
	rows, err := db.QueryContext(ctx, orderRowsSQL, block, txIndex, logIndex)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			f                                decode.Fact
			orderID                          string
			marketID, size, limit            sql.NullString
			long                             sql.NullBool
			makerID, takerID, price, fillUSD sql.NullString
		)
		if err := rows.Scan(&f.Block, &f.TxIndex, &f.LogIndex, &f.Sub, &f.Kind, &orderID, &marketID, &long, &size, &limit,
			&makerID, &takerID, &price, &fillUSD); err != nil {
			return nil, err
		}
		switch f.Kind {
		case "placed":
			out = append(out, decode.OrderPlacedRow{Fact: f, OrderID: orderID, MarketID: marketID.String, IsLong: long.Bool, SizeUSD: size.String})
		case "options":
			out = append(out, decode.OrderOptionsRow{Fact: f, OrderID: orderID, LimitPrice: cmp.Or(limit.String, "0")})
		case "cancelled":
			out = append(out, decode.OrderCancelledRow{Fact: f, OrderID: orderID})
		case "expired":
			out = append(out, decode.OrderExpiredRow{Fact: f, OrderID: orderID, MarketID: marketID.String})
		case "triggered":
			out = append(out, decode.OrderTriggeredRow{Fact: f, OrderID: orderID, MarketID: marketID.String})
		case "fill":
			if marketID.String == market {
				out = append(out, decode.FillRow{Fact: f, MarketID: marketID.String, MakerOrderID: makerID.String,
					TakerOrderID: takerID.String, TakerIsLong: long.Bool, Price: price.String, SizeUSD: fillUSD.String})
			}
		}
	}
	return out, rows.Err()
}

const orderRowsSQL = `SELECT block, tx_index, log_index, sub, kind, order_id::text, market_id::text, is_long, size_usd::text,
		limit_price::text, NULL, NULL, NULL, NULL
	FROM perps.order_events WHERE block = $1 AND tx_index = $2 AND log_index = $3
	UNION ALL
	SELECT block, tx_index, log_index, sub, kind, '0', market_id::text, taker_is_long, NULL, NULL,
		maker_order_id::text, taker_order_id::text, price::text, size_usd::text
	FROM perps.fills WHERE block = $1 AND tx_index = $2 AND log_index = $3
	ORDER BY 1, 2, 3, 4`
