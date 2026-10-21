package rest

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/feeds"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/records"
)

// gatewayService is the service name /api/markets/health reports.
const gatewayService = "perps-gateway"

// MarketRecord and Totals are the market record and platform totals of
// contracts/data-services.md section 11, shared with the socket topics.
type (
	MarketRecord = records.MarketRecord
	Totals       = records.Totals
)

// Markets serves the markets table, the platform totals and the markets
// health route from schema perps.
type Markets struct {
	db    *sql.DB
	now   func() time.Time
	start time.Time
}

// NewMarkets returns the markets routes reading db; now is the clock, time.Now
// when nil, and the uptime counts from this call.
func NewMarkets(db *sql.DB, now func() time.Time) (*Markets, error) {
	if db == nil {
		return nil, errors.New("rest: nil database")
	}
	if now == nil {
		now = time.Now
	}
	return &Markets{db: db, now: now, start: now()}, nil
}

// Register mounts the markets routes. /api/markets/health and the other fixed
// /api/markets/<name> routes are literal patterns, which the mux prefers over
// /api/markets/{symbol}.
func (m *Markets) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/markets", m.serveAll)
	mux.HandleFunc("GET /api/markets/health", m.serveHealth)
	mux.HandleFunc("GET /api/markets/{symbol}", m.serveOne)
	mux.HandleFunc("GET /api/totals", m.serveTotals)
}

type marketsBody struct {
	TS      int64          `json:"ts"`
	Markets []MarketRecord `json:"markets"`
}

type marketBody struct {
	TS     int64        `json:"ts"`
	Market MarketRecord `json:"market"`
}

type totalsBody struct {
	TS     int64  `json:"ts"`
	Totals Totals `json:"totals"`
}

type marketsHealthBody struct {
	OK        bool   `json:"ok"`
	Service   string `json:"service"`
	Symbols   int64  `json:"symbols"`
	UptimeSec int64  `json:"uptimeSec"`
}

func (m *Markets) serveAll(w http.ResponseWriter, r *http.Request) {
	records, err := m.Records(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, marketsBody{TS: m.now().UnixMilli(), Markets: records})
}

func (m *Markets) serveOne(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(r.PathValue("symbol"))
	record, ok, err := m.Record(r.Context(), symbol)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "Unknown symbol: "+symbol)
		return
	}
	writeJSON(w, http.StatusOK, marketBody{TS: m.now().UnixMilli(), Market: record})
}

func (m *Markets) serveTotals(w http.ResponseWriter, r *http.Request) {
	totals, err := m.Totals(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, totalsBody{TS: m.now().UnixMilli(), Totals: totals})
}

func (m *Markets) serveHealth(w http.ResponseWriter, r *http.Request) {
	var symbols int64
	if err := m.db.QueryRowContext(r.Context(), `SELECT count(*) FROM perps.markets`).Scan(&symbols); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, marketsHealthBody{
		OK:        true,
		Service:   gatewayService,
		Symbols:   symbols,
		UptimeSec: int64(m.now().Sub(m.start) / time.Second),
	})
}

// marketRecordsSQL builds the records of perps.markets ($4 null) or of the
// market whose symbol is $3: windows, open interest and the 24 hour change of
// the last fill price against the first fill of the window from
// perps.market_stats_rolling, mark, index and basis from the latest
// MarkPriceUpdated, the settled hourly rate and eight times it of the latest
// FundingSettled as exact decimal strings, the 24 hour contract and liquidation volumes from the
// fills since $2, and the open interest in contracts from the open positions.
const marketRecordsSQL = `SELECT m.symbol, m.name,
	(s.last_price - s.open_24h) / NULLIF(s.open_24h, 0),
	COALESCE(s.volume_24h, 0) / $1::numeric, COALESCE(s.volume_7d, 0) / $1::numeric, COALESCE(s.volume_30d, 0) / $1::numeric,
	COALESCE(s.trades_count, 0),
	COALESCE(s.open_interest_long, 0) / $1::numeric, COALESCE(s.open_interest_short, 0) / $1::numeric,
	mp.mark_price / $1::numeric, mp.index_price / $1::numeric,
	round((mp.mark_price - mp.index_price) * 10000 / NULLIF(mp.index_price, 0), 3), mp.block_time,
	fs.hourly, fs.eight,
	COALESCE(w.volume, 0) / $1::numeric, COALESCE(w.contracts, 0), COALESCE(w.liq_volume, 0) / $1::numeric,
	` + feeds.OIContractsSQL + `
FROM perps.markets m
LEFT JOIN perps.market_stats_rolling s ON s.market_id = m.market_id
LEFT JOIN LATERAL (SELECT mark_price, index_price, block_time FROM perps.mark_prices WHERE market_id = m.market_id
	ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1) mp ON true
LEFT JOIN LATERAL (SELECT ` + records.LastFundingRateHourlySQL + ` AS hourly, ` + records.Funding8hSQL + ` AS eight
	FROM perps.funding_settlements WHERE market_id = m.market_id
	ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1) fs ON true
CROSS JOIN LATERAL (SELECT sum(f.size_usd) AS volume, sum(f.size_usd / NULLIF(f.price, 0)) AS contracts,
	sum(f.size_usd) FILTER (WHERE ` + liquidationFill + `) AS liq_volume
	FROM perps.fills f WHERE f.market_id = m.market_id AND f.block_time >= $2) w
WHERE $3::text IS NULL OR upper(m.symbol) = $3
ORDER BY m.market_id`

// Records returns one record per row of perps.markets in market id order.
func (m *Markets) Records(ctx context.Context) ([]MarketRecord, error) {
	return m.records(ctx, nil)
}

// Record returns the record of symbol, matched case-insensitively; ok is false
// when perps.markets has no such symbol.
func (m *Markets) Record(ctx context.Context, symbol string) (MarketRecord, bool, error) {
	upper := strings.ToUpper(symbol)
	records, err := m.records(ctx, &upper)
	if err != nil || len(records) == 0 {
		return MarketRecord{}, false, err
	}
	return records[0], true, nil
}

func (m *Markets) records(ctx context.Context, symbol *string) ([]MarketRecord, error) {
	now := m.now()
	rows, err := m.db.QueryContext(ctx, marketRecordsSQL, usdScale, now.Add(-window24h), symbol)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nextFunding := now.UTC().Truncate(time.Hour).Add(time.Hour).UnixMilli()
	out := []MarketRecord{}
	for rows.Next() {
		var (
			rec                                       = MarketRecord{PerpNextFundingMs: nextFunding}
			change, mark, index, basis, hourly, eight sql.NullString
			vol24, vol7, vol30, longOI, shortOI       sql.NullString
			fillVolume, fillContracts, liqVolume      sql.NullString
			oiContracts                               sql.NullString
			markAt                                    sql.NullTime
		)
		if err := rows.Scan(&rec.Symbol, &rec.SymbolFull, &change, &vol24, &vol7, &vol30, &rec.TradesCount,
			&longOI, &shortOI, &mark, &index, &basis, &markAt, &hourly, &eight,
			&fillVolume, &fillContracts, &liqVolume, &oiContracts); err != nil {
			return nil, err
		}
		nullable := []struct {
			raw sql.NullString
			dst **float64
		}{
			{change, &rec.Change24h}, {mark, &rec.PerpMarkPrice}, {index, &rec.PerpIndexPrice},
			{basis, &rec.PerpBasisBps},
			{oiContracts, &rec.PerpOIContracts},
		}
		for _, v := range nullable {
			if *v.dst, err = records.Decimal(v.raw); err != nil {
				return nil, err
			}
		}
		var long, short float64
		zeroed := []struct {
			raw sql.NullString
			dst *float64
		}{
			{vol24, &rec.Volume24h}, {vol7, &rec.Volume7d}, {vol30, &rec.Volume30d},
			{longOI, &long}, {shortOI, &short},
			{fillVolume, &rec.PerpVolume24hUSD}, {fillContracts, &rec.PerpVolume24hContracts}, {liqVolume, &rec.PerpLiqVolume24h},
		}
		for _, v := range zeroed {
			if *v.dst, err = records.DecimalOrZero(v.raw); err != nil {
				return nil, err
			}
		}
		if hourly.Valid && eight.Valid {
			rec.LastFundingRateHourly, rec.PerpFundingRate, rec.Funding8h = &hourly.String, &hourly.String, &eight.String
		}
		rec.Price = rec.PerpIndexPrice
		rec.OpenInterest = long + short
		rec.PerpOIUSD = rec.OpenInterest
		if short != 0 {
			ratio := long / short
			rec.PerpLongShortRatio = &ratio
		}
		if markAt.Valid {
			ms := markAt.Time.UnixMilli()
			rec.LastUpdated = &ms
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

const totalsSQL = `SELECT COALESCE(sum(trades_count), 0), COALESCE(sum(volume_24h), 0) / $1::numeric,
	COALESCE(sum(volume_7d), 0) / $1::numeric, COALESCE(sum(volume_30d), 0) / $1::numeric,
	COALESCE(sum(open_interest_long + open_interest_short), 0) / $1::numeric
	FROM perps.market_stats_rolling`

// Totals sums trades, the three volume windows and both open interest sides of
// perps.market_stats_rolling.
func (m *Markets) Totals(ctx context.Context) (Totals, error) {
	var (
		out Totals
		raw [4]sql.NullString
	)
	if err := m.db.QueryRowContext(ctx, totalsSQL, usdScale).Scan(&out.TotalTrades, &raw[0], &raw[1], &raw[2], &raw[3]); err != nil {
		return out, err
	}
	dst := [4]*float64{&out.Volume24h, &out.Volume7d, &out.Volume30d, &out.TotalOI}
	for i := range raw {
		v, err := records.DecimalOrZero(raw[i])
		if err != nil {
			return out, err
		}
		*dst[i] = v
	}
	return out, nil
}
