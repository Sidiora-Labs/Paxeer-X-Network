package auth

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// registerDevelopmentOwnerRoutes mounts the owner surface a development
// identity (no event delivery) can serve truthfully. GET /v2/me/wallet-links
// is a plain read of identity_wallet_links and is served exactly as in
// production. Linking and unlinking publish identity revision events, which
// development has no delivery for, so those writes answer the frozen 503
// DEPENDENCY_UNAVAILABLE envelope with retryable=false instead of the
// router's plain-text 404.
func (s *Identity) registerDevelopmentOwnerRoutes(router chi.Router) {
	router.Get("/v2/me/wallet-links", s.walletLinksHTTP)
	notServed := func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.AuthenticateRequest(r.Context(), r); err != nil {
			s.writeIdentityError(w, err)
			return
		}
		s.writeIdentityError(w, notServedError())
	}
	router.Post("/v2/me/wallet-links/challenges", notServed)
	router.Post("/v2/me/wallet-links/{linkId}:verify", notServed)
	router.Delete("/v2/me/wallet-links/{linkId}", notServed)
}

func notServedError() *PublicError {
	return &PublicError{Code: "DEPENDENCY_UNAVAILABLE", Message: "This operation is not served in this deployment", RequestID: uuid.NewString(), Retryable: false, FieldErrors: []FieldError{}, Status: http.StatusServiceUnavailable}
}

// WriteNotServed answers an operation that has no backing service in this
// deployment with the frozen error envelope: 503 DEPENDENCY_UNAVAILABLE,
// retryable=false, retryAfterMs=null and no Retry-After, so clients show
// their unavailable state and do not poll.
func WriteNotServed(w http.ResponseWriter) {
	p := notServedError()
	if id := w.Header().Get("X-Request-Id"); validUUID(id) {
		p.RequestID = id
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}
