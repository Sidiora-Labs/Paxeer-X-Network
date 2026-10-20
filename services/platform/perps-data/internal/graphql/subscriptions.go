package graphql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/lib/pq"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/delivery"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/stream"
)

// insertBatch bounds the rows one wake of an insert subscription reads; a
// full batch wakes the subscription again.
const insertBatch = 500

// Feed fans the PERPS_V1 subjects of one chain out to the GraphQL
// subscriptions of the gateway process. A subscription is woken by any
// message on its subjects and reads the change from Postgres, so wakes
// coalesce; perps.<chain>.chain.rewind wakes every subscription with the
// rewind flag, which sends the current state again.
type Feed struct {
	prefix string
	logger *slog.Logger

	mu        sync.Mutex
	listeners map[*listener]struct{}
}

type listener struct {
	filters []string
	wake    chan struct{}
	rewind  atomic.Bool
	lost    atomic.Bool
}

func (l *listener) poke() {
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// NewFeed builds the Feed of chain; a nil logger selects slog.Default.
func NewFeed(chain uint64, logger *slog.Logger) (*Feed, error) {
	if chain == 0 {
		return nil, errors.New("graphql: feed chain required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Feed{prefix: "perps." + strconv.FormatUint(chain, 10) + ".", logger: logger, listeners: map[*listener]struct{}{}}, nil
}

// Deliver wakes the subscriptions of subject. It is the (*ws.Hub).Listen
// callback of the gateway, so the Feed sees the messages the hub applies, in
// stream order.
func (f *Feed) Deliver(subject string, _ delivery.Envelope) {
	rewind := subject == f.prefix+"chain.rewind"
	f.mu.Lock()
	defer f.mu.Unlock()
	for l := range f.listeners {
		if rewind {
			l.rewind.Store(true)
			l.poke()
			continue
		}
		for _, filter := range l.filters {
			if stream.Matches(filter, subject) {
				l.poke()
				break
			}
		}
	}
}

// Lose ends every subscription with an error, so its client subscribes
// again from the current state instead of reading deltas across messages
// the hub lost. It is the (*ws.Hub).ListenLoss callback of the gateway.
func (f *Feed) Lose() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for l := range f.listeners {
		l.lost.Store(true)
		l.poke()
	}
}

// errLost is the error a subscription ends with after a Lose.
var errLost = errors.New("subscription lost stream messages; subscribe again")

type lossKey struct{}

// withLoss returns ctx carrying lost, which run sets when a Lose ends the
// subscription of ctx.
func withLoss(ctx context.Context, lost *atomic.Bool) context.Context {
	return context.WithValue(ctx, lossKey{}, lost)
}

func (f *Feed) listen(filters []string) (*listener, error) {
	for _, filter := range filters {
		if !stream.ValidFilter(filter) {
			return nil, fmt.Errorf("graphql: invalid subject filter %q", filter)
		}
	}
	l := &listener{filters: filters, wake: make(chan struct{}, 1)}
	f.mu.Lock()
	f.listeners[l] = struct{}{}
	f.mu.Unlock()
	return l, nil
}

func (f *Feed) drop(l *listener) {
	f.mu.Lock()
	delete(f.listeners, l)
	f.mu.Unlock()
}

type feedKey struct{}

func withFeed(ctx context.Context, f *Feed) context.Context {
	return context.WithValue(ctx, feedKey{}, f)
}

// subscriptionFeed returns the Feed of the transport serving ctx and the
// database of r.
func (r *Resolver) subscriptionFeed(ctx context.Context) (*Feed, *sql.DB, error) {
	f, _ := ctx.Value(feedKey{}).(*Feed)
	if f == nil {
		return nil, nil, errNoTransport
	}
	if r == nil || r.DB == nil {
		return nil, nil, errors.New("graphql: subscriptions need a database")
	}
	return f, r.DB, nil
}

// step reads the items of one wake; rewind asks for the current state again
// and more asks for another wake at once.
type step[T any] func(ctx context.Context, rewind bool) (items []T, more bool, err error)

// run registers a listener on filters and sends the items of every wake
// until ctx ends or a Lose ends the subscription, which it records on the
// loss flag of ctx. A failed wake keeps its cursors and its rewind flag, so
// the next wake reads the same change again.
func run[T any](ctx context.Context, f *Feed, filters []string, next step[T]) (<-chan T, error) {
	l, err := f.listen(filters)
	if err != nil {
		return nil, err
	}
	marker, _ := ctx.Value(lossKey{}).(*atomic.Bool)
	out := make(chan T)
	lost := func() bool {
		if !l.lost.Load() {
			return false
		}
		if marker != nil {
			marker.Store(true)
		}
		return true
	}
	go func() {
		defer close(out)
		defer f.drop(l)
		for {
			select {
			case <-ctx.Done():
				return
			case <-l.wake:
			}
			if lost() {
				return
			}
			rewind := l.rewind.Swap(false)
			items, more, err := next(ctx, rewind)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				if rewind {
					l.rewind.Store(true)
				}
				f.logger.Error("graphql subscription read", slog.Any("err", err))
				continue
			}
			for _, item := range items {
				if lost() {
					return
				}
				select {
				case out <- item:
				case <-ctx.Done():
					return
				}
			}
			if more {
				l.poke()
			}
		}
	}()
	return out, nil
}

// Scalars of the schema.

type subDecimal string

func (subDecimal) ImplementsGraphQLType(name string) bool { return name == "BigDecimal" }

func (d *subDecimal) UnmarshalGraphQL(input any) error {
	s, ok := input.(string)
	if !ok {
		return fmt.Errorf("BigDecimal must be a string, got %T", input)
	}
	if _, ok := new(big.Int).SetString(s, 10); !ok {
		return fmt.Errorf("BigDecimal %q is not a decimal integer", s)
	}
	*d = subDecimal(s)
	return nil
}

func (d subDecimal) MarshalJSON() ([]byte, error) { return json.Marshal(string(d)) }

func optDecimal(s sql.NullString) *subDecimal {
	if !s.Valid {
		return nil
	}
	d := subDecimal(s.String)
	return &d
}

type subTime struct{ time.Time }

func (subTime) ImplementsGraphQLType(name string) bool { return name == "DateTime" }

func (t *subTime) UnmarshalGraphQL(input any) error {
	s, ok := input.(string)
	if !ok {
		return fmt.Errorf("DateTime must be a string, got %T", input)
	}
	v, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return fmt.Errorf("DateTime %q: %w", s, err)
	}
	t.Time = v
	return nil
}

func (t subTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.UTC().Format(time.RFC3339Nano))
}

func optTime(t sql.NullTime) *subTime {
	if !t.Valid {
		return nil
	}
	return &subTime{t.Time}
}

func optInt(v sql.NullInt32) *int32 {
	if !v.Valid {
		return nil
	}
	return &v.Int32
}

func optString(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}

type scanner interface{ Scan(dest ...any) error }

func queryAll[T any](ctx context.Context, db *sql.DB, scan func(scanner) (T, error), query string, args ...any) ([]T, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func maxRowID(ctx context.Context, db *sql.DB, table string) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, "SELECT COALESCE(max(row_id), 0) FROM perps."+table).Scan(&id)
	return id, err
}

// headHeight is the height of the head cursor, -1 before the first block.
func headHeight(ctx context.Context, db *sql.DB) (int64, error) {
	var h int64
	err := db.QueryRowContext(ctx, "SELECT height FROM perps.cursors WHERE name = 'head'").Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return -1, nil
	}
	return h, err
}

func accountArg(userAddress string) (string, []byte, error) {
	if !common.IsHexAddress(userAddress) || !strings.HasPrefix(userAddress, "0x") && !strings.HasPrefix(userAddress, "0X") {
		return "", nil, fmt.Errorf("invalid userAddress %q", userAddress)
	}
	a := common.HexToAddress(userAddress)
	return strings.ToLower(a.Hex()), a.Bytes(), nil
}

func marketArg(marketID int32) (string, error) {
	if marketID < 0 {
		return "", fmt.Errorf("invalid marketId %d", marketID)
	}
	return strconv.FormatInt(int64(marketID), 10), nil
}

// IndexerStatus.

type subIndexerStatus struct {
	LastIndexedBlock int32
	ChainHead        *int32
	BlocksScanned    *int32
	EventsProcessed  *int32
	IsSynced         *bool
}

// IndexerStatusUpdated sends the indexer status on every perps.<chain>.chain.block.
func (r *Resolver) IndexerStatusUpdated(ctx context.Context) (<-chan *subIndexerStatus, error) {
	f, db, err := r.subscriptionFeed(ctx)
	if err != nil {
		return nil, err
	}
	return run(ctx, f, []string{f.prefix + "chain.block"}, func(ctx context.Context, _ bool) ([]*subIndexerStatus, bool, error) {
		var s subIndexerStatus
		err := db.QueryRowContext(ctx, "SELECT height::int4 FROM perps.cursors WHERE name = 'head'").Scan(&s.LastIndexedBlock)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		return []*subIndexerStatus{&s}, false, nil
	})
}

// LatestPrice, PoolState, FundingRate and Market.

type subLatestPrice struct {
	MarketID         int32
	Price            subDecimal
	OnchainTimestamp int32
	BlockNumber      int32
	UpdatedAt        *subTime
}

const latestPricesSQL = `SELECT m.market_id::int4, mp.index_price::text, floor(extract(epoch FROM mp.block_time))::int4, mp.block::int4, mp.block_time
FROM perps.markets m
CROSS JOIN LATERAL (SELECT p.index_price, p.block, p.block_time FROM perps.mark_prices p WHERE p.market_id = m.market_id
    ORDER BY p.block DESC, p.tx_index DESC, p.log_index DESC, p.sub DESC LIMIT 1) mp
ORDER BY m.market_id`

func scanLatestPrice(s scanner) (*subLatestPrice, error) {
	var p subLatestPrice
	var at time.Time
	if err := s.Scan(&p.MarketID, &p.Price, &p.OnchainTimestamp, &p.BlockNumber, &at); err != nil {
		return nil, err
	}
	p.UpdatedAt = &subTime{at}
	return &p, nil
}

// LatestPricesUpdated sends the latest price of every market, price the
// index_price, once per wake that finds new perps.mark_prices rows.
func (r *Resolver) LatestPricesUpdated(ctx context.Context) (<-chan []*subLatestPrice, error) {
	f, db, err := r.subscriptionFeed(ctx)
	if err != nil {
		return nil, err
	}
	cursor, err := maxRowID(ctx, db, "mark_prices")
	if err != nil {
		return nil, err
	}
	return run(ctx, f, []string{f.prefix + "*.mark"}, func(ctx context.Context, rewind bool) ([][]*subLatestPrice, bool, error) {
		top, err := maxRowID(ctx, db, "mark_prices")
		if err != nil {
			return nil, false, err
		}
		if top <= cursor && !rewind {
			return nil, false, nil
		}
		prices, err := queryAll(ctx, db, scanLatestPrice, latestPricesSQL)
		if err != nil {
			return nil, false, err
		}
		cursor = top
		if prices == nil {
			prices = []*subLatestPrice{}
		}
		return [][]*subLatestPrice{prices}, false, nil
	})
}

type subPoolState struct {
	MarketID     int32
	BaseReserve  subDecimal
	QuoteReserve subDecimal
	OraclePrice  *subDecimal
	UpdatedAt    *subTime
	BlockNumber  *int32
}

type subFundingRate struct {
	ID             int32
	MarketID       int32
	RatePerSecond  subDecimal
	Rate24h        subDecimal
	BlockNumber    int32
	TxHash         string
	BlockTimestamp subTime
}

type subMarket struct {
	MarketID    int32
	Name        string
	Symbol      string
	MaxLeverage subDecimal
	Enabled     bool
	CreatedAt   *subTime
	LatestPrice *subLatestPrice
	PoolState   *subPoolState
	FundingRate *subFundingRate
}

const subMarketSQL = `SELECT m.market_id::int4, m.name, m.symbol, m.max_leverage::text, m.enabled,
    mp.index_price::text, floor(extract(epoch FROM mp.block_time))::int4, mp.block::int4, mp.block_time,
    fr.row_id::int4, fr.new_rate_per_second::text, fr.funding_rate24h::text, fr.block::int4, '0x' || encode(fr.tx_hash, 'hex'), fr.block_time
FROM perps.markets m
LEFT JOIN LATERAL (SELECT p.index_price, p.block, p.block_time FROM perps.mark_prices p WHERE p.market_id = m.market_id
    ORDER BY p.block DESC, p.tx_index DESC, p.log_index DESC, p.sub DESC LIMIT 1) mp ON true
LEFT JOIN LATERAL (SELECT r.row_id, r.new_rate_per_second, r.funding_rate24h, r.block, r.tx_hash, r.block_time FROM perps.funding_rates r
    WHERE r.market_id = m.market_id ORDER BY r.block DESC, r.tx_index DESC, r.log_index DESC, r.sub DESC LIMIT 1) fr ON true
WHERE m.market_id = $1`

// loadMarket reads market id with its latest mark price, pool state and
// funding rate; an unknown market is nil.
func loadMarket(ctx context.Context, db *sql.DB, id int32) (*subMarket, error) {
	var m subMarket
	var price, rate, rate24h, rateTx sql.NullString
	var onchain, priceBlock, rateID, rateBlock sql.NullInt32
	var priceAt, rateAt sql.NullTime
	err := db.QueryRowContext(ctx, subMarketSQL, strconv.FormatInt(int64(id), 10)).Scan(&m.MarketID, &m.Name, &m.Symbol, &m.MaxLeverage, &m.Enabled,
		&price, &onchain, &priceBlock, &priceAt, &rateID, &rate, &rate24h, &rateBlock, &rateTx, &rateAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.PoolState = &subPoolState{MarketID: m.MarketID, BaseReserve: "0", QuoteReserve: "0", OraclePrice: optDecimal(price), UpdatedAt: optTime(priceAt), BlockNumber: optInt(priceBlock)}
	if price.Valid {
		m.LatestPrice = &subLatestPrice{MarketID: m.MarketID, Price: subDecimal(price.String), OnchainTimestamp: onchain.Int32, BlockNumber: priceBlock.Int32, UpdatedAt: optTime(priceAt)}
	}
	if rateID.Valid {
		m.FundingRate = &subFundingRate{ID: rateID.Int32, MarketID: m.MarketID, RatePerSecond: subDecimal(rate.String), Rate24h: subDecimal(rate24h.String),
			BlockNumber: rateBlock.Int32, TxHash: rateTx.String, BlockTimestamp: subTime{rateAt.Time}}
	}
	return &m, nil
}

// PriceUpdate.

type subPriceUpdate struct {
	UpdatedAt        *subTime
	Timestamp        *int32
	ID               int32
	MarketID         int32
	Price            subDecimal
	OnchainTimestamp int32
	BlockNumber      int32
	TxHash           string
	BlockTimestamp   subTime
}

const priceColumns = `SELECT i.row_id, i.row_id::int4, i.market_id::int4, i.index_price::text, COALESCE(div(i.timestamp_ms, 1000), floor(extract(epoch FROM i.block_time)))::int4, i.block::int4,
    '0x' || encode(i.block_hash, 'hex'), i.block_time FROM perps.index_prices i`

func scanPrice(s scanner) (*subPriceUpdate, int64, error) {
	var p subPriceUpdate
	var rowID int64
	if err := s.Scan(&rowID, &p.ID, &p.MarketID, &p.Price, &p.OnchainTimestamp, &p.BlockNumber, &p.TxHash, &p.BlockTimestamp.Time); err != nil {
		return nil, 0, err
	}
	at, seconds := p.BlockTimestamp, p.OnchainTimestamp
	p.UpdatedAt, p.Timestamp = &at, &seconds
	return &p, rowID, nil
}

// PriceUpdated sends one PriceUpdate per insert into perps.index_prices of
// kind aggregate, price the index_price and txHash the block hash; a null
// marketId streams every market.
func (r *Resolver) PriceUpdated(ctx context.Context, args struct{ MarketID *int32 }) (<-chan *subPriceUpdate, error) {
	f, db, err := r.subscriptionFeed(ctx)
	if err != nil {
		return nil, err
	}
	if args.MarketID == nil {
		return streamInserts(ctx, f, db, []string{f.prefix + "*.index"}, inserts[*subPriceUpdate]{
			table: "index_prices",
			news:  priceColumns + ` WHERE i.row_id > $1 AND i.kind = 'aggregate' AND i.index_price IS NOT NULL ORDER BY i.row_id LIMIT ` + strconv.Itoa(insertBatch),
			latest: priceColumns + ` JOIN perps.markets m ON i.row_id = (SELECT l.row_id FROM perps.index_prices l WHERE l.market_id = m.market_id AND l.kind = 'aggregate' AND l.index_price IS NOT NULL
    ORDER BY l.block DESC, l.tx_index DESC, l.log_index DESC, l.sub DESC LIMIT 1) ORDER BY m.market_id`,
			scan: scanPrice,
		})
	}
	market, err := marketArg(*args.MarketID)
	if err != nil {
		return nil, err
	}
	return streamInserts(ctx, f, db, []string{f.prefix + market + ".index"}, inserts[*subPriceUpdate]{
		table:  "index_prices",
		news:   priceColumns + ` WHERE i.row_id > $1 AND i.kind = 'aggregate' AND i.index_price IS NOT NULL AND i.market_id = $2 ORDER BY i.row_id LIMIT ` + strconv.Itoa(insertBatch),
		latest: priceColumns + ` WHERE i.kind = 'aggregate' AND i.index_price IS NOT NULL AND i.market_id = $1 ORDER BY i.block DESC, i.tx_index DESC, i.log_index DESC, i.sub DESC LIMIT 1`,
		args:   []any{market},
		scan:   scanPrice,
	})
}

// inserts describes a subscription to the rows inserted into table after it
// starts: news reads the rows after the row_id $1 in row_id order (args are
// $2 onwards), latest reads the current state a rewind sends again (args are
// $1 onwards) and scan returns a row with its row_id.
type inserts[T any] struct {
	table  string
	news   string
	latest string
	args   []any
	scan   func(scanner) (T, int64, error)
}

type identified[T any] struct {
	item T
	id   int64
}

func streamInserts[T any](ctx context.Context, f *Feed, db *sql.DB, filters []string, s inserts[T]) (<-chan T, error) {
	cursor, err := maxRowID(ctx, db, s.table)
	if err != nil {
		return nil, err
	}
	scan := func(sc scanner) (identified[T], error) {
		item, id, err := s.scan(sc)
		return identified[T]{item, id}, err
	}
	return run(ctx, f, filters, func(ctx context.Context, rewind bool) ([]T, bool, error) {
		news, err := queryAll(ctx, db, scan, s.news, append([]any{cursor}, s.args...)...)
		if err != nil {
			return nil, false, err
		}
		var items []T
		if rewind {
			state, err := queryAll(ctx, db, scan, s.latest, s.args...)
			if err != nil {
				return nil, false, err
			}
			fresh := make(map[int64]bool, len(news))
			for _, n := range news {
				fresh[n.id] = true
			}
			for _, st := range state {
				if !fresh[st.id] {
					items = append(items, st.item)
				}
			}
		}
		for _, n := range news {
			items = append(items, n.item)
			cursor = n.id
		}
		return items, len(news) == insertBatch, nil
	})
}

// Position.

type subPosition struct {
	PositionID       subDecimal
	UserAddress      string
	MarketID         int32
	IsLong           bool
	SizeUsd          subDecimal
	Leverage         subDecimal
	EntryPrice       subDecimal
	CollateralToken  *string
	CollateralAmount subDecimal
	CollateralUsd    subDecimal
	Status           string
	RealizedPnl      *subDecimal
	ExitPrice        *subDecimal
	OpenedAt         *subTime
	ClosedAt         *subTime
	OpenBlock        *int32
	CloseBlock       *int32
	OpenTxHash       *string
	CloseTxHash      *string

	db *sql.DB
}

// Market resolves Position.market.
func (p *subPosition) Market(ctx context.Context) (*subMarket, error) {
	return loadMarket(ctx, p.db, p.MarketID)
}

const positionsSQL = `SELECT p.position_id::text, '0x' || encode(p.account, 'hex'), p.market_id::int4, p.is_long, p.size_usd::text, p.leverage::text,
    p.entry_price::text, '0x' || encode(p.collateral_token, 'hex'), p.collateral_amount::text, p.collateral_usd::text, p.status, p.realized_pnl::text,
    CASE WHEN p.status <> 'open' THEN (SELECT t.execution_price::text FROM perps.trade_settlements t WHERE t.position_id = p.position_id AND t.block = p.closed_block
        ORDER BY t.tx_index DESC, t.log_index DESC, t.sub DESC LIMIT 1) END,
    bo.time, bc.time, p.opened_block::int4, p.closed_block::int4,
    (SELECT '0x' || encode(e.tx_hash, 'hex') FROM perps.position_events e WHERE e.position_id = p.position_id AND e.block = p.opened_block
        ORDER BY e.tx_index, e.log_index, e.sub LIMIT 1),
    COALESCE((SELECT '0x' || encode(e.tx_hash, 'hex') FROM perps.position_events e WHERE e.position_id = p.position_id AND e.block = p.closed_block
        ORDER BY e.tx_index DESC, e.log_index DESC, e.sub DESC LIMIT 1),
        (SELECT '0x' || encode(l.tx_hash, 'hex') FROM perps.liquidations l WHERE l.position_id = p.position_id AND l.block = p.closed_block
        ORDER BY l.tx_index DESC, l.log_index DESC, l.sub DESC LIMIT 1))
FROM perps.positions_current p
LEFT JOIN perps.blocks bo ON bo.number = p.opened_block
LEFT JOIN perps.blocks bc ON bc.number = p.closed_block
WHERE p.account = $1 AND (p.status = 'open' OR p.closed_block > $2)
ORDER BY p.position_id`

// PositionChanged sends, on every perps.<chain>.user.<account>.position, the
// open positions of the account and the ones it closed since the previous
// message.
func (r *Resolver) PositionChanged(ctx context.Context, args struct{ UserAddress string }) (<-chan []*subPosition, error) {
	f, db, err := r.subscriptionFeed(ctx)
	if err != nil {
		return nil, err
	}
	account, raw, err := accountArg(args.UserAddress)
	if err != nil {
		return nil, err
	}
	since, err := headHeight(ctx, db)
	if err != nil {
		return nil, err
	}
	scan := func(s scanner) (*subPosition, error) {
		p := subPosition{db: db}
		var token, pnl, exit, openTx, closeTx sql.NullString
		var opened, closed sql.NullTime
		var openBlock, closeBlock sql.NullInt32
		if err := s.Scan(&p.PositionID, &p.UserAddress, &p.MarketID, &p.IsLong, &p.SizeUsd, &p.Leverage, &p.EntryPrice, &token, &p.CollateralAmount,
			&p.CollateralUsd, &p.Status, &pnl, &exit, &opened, &closed, &openBlock, &closeBlock, &openTx, &closeTx); err != nil {
			return nil, err
		}
		p.CollateralToken, p.RealizedPnl, p.ExitPrice = optString(token), optDecimal(pnl), optDecimal(exit)
		p.OpenedAt, p.ClosedAt, p.OpenBlock, p.CloseBlock = optTime(opened), optTime(closed), optInt(openBlock), optInt(closeBlock)
		p.OpenTxHash, p.CloseTxHash = optString(openTx), optString(closeTx)
		return &p, nil
	}
	return run(ctx, f, []string{f.prefix + "user." + account + ".position"}, func(ctx context.Context, _ bool) ([][]*subPosition, bool, error) {
		head, err := headHeight(ctx, db)
		if err != nil {
			return nil, false, err
		}
		positions, err := queryAll(ctx, db, scan, positionsSQL, raw, since)
		if err != nil {
			return nil, false, err
		}
		since = head
		if positions == nil {
			positions = []*subPosition{}
		}
		return [][]*subPosition{positions}, false, nil
	})
}

// Order.

type subOrder struct {
	RemainingSize  *subDecimal
	BlockTimestamp *subTime
	OrderID        subDecimal
	UserAddress    string
	MarketID       int32
	OrderType      int32
	OrderTypeName  *string
	IsLong         bool
	TriggerPrice   subDecimal
	SizeUsd        subDecimal
	Status         string
	PositionID     *subDecimal
	ExecutionPrice *subDecimal
	FailureReason  *string
	PlacedAt       *subTime
	ResolvedAt     *subTime
	PlacedBlock    *int32
	ResolvedBlock  *int32
	PlacedTxHash   *string
	ResolvedTxHash *string
}

// orderTypeLabels are the labels of the orderType numbering of
// contracts/perps.md section 2.
var orderTypeLabels = map[int32]string{0: "Limit", 1: "Stop-Limit", 2: "Take Profit", 3: "Stop Loss", 4: "Market", 5: "Stop Market"}

const ordersSQL = `SELECT o.order_id::text, '0x' || encode(o.account, 'hex'), o.market_id::int4, o.order_type::int4, o.is_long, o.trigger_price::text,
    o.size_usd::text, o.status, o.position_id::text, o.execution_price::text, o.remaining_size_usd::text, o.placed_block::int4, o.closed_block::int4,
    bp.time, bc.time,
    (SELECT '0x' || encode(e.tx_hash, 'hex') FROM perps.order_events e WHERE e.order_id = o.order_id AND e.kind = 'placed'
        ORDER BY e.block, e.tx_index, e.log_index, e.sub LIMIT 1),
    COALESCE((SELECT '0x' || encode(e.tx_hash, 'hex') FROM perps.order_events e WHERE e.order_id = o.order_id AND e.block = o.closed_block
        AND e.kind IN ('cancelled', 'expired', 'executed') ORDER BY e.tx_index DESC, e.log_index DESC, e.sub DESC LIMIT 1),
        (SELECT '0x' || encode(x.tx_hash, 'hex') FROM perps.fills x WHERE x.block = o.closed_block
        AND ((x.maker = o.account AND x.maker_order_id = o.order_id) OR (x.taker = o.account AND x.taker_order_id = o.order_id))
        ORDER BY x.tx_index DESC, x.log_index DESC, x.sub DESC LIMIT 1)),
    (SELECT b.time FROM perps.blocks b WHERE b.number = GREATEST(o.placed_block, o.closed_block,
        (SELECT max(e.block) FROM perps.order_events e WHERE e.order_id = o.order_id),
        (SELECT max(x.block) FROM perps.fills x WHERE (x.maker = o.account AND x.maker_order_id = o.order_id) OR (x.taker = o.account AND x.taker_order_id = o.order_id))))
FROM perps.orders o
LEFT JOIN perps.blocks bp ON bp.number = o.placed_block
LEFT JOIN perps.blocks bc ON bc.number = o.closed_block
WHERE o.account = $1`

func scanOrder(s scanner) (*subOrder, error) {
	var o subOrder
	var position, execution, remaining, placedTx, resolvedTx sql.NullString
	var placedBlock, resolvedBlock sql.NullInt32
	var placed, resolved, changed sql.NullTime
	if err := s.Scan(&o.OrderID, &o.UserAddress, &o.MarketID, &o.OrderType, &o.IsLong, &o.TriggerPrice, &o.SizeUsd, &o.Status, &position, &execution,
		&remaining, &placedBlock, &resolvedBlock, &placed, &resolved, &placedTx, &resolvedTx, &changed); err != nil {
		return nil, err
	}
	if name, ok := orderTypeLabels[o.OrderType]; ok {
		o.OrderTypeName = &name
	}
	o.PositionID, o.ExecutionPrice, o.RemainingSize = optDecimal(position), optDecimal(execution), optDecimal(remaining)
	o.PlacedBlock, o.ResolvedBlock, o.PlacedAt, o.ResolvedAt = optInt(placedBlock), optInt(resolvedBlock), optTime(placed), optTime(resolved)
	o.PlacedTxHash, o.ResolvedTxHash, o.BlockTimestamp = optString(placedTx), optString(resolvedTx), optTime(changed)
	return &o, nil
}

type orderTouch struct {
	orderID string
	id      int64
}

func scanTouch(s scanner) (orderTouch, error) {
	var t orderTouch
	return t, s.Scan(&t.orderID, &t.id)
}

// OrderChanged sends one Order per order of the account changed by a new
// perps.order_events or perps.fills row, woken by
// perps.<chain>.user.<account>.order and .fill; a rewind sends the open
// orders of the account again.
func (r *Resolver) OrderChanged(ctx context.Context, args struct{ UserAddress string }) (<-chan *subOrder, error) {
	f, db, err := r.subscriptionFeed(ctx)
	if err != nil {
		return nil, err
	}
	account, raw, err := accountArg(args.UserAddress)
	if err != nil {
		return nil, err
	}
	events, err := maxRowID(ctx, db, "order_events")
	if err != nil {
		return nil, err
	}
	fills, err := maxRowID(ctx, db, "fills")
	if err != nil {
		return nil, err
	}
	batch := strconv.Itoa(insertBatch)
	user := f.prefix + "user." + account + "."
	return run(ctx, f, []string{user + "order", user + "fill"}, func(ctx context.Context, rewind bool) ([]*subOrder, bool, error) {
		touchedEvents, err := queryAll(ctx, db, scanTouch, `SELECT order_id::text, row_id FROM perps.order_events WHERE row_id > $1 AND account = $2
ORDER BY row_id LIMIT `+batch, events, raw)
		if err != nil {
			return nil, false, err
		}
		touchedFills, err := queryAll(ctx, db, scanTouch, `SELECT CASE WHEN maker = $2 THEN maker_order_id ELSE taker_order_id END::text, row_id
FROM perps.fills WHERE row_id > $1 AND (maker = $2 OR taker = $2) ORDER BY row_id LIMIT `+batch, fills, raw)
		if err != nil {
			return nil, false, err
		}
		var ids []string
		listed := map[string]bool{}
		for _, t := range append(touchedEvents, touchedFills...) {
			if !listed[t.orderID] {
				listed[t.orderID] = true
				ids = append(ids, t.orderID)
			}
		}
		var orders []*subOrder
		if len(ids) > 0 {
			orders, err = queryAll(ctx, db, scanOrder, ordersSQL+` AND o.order_id = ANY($2::numeric[]) ORDER BY o.order_id`, raw, pq.Array(ids))
			if err != nil {
				return nil, false, err
			}
		}
		if rewind {
			open, err := queryAll(ctx, db, scanOrder, ordersSQL+` AND o.status IN ('active', 'triggered') ORDER BY o.order_id`, raw)
			if err != nil {
				return nil, false, err
			}
			for _, o := range open {
				if !listed[string(o.OrderID)] {
					orders = append(orders, o)
				}
			}
		}
		if n := len(touchedEvents); n > 0 {
			events = touchedEvents[n-1].id
		}
		if n := len(touchedFills); n > 0 {
			fills = touchedFills[n-1].id
		}
		return orders, len(touchedEvents) == insertBatch || len(touchedFills) == insertBatch, nil
	})
}

// Trade.

type subTrade struct {
	ExecutionPrice *subDecimal
	ID             int32
	PositionID     subDecimal
	UserAddress    *string
	MarketID       *int32
	TradeType      string
	IsLong         *bool
	SizeUsd        subDecimal
	Price          subDecimal
	RealizedPnl    *subDecimal
	FeeUsd         *subDecimal
	BlockNumber    int32
	TxHash         string
	BlockTimestamp subTime
}

const tradeColumns = `SELECT t.row_id, t.row_id::int4, t.position_id::text, '0x' || encode(t."user", 'hex'), t.market_id::int4, t.trade_type::int4, p.is_long,
    t.size_usd::text, t.execution_price::text, t.gross_pnl::text, t.total_fees_usd::text, t.block::int4, '0x' || encode(t.tx_hash, 'hex'), t.block_time
FROM perps.trade_settlements t LEFT JOIN perps.positions_current p ON p.position_id = t.position_id`

func scanTrade(s scanner) (*subTrade, int64, error) {
	var t subTrade
	var rowID int64
	var user string
	var market, tradeType int32
	var isLong sql.NullBool
	var pnl, fee subDecimal
	if err := s.Scan(&rowID, &t.ID, &t.PositionID, &user, &market, &tradeType, &isLong, &t.SizeUsd, &t.Price, &pnl, &fee, &t.BlockNumber,
		&t.TxHash, &t.BlockTimestamp.Time); err != nil {
		return nil, 0, err
	}
	// A tradeType outside the enumeration is named by its number.
	t.TradeType = strconv.FormatInt(int64(tradeType), 10)
	if tradeType >= 0 && int(tradeType) < len(tradeTypeNames) {
		t.TradeType = tradeTypeNames[tradeType]
	}
	price := t.Price
	t.ExecutionPrice, t.UserAddress, t.MarketID, t.RealizedPnl, t.FeeUsd = &price, &user, &market, &pnl, &fee
	if isLong.Valid {
		t.IsLong = &isLong.Bool
	}
	return &t, rowID, nil
}

// TradeCreated sends one Trade per insert into perps.trade_settlements of
// the market, woken by perps.<chain>.<market>.fill and .liq.
func (r *Resolver) TradeCreated(ctx context.Context, args struct{ MarketID int32 }) (<-chan *subTrade, error) {
	f, db, err := r.subscriptionFeed(ctx)
	if err != nil {
		return nil, err
	}
	market, err := marketArg(args.MarketID)
	if err != nil {
		return nil, err
	}
	return streamInserts(ctx, f, db, []string{f.prefix + market + ".fill", f.prefix + market + ".liq"}, inserts[*subTrade]{
		table:  "trade_settlements",
		news:   tradeColumns + ` WHERE t.row_id > $1 AND t.market_id = $2 ORDER BY t.row_id LIMIT ` + strconv.Itoa(insertBatch),
		latest: tradeColumns + ` WHERE t.market_id = $1 ORDER BY t.block DESC, t.tx_index DESC, t.log_index DESC, t.sub DESC LIMIT 1`,
		args:   []any{market},
		scan:   scanTrade,
	})
}

// TradeExecuted.

type subTradeExecuted struct {
	TradeID        *string
	MarketID       *int32
	IsLong         *bool
	SizeUsd        *subDecimal
	Price          *subDecimal
	BlockTimestamp *int32
}

const fillColumns = `SELECT x.row_id, x.row_id::text, x.market_id::int4, x.taker_is_long, x.size_usd::text, x.price::text,
    floor(extract(epoch FROM x.block_time))::int4 FROM perps.fills x`

func scanFill(s scanner) (*subTradeExecuted, int64, error) {
	var rowID int64
	var id string
	var market, at int32
	var isLong bool
	var size, price subDecimal
	if err := s.Scan(&rowID, &id, &market, &isLong, &size, &price, &at); err != nil {
		return nil, 0, err
	}
	return &subTradeExecuted{TradeID: &id, MarketID: &market, IsLong: &isLong, SizeUsd: &size, Price: &price, BlockTimestamp: &at}, rowID, nil
}

// TradeExecuted sends one TradeExecuted per insert into perps.fills, tradeId
// the row_id and isLong the taker side, woken by perps.<chain>.<market>.fill;
// a null marketId streams every market.
func (r *Resolver) TradeExecuted(ctx context.Context, args struct{ MarketID *int32 }) (<-chan *subTradeExecuted, error) {
	f, db, err := r.subscriptionFeed(ctx)
	if err != nil {
		return nil, err
	}
	batch := strconv.Itoa(insertBatch)
	if args.MarketID == nil {
		return streamInserts(ctx, f, db, []string{f.prefix + "*.fill"}, inserts[*subTradeExecuted]{
			table: "fills",
			news:  fillColumns + ` WHERE x.row_id > $1 ORDER BY x.row_id LIMIT ` + batch,
			latest: fillColumns + ` WHERE x.row_id IN (SELECT DISTINCT ON (l.market_id) l.row_id FROM perps.fills l
    ORDER BY l.market_id, l.block DESC, l.tx_index DESC, l.log_index DESC, l.sub DESC) ORDER BY x.market_id`,
			scan: scanFill,
		})
	}
	market, err := marketArg(*args.MarketID)
	if err != nil {
		return nil, err
	}
	return streamInserts(ctx, f, db, []string{f.prefix + market + ".fill"}, inserts[*subTradeExecuted]{
		table:  "fills",
		news:   fillColumns + ` WHERE x.row_id > $1 AND x.market_id = $2 ORDER BY x.row_id LIMIT ` + batch,
		latest: fillColumns + ` WHERE x.market_id = $1 ORDER BY x.block DESC, x.tx_index DESC, x.log_index DESC, x.sub DESC LIMIT 1`,
		args:   []any{market},
		scan:   scanFill,
	})
}

// Liquidation.

type subLiquidation struct {
	SizeUsd        *subDecimal
	ExecutionPrice *subDecimal
	ID             int32
	PositionID     subDecimal
	UserAddress    string
	MarketID       int32
	Price          subDecimal
	Penalty        subDecimal
	Keeper         string
	BlockNumber    int32
	TxHash         string
	BlockTimestamp subTime
}

const liquidationColumns = `SELECT l.row_id, l.row_id::int4, l.position_id::text, '0x' || encode(l."user", 'hex'), l.market_id::int4, l.liquidation_price::text,
    l.penalty::text, '0x' || encode(l.keeper, 'hex'), l.block::int4, '0x' || encode(l.tx_hash, 'hex'), l.block_time,
    (SELECT t.size_usd::text FROM perps.trade_settlements t WHERE t.position_id = l.position_id AND t.block = l.block AND t.tx_index = l.tx_index
        AND t.trade_type = 2 ORDER BY t.log_index DESC, t.sub DESC LIMIT 1)
FROM perps.liquidations l`

func scanLiquidation(s scanner) (*subLiquidation, int64, error) {
	var l subLiquidation
	var rowID int64
	var size sql.NullString
	if err := s.Scan(&rowID, &l.ID, &l.PositionID, &l.UserAddress, &l.MarketID, &l.Price, &l.Penalty, &l.Keeper, &l.BlockNumber, &l.TxHash,
		&l.BlockTimestamp.Time, &size); err != nil {
		return nil, 0, err
	}
	price := l.Price
	l.ExecutionPrice, l.SizeUsd = &price, optDecimal(size)
	return &l, rowID, nil
}

// LiquidationCreated sends one Liquidation per insert into perps.liquidations
// of the market, woken by perps.<chain>.<market>.liq; sizeUsd is the size of
// the liquidation settlement of the position in the same transaction.
func (r *Resolver) LiquidationCreated(ctx context.Context, args struct{ MarketID int32 }) (<-chan *subLiquidation, error) {
	f, db, err := r.subscriptionFeed(ctx)
	if err != nil {
		return nil, err
	}
	market, err := marketArg(args.MarketID)
	if err != nil {
		return nil, err
	}
	return streamInserts(ctx, f, db, []string{f.prefix + market + ".liq"}, inserts[*subLiquidation]{
		table:  "liquidations",
		news:   liquidationColumns + ` WHERE l.row_id > $1 AND l.market_id = $2 ORDER BY l.row_id LIMIT ` + strconv.Itoa(insertBatch),
		latest: liquidationColumns + ` WHERE l.market_id = $1 ORDER BY l.block DESC, l.tx_index DESC, l.log_index DESC, l.sub DESC LIMIT 1`,
		args:   []any{market},
		scan:   scanLiquidation,
	})
}
