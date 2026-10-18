package rest

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// DevelopmentSeriesModelVersion labels candle documents built from the
// development candles.candles table (core/trading-charts legacy builder).
const DevelopmentSeriesModelVersion = "development-legacy-candles-v1"

const hsdTimestamp = "2006-01-02T15:04:05.000Z"

// devHistory serves GET /v2/pools/{poolAddress}/trades and /candles outside
// production. Requests are validated exactly as the production handler
// validates them (accountingRequest: TradesRequest / CandlesRequest).
//
// The development tables (indexer.swaps, candles.candles) do not record the
// block hash or transaction index of a fill, so they cannot back a contract
// Trade or Candle: every item requires a ChainPosition carrying both. A pool
// the development pipeline has no rows for in the requested scope is answered
// with a valid empty document; a scope that does hold development rows is
// answered 503 INTEGRITY_QUARANTINED rather than with invented positions.
func devHistory(st *store.Store, candles bool) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r, token, err := accountingRequest(req, candles)
		if err != nil {
			WriteV2APIError(w, req, 422, "FIELD_INVALID")
			return
		}
		if token != "" {
			// The development surface never issues cursors.
			WriteV2APIError(w, req, 400, "CURSOR_INVALID")
			return
		}
		ctx := req.Context()
		indexed, err := st.DevelopmentPoolIndexed(ctx, r.Pool)
		if err != nil {
			WriteV2APIError(w, req, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		if !indexed {
			WriteV2APIError(w, req, 404, "NOT_FOUND")
			return
		}
		var recorded bool
		if candles {
			recorded, err = st.DevelopmentCandlesRecorded(ctx, r.Pool, r.Interval, r.From/1000, (r.To+999)/1000)
		} else {
			recorded, err = st.DevelopmentSwapsRecorded(ctx, r.Pool, r.Side)
		}
		if err != nil {
			WriteV2APIError(w, req, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		if recorded {
			WriteV2APIError(w, req, 503, "INTEGRITY_QUARANTINED")
			return
		}
		data := map[string]any{"items": []any{}}
		name := "TradesResponse"
		if candles {
			name = "CandlesResponse"
			data["snapshotId"] = uuid.NewString()
			data["seriesModelVersion"] = DevelopmentSeriesModelVersion
		}
		writeHSDEnvelope(w, req, name, data, nil)
	}
}

// writeHSDEnvelope writes a v2 read envelope with no watermark or
// revision (the development stores carry neither) and validates it against
// the named contract document before sending it.
func writeHSDEnvelope(w http.ResponseWriter, req *http.Request, name string, data any, next *string) {
	body, err := json.Marshal(map[string]any{"requestId": responseRequestID(w), "data": data, "watermark": nil, "revision": nil, "quality": "partial", "nextCursor": next, "generatedAt": time.Now().UTC().Format(hsdTimestamp)})
	if err != nil || len(body) > store.PublicJSONMaxBytes || interfaces.ValidateJSON(name, body) != nil {
		WriteV2APIError(w, req, 503, "INTEGRITY_QUARANTINED")
		return
	}
	writeHSDBody(w, body, "no-store")
}

func writeHSDBody(w http.ResponseWriter, body []byte, cache string) {
	w.Header().Set("X-Schema-Version", "2")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", cache)
	_, _ = w.Write(body)
}
