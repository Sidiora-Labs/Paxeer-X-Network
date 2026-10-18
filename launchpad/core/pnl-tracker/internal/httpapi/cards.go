package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	goredis "github.com/redis/go-redis/v9"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/card"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/ogrender"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/pnlcache"
)

// CardDeps wires the card routes.
type CardDeps struct {
	Cards    *card.Service
	Renderer *ogrender.Renderer
	Redis    *goredis.Client
	// ShareLabel builds the footer label drawn on the OG image from a short code,
	// e.g. "sidiora.fun/pnl/abc123".
	ShareLabel func(shortCode string) string
}

// RegisterCards wires card mint, hydrate, and OG image rendering.
func RegisterCards(r chi.Router, deps CardDeps) {
	r.Post("/pnl/cards", mintCard(deps))
	r.Get("/pnl/cards/{cardId}", getCard(deps))
	r.Get("/pnl/cards/{cardId}/og.png", ogImage(deps))
}

type mintCardBody struct {
	OwnerAddress string `json:"ownerAddress"`
	PoolAddress  string `json:"poolAddress"`
}

func mintCard(deps CardDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body mintCardBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
			return
		}
		if body.OwnerAddress == "" || body.PoolAddress == "" {
			sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "ownerAddress and poolAddress required")
			return
		}
		minted, err := deps.Cards.Mint(r.Context(), strings.ToLower(body.OwnerAddress), strings.ToLower(body.PoolAddress))
		if errors.Is(err, card.ErrNoPosition) {
			sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "no position to mint a card for")
			return
		}
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "card mint failed")
			return
		}
		sharedhttp.WriteJSON(w, http.StatusOK, minted)
	}
}

func getCard(deps CardDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cardID := chi.URLParam(r, "cardId")
		minted, err := deps.Cards.Get(r.Context(), cardID)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "card lookup failed")
			return
		}
		if minted == nil {
			sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "card not found")
			return
		}
		sharedhttp.WriteJSON(w, http.StatusOK, minted)
	}
}

// ogImage renders (and caches) the PnL card PNG. The snapshot is immutable, so a
// hit serves the cached bytes; a miss renders with gg and caches for OGTTL.
func ogImage(deps CardDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		cardID := chi.URLParam(r, "cardId")
		key := pnlcache.KeyOG(cardID)

		if deps.Redis != nil {
			if cached, err := deps.Redis.Get(ctx, key).Bytes(); err == nil && len(cached) > 0 {
				writePNG(w, cached)
				return
			}
		}

		minted, err := deps.Cards.Get(ctx, cardID)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "card lookup failed")
			return
		}
		if minted == nil {
			sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "card not found")
			return
		}

		footer := minted.ShortCode
		if deps.ShareLabel != nil {
			footer = deps.ShareLabel(minted.ShortCode)
		}
		png, err := deps.Renderer.Render(card.BuildRenderInput(minted.Snapshot, footer))
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "card render failed")
			return
		}
		if deps.Redis != nil {
			deps.Redis.Set(ctx, key, png, pnlcache.OGTTL*time.Second)
		}
		writePNG(w, png)
	}
}

// writePNG writes PNG bytes with immutable-friendly caching headers.
func writePNG(w http.ResponseWriter, png []byte) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	w.WriteHeader(http.StatusOK)
	// G705 false positive: png is server-rendered image bytes, served as
	// image/png with nosniff (no MIME sniffing), and never reflects client input.
	if _, err := w.Write(png); err != nil { //nolint:gosec // PNG image bytes, not HTML
		return
	}
}
