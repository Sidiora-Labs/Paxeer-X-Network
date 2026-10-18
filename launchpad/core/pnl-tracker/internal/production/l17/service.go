package l17

import (
	"context"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/go-chi/chi/v5"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

var ErrUnavailable = errors.New("portfolio dependency unavailable")
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$|^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func validUUID(s string) bool { return uuidPattern.MatchString(s) }

type Service struct {
	Repository Repository
	Authority  auth.RequestIdentityAuthority
	Cache      *RevisionCache
	CursorKey  []byte
	ExportsDir string
	Display    *DisplayProvider
}

type PortfolioResponse struct {
	Page           Page                `json:"data"`
	Errors         map[string][]string `json:"positionErrors"`
	QuoteAsset     string              `json:"quoteAsset"`
	Decimals       int                 `json:"decimals"`
	Algorithm      string              `json:"algorithm"`
	Display        *USDDisplay         `json:"display,omitempty"`
	OriginDuration time.Duration       `json:"-"`
}

func (s Service) Principal(ctx context.Context, r *http.Request, wallet string) (auth.IdentityPrincipal, error) {
	if s.Authority == nil {
		return auth.IdentityPrincipal{}, ErrUnavailable
	}
	p, e := s.Authority.AuthenticateRequest(ctx, r)
	if e != nil {
		return p, e
	}
	if e = RequireOwner(wallet, p.Wallet); e != nil {
		return p, e
	}
	return p, nil
}

func (r Repository) Latest(ctx context.Context, wallet string, now time.Time) (string, string, time.Time, error) {
	wallet, e := Wallet(wallet)
	if e != nil {
		return "", "", time.Time{}, e
	}
	if r.Pool == nil {
		return "", "", time.Time{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	admission := r.ReadAdmission
	if admission == nil {
		admission = defaultAdmission
	}
	release, e := admission.Acquire(ctx, wallet)
	if e != nil {
		return "", "", time.Time{}, e
	}
	defer release()
	var id, revision string
	var expires time.Time
	e = r.Pool.QueryRow(ctx, "SELECT id::text,revision::text,expires_at FROM portfolio_snapshots WHERE wallet=$1 ORDER BY revision DESC LIMIT 1", wallet).Scan(&id, &revision, &expires)
	if e == nil && !now.Before(expires) {
		return "", "", expires, ErrRestart
	}
	return id, revision, expires, e
}

func (s Service) Portfolio(ctx context.Context, wallet, token string, limit int, now time.Time) (PortfolioResponse, error) {
	var out PortfolioResponse
	wallet, e := Wallet(wallet)
	if e != nil {
		return out, e
	}
	limit, e = Limit(limit)
	if e != nil {
		return out, e
	}
	if s.Repository.Pool == nil {
		return out, ErrUnavailable
	}
	var cursor Cursor
	var originDuration time.Duration
	if token != "" {
		cursor, e = VerifyPortfolioCursor(s.CursorKey, token, wallet, now)
		if e != nil {
			return out, e
		}
		if cursor.Limit != limit || cursor.Revision == "" {
			return out, ErrInvalid
		}
	} else {
		originStarted := time.Now()
		id, revision, expires, e := s.Repository.Latest(ctx, wallet, now)
		originDuration += time.Since(originStarted)
		if e != nil {
			return out, e
		}
		if cap := now.Add(15 * time.Minute); expires.After(cap) {
			expires = cap
		}
		cursor = Cursor{Wallet: wallet, Snapshot: id, Revision: revision, Filter: "all", Sort: "snapshot_ordinal_v1", After: -1, Limit: limit, Expires: expires.Unix()}
	}
	key, e := CacheKey(wallet, "", cursor.Revision, false)
	if e != nil {
		return out, e
	}
	key = key + ":" + cursor.Snapshot + ":" + stringInt(cursor.After) + ":" + stringInt(limit)
	if s.Cache != nil {
		cached, found, cacheErr := s.Cache.Get(ctx, key, now)
		if cacheErr == nil && found && cached.Page.Summary.Wallet == wallet && cached.Page.Summary.SnapshotID == cursor.Snapshot && cached.Page.Summary.Watermark.ProjectionRevision == cursor.Revision && cached.Algorithm == Algorithm && len(cached.Page.Positions) <= limit {
			if cached.Display != nil && cached.Display.Quote != nil && s.Display != nil {
				q := cached.Display.Quote
				if q.ObservedAt.After(now) || now.Sub(q.ObservedAt) > s.Display.MaxAge {
					q.Stale = true
					cached.Display.UsdMicro = nil
				}
			}
			cached.OriginDuration = originDuration
			return cached, nil
		}
	}
	originStarted := time.Now()
	page, e := s.Repository.Page(ctx, wallet, cursor.Snapshot, cursor.After, limit, now)
	originDuration += time.Since(originStarted)
	if e != nil {
		return out, e
	}
	if page.Summary.Watermark.ProjectionRevision != cursor.Revision {
		return out, ErrRestart
	}
	if cursor.After+1+len(page.Positions) < page.Summary.PositionCount {
		cursor.After += len(page.Positions)
		page.Next, e = SignPortfolioCursor(s.CursorKey, cursor)
		if e != nil {
			return out, e
		}
	}
	out = PortfolioResponse{Page: page, Errors: map[string][]string{}, QuoteAsset: "USDL", Decimals: 6, Algorithm: Algorithm, OriginDuration: originDuration}
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
			out.Errors[p.PositionID] = errs
		}
	}
	if s.Display != nil && page.Summary.CompleteValueUsdlRaw != nil {
		display := s.Display.Convert(ctx, *page.Summary.CompleteValueUsdlRaw, now)
		out.Display = &display
	}
	if s.Cache != nil {
		_ = s.Cache.SetUntil(ctx, key, out, now, time.Unix(cursor.Expires, 0))
	}
	return out, nil
}

func AuthorizePortfolioSubscription(p auth.IdentityPrincipal, wallet, origin string) error {
	if e := RequireOwner(wallet, p.Wallet); e != nil {
		return e
	}
	topics := []string{"private.portfolio." + p.Wallet}
	return auth.ValidateStreamBinding(p.StreamPrincipal(), auth.StreamTicketBinding{SchemaMajor: 2, Endpoint: "/v2/stream", Topics: topics, FilterDigest: auth.StreamFilterDigest(topics), Origin: origin})
}

func (s Service) PortfolioV3HTTP(w http.ResponseWriter, r *http.Request) {
	principal, e := s.self(r)
	if e != nil {
		writeFailure(w, e)
		return
	}
	s.portfolioV3HTTP(w, r, principal.Wallet)
}
func (s Service) PublicPortfolioV3HTTP(w http.ResponseWriter, r *http.Request) {
	wallet, e := Wallet(chi.URLParam(r, "wallet"))
	if e != nil {
		writeFailure(w, e)
		return
	}
	s.portfolioV3HTTP(w, r, wallet)
}
func (s Service) portfolioV3HTTP(w http.ResponseWriter, r *http.Request, wallet string) {
	query, queryErr := portfolioQuery(r)
	if queryErr != nil {
		writeFailure(w, queryErr)
		return
	}
	for key, values := range query {
		if (key != "cursor" && key != "limit") || len(values) != 1 {
			writeFailure(w, ErrInvalid)
			return
		}
	}
	limit := 0
	var e error
	if query.Get("limit") != "" {
		limit, e = strconv.Atoi(query.Get("limit"))
		if e != nil {
			writeFailure(w, ErrInvalid)
			return
		}
	}
	result, e := s.Portfolio(r.Context(), wallet, query.Get("cursor"), limit, time.Now().UTC())
	if e != nil {
		writeFailure(w, e)
		return
	}
	w.Header().Set("Server-Timing", "portfolio_origin;dur="+strconv.FormatFloat(float64(result.OriginDuration)/float64(time.Millisecond), 'f', 6, 64))
	writeJSON(w, http.StatusOK, struct {
		Version int `json:"version"`
		PortfolioResponse
	}{3, result})
}
