package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	goredis "github.com/redis/go-redis/v9"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

// statsCacheTTL mirrors the TS `EX 10` on the stats:<pool> read-through cache.
const statsCacheTTL = 10 * time.Second

func cacheKey(poolAddress string) string { return "stats:" + poolAddress }

// RegisterPoolStats registers the pool-stats read routes. The static /stats/batch
// route is registered before the /stats/{poolAddress} param route so chi resolves
// it first.
func RegisterPoolStats(r chi.Router, st *store.Store, rdb *goredis.Client) {
	r.Get("/stats/batch", statsBatch(st, rdb))
	r.Get("/stats/{poolAddress}", statsByPool(st, rdb))
}

// statsByPool serves GET /stats/:poolAddress, read-through cached for 10s. Ports
// the TS pool-stats single route.
func statsByPool(st *store.Store, rdb *goredis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		poolAddress := chi.URLParam(r, "poolAddress")
		ctx := r.Context()

		if cached, err := rdb.Get(ctx, cacheKey(poolAddress)).Result(); err == nil && cached != "" {
			var row store.PoolStatsRow
			if json.Unmarshal([]byte(cached), &row) == nil {
				sharedhttp.WriteJSON(w, http.StatusOK, row)
				return
			}
		}

		row, err := st.GetPoolStats(ctx, poolAddress)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "stats lookup failed")
			return
		}
		if row == nil {
			sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "Pool not found")
			return
		}

		if payload, err := json.Marshal(row); err == nil {
			rdb.Set(ctx, cacheKey(poolAddress), payload, statsCacheTTL)
		}
		sharedhttp.WriteJSON(w, http.StatusOK, row)
	}
}

// statsBatch serves GET /stats/batch?pools=0xA,0xB — a Redis-pipelined multi-get
// with a DB fallback for misses (parity with the TS batch route). Returns a map
// keyed by pool address.
func statsBatch(st *store.Store, rdb *goredis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		poolsParam := r.URL.Query().Get("pools")
		addresses := strings.Split(poolsParam, ",")

		result := make(map[string]json.RawMessage, len(addresses))
		var missed []string

		pipe := rdb.Pipeline()
		cmds := make([]*goredis.StringCmd, len(addresses))
		for i, addr := range addresses {
			cmds[i] = pipe.Get(ctx, cacheKey(addr))
		}
		_, _ = pipe.Exec(ctx) //nolint:errcheck // redis.Nil for misses is expected; handled per-cmd

		for i, addr := range addresses {
			if val, err := cmds[i].Result(); err == nil && val != "" {
				result[addr] = json.RawMessage(val)
			} else {
				missed = append(missed, addr)
			}
		}

		if len(missed) > 0 {
			rows, err := st.GetPoolStatsBatch(ctx, missed)
			if err != nil {
				sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "stats batch failed")
				return
			}
			for _, row := range rows {
				if payload, err := json.Marshal(row); err == nil {
					result[row.PoolAddress] = json.RawMessage(payload)
					rdb.Set(ctx, cacheKey(row.PoolAddress), payload, statsCacheTTL)
				}
			}
		}

		sharedhttp.WriteJSON(w, http.StatusOK, result)
	}
}
