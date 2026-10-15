// Package rest serves the REST routes of the perps gateway. The symbol routes
// follow the application data protocol under /api/<symbol>/ (contracts/data-services.md
// section 9); chart candles are not served here and stay on the Chainflow feed
// (contracts/data-services.md section 12).
package rest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// marketIDs maps the eighteen symbols of the application feed list
// (Hyperpax:src/core/feeds.ts:4) to their market ids (contracts/perps-markets.md
// section 2).
var marketIDs = map[string]int64{
	"BTC": 0, "ETH": 1, "SOL": 2, "AVAX": 3, "LINK": 4, "TSLA": 5, "NVDA": 6, "NAS100": 7, "XAU": 8,
	"SPX500": 9, "GOOGL": 10, "PAX": 11, "SID": 12, "HYPE": 13, "XRP": 14, "ASTER": 15, "TRUMP": 16, "BNB": 17,
}

// spotTokenRank is the position of a spot token among the registered
// non-stablecoin tokens of 0x1025, which InitV6120 registers as USDL, PAX, SID
// (contracts/spot.md, Opening pairs).
var spotTokenRank = map[string]int{"PAX": 0, "SID": 1}

const (
	// usdScale is 10^18: perps prices, USD notionals and rates are 18-decimal
	// fixed point (contracts/perps.md section 2), as are spot execution prices
	// (contracts/spot.md, Pricing).
	usdScale = "1000000000000000000"
	// spotScale is 10^6: USDL, PAX and SID amounts carry 6 decimals
	// (contracts/spot.md, Token model).
	spotScale = "1000000"
	// secondsPerHour turns a per-second funding rate into the hourly rate.
	secondsPerHour = 3600

	defaultLimit = 100
	maxLimit     = 1000
	window24h    = 24 * time.Hour
	isoMillis    = "2006-01-02T15:04:05.000Z"
)

// Symbol serves the symbol REST routes from schema perps.
type Symbol struct {
	db *sql.DB
}

// NewSymbol returns the symbol routes reading db.
func NewSymbol(db *sql.DB) (*Symbol, error) {
	if db == nil {
		return nil, errors.New("rest: nil database")
	}
	return &Symbol{db: db}, nil
}

// Register mounts the symbol routes under /api/<symbol>/. One wildcard pattern
// carries every route so that the fixed /api/markets/... patterns of the
// gateway stay strictly more specific than it.
func (s *Symbol) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/{symbol}/{route...}", s.serve)
}

func (s *Symbol) serve(w http.ResponseWriter, r *http.Request) {
	path := r.PathValue("symbol")
	symbol := strings.ToUpper(path)
	market, ok := marketIDs[symbol]
	if !ok {
		writeError(w, http.StatusNotFound, "Unknown symbol: "+symbol)
		return
	}
	if path != strings.ToLower(path) {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	name, arg, hasArg := strings.Cut(r.PathValue("route"), "/")
	if hasArg && name != "trades" && name != "perp_trades" {
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	if hasArg && strings.ToUpper(arg) != symbol {
		writeError(w, http.StatusNotFound, "Unknown symbol: "+strings.ToUpper(arg))
		return
	}
	ctx := r.Context()
	var (
		body  any
		err   error
		cache bool
	)
	switch name {
	case "price":
		body, err = s.price(ctx, symbol, market)
		cache = true
	case "stats":
		body, err = s.stats(ctx, symbol, market)
		cache = true
	case "perp_stats":
		body, err = s.perpStats(ctx, symbol, market)
	case "trades", "perp_trades":
		limit, lerr := parseLimit(r.URL.Query())
		if lerr != nil {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		if name == "trades" {
			body, err = s.trades(ctx, symbol, limit)
		} else {
			body, err = s.perpTrades(ctx, symbol, market, limit)
		}
	case "health":
		body, err = s.health(ctx, symbol, market)
	default:
		writeError(w, http.StatusNotFound, "Not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if cache {
		w.Header().Set("Cache-Control", "max-age=1")
	}
	writeJSON(w, http.StatusOK, body)
}

// parseLimit reads limit, clamped to [1, 1000] with default 100
// (Hyperpax:data_api.md:154).
func parseLimit(q map[string][]string) (int, error) {
	raw, ok := q["limit"]
	if !ok || len(raw) == 0 || raw[0] == "" {
		return defaultLimit, nil
	}
	n, err := strconv.ParseInt(raw[0], 10, 64)
	if err != nil {
		var numErr *strconv.NumError
		if !errors.As(err, &numErr) || !errors.Is(numErr.Err, strconv.ErrRange) {
			return 0, err
		}
		if strings.HasPrefix(raw[0], "-") {
			return 1, nil
		}
		return maxLimit, nil
	}
	return int(min(max(n, 1), maxLimit)), nil
}

type priceBody struct {
	Symbol    string   `json:"symbol"`
	Price     *float64 `json:"price"`
	Timestamp *string  `json:"timestamp"`
	Open      *float64 `json:"open"`
	High      *float64 `json:"high"`
	Low       *float64 `json:"low"`
}

type statsBody struct {
	Symbol          string   `json:"symbol"`
	Volume24h       float64  `json:"volume_24h"`
	OpenInterest    float64  `json:"open_interest"`
	EstFundingRate  *float64 `json:"est_funding_rate"`
	LastFundingRate *float64 `json:"last_funding_rate"`
}

type perpStatsBody struct {
	statsBody
	MarkPrice      *float64 `json:"mark_price"`
	IndexPrice     *float64 `json:"index_price"`
	BasisBps       *float64 `json:"basis_bps"`
	LongShortRatio *float64 `json:"long_short_ratio"`
	LiqVolume24h   float64  `json:"liq_volume_24h"`
}

type spotTrade struct {
	ID        string  `json:"id"`
	Side      string  `json:"side"`
	Price     float64 `json:"price"`
	Size      float64 `json:"size"`
	TradeTime int64   `json:"trade_time"`
}

type perpTrade struct {
	ID          string  `json:"id"`
	Side        string  `json:"side"`
	Price       float64 `json:"price"`
	Contracts   float64 `json:"contracts"`
	Notional    float64 `json:"notional"`
	Liquidation bool    `json:"liquidation"`
	TradeTime   int64   `json:"trade_time"`
}

type tradesBody[T any] struct {
	Symbol string `json:"symbol"`
	Count  int    `json:"count"`
	Trades []T    `json:"trades"`
}

type healthBody struct {
	Status   string  `json:"status"`
	Backfill string  `json:"backfill"`
	LastFill *string `json:"lastFill"`
	Symbol   string  `json:"symbol"`
}

// liquidationFill is true when a Liquidation of the fill's taker in the same
// market is in the same receipt as the fill: the liquidated account's
// reduce-only market order is the taker of the slice (contracts/perps.md
// section 14).
const liquidationFill = `EXISTS (SELECT 1 FROM perps.liquidations l
	WHERE l.block = f.block AND l.tx_index = f.tx_index AND l.market_id = f.market_id AND l."user" = f.taker)`

const latestMarkSQL = `SELECT mark_price / $2::numeric, index_price / $2::numeric,
	round((mark_price - index_price) * 10000 / NULLIF(index_price, 0), 3), block_time
	FROM perps.mark_prices WHERE market_id = $1
	ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1`

const fillWindowSQL = `SELECT (array_agg(f.price ORDER BY f.block, f.tx_index, f.log_index, f.sub))[1] / $3::numeric,
	max(f.price) / $3::numeric, min(f.price) / $3::numeric,
	COALESCE(sum(f.size_usd), 0) / $3::numeric,
	COALESCE(sum(f.size_usd) FILTER (WHERE ` + liquidationFill + `), 0) / $3::numeric
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
	err := s.db.QueryRowContext(ctx, latestMarkSQL, market, usdScale).Scan(&mark, &index, &basis, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if out.mark, err = decimal(mark); err != nil {
		return out, err
	}
	if out.index, err = decimal(index); err != nil {
		return out, err
	}
	if out.basis, err = decimal(basis); err != nil {
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
	since := time.Now().Add(-window24h)
	if err := s.db.QueryRowContext(ctx, fillWindowSQL, market, since, usdScale).Scan(&open, &high, &low, &volume, &liqVolume); err != nil {
		return out, err
	}
	values := []struct {
		raw sql.NullString
		dst **float64
	}{{open, &out.open}, {high, &out.high}, {low, &out.low}}
	for _, v := range values {
		parsed, err := decimal(v.raw)
		if err != nil {
			return out, err
		}
		*v.dst = parsed
	}
	var err error
	if out.volume, err = decimalOrZero(volume); err != nil {
		return out, err
	}
	out.liqVolume, err = decimalOrZero(liqVolume)
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
	if err := s.db.QueryRowContext(ctx, marketStateSQL, market, usdScale, secondsPerHour).Scan(&raw[0], &raw[1], &raw[2], &raw[3]); err != nil {
		return out, err
	}
	dst := [4]**float64{&out.longOI, &out.shortOI, &out.estRate, &out.lastRate}
	for i := range raw {
		parsed, err := decimal(raw[i])
		if err != nil {
			return out, err
		}
		*dst[i] = parsed
	}
	return out, nil
}

func (s *Symbol) price(ctx context.Context, symbol string, market int64) (priceBody, error) {
	body := priceBody{Symbol: symbol}
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
		ts := mark.at.UTC().Format(isoMillis)
		body.Timestamp = &ts
	}
	body.Open, body.High, body.Low = window.open, window.high, window.low
	return body, nil
}

func statsFrom(symbol string, window fillWindow, state marketState) statsBody {
	oi := 0.0
	if state.longOI != nil {
		oi += *state.longOI
	}
	if state.shortOI != nil {
		oi += *state.shortOI
	}
	return statsBody{
		Symbol:          symbol,
		Volume24h:       window.volume,
		OpenInterest:    oi,
		EstFundingRate:  state.estRate,
		LastFundingRate: state.lastRate,
	}
}

func (s *Symbol) stats(ctx context.Context, symbol string, market int64) (statsBody, error) {
	window, err := s.fillWindow(ctx, market)
	if err != nil {
		return statsBody{}, err
	}
	state, err := s.marketState(ctx, market)
	if err != nil {
		return statsBody{}, err
	}
	return statsFrom(symbol, window, state), nil
}

func (s *Symbol) perpStats(ctx context.Context, symbol string, market int64) (perpStatsBody, error) {
	window, err := s.fillWindow(ctx, market)
	if err != nil {
		return perpStatsBody{}, err
	}
	state, err := s.marketState(ctx, market)
	if err != nil {
		return perpStatsBody{}, err
	}
	mark, err := s.latestMark(ctx, market)
	if err != nil {
		return perpStatsBody{}, err
	}
	body := perpStatsBody{
		statsBody:    statsFrom(symbol, window, state),
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

func (s *Symbol) trades(ctx context.Context, symbol string, limit int) (tradesBody[spotTrade], error) {
	body := tradesBody[spotTrade]{Symbol: symbol, Trades: []spotTrade{}}
	rank, ok := spotTokenRank[symbol]
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
	rows, err := s.db.QueryContext(ctx, spotTradesSQL, token, limit, spotScale, usdScale, symbol == "PAX")
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
		trade := spotTrade{ID: tradeID(block, logIndex), Side: side(buy), TradeTime: at.UnixMilli()}
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
	f.price / $3::numeric, f.size_usd / NULLIF(f.price, 0), f.size_usd / $3::numeric, ` + liquidationFill + `
	FROM perps.fills f WHERE f.market_id = $1
	ORDER BY f.block DESC, f.tx_index DESC, f.log_index DESC, f.sub DESC LIMIT $2`

func (s *Symbol) perpTrades(ctx context.Context, symbol string, market int64, limit int) (tradesBody[perpTrade], error) {
	body := tradesBody[perpTrade]{Symbol: symbol, Trades: []perpTrade{}}
	rows, err := s.db.QueryContext(ctx, perpTradesSQL, market, limit, usdScale)
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
		trade := perpTrade{ID: tradeID(block, logIndex), Side: side(long), Liquidation: liquidation, TradeTime: at.UnixMilli()}
		if trade.Price, err = strconv.ParseFloat(price, 64); err != nil {
			return body, err
		}
		if trade.Contracts, err = decimalOrZero(contracts); err != nil {
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

func (s *Symbol) health(ctx context.Context, symbol string, market int64) (healthBody, error) {
	var (
		last   sql.NullTime
		cursor bool
	)
	if err := s.db.QueryRowContext(ctx, healthSQL, market).Scan(&last, &cursor); err != nil {
		return healthBody{}, err
	}
	body := healthBody{Status: "ok", Backfill: "pending", Symbol: symbol}
	if cursor {
		body.Backfill = "complete"
	}
	if last.Valid {
		ts := last.Time.UTC().Format(isoMillis)
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

func decimal(raw sql.NullString) (*float64, error) {
	if !raw.Valid {
		return nil, nil
	}
	v, err := strconv.ParseFloat(raw.String, 64)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func decimalOrZero(raw sql.NullString) (float64, error) {
	v, err := decimal(raw)
	if err != nil || v == nil {
		return 0, err
	}
	return *v, nil
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	raw, err := json.Marshal(body)
	if err != nil {
		status = http.StatusInternalServerError
		raw = []byte(`{"error":"internal error"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}
