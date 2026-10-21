// Package httpapi implements the HTTP routes for core/trading-charts: TradingView
// UDF endpoints (config, symbols, history, time), HMAC-authenticated webhook
// receiver, and a WebSocket candle stream. Ports candles/src/routes.
package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
)

// RegisterUDF registers all TradingView UDF routes on the router.
func RegisterUDF(r chi.Router, st *store.Store) {
	r.Get("/config", udfConfig)
	r.Get("/symbols", udfSymbols)
	r.Get("/history", udfHistory(st))
	r.Get("/time", udfTime)
}

func udfConfig(w http.ResponseWriter, _ *http.Request) {
	sharedhttp.WriteJSON(w, 200, map[string]interface{}{
		"supports_search":          true,
		"supports_group_request":   false,
		"supports_marks":           false,
		"supports_timescale_marks": false,
		"supports_time":            true,
		"exchanges":                []map[string]string{{"value": "sidiora", "name": "Sidiora", "desc": "Sidiora Launchpad"}},
		"symbols_types":            []map[string]string{{"name": "token", "value": "token"}},
		"supported_resolutions":    supportedResolutions(),
		"currency_codes":           []map[string]string{{"id": "USDL", "code": "USDL", "description": "USDL Stablecoin"}},
	})
}

func udfTime(w http.ResponseWriter, _ *http.Request) {
	sharedhttp.WriteJSON(w, 200, time.Now().Unix())
}

func RegisterProductionUDF(r chi.Router, symbols SymbolsHandler, history ProductionHistoryHandler) {
	r.Get("/v2/udf/config", udfConfig)
	r.Get("/v2/udf/symbols", symbols.ServeHTTP)
	r.Get("/v2/udf/history", history.ServeHTTP)
	r.Get("/v2/udf/time", udfTime)
}
