package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

// RegisterCrossTokenSwaps registers the multihop swap-history routes. The static
// /token/ segment is registered before the {wallet} param route so chi resolves
// it first.
func RegisterCrossTokenSwaps(r chi.Router, st *store.Store) {
	r.Get("/stats/cross-token-swaps/token/{tokenAddress}", crossSwapsByToken(st))
	r.Get("/stats/cross-token-swaps/{wallet}", crossSwapsByWallet(st))
}

// crossSwapsByWallet serves a wallet's multihop swap history. Ports the TS route.
func crossSwapsByWallet(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wallet := chi.URLParam(r, "wallet")
		q := r.URL.Query()
		limit := parseIntDefault(q.Get("limit"), 50)
		offset := parseIntDefault(q.Get("offset"), 0)

		swaps, err := st.ListCrossTokenSwapsByWallet(r.Context(), strings.ToLower(wallet), limit, offset)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "cross-token swaps lookup failed")
			return
		}
		sharedhttp.WriteJSON(w, http.StatusOK, map[string]any{
			"wallet": wallet,
			"swaps":  swaps,
			"limit":  limit,
			"offset": offset,
		})
	}
}

// crossSwapsByToken serves all multihop swaps involving a token. Ports the TS route.
func crossSwapsByToken(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenAddress := chi.URLParam(r, "tokenAddress")
		q := r.URL.Query()
		limit := parseIntDefault(q.Get("limit"), 50)
		offset := parseIntDefault(q.Get("offset"), 0)

		swaps, err := st.ListCrossTokenSwapsByToken(r.Context(), strings.ToLower(tokenAddress), limit, offset)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "cross-token swaps lookup failed")
			return
		}
		sharedhttp.WriteJSON(w, http.StatusOK, map[string]any{
			"tokenAddress": tokenAddress,
			"swaps":        swaps,
			"limit":        limit,
			"offset":       offset,
		})
	}
}
