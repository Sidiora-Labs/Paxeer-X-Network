// Package records builds the market data the REST routes and the socket
// topics of the perps gateway both serve: the symbol stats, trades, price and
// health bodies of the application data protocol (contracts/data-services.md
// section 9) and the market record and totals types (section 11).
package records

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"
)

// MarketIDs maps the eighteen symbols of the application feed list
// (Hyperpax:src/core/feeds.ts:4) to their market ids (contracts/perps-markets.md
// section 2).
var MarketIDs = map[string]int64{
	"BTC": 0, "ETH": 1, "SOL": 2, "AVAX": 3, "LINK": 4, "TSLA": 5, "NVDA": 6, "NAS100": 7, "XAU": 8,
	"SPX500": 9, "GOOGL": 10, "PAX": 11, "SID": 12, "HYPE": 13, "XRP": 14, "ASTER": 15, "TRUMP": 16, "BNB": 17,
}

// SpotTokenRank is the position of a spot token among the registered
// non-stablecoin tokens of 0x1025, which InitV6120 registers as USDL, PAX, SID
// (contracts/spot.md, Opening pairs).
var SpotTokenRank = map[string]int{"PAX": 0, "SID": 1}

const (
	// USDScale is 10^18: perps prices, USD notionals and rates are 18-decimal
	// fixed point (contracts/perps.md section 2), as are spot execution prices
	// (contracts/spot.md, Pricing).
	USDScale = "1000000000000000000"
	// SpotScale is 10^6: USDL, PAX and SID amounts carry 6 decimals
	// (contracts/spot.md, Token model).
	SpotScale = "1000000"
	// SecondsPerHour turns a per-second funding rate into the hourly rate.
	SecondsPerHour = 3600

	Window24h = 24 * time.Hour
	ISOMillis = "2006-01-02T15:04:05.000Z"
)

// ErrUnknownSymbol reports a symbol outside MarketIDs.
var ErrUnknownSymbol = errors.New("records: unknown symbol")

// Symbol reads the symbol bodies from schema perps.
type Symbol struct {
	db *sql.DB
}

// NewSymbol returns the symbol reader of db.
func NewSymbol(db *sql.DB) (*Symbol, error) {
	if db == nil {
		return nil, errors.New("records: nil database")
	}
	return &Symbol{db: db}, nil
}

// MarketRecord is one record of the markets table (Hyperpax:data_api.md:424-461,
// contracts/data-services.md section 11): twelve market fields and the eleven
// perp_ fields. Prices and notionals are USD, rates are per-hour ratios except
// funding_8h, and times are epoch milliseconds.
type MarketRecord struct {
	Symbol         string   `json:"symbol"`
	SymbolFull     string   `json:"symbol_full"`
	Price          *float64 `json:"price"`
	Change24h      *float64 `json:"change_24h"`
	Volume24h      float64  `json:"volume_24h"`
	OpenInterest   float64  `json:"open_interest"`
	Funding8h      *float64 `json:"funding_8h"`
	EstFundingRate *float64 `json:"est_funding_rate"`
	Volume7d       float64  `json:"volume_7d"`
	Volume30d      float64  `json:"volume_30d"`
	TradesCount    int64    `json:"trades_count"`
	LastUpdated    *int64   `json:"last_updated"`

	PerpMarkPrice          *float64 `json:"perp_mark_price"`
	PerpIndexPrice         *float64 `json:"perp_index_price"`
	PerpBasisBps           *float64 `json:"perp_basis_bps"`
	PerpFundingRate        *float64 `json:"perp_funding_rate"`
	PerpNextFundingMs      int64    `json:"perp_next_funding_ms"`
	PerpLongShortRatio     *float64 `json:"perp_long_short_ratio"`
	PerpLiqVolume24h       float64  `json:"perp_liq_volume_24h"`
	PerpOIContracts        *float64 `json:"perp_oi_contracts"`
	PerpOIUSD              float64  `json:"perp_oi_usd"`
	PerpVolume24hContracts float64  `json:"perp_volume_24h_contracts"`
	PerpVolume24hUSD       float64  `json:"perp_volume_24h_usd"`
}

// Totals is the platform totals object of /api/totals (Hyperpax:data_api.md:473),
// summed over perps.market_stats_rolling.
type Totals struct {
	TotalTrades int64   `json:"total_trades"`
	Volume24h   float64 `json:"volume_24h"`
	Volume7d    float64 `json:"volume_7d"`
	Volume30d   float64 `json:"volume_30d"`
	TotalOI     float64 `json:"total_oi"`
}

type Price struct {
	Symbol    string   `json:"symbol"`
	Price     *float64 `json:"price"`
	Timestamp *string  `json:"timestamp"`
	Open      *float64 `json:"open"`
	High      *float64 `json:"high"`
	Low       *float64 `json:"low"`
}

type Stats struct {
	Symbol          string   `json:"symbol"`
	Volume24h       float64  `json:"volume_24h"`
	OpenInterest    float64  `json:"open_interest"`
	EstFundingRate  *float64 `json:"est_funding_rate"`
	LastFundingRate *float64 `json:"last_funding_rate"`
}

type PerpStats struct {
	Stats
	MarkPrice      *float64 `json:"mark_price"`
	IndexPrice     *float64 `json:"index_price"`
	BasisBps       *float64 `json:"basis_bps"`
	LongShortRatio *float64 `json:"long_short_ratio"`
	LiqVolume24h   float64  `json:"liq_volume_24h"`
}

type SpotTrade struct {
	ID        string  `json:"id"`
	Side      string  `json:"side"`
	Price     float64 `json:"price"`
	Size      float64 `json:"size"`
	TradeTime int64   `json:"trade_time"`
}

type PerpTrade struct {
	ID          string  `json:"id"`
	Side        string  `json:"side"`
	Price       float64 `json:"price"`
	Contracts   float64 `json:"contracts"`
	Notional    float64 `json:"notional"`
	Liquidation bool    `json:"liquidation"`
	TradeTime   int64   `json:"trade_time"`
}

type Trades[T any] struct {
	Symbol string `json:"symbol"`
	Count  int    `json:"count"`
	Trades []T    `json:"trades"`
}

type Health struct {
	Status   string  `json:"status"`
	Backfill string  `json:"backfill"`
	LastFill *string `json:"lastFill"`
	Symbol   string  `json:"symbol"`
}

// LiquidationFill is true when the fill shares a log group with a
// Liquidation of its taker in the same market: the slice's reduce-only market
// order (contracts/perps.md section 14) is the taker order of the last fill of
// that account and market before the Liquidation log in the receipt, and only
// fills of that order belong to the group. Other fills of the same account and
// market in the receipt, such as EndBlock triggered orders, do not.
const LiquidationFill = `EXISTS (SELECT 1 FROM perps.liquidations l
	WHERE l.block = f.block AND l.tx_index = f.tx_index AND l.market_id = f.market_id AND l."user" = f.taker
		AND l.log_index > f.log_index
		AND f.taker_order_id = (SELECT g.taker_order_id FROM perps.fills g
			WHERE g.block = l.block AND g.tx_index = l.tx_index AND g.market_id = l.market_id AND g.taker = l."user"
				AND g.log_index < l.log_index
			ORDER BY g.log_index DESC, g.sub DESC LIMIT 1))`

// latestMarkSQL reads the latest mark of perps.mark_prices and the latest index
// of either a MarkPriceUpdated row or a successful pricefeed aggregate: the
// pricefeed emits an aggregate for every updated aggregate, while perps logs
// MarkPriceUpdated only when its mark changes (contracts/pricefeed.md, Events).
const latestMarkSQL = `WITH m AS (
	SELECT mark_price FROM perps.mark_prices WHERE market_id = $1
	ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1
), i AS (
	SELECT index_price, block_time FROM (
		(SELECT block, tx_index, log_index, sub, index_price, block_time FROM perps.mark_prices WHERE market_id = $1
			ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1)
		UNION ALL
		(SELECT block, tx_index, log_index, sub, index_price, block_time FROM perps.index_prices
			WHERE market_id = $1 AND kind = 'aggregate' AND index_price IS NOT NULL
			ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1)
	) u ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1
)
SELECT m.mark_price / $2::numeric, i.index_price / $2::numeric,
	round((m.mark_price - i.index_price) * 10000 / NULLIF(i.index_price, 0), 3), i.block_time
	FROM i LEFT JOIN m ON true`

const fillWindowSQL = `SELECT (array_agg(f.price ORDER BY f.block, f.tx_index, f.log_index, f.sub))[1] / $3::numeric,
	max(f.price) / $3::numeric, min(f.price) / $3::numeric,
	COALESCE(sum(f.size_usd), 0) / $3::numeric,
	COALESCE(sum(f.size_usd) FILTER (WHERE ` + LiquidationFill + `), 0) / $3::numeric
	FROM perps.fills f WHERE f.market_id = $1 AND f.block_time >= $2`

const marketStateSQL = `SELECT
	(SELECT long_oi / $2::numeric FROM perps.open_interest WHERE market_id = $1
		ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1),
	(SELECT short_oi / $2::numeric FROM perps.open_interest WHERE market_id = $1
		ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1),
	(SELECT new_rate_per_second * $3::numeric / $2::numeric FROM perps.funding_rates WHERE market_id = $1
		ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1),
	(SELECT funding_rate / $2::numeric FROM perps.funding_settlements WHERE market_id = $1
		ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1)`

type latestMark struct {
	mark, index, basis *float64
	at                 *time.Time
}

func (s *Symbol) latestMark(ctx context.Context, market int64) (latestMark, error) {
	var (
		mark, index, basis sql.NullString
		at                 time.Time
		out                latestMark
	)
	err := s.db.QueryRowContext(ctx, latestMarkSQL, market, USDScale).Scan(&mark, &index, &basis, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if out.mark, err = Decimal(mark); err != nil {
		return out, err
	}
	if out.index, err = Decimal(index); err != nil {
		return out, err
	}
	if out.basis, err = Decimal(basis); err != nil {
		return out, err
	}
	out.at = &at
	return out, nil
}

type fillWindow struct {
	open, high, low   *float64
	volume, liqVolume float64
}

func (s *Symbol) fillWindow(ctx context.Context, market int64) (fillWindow, error) {
	var (
		open, high, low, volume, liqVolume sql.NullString
		out                                fillWindow
	)
	since := time.Now().Add(-Window24h)
	if err := s.db.QueryRowContext(ctx, fillWindowSQL, market, since, USDScale).Scan(&open, &high, &low, &volume, &liqVolume); err != nil {
		return out, err
	}
	values := []struct {
		raw sql.NullString
		dst **float64
	}{{open, &out.open}, {high, &out.high}, {low, &out.low}}
	for _, v := range values {
		parsed, err := Decimal(v.raw)
		if err != nil {
			return out, err
		}
		*v.dst = parsed
	}
	var err error
	if out.volume, err = DecimalOrZero(volume); err != nil {
		return out, err
	}
	out.liqVolume, err = DecimalOrZero(liqVolume)
	return out, err
}

type marketState struct {
	longOI, shortOI *float64
	estRate         *float64
	lastRate        *float64
}

func (s *Symbol) marketState(ctx context.Context, market int64) (marketState, error) {
	var (
		raw [4]sql.NullString
		out marketState
	)
	if err := s.db.QueryRowContext(ctx, marketStateSQL, market, USDScale, SecondsPerHour).Scan(&raw[0], &raw[1], &raw[2], &raw[3]); err != nil {
		return out, err
	}
	dst := [4]**float64{&out.longOI, &out.shortOI, &out.estRate, &out.lastRate}
	for i := range raw {
		parsed, err := Decimal(raw[i])
		if err != nil {
			return out, err
		}
		*dst[i] = parsed
	}
	return out, nil
}

func (s *Symbol) Price(ctx context.Context, symbol string) (Price, error) {
	market, ok := MarketIDs[symbol]
	if !ok {
		return Price{}, ErrUnknownSymbol
	}
	body := Price{Symbol: symbol}
	mark, err := s.latestMark(ctx, market)
	if err != nil {
		return body, err
	}
	window, err := s.fillWindow(ctx, market)
	if err != nil {
		return body, err
	}
	body.Price = mark.index
	if mark.at != nil {
		ts := mark.at.UTC().Format(ISOMillis)
		body.Timestamp = &ts
	}
	body.Open, body.High, body.Low = window.open, window.high, window.low
	return body, nil
}

func statsFrom(symbol string, window fillWindow, state marketState) Stats {
	oi := 0.0
	if state.longOI != nil {
		oi += *state.longOI
	}
	if state.shortOI != nil {
		oi += *state.shortOI
	}
	return Stats{
		Symbol:          symbol,
		Volume24h:       window.volume,
		OpenInterest:    oi,
		EstFundingRate:  state.estRate,
		LastFundingRate: state.lastRate,
	}
}

func (s *Symbol) Stats(ctx context.Context, symbol string) (Stats, error) {
	market, ok := MarketIDs[symbol]
	if !ok {
		return Stats{}, ErrUnknownSymbol
	}
	window, err := s.fillWindow(ctx, market)
	if err != nil {
		return Stats{}, err
	}
	state, err := s.marketState(ctx, market)
	if err != nil {
		return Stats{}, err
	}
	return statsFrom(symbol, window, state), nil
}

func (s *Symbol) PerpStats(ctx context.Context, symbol string) (PerpStats, error) {
	market, ok := MarketIDs[symbol]
	if !ok {
		return PerpStats{}, ErrUnknownSymbol
	}
	window, err := s.fillWindow(ctx, market)
	if err != nil {
		return PerpStats{}, err
	}
	state, err := s.marketState(ctx, market)
	if err != nil {
		return PerpStats{}, err
	}
	mark, err := s.latestMark(ctx, market)
	if err != nil {
		return PerpStats{}, err
	}
	body := PerpStats{
		Stats:        statsFrom(symbol, window, state),
		MarkPrice:    mark.mark,
		IndexPrice:   mark.index,
		BasisBps:     mark.basis,
		LiqVolume24h: window.liqVolume,
	}
	if state.longOI != nil && state.shortOI != nil && *state.shortOI != 0 {
		ratio := *state.longOI / *state.shortOI
		body.LongShortRatio = &ratio
	}
	return body, nil
}

const spotTokenSQL = `SELECT token FROM perps.spot_token_registry
	WHERE kind = 'token_registered' AND is_stablecoin = false
	ORDER BY block, tx_index, log_index, sub OFFSET $1 LIMIT 1`

// spotTradesSQL lists the spot trades of token $1: market orders of the token,
// simple swaps into it at the execution price, simple swaps out of it into a
// stablecoin, native swaps of PAX ($5) against any token at the other amount per
// PAX, native swaps of the token against PAX at the PAX amount per token, and
// executed limit, stop-limit and stop-loss orders of the token at their execution
// price. Buy orders escrow the stable amount, so their size is that amount over
// the price.
const spotTradesSQL = `WITH stable AS (
	SELECT token FROM perps.spot_token_registry WHERE kind = 'token_registered' AND is_stablecoin
), trades AS (
	SELECT s.block, s.tx_index, s.log_index, s.sub, s.block_time, s.is_buy AS buy,
		s.stablecoin_amount / s.token_amount AS price, s.token_amount / $3::numeric AS size
	FROM perps.spot_swaps s
	WHERE s.kind = 'market_order' AND s.token = $1 AND s.token_amount > 0
	UNION ALL
	SELECT s.block, s.tx_index, s.log_index, s.sub, s.block_time, true,
		s.execution_price / $4::numeric, s.amount_out / $3::numeric
	FROM perps.spot_swaps s
	WHERE s.kind = 'simple_swap' AND s.token_out = $1
	UNION ALL
	SELECT s.block, s.tx_index, s.log_index, s.sub, s.block_time, false,
		s.amount_out / s.amount_in, s.amount_in / $3::numeric
	FROM perps.spot_swaps s
	WHERE s.kind = 'simple_swap' AND s.token_in = $1 AND s.amount_in > 0 AND s.token_out IN (SELECT token FROM stable)
	UNION ALL
	SELECT s.block, s.tx_index, s.log_index, s.sub, s.block_time, NOT s.native_is_input,
		s.token_amount / s.native_amount, s.native_amount / $3::numeric
	FROM perps.spot_swaps s
	WHERE $5::boolean AND s.kind = 'native_swap' AND s.native_amount > 0
	UNION ALL
	SELECT s.block, s.tx_index, s.log_index, s.sub, s.block_time, s.native_is_input,
		s.native_amount / s.token_amount, s.token_amount / $3::numeric
	FROM perps.spot_swaps s
	WHERE s.kind = 'native_swap' AND s.token_other = $1 AND s.token_amount > 0
	UNION ALL
	SELECT e.block, e.tx_index, e.log_index, e.sub, e.block_time, COALESCE(c.order_type IN (0, 3), false),
		e.execution_price / $4::numeric,
		CASE WHEN COALESCE(c.order_type IN (0, 3), false) THEN c.amount * $4::numeric / e.execution_price ELSE c.amount END / $3::numeric
	FROM perps.spot_order_events e
	JOIN perps.spot_order_events c ON c.order_id = e.order_id
		AND c.kind IN ('limit_created', 'stop_limit_created', 'stop_loss_created')
	WHERE e.kind IN ('limit_executed', 'stop_limit_executed', 'stop_loss_executed') AND e.token = $1 AND e.execution_price > 0
)
SELECT block, log_index, block_time, buy, price, size FROM trades
ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT $2`

func (s *Symbol) Trades(ctx context.Context, symbol string, limit int) (Trades[SpotTrade], error) {
	if _, ok := MarketIDs[symbol]; !ok {
		return Trades[SpotTrade]{}, ErrUnknownSymbol
	}
	body := Trades[SpotTrade]{Symbol: symbol, Trades: []SpotTrade{}}
	rank, ok := SpotTokenRank[symbol]
	if !ok {
		return body, nil
	}
	var token []byte
	err := s.db.QueryRowContext(ctx, spotTokenSQL, rank).Scan(&token)
	if errors.Is(err, sql.ErrNoRows) {
		return body, nil
	}
	if err != nil {
		return body, err
	}
	rows, err := s.db.QueryContext(ctx, spotTradesSQL, token, limit, SpotScale, USDScale, symbol == "PAX")
	if err != nil {
		return body, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			block       int64
			logIndex    int
			at          time.Time
			buy         bool
			price, size string
		)
		if err := rows.Scan(&block, &logIndex, &at, &buy, &price, &size); err != nil {
			return body, err
		}
		trade := SpotTrade{ID: tradeID(block, logIndex), Side: side(buy), TradeTime: at.UnixMilli()}
		if trade.Price, err = strconv.ParseFloat(price, 64); err != nil {
			return body, err
		}
		if trade.Size, err = strconv.ParseFloat(size, 64); err != nil {
			return body, err
		}
		body.Trades = append(body.Trades, trade)
	}
	if err := rows.Err(); err != nil {
		return body, err
	}
	body.Count = len(body.Trades)
	return body, nil
}

const perpTradesSQL = `SELECT f.block, f.log_index, f.block_time, f.taker_is_long,
	f.price / $3::numeric, f.size_usd / NULLIF(f.price, 0), f.size_usd / $3::numeric, ` + LiquidationFill + `
	FROM perps.fills f WHERE f.market_id = $1
	ORDER BY f.block DESC, f.tx_index DESC, f.log_index DESC, f.sub DESC LIMIT $2`

func (s *Symbol) PerpTrades(ctx context.Context, symbol string, limit int) (Trades[PerpTrade], error) {
	market, ok := MarketIDs[symbol]
	if !ok {
		return Trades[PerpTrade]{}, ErrUnknownSymbol
	}
	body := Trades[PerpTrade]{Symbol: symbol, Trades: []PerpTrade{}}
	rows, err := s.db.QueryContext(ctx, perpTradesSQL, market, limit, USDScale)
	if err != nil {
		return body, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			block             int64
			logIndex          int
			at                time.Time
			long, liquidation bool
			price, notional   string
			contracts         sql.NullString
		)
		if err := rows.Scan(&block, &logIndex, &at, &long, &price, &contracts, &notional, &liquidation); err != nil {
			return body, err
		}
		trade := PerpTrade{ID: tradeID(block, logIndex), Side: side(long), Liquidation: liquidation, TradeTime: at.UnixMilli()}
		if trade.Price, err = strconv.ParseFloat(price, 64); err != nil {
			return body, err
		}
		if trade.Contracts, err = DecimalOrZero(contracts); err != nil {
			return body, err
		}
		if trade.Notional, err = strconv.ParseFloat(notional, 64); err != nil {
			return body, err
		}
		body.Trades = append(body.Trades, trade)
	}
	if err := rows.Err(); err != nil {
		return body, err
	}
	body.Count = len(body.Trades)
	return body, nil
}

const healthSQL = `SELECT
	(SELECT block_time FROM perps.fills WHERE market_id = $1
		ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1),
	EXISTS (SELECT 1 FROM perps.cursors WHERE name = 'head')`

func (s *Symbol) Health(ctx context.Context, symbol string) (Health, error) {
	market, ok := MarketIDs[symbol]
	if !ok {
		return Health{}, ErrUnknownSymbol
	}
	var (
		last   sql.NullTime
		cursor bool
	)
	if err := s.db.QueryRowContext(ctx, healthSQL, market).Scan(&last, &cursor); err != nil {
		return Health{}, err
	}
	body := Health{Status: "ok", Backfill: "pending", Symbol: symbol}
	if cursor {
		body.Backfill = "complete"
	}
	if last.Valid {
		ts := last.Time.UTC().Format(ISOMillis)
		body.LastFill = &ts
	}
	return body, nil
}

func tradeID(block int64, logIndex int) string {
	return strconv.FormatInt(block, 10) + "-" + strconv.Itoa(logIndex)
}

func side(buy bool) string {
	if buy {
		return "BUY"
	}
	return "SELL"
}

func Decimal(raw sql.NullString) (*float64, error) {
	if !raw.Valid {
		return nil, nil
	}
	v, err := strconv.ParseFloat(raw.String, 64)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func DecimalOrZero(raw sql.NullString) (float64, error) {
	v, err := Decimal(raw)
	if err != nil || v == nil {
		return 0, err
	}
	return *v, nil
}
