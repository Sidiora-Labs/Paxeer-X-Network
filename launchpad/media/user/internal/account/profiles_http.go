package account

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// The frozen v2 Profile is the stable PublicProfile projection. createdAt stays
// absent: account_state records no creation instant, so none can be served
// truthfully.

const profilesBatchLimit = 50

func (h HTTP) publicProjection(ctx context.Context, w http.ResponseWriter, wallet string) (PublicProfile, int64, error) {
	if h.Runtime != nil && h.Runtime.UseReadSwitch {
		projection, e := h.Store.PublicFromReadSwitch(ctx, wallet)
		if e != nil {
			return PublicProfile{}, 0, e
		}
		w.Header().Set("X-Profile-Projection", projection.Source)
		return projection.Profile, projection.Generation, nil
	}
	profile, e := h.Store.Public(ctx, wallet)
	return profile, 0, e
}

func profileETag(profile PublicProfile, generation int64) string {
	if generation > 0 {
		return `"` + profile.Wallet + ":" + profile.Revision + ":" + strconv.FormatInt(generation, 10) + `"`
	}
	return `"` + profile.Wallet + ":" + profile.Revision + `"`
}

func (h HTTP) profileV2Get(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	if len(r.URL.Query()) > 0 {
		WriteError(w, ErrUnknown, id)
		return
	}
	profile, generation, e := h.publicProjection(r.Context(), w, r.PathValue("wallet"))
	if e != nil {
		WriteError(w, e, id)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=1, stale-while-revalidate=2")
	etag := profileETag(profile, generation)
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.respond(w, id, map[string]PublicProfile{"profile": profile}, profile.Revision, nil)
}

func batchWallets(query url.Values) ([]string, error) {
	for key, values := range query {
		if key != "wallets" || len(values) != 1 {
			return nil, ErrUnknown
		}
	}
	raw := query.Get("wallets")
	if raw == "" {
		return nil, ValidationError{"/wallets", ErrInvalid}
	}
	parts := strings.Split(raw, ",")
	if len(parts) > profilesBatchLimit {
		return nil, ValidationError{"/wallets", ErrInvalid}
	}
	seen := make(map[string]bool, len(parts))
	wallets := make([]string, 0, len(parts))
	for _, part := range parts {
		wallet, e := Wallet(part)
		if e != nil || seen[wallet] {
			return nil, ValidationError{"/wallets", ErrInvalid}
		}
		seen[wallet] = true
		wallets = append(wallets, wallet)
	}
	return wallets, nil
}

func (h HTTP) profilesBatchGet(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	wallets, e := batchWallets(r.URL.Query())
	if e != nil {
		WriteError(w, e, id)
		return
	}
	items := make([]PublicProfile, 0, len(wallets))
	for _, wallet := range wallets {
		profile, _, e := h.publicProjection(r.Context(), w, wallet)
		if errors.Is(e, ErrMissing) {
			continue
		}
		if e != nil {
			WriteError(w, e, id)
			return
		}
		items = append(items, profile)
	}
	w.Header().Set("Cache-Control", "public, max-age=1")
	h.respond(w, id, map[string][]PublicProfile{"items": items}, "0", nil)
}
