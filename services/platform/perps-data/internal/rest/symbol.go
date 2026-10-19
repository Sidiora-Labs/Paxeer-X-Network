// Package rest serves the REST routes of the perps gateway. The symbol routes
// follow the application data protocol under /api/<symbol>/ (contracts/data-services.md
// section 9); chart candles are not served here and stay on the Chainflow feed
// (contracts/data-services.md section 12).
package rest

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/records"
)

// The market set and the fixed-point scales shared with the socket topics.
var marketIDs = records.MarketIDs

const (
	usdScale        = records.USDScale
	secondsPerHour  = records.SecondsPerHour
	window24h       = records.Window24h
	liquidationFill = records.LiquidationFill

	defaultLimit = 100
	maxLimit     = 1000
)

// Symbol serves the symbol REST routes from schema perps.
type Symbol struct {
	data *records.Symbol
}

// NewSymbol returns the symbol routes reading db.
func NewSymbol(db *sql.DB) (*Symbol, error) {
	data, err := records.NewSymbol(db)
	if err != nil {
		return nil, err
	}
	return &Symbol{data: data}, nil
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
	_, ok := marketIDs[symbol]
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
		body, err = s.data.Price(ctx, symbol)
		cache = true
	case "stats":
		body, err = s.data.Stats(ctx, symbol)
		cache = true
	case "perp_stats":
		body, err = s.data.PerpStats(ctx, symbol)
	case "trades", "perp_trades":
		limit, lerr := parseLimit(r.URL.Query())
		if lerr != nil {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		if name == "trades" {
			body, err = s.data.Trades(ctx, symbol, limit)
		} else {
			body, err = s.data.PerpTrades(ctx, symbol, limit)
		}
	case "health":
		body, err = s.data.Health(ctx, symbol)
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
