package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	goredis "github.com/redis/go-redis/v9"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/pnlcache"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

// RegisterReads wires the portfolio read endpoints. The positions list and the
// net-worth portfolio are read-through cached in Redis (PG+Redis); the single
// position and trade history are PG-only.
func RegisterReads(r chi.Router, st *store.Store, rdb *goredis.Client) {
	r.Get("/users/{address}/positions", listPositions(st, rdb))
	r.Get("/users/{address}/positions/{poolAddress}", getPosition(st))
	r.Get("/users/{address}/trades", listTrades(st))
	r.Get("/users/{address}/portfolio", getPortfolio(st, rdb))
}

func listPositions(st *store.Store, rdb *goredis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := strings.ToLower(chi.URLParam(r, "address"))
		cachedJSON(w, r, rdb, pnlcache.KeyPositions(user), pnlcache.PositionsTTL*time.Second,
			func(ctx context.Context) (any, error) {
				positions, err := st.ListPositions(ctx, user)
				if err != nil {
					return nil, err
				}
				if positions == nil {
					positions = []*store.PositionRow{}
				}
				return map[string]any{"user": user, "positions": positions}, nil
			})
	}
}

func getPosition(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := strings.ToLower(chi.URLParam(r, "address"))
		pool := strings.ToLower(chi.URLParam(r, "poolAddress"))
		pos, err := st.GetPosition(r.Context(), user, pool)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "position lookup failed")
			return
		}
		if pos == nil {
			sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "position not found")
			return
		}
		sharedhttp.WriteJSON(w, http.StatusOK, pos)
	}
}

func listTrades(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := strings.ToLower(chi.URLParam(r, "address"))
		q := r.URL.Query()
		limit := parseIntDefault(q.Get("limit"), 50)
		offset := parseIntDefault(q.Get("offset"), 0)
		filter := store.TradeFilter{
			Pool:   q.Get("pool"),
			From:   parseOptInt64(q.Get("from")),
			To:     parseOptInt64(q.Get("to")),
			Limit:  limit,
			Offset: offset,
		}
		trades, err := st.ListTrades(r.Context(), user, filter)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "trades lookup failed")
			return
		}
		if trades == nil {
			trades = []store.TradeRow{}
		}
		sharedhttp.WriteJSON(w, http.StatusOK, map[string]any{
			"user":   user,
			"trades": trades,
			"limit":  limit,
			"offset": offset,
		})
	}
}

func getPortfolio(st *store.Store, rdb *goredis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := strings.ToLower(chi.URLParam(r, "address"))
		cachedJSON(w, r, rdb, pnlcache.KeyPortfolio(user), pnlcache.PortfolioTTL*time.Second,
			func(ctx context.Context) (any, error) {
				positions, total, err := st.Portfolio(ctx, user)
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"user":           user,
					"totalValueUsdl": total,
					"positions":      positions,
				}, nil
			})
	}
}
