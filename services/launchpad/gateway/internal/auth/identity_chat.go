package auth

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/go-chi/chi/v5"
	"io"
	"net/http"
	"strings"
)

func chatBinding(binding sharedauth.StreamTicketBinding) bool {
	for _, topic := range binding.Topics {
		if strings.HasPrefix(topic, "chat.") || strings.HasPrefix(topic, "private.chat.") {
			return true
		}
	}
	return false
}
func (s *Identity) IssueChatStreamTicket(ctx context.Context, p Principal, binding sharedauth.StreamTicketBinding) (string, error) {
	return s.issueBoundTicket(ctx, p, binding, sharedauth.ValidateChatStreamBinding)
}
func (s *Identity) ConsumeChatStreamTicket(ctx context.Context, secret string, binding sharedauth.StreamTicketBinding) (sharedauth.StreamPrincipal, error) {
	return s.consumeBoundTicket(ctx, secret, binding, sharedauth.ValidateChatStreamBinding)
}
func (s *Identity) registerChatTicketRoute(router chi.Router, internal func(http.HandlerFunc) http.HandlerFunc) {
	router.Post("/internal/v2/identity/chat-ticket:consume", internal(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Ticket  string                         `json:"ticket"`
			Binding sharedauth.StreamTicketBinding `json:"binding"`
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
		if err != nil || len(raw) > 16384 || validateJSONKeys(json.NewDecoder(bytes.NewReader(raw))) != nil || strictJSON(raw, &request) != nil {
			s.writeIdentityError(w, invalid())
			return
		}
		principal, err := s.ConsumeChatStreamTicket(r.Context(), request.Ticket, request.Binding)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(principal)
	}))
}
func authenticateIdentityService(r *http.Request, cfg IdentityHTTPConfig) error {
	if len(cfg.ServiceVerifiers) > 0 {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
		if err != nil || len(raw) > 16384 {
			return unauthorized()
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		for _, verifier := range cfg.ServiceVerifiers {
			if verifier.Authenticate(r, raw) == nil {
				return nil
			}
		}
		return unauthorized()
	}
	if len(r.Header.Values("Authorization")) != 1 || subtle.ConstantTimeCompare([]byte(bearer(r.Header.Get("Authorization"))), []byte(cfg.ServiceToken)) != 1 {
		return unauthorized()
	}
	return nil
}
