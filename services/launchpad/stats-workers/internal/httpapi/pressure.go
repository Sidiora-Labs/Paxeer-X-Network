package httpapi

import (
	"encoding/json"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/analytics"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/risk"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"
	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

const pressureCacheTTL = 10 * time.Second

// RegisterPressure registers GET /stats/{poolAddress}/pressure.
func RegisterPressure(r chi.Router, st *store.Store, rdb *goredis.Client) {
	r.Get("/stats/{poolAddress}/pressure", pressure(st, rdb))
}

// round2 mirrors JS Math.round(x*100)/100.
func round2(x float64) float64 { return math.Round(x*100) / 100 }

// direction classifies a buy percentage (>=55 bullish, <=45 bearish, else neutral).
func direction(buyPct float64) string {
	switch {
	case buyPct >= 55:
		return "bullish"
	case buyPct <= 45:
		return "bearish"
	default:
		return "neutral"
	}
}

func pressure(st *store.Store, rdb *goredis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		poolAddress := chi.URLParam(r, "poolAddress")
		ctx := r.Context()
		cacheKey := "pressure:" + poolAddress

		if cached, err := rdb.Get(ctx, cacheKey).Result(); err == nil && cached != "" {
			sharedhttp.WriteJSON(w, http.StatusOK, json.RawMessage(cached))
			return
		}

		now := shareddb.NowSeconds()
		ps, found, err := st.PressureStats(ctx, poolAddress, now-3600)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "pressure lookup failed")
			return
		}
		if !found {
			sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "Pool not found")
			return
		}

		totalTrades24h := ps.BuyCount24h + ps.SellCount24h
		buyPct24h := 50.0
		if totalTrades24h > 0 {
			buyPct24h = float64(ps.BuyCount24h) / float64(totalTrades24h) * 100
		}
		totalTrades1h := ps.BuyCount1h + ps.SellCount1h
		buyPct1h := 50.0
		if totalTrades1h > 0 {
			buyPct1h = float64(ps.BuyCount1h) / float64(totalTrades1h) * 100
		}

		result := map[string]any{
			"poolAddress": poolAddress,
			"24h": map[string]any{
				"buyCount":  ps.BuyCount24h,
				"sellCount": ps.SellCount24h,
				"buyPct":    round2(buyPct24h),
				"sellPct":   round2(100 - buyPct24h),
				"volume":    ps.Volume24h,
				"direction": direction(buyPct24h),
			},
			"1h": map[string]any{
				"buyCount":   ps.BuyCount1h,
				"sellCount":  ps.SellCount1h,
				"buyPct":     round2(buyPct1h),
				"sellPct":    round2(100 - buyPct1h),
				"buyVolume":  ps.BuyVolume1h,
				"sellVolume": ps.SellVolume1h,
				"direction":  direction(buyPct1h),
			},
			"updatedAt": now,
		}

		if payload, err := json.Marshal(result); err == nil {
			rdb.Set(ctx, cacheKey, payload, pressureCacheTTL)
			// Push-first: publish the freshly-computed pressure on pressure:update
			// so subscribers of this pool update in place. Best-effort.
			_ = sharedredis.PublishJSON(ctx, rdb, constants.ChannelPressureUpdate, payload)
		}
		sharedhttp.WriteJSON(w, http.StatusOK, result)
	}
}

func RegisterDurableAnalytics(r chi.Router, a analytics.ReactionAuthority) {
	snapshot := func(model string) http.HandlerFunc {
		return func(w http.ResponseWriter, request *http.Request) {
			pool := strings.ToLower(chi.URLParam(request, "poolAddress"))
			result, err := a.ReadSnapshot(request.Context(), pool, model, time.Now().Unix())
			if err != nil {
				durableReactionError(w, err)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			response := map[string]any{"snapshot": result.Snapshot, "stale": result.Stale}
			if model == risk.ModelVersion {
				response["model_metadata"] = risk.ModelMetadata()
			}
			sharedhttp.WriteJSON(w, 200, response)
		}
	}
	r.Get("/v2/stats/{poolAddress}/pressure", snapshot("pressure-v1"))
	r.Get("/v2/stats/{poolAddress}/risk", snapshot(risk.ModelVersion))
	r.Get("/v2/stats/{poolAddress}/analytics/revisions", func(w http.ResponseWriter, request *http.Request) {
		limit := 100
		if raw := request.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > 100 {
				sharedhttp.WriteError(w, 400, "Bad Request", "invalid analytics page limit")
				return
			}
			limit = n
		}
		after := request.URL.Query().Get("after")
		if after == "" {
			after = "0"
		}
		page, err := a.ReplaySnapshots(request.Context(), strings.ToLower(chi.URLParam(request, "poolAddress")), after, limit)
		if err != nil {
			durableReactionError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		sharedhttp.WriteJSON(w, 200, page)
	})
}
