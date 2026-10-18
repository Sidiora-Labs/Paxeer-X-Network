package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
)

// DevelopmentSeriesModelVersion labels history documents built from the
// development candles.candles table (the legacy swap builder). core/api's
// development /v2/pools/{pool}/candles uses the same label.
const DevelopmentSeriesModelVersion = "development-legacy-candles-v1"

// developmentSupplyUnavailable is the per-bar reason production emits when a
// bar's historical supply cannot be bound to the price source block. The
// development candles carry no source positions, so no bar can be bound.
const developmentSupplyUnavailable = "historical_supply_unavailable"

// RegisterDevelopmentUDF mounts the versioned UDF surface outside production:
// the same paths and documents as RegisterProductionUDF, with history read
// from the development candle store and symbols resolved from the
// development indexer.
func RegisterDevelopmentUDF(r chi.Router, symbols SymbolsHandler, history DevelopmentHistoryHandler) {
	r.Get("/v2/udf/config", udfConfig)
	r.Get("/v2/udf/symbols", symbols.ServeHTTP)
	r.Get("/v2/udf/history", history.ServeHTTP)
	r.Get("/v2/udf/time", udfTime)
}

// DevelopmentHistoryHandler serves GET /v2/udf/history (modes price and
// total_supply_valuation) over the development candle store. Requests are
// validated exactly as ProductionHistoryHandler validates them and the body
// carries the production envelope: s/t/o/h/l/c/v plus mode, valuationBasis,
// valuationAvailable, valuations, snapshotId, algorithmVersion and watermark.
type DevelopmentHistoryHandler struct {
	Source HistorySource
}

func (h DevelopmentHistoryHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if len(req.URL.RawQuery) > 16384 {
		historyError(w, 422, "FIELD_INVALID", "history query too large", false)
		return
	}
	q, err := url.ParseQuery(req.URL.RawQuery)
	if err != nil {
		historyError(w, 422, "FIELD_INVALID", "malformed history query", false)
		return
	}
	mode := q.Get("mode")
	if mode == "" {
		mode = "price"
	}
	if len(q["mode"]) > 1 || (mode != "price" && mode != "total_supply_valuation") {
		historyError(w, 422, "FIELD_INVALID", "unsupported chart mode", false)
		return
	}
	q.Del("mode")
	r, err := parseHistory(q)
	if err != nil || q.Has("cursor") {
		historyError(w, 422, "FIELD_INVALID", "invalid versioned history request", false)
		return
	}
	if r.From == 0 && r.Countback > 0 && r.To > maxHistorySpan {
		r.From = r.To - maxHistorySpan
	}
	if h.Source == nil {
		historyError(w, 503, "DEPENDENCY_UNAVAILABLE", "canonical history unavailable", true)
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), 3500*time.Millisecond)
	defer cancel()
	limit := MaxHistoryBars + 1
	if r.Countback > 0 {
		limit = r.Countback
	}
	rows, err := h.Source.QueryHistory(ctx, r, limit)
	if err != nil || ctx.Err() != nil || len(rows) > MaxHistoryBars {
		historyError(w, 503, "DEPENDENCY_UNAVAILABLE", "canonical history unavailable", true)
		return
	}
	body, err := developmentValuationHistory(r, rows, mode)
	if err != nil {
		historyError(w, 503, "INTEGRITY_QUARANTINED", "malformed stored candle", false)
		return
	}
	digest := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(digest[:]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("X-Schema-Version", "2")
	w.Header().Set("Content-Type", "application/json")
	if req.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(body)
}

// developmentSnapshotID names the stored rows a document was built from: the
// same scope and rows always give the same id, any change gives a new one.
func developmentSnapshotID(r HistoryRequest, rows []store.CandleRow) string {
	raw, _ := json.Marshal([]any{"charts-dev-history-v1", "125", r.Symbol, r.Timeframe, r.From, r.To, r.Countback, rows})
	return uuid.NewSHA1(uuid.NameSpaceOID, raw).String()
}

// developmentValuationHistory renders stored candles into the document
// valuationHistory renders for a canonical page whose supply cannot be bound.
func developmentValuationHistory(r HistoryRequest, rows []store.CandleRow, mode string) ([]byte, error) {
	if mode != "price" && mode != "total_supply_valuation" {
		return nil, errors.New("invalid valuation mode")
	}
	ordered := append([]store.CandleRow(nil), rows...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].CandleStart < ordered[j].CandleStart })
	bars, err := exactHistory(ordered, MaxHistoryBars)
	if err != nil {
		return nil, err
	}
	times := []int64{}
	values := make([][]float64, 5)
	for i := range values {
		values[i] = []float64{}
	}
	valuations := []any{}
	for _, bar := range bars {
		if bar.Time < r.From || bar.Time >= r.To {
			return nil, errors.New("stored candle outside requested scope")
		}
		times = append(times, bar.Time)
		for i, s := range []string{bar.Open, bar.High, bar.Low, bar.Close, bar.Volume} {
			v, e := displayNumber(s)
			if e != nil {
				return nil, e
			}
			values[i] = append(values[i], v)
		}
		valuations = append(valuations, map[string]any{"available": false, "valuationBasis": "total_supply_valuation", "reason": developmentSupplyUnavailable})
	}
	available := len(bars) == 0
	body := map[string]any{"s": "ok", "t": times, "o": values[0], "h": values[1], "l": values[2], "c": values[3], "v": values[4], "mode": mode, "valuationBasis": "total_supply_valuation", "valuationAvailable": available, "valuations": valuations, "snapshotId": developmentSnapshotID(r, ordered), "algorithmVersion": DevelopmentSeriesModelVersion, "watermark": nil}
	if mode == "total_supply_valuation" && available {
		// Production substitutes the valued series, which are nil (JSON null)
		// when there are no bars to value.
		for _, key := range []string{"o", "h", "l", "c"} {
			body[key] = []float64(nil)
		}
	}
	if len(times) == 0 {
		body["s"] = "no_data"
	}
	if mode == "total_supply_valuation" && !available {
		body["s"] = "unavailable"
		body["priceHistory"] = map[string]any{"t": times, "o": values[0], "h": values[1], "l": values[2], "c": values[3]}
		delete(body, "o")
		delete(body, "h")
		delete(body, "l")
		delete(body, "c")
	}
	return json.Marshal(body)
}
