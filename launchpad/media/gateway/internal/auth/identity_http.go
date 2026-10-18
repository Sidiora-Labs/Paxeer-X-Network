package auth

import (
	"bytes"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

func identityClientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	return ip
}
func (s *Identity) identityRequestBoundary(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if !validUUID(id) {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		for key := range r.URL.Query() {
			if strings.EqualFold(key, "token") || strings.EqualFold(key, "accessToken") || strings.EqualFold(key, "refreshToken") {
				StripLegacyCredentialQuery(r)
				s.writeIdentityError(w, unauthorized())
				return
			}
		}
		if err := validateIdentityTransport(r); err != nil {
			s.writeIdentityError(w, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func decodeOwnerRequest(r *http.Request, dest any, pathID string) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
	if err != nil || len(raw) > 16384 {
		return nil, invalid()
	}
	if pathID == "" {
		if err = DecodeIdentityRequest(bytes.NewReader(raw), dest); err != nil {
			return nil, err
		}
		return raw, nil
	}
	if !validUUID(pathID) || validateJSONKeys(json.NewDecoder(bytes.NewReader(raw))) != nil {
		return nil, invalid()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, invalid()
	}
	if _, exists := fields["linkId"]; exists {
		return nil, identityError("FIELD_UNKNOWN", 422)
	}
	id, _ := json.Marshal(pathID)
	fields["linkId"] = id
	combined, err := json.Marshal(fields)
	if err != nil {
		return nil, invalid()
	}
	if err = DecodeIdentityRequest(bytes.NewReader(combined), dest); err != nil {
		return nil, err
	}
	return raw, nil
}
func (s *Identity) registerOwnerV2Routes(router chi.Router) {
	router.Get("/v2/auth/key-metadata", func(w http.ResponseWriter, r *http.Request) {
		s.writeIdentity(w, struct {
			Keys []IdentityKeyMetadata `json:"keys"`
		}{s.tokens.KeyMetadata()})
	})
	router.Post("/v2/me/wallet-links/challenges", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.AuthenticateRequest(r.Context(), r)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		var req WalletLinkChallengeRequest
		body, err := decodeOwnerRequest(r, &req, "")
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		data, err := s.IssueLinkIdempotent(r.Context(), p, req, identityClientIP(r), r.Header.Get("Idempotency-Key"), body)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		s.writeIdentityStatus(w, http.StatusCreated, data)
	})
	router.Post("/v2/me/wallet-links/{linkId}:verify", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.AuthenticateRequest(r.Context(), r)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		var req WalletLinkVerifyRequest
		body, err := decodeOwnerRequest(r, &req, chi.URLParam(r, "linkId"))
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		data, err := s.VerifyLinkIdempotent(r.Context(), p, req, r.Header.Get("Idempotency-Key"), body)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		s.writeIdentity(w, data)
	})
	router.Delete("/v2/me/wallet-links/{linkId}", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.AuthenticateRequest(r.Context(), r)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		var req WalletLinkRemoveRequest
		body, err := decodeOwnerRequest(r, &req, chi.URLParam(r, "linkId"))
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		_, err = s.UnlinkIdempotent(r.Context(), p, req, r.Header.Get("Idempotency-Key"), body)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	})
	router.Get("/v2/me/wallet-links", s.walletLinksHTTP)
	router.Post("/v2/auth/sessions:logout-all", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.AuthenticateRequest(r.Context(), r)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		var req RefreshRequest
		if _, err = decodeOwnerRequest(r, &req, ""); err != nil {
			s.writeIdentityError(w, err)
			return
		}
		if err = s.Logout(r.Context(), p, true); err != nil {
			s.writeIdentityError(w, err)
			return
		}
		for _, name := range []string{"__Host-kindle_refresh", "__Host-kindle_access", "__Host-kindle_csrf"} {
			cookie := SessionCookie(name, "", time.Unix(0, 0))
			cookie.MaxAge = -1
			http.SetCookie(w, cookie)
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	})
	router.Post("/v2/auth/reauth/challenges", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.AuthenticateRequest(r.Context(), r)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		var req struct {
			Action string `json:"action"`
		}
		if err = decodeClosedOwner(r, &req); err != nil {
			s.writeIdentityError(w, err)
			return
		}
		var challenge Challenge
		switch req.Action {
		case "wallet_unlink":
			challenge, err = s.IssueStepUp(r.Context(), p, identityClientIP(r))
		case "account.delete":
			challenge, err = s.IssueActionStepUp(r.Context(), p, identityClientIP(r), req.Action)
		default:
			err = invalid()
		}
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		s.writeIdentityStatus(w, http.StatusCreated, struct {
			Challenge Challenge `json:"challenge"`
		}{challenge})
	})
	router.Post("/v2/auth/reauth:verify", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.AuthenticateRequest(r.Context(), r)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		var req struct {
			Action      string      `json:"action"`
			ChallengeID string      `json:"challengeId"`
			Proof       WalletProof `json:"proof"`
		}
		if err = decodeClosedOwner(r, &req); err != nil || validateWalletProof(req.Proof) != nil || !validUUID(req.ChallengeID) {
			s.writeIdentityError(w, invalid())
			return
		}
		var receipt string
		switch req.Action {
		case "wallet_unlink":
			receipt, err = s.VerifyStepUp(r.Context(), p, req.ChallengeID, req.Proof)
		case "account.delete":
			receipt, err = s.VerifyActionStepUp(r.Context(), p, req.ChallengeID, req.Proof, req.Action)
		default:
			err = invalid()
		}
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		s.writeIdentity(w, struct {
			ReauthProof string `json:"reauthProof"`
			ExpiresAt   string `json:"expiresAt"`
		}{receipt, wireTime(s.now().Add(5 * time.Minute))})
	})
	router.Get("/v2/auth/recovery-guidance", func(w http.ResponseWriter, r *http.Request) {
		s.observe("recovery_guidance")
		s.writeIdentity(w, struct {
			State          string `json:"state"`
			Guidance       string `json:"guidance"`
			CanRecoverKeys bool   `json:"canRecoverKeys"`
		}{"verified_wallet_required", "Sign in with another verified wallet. If the only verified wallet is unavailable, use your wallet provider's recovery process. KindleLaunch cannot restore keys or transfer token ownership.", false})
	})
}
func decodeClosedOwner(r *http.Request, dest any) error {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
	if err != nil || len(raw) > 16384 || validateJSONKeys(json.NewDecoder(bytes.NewReader(raw))) != nil || strictJSON(raw, dest) != nil {
		return invalid()
	}
	return nil
}
func (s *Identity) writeIdentityStatus(w http.ResponseWriter, status int, data any) {
	envelope := s.Envelope(data)
	if id := w.Header().Get("X-Request-Id"); validUUID(id) {
		envelope.RequestID = id
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(envelope)
}

// walletLinksHTTP serves GET /v2/me/wallet-links for the session's account.
func (s *Identity) walletLinksHTTP(w http.ResponseWriter, r *http.Request) {
	p, err := s.AuthenticateRequest(r.Context(), r)
	if err != nil {
		s.writeIdentityError(w, err)
		return
	}
	if len(r.URL.Query()) != 0 {
		s.writeIdentityError(w, invalid())
		return
	}
	items, err := s.WalletLinks(r.Context(), p)
	if err != nil {
		s.writeIdentityError(w, err)
		return
	}
	s.writeIdentity(w, struct {
		Items []WalletLink `json:"items"`
	}{items})
}
