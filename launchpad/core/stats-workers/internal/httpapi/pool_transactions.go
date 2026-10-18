package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

// RegisterPoolTransactions registers GET /stats/{poolAddress}/transactions.
func RegisterPoolTransactions(r chi.Router, st *store.Store) {
	r.Get("/stats/{poolAddress}/transactions", poolTransactions(st))
}

// poolTransactions serves a pool's transaction history, newest first, optionally
// filtered by side (type=buy|sell|all). Ports the TS pool-transactions route.
func poolTransactions(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		poolAddress := chi.URLParam(r, "poolAddress")
		q := r.URL.Query()
		limit := parseIntDefault(q.Get("limit"), 50)
		offset := parseIntDefault(q.Get("offset"), 0)
		txType := q.Get("type")
		if txType == "" {
			txType = "all"
		}

		txs, err := st.ListTransactions(r.Context(), poolAddress, limit, offset, txType)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "transactions lookup failed")
			return
		}

		sharedhttp.WriteJSON(w, http.StatusOK, map[string]any{
			"transactions": txs,
			"limit":        limit,
			"offset":       offset,
		})
	}
}
