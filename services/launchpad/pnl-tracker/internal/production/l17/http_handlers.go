package l17

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"unicode/utf8"
)

func (s Service) RegisterHTTP(router chi.Router) {
	router.Get("/v2/me/portfolio", s.portfolioHTTP)
	router.Get("/v2/me/portfolio/history", s.historyHTTP)
	router.Get("/v2/wallets/{wallet}/portfolio", s.publicPortfolioHTTP)
	router.Post("/v2/me/portfolio/exports", s.createExportHTTP)
	router.Get("/v2/me/portfolio/exports/{jobId}", s.exportStatusHTTP)
	router.Get("/v2/me/portfolio/exports/{jobId}/download", s.downloadHTTP)
	router.Delete("/v2/me/portfolio/exports/{jobId}", s.cancelExportHTTP)
	router.Post("/v2/me/portfolio/cards", s.createCardHTTP)
	router.Get("/v2/me/portfolio/cards/{cardId}", s.ownerCardHTTP)
	router.Delete("/v2/me/portfolio/cards/{cardId}", s.revokeCardHTTP)
	router.Get("/v2/portfolio/cards/{cardId}/image.png", s.publicCardHTTP)
}

func (s Service) self(r *http.Request) (auth.IdentityPrincipal, error) {
	if s.Authority == nil {
		return auth.IdentityPrincipal{}, ErrUnavailable
	}
	return s.Authority.AuthenticateRequest(r.Context(), r)
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeFailure(w http.ResponseWriter, e error) {
	status := http.StatusServiceUnavailable
	code := "DEPENDENCY_UNAVAILABLE"
	retry := true
	switch {
	case errors.Is(e, ErrInvalid):
		status = 400
		code = "FIELD_INVALID"
		retry = false
	case errors.Is(e, ErrRestart):
		status = 409
		code = "REVISION_MISMATCH"
		retry = false
	case errors.Is(e, ErrConflict):
		status = 409
		code = "IDEMPOTENCY_CONFLICT"
		retry = false
	case errors.Is(e, ErrUnauthorized) || errors.Is(e, auth.ErrScopeDenied):
		status = 403
		code = "SCOPE_DENIED"
		retry = false
	case errors.Is(e, ErrGone):
		status = 410
		code = "RESOURCE_DELETED"
		retry = false
	case errors.Is(e, ErrNotFound) || errors.Is(e, pgx.ErrNoRows):
		status = 404
		code = "NOT_FOUND"
		retry = false
	case errors.Is(e, ErrBusy):
		status = 429
		code = "RATE_LIMITED"
		w.Header().Set("Retry-After", "1")
	case errors.Is(e, context.DeadlineExceeded):
		w.Header().Set("Retry-After", "1")
	}
	var authority *auth.IdentityAuthorityError
	if errors.As(e, &authority) {
		status, code, retry = auth.PublicCode(e)
	}
	id, _ := opaqueID()
	writeJSON(w, status, interfaces.Error{Code: code, Message: "Portfolio request unavailable", RequestId: id, Retryable: retry, FieldErrors: []interfaces.FieldError{}, Details: nil, RetryAfterMs: nil})
}
func decodeBody(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	raw, e := io.ReadAll(r.Body)
	if e != nil || !utf8.Valid(raw) {
		return ErrInvalid
	}
	if e = uniqueJSON(raw); e != nil {
		return e
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(v); e != nil {
		return ErrInvalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return ErrInvalid
	}
	return nil
}
func (s Service) portfolioHTTP(w http.ResponseWriter, r *http.Request) {
	p, e := s.self(r)
	if e != nil {
		writeFailure(w, e)
		return
	}
	s.servePortfolioHTTP(w, r, p.Wallet)
}
func (s Service) publicPortfolioHTTP(w http.ResponseWriter, r *http.Request) {
	wallet, e := Wallet(chi.URLParam(r, "wallet"))
	if e != nil {
		writeFailure(w, ErrInvalid)
		return
	}
	s.servePortfolioHTTP(w, r, wallet)
}
func (s Service) servePortfolioHTTP(w http.ResponseWriter, r *http.Request, wallet string) {
	var e error
	query, queryErr := portfolioQuery(r)
	if queryErr != nil {
		writeFailure(w, queryErr)
		return
	}
	for k, v := range query {
		if (k != "cursor" && k != "limit") || len(v) != 1 {
			writeFailure(w, ErrInvalid)
			return
		}
	}
	limit := 0
	if query.Get("limit") != "" {
		limit, e = strconv.Atoi(query.Get("limit"))
		if e != nil {
			writeFailure(w, ErrInvalid)
			return
		}
	}
	now := time.Now().UTC()
	result, e := s.Portfolio(r.Context(), wallet, query.Get("cursor"), limit, now)
	if e != nil {
		writeFailure(w, e)
		return
	}
	w.Header().Set("Server-Timing", "portfolio_origin;dur="+strconv.FormatFloat(float64(result.OriginDuration)/float64(time.Millisecond), 'f', 6, 64))
	raw, e := json.Marshal(struct {
		Summary Summary    `json:"summary"`
		Items   []Position `json:"items"`
	}{result.Page.Summary, result.Page.Positions})
	if e != nil {
		writeFailure(w, e)
		return
	}
	var data interfaces.PortfolioData
	if e = json.Unmarshal(raw, &data); e != nil {
		writeFailure(w, e)
		return
	}
	var next *string
	if result.Page.Next != "" {
		next = ptr(result.Page.Next)
	}
	requestID, _ := opaqueID()
	revision := interfaces.PositiveUInt256(result.Page.Summary.Watermark.ProjectionRevision)
	wmRaw, _ := json.Marshal(result.Page.Summary.Watermark)
	var watermark interfaces.Watermark
	if e = json.Unmarshal(wmRaw, &watermark); e != nil {
		writeFailure(w, e)
		return
	}
	quality := "complete"
	if data.Summary.BasisQuality != "complete" || data.Summary.PricingQuality != "complete" {
		quality = "partial"
	}
	response := interfaces.PortfolioResponse{RequestId: requestID, Data: data, Watermark: &watermark, Revision: &revision, Quality: quality, NextCursor: next, GeneratedAt: now.Format("2006-01-02T15:04:05.000Z")}
	view := struct {
		interfaces.PortfolioResponse
		PositionErrors map[string][]string `json:"positionErrors"`
		Display        *USDDisplay         `json:"display,omitempty"`
		QuoteAsset     string              `json:"quoteAsset"`
		Decimals       int                 `json:"decimals"`
		Algorithm      string              `json:"algorithm"`
	}{response, result.Errors, result.Display, result.QuoteAsset, result.Decimals, result.Algorithm}
	raw, e = json.Marshal(view)
	if e != nil {
		writeFailure(w, e)
		return
	}
	if e = interfaces.ValidateJSON("PortfolioResponse", raw); e != nil {
		writeFailure(w, e)
		return
	}
	writeJSON(w, 200, view)
}
func (s Service) createExportHTTP(w http.ResponseWriter, r *http.Request) {
	p, e := s.self(r)
	if e != nil {
		writeFailure(w, e)
		return
	}
	var body struct {
		Snapshot string `json:"snapshotId"`
		Format   string `json:"format"`
	}
	if e = decodeBody(w, r, &body); e != nil {
		writeFailure(w, e)
		return
	}
	job, e := s.Repository.CreateExportJob(r.Context(), p.Wallet, p.Wallet, body.Snapshot, body.Format, r.Header.Get("Idempotency-Key"))
	if e != nil {
		writeFailure(w, e)
		return
	}
	writeJSON(w, 202, job)
}
func (s Service) exportStatusHTTP(w http.ResponseWriter, r *http.Request) {
	p, e := s.self(r)
	if e != nil {
		writeFailure(w, e)
		return
	}
	job, e := s.Repository.ExportJob(r.Context(), chi.URLParam(r, "jobId"), p.Wallet, p.Wallet)
	if e != nil {
		writeFailure(w, e)
		return
	}
	if job.Expires != nil && !time.Now().Before(*job.Expires) {
		job.Manifest = nil
		job.State = "expired"
	}
	writeJSON(w, 200, job)
}
func (s Service) cancelExportHTTP(w http.ResponseWriter, r *http.Request) {
	p, e := s.self(r)
	if e != nil {
		writeFailure(w, e)
		return
	}
	if e = s.Repository.CancelExport(r.Context(), chi.URLParam(r, "jobId"), p.Wallet, p.Wallet); e != nil {
		writeFailure(w, e)
		return
	}
	writeJSON(w, 202, struct {
		State string `json:"state"`
	}{"cancelled"})
}
func (s Service) downloadHTTP(w http.ResponseWriter, r *http.Request) {
	p, e := s.self(r)
	if e != nil {
		writeFailure(w, e)
		return
	}
	file, manifest, e := s.Repository.OpenExport(r.Context(), s.ExportsDir, chi.URLParam(r, "jobId"), p.Wallet, p.Wallet)
	if e != nil {
		writeFailure(w, e)
		return
	}
	defer file.Close()
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="portfolio.`+manifest.Format+`"`)
	w.Header().Set("Content-Type", "application/x-ndjson")
	if manifest.Format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	}
	_, _ = io.Copy(w, file)
}
func (s Service) createCardHTTP(w http.ResponseWriter, r *http.Request) {
	p, e := s.self(r)
	if e != nil {
		writeFailure(w, e)
		return
	}
	var body struct {
		Snapshot      string `json:"snapshotId"`
		IncludeWallet bool   `json:"includeWallet"`
	}
	if e = decodeBody(w, r, &body); e != nil {
		writeFailure(w, e)
		return
	}
	card, e := s.Repository.CreateCardV2(r.Context(), p.Wallet, p.Wallet, body.Snapshot, r.Header.Get("Idempotency-Key"), body.IncludeWallet, time.Now())
	if e != nil {
		writeFailure(w, e)
		return
	}
	writeJSON(w, 202, card)
}
func (s Service) ownerCardHTTP(w http.ResponseWriter, r *http.Request) {
	p, e := s.self(r)
	if e != nil {
		writeFailure(w, e)
		return
	}
	card, e := s.Repository.OwnerCardV2(r.Context(), chi.URLParam(r, "cardId"), p.Wallet, p.Wallet, time.Now())
	if e != nil {
		writeFailure(w, e)
		return
	}
	writeJSON(w, 200, card)
}
func (s Service) revokeCardHTTP(w http.ResponseWriter, r *http.Request) {
	p, e := s.self(r)
	if e != nil {
		writeFailure(w, e)
		return
	}
	if e = s.Repository.RevokeCard(r.Context(), chi.URLParam(r, "cardId"), p.Wallet, p.Wallet, time.Now()); e != nil {
		writeFailure(w, e)
		return
	}
	writeJSON(w, 202, struct {
		State string `json:"state"`
	}{"revoked"})
}
func (s Service) publicCardHTTP(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "cardId")
	if !validUUID(id) {
		writeFailure(w, ErrNotFound)
		return
	}
	card, e := s.Repository.PublicCard(r.Context(), id)
	if e != nil {
		writeFailure(w, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(card.Image)
}

func uniqueJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value func() error
	value = func() error {
		token, e := decoder.Token()
		if e != nil {
			return ErrInvalid
		}
		delimiter, container := token.(json.Delim)
		if !container {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, e := decoder.Token()
				if e != nil {
					return ErrInvalid
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return ErrInvalid
				}
				seen[name] = true
				if e = value(); e != nil {
					return e
				}
			}
			end, e := decoder.Token()
			if e != nil || end != json.Delim('}') {
				return ErrInvalid
			}
		case '[':
			for decoder.More() {
				if e = value(); e != nil {
					return e
				}
			}
			end, e := decoder.Token()
			if e != nil || end != json.Delim(']') {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
		return nil
	}
	if e := value(); e != nil {
		return e
	}
	if _, e := decoder.Token(); e != io.EOF {
		return ErrInvalid
	}
	return nil
}

func (s Service) historyHTTP(w http.ResponseWriter, r *http.Request) {
	p, e := s.self(r)
	if e != nil {
		writeFailure(w, e)
		return
	}
	query, queryErr := portfolioQuery(r)
	if queryErr != nil {
		writeFailure(w, queryErr)
		return
	}
	for key, values := range query {
		if (key != "from" && key != "to" && key != "interval" && key != "limit") || len(values) != 1 {
			writeFailure(w, ErrInvalid)
			return
		}
	}
	from, e := time.Parse("2006-01-02T15:04:05.000Z", query.Get("from"))
	if e != nil {
		writeFailure(w, ErrInvalid)
		return
	}
	to, e := time.Parse("2006-01-02T15:04:05.000Z", query.Get("to"))
	if e != nil {
		writeFailure(w, ErrInvalid)
		return
	}
	limit := 0
	if query.Get("limit") != "" {
		limit, e = strconv.Atoi(query.Get("limit"))
		if e != nil {
			writeFailure(w, ErrInvalid)
			return
		}
	}
	items, last, e := s.Repository.HistorySeries(r.Context(), p.Wallet, p.Wallet, from, to, query.Get("interval"), limit)
	if e != nil {
		writeFailure(w, e)
		return
	}
	now := time.Now().UTC()
	id, _ := opaqueID()
	var revision *interfaces.PositiveUInt256
	var watermark *interfaces.Watermark
	if last != nil {
		v := interfaces.PositiveUInt256(last.Watermark.ProjectionRevision)
		revision = &v
		raw, _ := json.Marshal(last.Watermark)
		var wm interfaces.Watermark
		if e = json.Unmarshal(raw, &wm); e != nil {
			writeFailure(w, e)
			return
		}
		watermark = &wm
	}
	response := interfaces.PnlHistoryResponse{RequestId: id, Data: interfaces.PnlHistoryData{Items: items}, Revision: revision, Watermark: watermark, GeneratedAt: now.Format("2006-01-02T15:04:05.000Z"), NextCursor: nil, Quality: "partial"}
	historical := HistoricalReturn{Method: "unavailable", Reason: "incomplete_cash_flow_or_valuation_history"}
	if last != nil {
		historical, e = s.Repository.HistoricalReturn(r.Context(), p.Wallet, p.Wallet, last.SnapshotID, from, to)
		if e != nil {
			writeFailure(w, e)
			return
		}
	}
	view := struct {
		interfaces.PnlHistoryResponse
		Return        HistoricalReturn `json:"historicalReturn"`
		MoneyWeighted HistoricalReturn `json:"moneyWeightedReturn"`
	}{response, historical, HistoricalReturn{Method: "unavailable", Reason: "approved_money_weighted_algorithm_unavailable"}}
	raw, e := json.Marshal(view)
	if e != nil {
		writeFailure(w, e)
		return
	}
	if e = interfaces.ValidateJSON("PnlHistoryResponse", raw); e != nil {
		writeFailure(w, e)
		return
	}
	writeJSON(w, 200, view)
}

func portfolioQuery(r *http.Request) (url.Values, error) {
	if len(r.URL.RawQuery) > 4096 {
		return nil, ErrInvalid
	}
	query, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return nil, ErrInvalid
	}
	return query, nil
}
