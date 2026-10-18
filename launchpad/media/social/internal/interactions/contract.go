package interactions

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// RegisterContractRoutes mounts the frozen v2 follow-graph surface (IF_FOLLOW,
// IF_UNFOLLOW, IF_FOLLOWERS, IF_FOLLOW_STATS) over the interaction follow store,
// so the gateway can forward the SDK paths to the social upstream unchanged.
func (h *HTTP) RegisterContractRoutes(r chi.Router) {
	r.Get("/v2/profiles/{wallet}/follow-stats", h.contractStats)
	r.Get("/v2/profiles/{wallet}/followers", h.contractFollowers)
	r.Put("/v2/me/following/{wallet}", func(w http.ResponseWriter, r *http.Request) { h.contractFollow(w, r, false) })
	r.Delete("/v2/me/following/{wallet}", func(w http.ResponseWriter, r *http.Request) { h.contractFollow(w, r, true) })
}

// PublicContractRead reports whether the request is one of the viewer-aware
// contract reads that may be served without a principal.
func PublicContractRead(r *http.Request) bool {
	if r == nil || r.URL == nil || r.Method != http.MethodGet {
		return false
	}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 5 || parts[0] != "" || parts[1] != "v2" || parts[2] != "profiles" {
		return false
	}
	if _, e := Wallet(parts[3]); e != nil {
		return false
	}
	return parts[4] == "followers" || parts[4] == "follow-stats"
}

func contractHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Add("Vary", "Cookie")
	w.Header().Add("Vary", "Authorization")
}

// contractKey derives the mutation idempotency key for a contract write: the
// client's Idempotency-Key when supplied, else the request id, else a fresh id.
func contractKey(r *http.Request) string {
	if key, e := requestKey(r); e == nil {
		return key
	}
	if id, e := uuid.Parse(r.Header.Get("X-Request-ID")); e == nil && id != uuid.Nil {
		return "request:" + id.String()
	}
	return "request:" + uuid.NewString()
}

func (h *HTTP) contractStats(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Store.DB == nil || h.Policy == nil {
		httpError(w, ErrDependency)
		return
	}
	contractHeaders(w)
	viewer := ""
	if p, e := Identity(r.Context()); e == nil {
		viewer = p.Wallet
	} else if _, present := principalValue(r.Context()); present {
		httpError(w, e)
		return
	}
	v, tag, e := h.Store.FollowStats(r.Context(), chi.URLParam(r, "wallet"), viewer, h.Cache)
	if e != nil {
		httpError(w, e)
		return
	}
	followers, e := strconv.ParseInt(v.Followers, 10, 64)
	if e != nil {
		httpError(w, ErrDependency)
		return
	}
	following, e := strconv.ParseInt(v.Following, 10, 64)
	if e != nil {
		httpError(w, ErrDependency)
		return
	}
	w.Header().Set("ETag", tag)
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeResponse(w, v2.FollowStatsData{Followers: v2.SafeInteger(followers), Following: v2.SafeInteger(following), IsFollowing: v.IsFollowing}, nil)
}

func (h *HTTP) contractFollowers(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Store.DB == nil || h.Policy == nil {
		httpError(w, ErrDependency)
		return
	}
	contractHeaders(w)
	limit, e := limitOf(r)
	if e != nil {
		httpError(w, e)
		return
	}
	cursor := r.URL.Query().Get("cursor")
	if len(cursor) > 256 {
		httpError(w, ErrInvalid)
		return
	}
	page, e := h.visibleFollows(r.Context(), chi.URLParam(r, "wallet"), false, limit, cursor)
	if e != nil {
		httpError(w, e)
		return
	}
	wallets := make([]string, 0, len(page.Edges))
	for _, edge := range page.Edges {
		wallets = append(wallets, edge.Wallet)
	}
	items, e := h.Policy.profiles(r.Context(), wallets)
	if e != nil {
		httpError(w, e)
		return
	}
	var next *string
	if page.Next != "" {
		next = &page.Next
	}
	writeResponse(w, v2.FollowersData{Items: items}, next)
}

func (h *HTTP) contractFollow(w http.ResponseWriter, r *http.Request, remove bool) {
	if h == nil || h.Store.DB == nil || h.Policy == nil {
		httpError(w, ErrDependency)
		return
	}
	contractHeaders(w)
	principal, e := Identity(r.Context())
	if e != nil {
		httpError(w, e)
		return
	}
	input := Request{Actor: principal.Wallet, ID: chi.URLParam(r, "wallet"), Key: contractKey(r), Remove: remove}
	if e = h.Store.Follow(r.Context(), input); e != nil {
		httpError(w, e)
		return
	}
	if remove {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeResponse(w, v2.FollowData{Following: true}, nil)
}

// profiles projects follow edges onto the frozen Profile shape from the stable
// account store the user service owns (its profile document is authoritative; a
// deleted account keeps only its wallet and revision, as the user service serves
// it). Wallets without an account record are omitted.
func (p *PGPolicy) profiles(ctx context.Context, wallets []string) ([]v2.Profile, error) {
	out := []v2.Profile{}
	if len(wallets) == 0 {
		return out, nil
	}
	if p == nil || p.DB == nil || len(p.Accounts) != 2 {
		return nil, ErrDependency
	}
	rows, e := p.DB.Query(ctx, `SELECT wallet,profile,deleted,revision::text FROM `+p.Accounts.Sanitize()+` WHERE wallet=ANY($1)`, wallets)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	found := map[string]v2.Profile{}
	for rows.Next() {
		var wallet, revision string
		var raw []byte
		var deleted bool
		if e = rows.Scan(&wallet, &raw, &deleted, &revision); e != nil {
			return nil, e
		}
		profile, e := contractProfile(wallet, raw, deleted, revision)
		if e != nil {
			return nil, e
		}
		found[wallet] = profile
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	for _, wallet := range wallets {
		if profile, ok := found[wallet]; ok {
			out = append(out, profile)
		}
	}
	return out, nil
}

func contractProfile(wallet string, raw []byte, deleted bool, revision string) (v2.Profile, error) {
	profile := v2.Profile{Wallet: wallet, Revision: v2.UInt256(revision)}
	if deleted {
		return profile, nil
	}
	if e := json.Unmarshal(raw, &profile); e != nil {
		return v2.Profile{}, ErrDependency
	}
	profile.Wallet = wallet
	if profile.Revision == "" {
		profile.Revision = v2.UInt256(revision)
	}
	return profile, nil
}
