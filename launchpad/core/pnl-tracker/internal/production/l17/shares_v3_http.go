package l17

import (
	"encoding/json"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/go-chi/chi/v5"
	"net/http"
)

func (s *SharesV3) RegisterHTTP(router chi.Router) {
	router.Post("/v3/me/portfolio/shares/preview", s.previewHTTP)
	router.Post("/v3/me/portfolio/shares", s.publishHTTP)
	router.Get("/v3/me/portfolio/shares/{shareId}", s.ownerHTTP)
	router.Delete("/v3/me/portfolio/shares/{shareId}", s.revokeHTTP)
	router.Get("/v3/portfolio/shares/{shareId}", s.publicHTTP)
	router.Get("/v3/portfolio/shares/{shareId}/image.png", s.imageHTTP)
}
func (s *SharesV3) principalV3(r *http.Request) (auth.IdentityPrincipal, error) {
	if s == nil || s.authority == nil {
		return auth.IdentityPrincipal{}, ErrUnavailable
	}
	if r.URL.RawQuery != "" {
		return auth.IdentityPrincipal{}, ErrInvalid
	}
	for _, header := range []string{"Authorization", "Origin", "X-CSRF-Token", "Idempotency-Key"} {
		if len(r.Header.Values(header)) > 1 {
			return auth.IdentityPrincipal{}, ErrUnauthorized
		}
	}
	cookies := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == "__Host-kindle_access" {
			cookies++
		}
	}
	if cookies > 1 {
		return auth.IdentityPrincipal{}, ErrUnauthorized
	}
	return s.authority.AuthenticateRequest(r.Context(), r)
}
func (s *SharesV3) previewHTTP(w http.ResponseWriter, r *http.Request) {
	p, e := s.principalV3(r)
	if e != nil {
		writeFailure(w, e)
		return
	}
	var body struct {
		SnapshotID    string          `json:"snapshotId"`
		Fields        []string        `json:"fields"`
		IncludeWallet json.RawMessage `json:"includeWallet"`
		ExpiresAt     json.RawMessage `json:"expiresAt"`
	}
	if e = decodeBody(w, r, &body); e != nil {
		writeFailure(w, e)
		return
	}
	selection := ShareSelectionV3{SnapshotID: body.SnapshotID, Fields: body.Fields}
	if len(body.IncludeWallet) > 0 {
		if string(body.IncludeWallet) == "null" || json.Unmarshal(body.IncludeWallet, &selection.IncludeWallet) != nil {
			writeFailure(w, ErrInvalid)
			return
		}
	}
	if len(body.ExpiresAt) == 0 || string(body.ExpiresAt) == "null" || json.Unmarshal(body.ExpiresAt, &selection.ExpiresAt) != nil {
		writeFailure(w, ErrInvalid)
		return
	}
	preview, e := s.Preview(r.Context(), p, selection)
	if e != nil {
		writeFailure(w, e)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}
func (s *SharesV3) publishHTTP(w http.ResponseWriter, r *http.Request) {
	p, e := s.principalV3(r)
	if e != nil {
		writeFailure(w, e)
		return
	}
	var body SharePublishV3
	if e = decodeBody(w, r, &body); e != nil {
		writeFailure(w, e)
		return
	}
	share, e := s.Publish(r.Context(), p, body, r.Header.Get("Idempotency-Key"))
	if e != nil {
		writeFailure(w, e)
		return
	}
	writeJSON(w, http.StatusAccepted, share)
}
func (s *SharesV3) ownerHTTP(w http.ResponseWriter, r *http.Request) {
	p, e := s.principalV3(r)
	if e != nil {
		writeFailure(w, e)
		return
	}
	share, e := s.Owner(r.Context(), p, chi.URLParam(r, "shareId"))
	if e != nil {
		writeFailure(w, e)
		return
	}
	writeJSON(w, http.StatusOK, share)
}
func (s *SharesV3) revokeHTTP(w http.ResponseWriter, r *http.Request) {
	p, e := s.principalV3(r)
	if e != nil {
		writeFailure(w, e)
		return
	}
	share, e := s.Revoke(r.Context(), p, chi.URLParam(r, "shareId"))
	if e != nil {
		writeFailure(w, e)
		return
	}
	writeJSON(w, http.StatusAccepted, share)
}
func (s *SharesV3) publicHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeFailure(w, ErrInvalid)
		return
	}
	share, _, e := s.Public(r.Context(), chi.URLParam(r, "shareId"))
	if e != nil {
		writeFailure(w, e)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	writeJSON(w, http.StatusOK, share)
}
func (s *SharesV3) imageHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeFailure(w, ErrInvalid)
		return
	}
	_, image, e := s.Public(r.Context(), chi.URLParam(r, "shareId"))
	if e != nil {
		writeFailure(w, e)
		return
	}
	if len(image) == 0 {
		writeFailure(w, ErrUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(image)
}
