package devreads

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l17"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// errAuthRequired marks a /me request that carries no credential at all.
var errAuthRequired = errors.New("devreads: authentication required")

// Deps wires the development read surface.
type Deps struct {
	Reader Reader
	Store  *store.Store
	// Identity verifies browser sessions for /me routes (the gateway's v2
	// identity introspection). Nil leaves /me routes answering 503.
	Identity auth.RequestIdentityAuthority
	// CursorSecret keys the portfolio page cursors (HMAC).
	CursorSecret        string
	RewardPerConversion int
	Logger              *slog.Logger
	Now                 func() time.Time
}

type handlers struct {
	Deps
	cursorKey []byte
}

// Register mounts the development portfolio, history, rewards and referral
// reads. Callers gate it on NodeEnv != "production".
func Register(r chi.Router, d Deps) {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	mac := hmac.New(sha256.New, []byte(d.CursorSecret))
	mac.Write([]byte("pnl-development-portfolio-cursor-v1"))
	h := &handlers{Deps: d, cursorKey: mac.Sum(nil)}
	r.Get("/v3/wallets/{wallet}/portfolio", h.publicPortfolio)
	r.Get("/v3/me/portfolio", h.privatePortfolio)
	r.Get("/v3/me/pnl/history", h.intervalHistory)
	r.Get("/v3/me/portfolio/history", h.intervalHistory)
	r.Get("/v2/me/portfolio/history", h.valueHistory)
	r.Get("/v2/me/rewards", h.rewards)
	r.Get("/v2/me/referrals", h.referrals)
	registerUnserved(r)
}

// principal resolves the verified wallet of a /me request.
func (h *handlers) principal(r *http.Request) (string, error) {
	if r.Header.Get("Authorization") == "" {
		if _, err := r.Cookie("__Host-kindle_access"); err != nil {
			return "", errAuthRequired
		}
	}
	if h.Identity == nil {
		return "", l17.ErrUnavailable
	}
	p, err := h.Identity.AuthenticateRequest(r.Context(), r)
	if err != nil {
		return "", err
	}
	return l17.Wallet(p.Wallet)
}

func (h *handlers) publicPortfolio(w http.ResponseWriter, r *http.Request) {
	wallet, err := l17.Wallet(chi.URLParam(r, "wallet"))
	if err != nil {
		h.fail(w, l17.ErrInvalid)
		return
	}
	h.portfolio(w, r, wallet)
}

func (h *handlers) privatePortfolio(w http.ResponseWriter, r *http.Request) {
	wallet, err := h.principal(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.portfolio(w, r, wallet)
}

// portfolio serves ConnectedPortfolioV3Response with production paging rules:
// limit 1..100 (default 50), an HMAC cursor bound to wallet, snapshot and
// revision, and REVISION_MISMATCH when the wallet's projection moved on.
func (h *handlers) portfolio(w http.ResponseWriter, r *http.Request, wallet string) {
	query, err := strictQuery(r, "cursor", "limit")
	if err != nil {
		h.fail(w, err)
		return
	}
	limit := 0
	if query.Get("limit") != "" {
		if limit, err = strconv.Atoi(query.Get("limit")); err != nil {
			h.fail(w, l17.ErrInvalid)
			return
		}
	}
	if limit, err = l17.Limit(limit); err != nil {
		h.fail(w, err)
		return
	}
	now := h.Now().UTC()
	started := time.Now()
	snap, err := h.Reader.Snapshot(r.Context(), wallet)
	if err != nil {
		h.fail(w, err)
		return
	}
	after := -1
	if token := query.Get("cursor"); token != "" {
		c, err := l17.VerifyPortfolioCursor(h.cursorKey, token, wallet, now)
		if err != nil {
			h.fail(w, err)
			return
		}
		if c.Limit != limit {
			h.fail(w, l17.ErrInvalid)
			return
		}
		if c.Revision != snap.Revision || c.Snapshot != snap.ID {
			h.fail(w, l17.ErrRestart)
			return
		}
		after = c.After
	}
	start := after + 1
	if start > len(snap.Positions) {
		h.fail(w, l17.ErrInvalid)
		return
	}
	end := start + limit
	if end > len(snap.Positions) {
		end = len(snap.Positions)
	}
	page := l17.Page{Summary: snap.Summary, Positions: snap.Positions[start:end]}
	if end < len(snap.Positions) {
		page.Next, err = l17.SignPortfolioCursor(h.cursorKey, l17.Cursor{Wallet: wallet, Snapshot: snap.ID, Revision: snap.Revision, Filter: "all", Sort: "snapshot_ordinal_v1", After: end - 1, Limit: limit, Expires: now.Add(15 * time.Minute).Unix()})
		if err != nil {
			h.fail(w, err)
			return
		}
	}
	result := l17.PortfolioResponse{Page: page, Errors: map[string][]string{}, QuoteAsset: "USDL", Decimals: 6, Algorithm: l17.Algorithm}
	for _, p := range page.Positions {
		var errs []string
		if p.BasisQuality != "complete" {
			errs = append(errs, "basis_incomplete")
		}
		if p.RealizedBasisQuality != "complete" {
			errs = append(errs, "historical_realized_basis_incomplete")
		}
		if p.PricingQuality != "priced" {
			errs = append(errs, "current_valuation_unavailable")
		}
		if len(errs) > 0 {
			result.Errors[p.PositionID] = errs
		}
	}
	w.Header().Set("Server-Timing", "portfolio_origin;dur="+strconv.FormatFloat(float64(time.Since(started))/float64(time.Millisecond), 'f', 6, 64))
	h.write(w, "ConnectedPortfolioV3Response", struct {
		Version int `json:"version"`
		l17.PortfolioResponse
	}{3, result})
}

// write validates body against the named shared contract and writes it; a
// body that fails its own contract is never sent.
func (h *handlers) write(w http.ResponseWriter, contract string, body any) {
	raw, err := json.Marshal(body)
	if err == nil {
		err = interfaces.ValidateJSON(contract, raw)
	}
	if err != nil {
		h.Logger.Error("development read failed its contract", slog.String("contract", contract), slog.Any("err", err))
		h.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(raw, '\n'))
}

// fail writes the shared v2 Error envelope with production's status mapping.
func (h *handlers) fail(w http.ResponseWriter, e error) {
	status, code, retry := http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", true
	var authority *auth.IdentityAuthorityError
	switch {
	case errors.Is(e, errAuthRequired):
		status, code, retry = http.StatusUnauthorized, "AUTH_REQUIRED", false
	case errors.As(e, &authority):
		switch authority.Status {
		case http.StatusUnauthorized:
			status, code, retry = http.StatusUnauthorized, "AUTH_EXPIRED", false
		case http.StatusForbidden:
			status, code, retry = http.StatusForbidden, "SCOPE_DENIED", false
		case http.StatusBadRequest:
			status, code, retry = http.StatusBadRequest, "FIELD_INVALID", false
		case http.StatusTooManyRequests:
			status, code = http.StatusTooManyRequests, "RATE_LIMITED"
			w.Header().Set("Retry-After", "1")
		}
	case errors.Is(e, auth.ErrScopeDenied) || errors.Is(e, l17.ErrUnauthorized):
		status, code, retry = http.StatusForbidden, "SCOPE_DENIED", false
	case errors.Is(e, l17.ErrInvalid):
		status, code, retry = http.StatusBadRequest, "FIELD_INVALID", false
	case errors.Is(e, l17.ErrRestart):
		status, code, retry = http.StatusConflict, "REVISION_MISMATCH", false
	case errors.Is(e, l17.ErrBusy):
		status, code = http.StatusTooManyRequests, "RATE_LIMITED"
		w.Header().Set("Retry-After", "1")
	case errors.Is(e, pgx.ErrNoRows):
		status, code, retry = http.StatusNotFound, "NOT_FOUND", false
	case errors.Is(e, context.DeadlineExceeded):
		w.Header().Set("Retry-After", "1")
	}
	if status == http.StatusServiceUnavailable {
		h.Logger.Warn("development read unavailable", slog.Any("err", e))
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(interfaces.Error{Code: code, Message: "Portfolio request unavailable", RequestId: requestID(), Retryable: retry, FieldErrors: []interfaces.FieldError{}, Details: nil, RetryAfterMs: nil})
}

// strictQuery parses the query and rejects unknown or repeated keys.
func strictQuery(r *http.Request, allowed ...string) (url.Values, error) {
	if len(r.URL.RawQuery) > 4096 {
		return nil, l17.ErrInvalid
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, l17.ErrInvalid
	}
	for key, values := range q {
		ok := false
		for _, a := range allowed {
			ok = ok || key == a
		}
		if !ok || len(values) != 1 {
			return nil, l17.ErrInvalid
		}
	}
	return q, nil
}

// requestID returns a random v4 UUID.
func requestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	s := hex.EncodeToString(b)
	return strings.Join([]string{s[:8], s[8:12], s[12:16], s[16:20], s[20:]}, "-")
}
