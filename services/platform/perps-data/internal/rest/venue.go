package rest

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/feeds"
)

type Venue struct {
	db *sql.DB
}

func NewVenue(db *sql.DB) (*Venue, error) {
	if db == nil {
		return nil, errors.New("rest: nil database")
	}
	return &Venue{db: db}, nil
}

func (v *Venue) Register(mux *http.ServeMux) {
	for _, prefix := range []string{"/api", "/api/markets"} {
		mux.HandleFunc("GET "+prefix+"/orderbook/{symbol}", v.orderbook)
		mux.HandleFunc("GET "+prefix+"/open-interest", v.openInterest)
		mux.HandleFunc("GET "+prefix+"/ticker/{symbol}", v.ticker)
		mux.HandleFunc("GET "+prefix+"/tickers", v.tickers)
	}
}

type venueResponse struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

func venueUnavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, venueResponse{})
}

func venueUnknown(w http.ResponseWriter, symbol string) {
	writeJSON(w, http.StatusNotFound, venueResponse{Error: "Unknown symbol: " + symbol})
}

func (v *Venue) market(w http.ResponseWriter, r *http.Request) (string, bool) {
	symbol := strings.ToUpper(r.PathValue("symbol"))
	var market string
	err := v.db.QueryRowContext(r.Context(), `SELECT market_id::text FROM perps.markets WHERE symbol = $1`, symbol).Scan(&market)
	if errors.Is(err, sql.ErrNoRows) {
		venueUnknown(w, symbol)
		return "", false
	}
	if err != nil {
		venueUnavailable(w)
		return "", false
	}
	return market, true
}

type venueLevel struct {
	Price    float64 `json:"price"`
	Quantity float64 `json:"quantity"`
}

type venueBook struct {
	Asks []venueLevel `json:"asks"`
	Bids []venueLevel `json:"bids"`
}

func venueDepth(raw string) (int, error) {
	if raw == "" {
		return 20, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, err
	}
	return int(min(max(n, 1), 100)), nil
}

func (v *Venue) orderbook(w http.ResponseWriter, r *http.Request) {
	market, ok := v.market(w, r)
	if !ok {
		return
	}
	depth, err := venueDepth(r.URL.Query().Get("depth"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, venueResponse{Error: "invalid depth"})
		return
	}
	book, err := feeds.LoadBook(r.Context(), v.db, market)
	if err != nil {
		venueUnavailable(w)
		return
	}
	asks, bids := book.NotionalLevels(depth)
	levels := func(side []feeds.Level) ([]venueLevel, error) {
		out := make([]venueLevel, 0, min(len(side), depth))
		for _, level := range side[:min(len(side), depth)] {
			if !venueFinite(level[0]) || !venueFinite(level[1]) || level[0] <= 0 || level[1] <= 0 {
				return nil, errors.New("rest: invalid book level")
			}
			out = append(out, venueLevel{Price: level[0], Quantity: level[1]})
		}
		return out, nil
	}
	var body venueBook
	if body.Asks, err = levels(asks); err != nil {
		venueUnavailable(w)
		return
	}
	if body.Bids, err = levels(bids); err != nil {
		venueUnavailable(w)
		return
	}
	writeJSON(w, http.StatusOK, venueResponse{Success: true, Data: body})
}

type venueOI struct {
	PaxeerSymbol string  `json:"paxeerSymbol"`
	LongOI       float64 `json:"long_oi"`
	ShortOI      float64 `json:"short_oi"`
}

const venueOpenInterestSQL = `SELECT m.symbol, COALESCE(oi.long_oi, 0) / $1::numeric,
    COALESCE(oi.short_oi, 0) / $1::numeric
FROM perps.markets m
LEFT JOIN LATERAL (SELECT long_oi, short_oi FROM perps.open_interest WHERE market_id = m.market_id
    ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1) oi ON true
ORDER BY m.market_id`

func (v *Venue) openInterest(w http.ResponseWriter, r *http.Request) {
	rows, err := v.db.QueryContext(r.Context(), venueOpenInterestSQL, usdScale)
	if err != nil {
		venueUnavailable(w)
		return
	}
	defer rows.Close()
	data := []venueOI{}
	for rows.Next() {
		var entry venueOI
		if err := rows.Scan(&entry.PaxeerSymbol, &entry.LongOI, &entry.ShortOI); err != nil ||
			!venueFinite(entry.LongOI) || !venueFinite(entry.ShortOI) {
			venueUnavailable(w)
			return
		}
		data = append(data, entry)
	}
	if rows.Err() != nil {
		venueUnavailable(w)
		return
	}
	writeJSON(w, http.StatusOK, venueResponse{Success: true, Data: data})
}

type venueTicker struct {
	Symbol          string  `json:"symbol"`
	IndexPrice      float64 `json:"index_price"`
	MarkPrice       float64 `json:"mark_price"`
	EstFundingRate  float64 `json:"est_funding_rate"`
	LastFundingRate float64 `json:"last_funding_rate"`
	OpenInterest    float64 `json:"open_interest"`
	Volume24h       float64 `json:"volume_24h"`
}

type venueTickerEntry struct {
	Symbol          string  `json:"symbol"`
	PaxeerSymbol    string  `json:"paxeerSymbol"`
	MarkPrice       float64 `json:"mark_price"`
	IndexPrice      float64 `json:"index_price"`
	OpenInterest    float64 `json:"open_interest"`
	EstFundingRate  float64 `json:"est_funding_rate"`
	LastFundingRate float64 `json:"last_funding_rate"`
	Open24h         float64 `json:"24h_open"`
	Close24h        float64 `json:"24h_close"`
	High24h         float64 `json:"24h_high"`
	Low24h          float64 `json:"24h_low"`
	Amount24h       float64 `json:"24h_amount"`
	Volume24h       float64 `json:"24h_volume"`
}

const venueTickersSQL = `SELECT m.symbol,
    COALESCE(mp.mark_price, 0) / $1::numeric, COALESCE(mp.index_price, 0) / $1::numeric,
    (COALESCE(oi.long_oi, 0) + COALESCE(oi.short_oi, 0)) / $1::numeric,
    COALESCE(fr.funding_rate24h, 0) / $1::numeric,
    COALESCE(fr.new_rate_per_second, 0) * $2::numeric / $1::numeric,
    COALESCE(s.open_24h, 0) / $1::numeric, COALESCE(s.last_price, 0) / $1::numeric,
    COALESCE(s.high_24h, 0) / $1::numeric, COALESCE(s.low_24h, 0) / $1::numeric,
    COALESCE(amount.contracts, 0), COALESCE(s.volume_24h, 0) / $1::numeric
FROM perps.markets m
LEFT JOIN LATERAL (SELECT mark_price, index_price FROM perps.mark_prices WHERE market_id = m.market_id
    ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1) mp ON true
LEFT JOIN LATERAL (SELECT new_rate_per_second, funding_rate24h FROM perps.funding_rates WHERE market_id = m.market_id
    ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1) fr ON true
LEFT JOIN LATERAL (SELECT long_oi, short_oi FROM perps.open_interest WHERE market_id = m.market_id
    ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1) oi ON true
LEFT JOIN perps.market_stats_rolling s ON s.market_id = m.market_id
LEFT JOIN LATERAL (SELECT sum(size_usd / price) AS contracts FROM perps.fills
    WHERE market_id = m.market_id AND price > 0 AND block_time > CURRENT_TIMESTAMP - interval '24 hours') amount ON true
WHERE ($3 = '' OR m.symbol = $3)
ORDER BY m.market_id`

func venueFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func (v *Venue) readTickers(ctx context.Context, symbol string) ([]venueTickerEntry, error) {
	rows, err := v.db.QueryContext(ctx, venueTickersSQL, usdScale, secondsPerHour, symbol)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	data := []venueTickerEntry{}
	for rows.Next() {
		var entry venueTickerEntry
		values := []*float64{&entry.MarkPrice, &entry.IndexPrice, &entry.OpenInterest,
			&entry.EstFundingRate, &entry.LastFundingRate, &entry.Open24h, &entry.Close24h,
			&entry.High24h, &entry.Low24h, &entry.Amount24h, &entry.Volume24h}
		dest := []any{&entry.PaxeerSymbol}
		for _, value := range values {
			dest = append(dest, value)
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		for _, value := range values {
			if !venueFinite(*value) {
				return nil, errors.New("rest: invalid ticker number")
			}
		}
		entry.Symbol = "PERP_" + entry.PaxeerSymbol + "_USDC"
		data = append(data, entry)
	}
	return data, rows.Err()
}

func (v *Venue) ticker(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(r.PathValue("symbol"))
	data, err := v.readTickers(r.Context(), symbol)
	if err != nil {
		venueUnavailable(w)
		return
	}
	if len(data) == 0 {
		venueUnknown(w, symbol)
		return
	}
	entry := data[0]
	body := venueTicker{Symbol: entry.PaxeerSymbol, IndexPrice: entry.IndexPrice, MarkPrice: entry.MarkPrice,
		EstFundingRate: entry.EstFundingRate, LastFundingRate: entry.LastFundingRate,
		OpenInterest: entry.OpenInterest, Volume24h: entry.Volume24h}
	writeJSON(w, http.StatusOK, venueResponse{Success: true, Data: body})
}

func (v *Venue) tickers(w http.ResponseWriter, r *http.Request) {
	data, err := v.readTickers(r.Context(), "")
	if err != nil {
		venueUnavailable(w)
		return
	}
	writeJSON(w, http.StatusOK, venueResponse{Success: true, Data: data})
}
