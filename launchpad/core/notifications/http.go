package notifications

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/core/discovery"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type Authenticate func(*http.Request) (string, error)
type HTTP struct {
	Store             Store
	Authenticate      Authenticate
	Discovery         discovery.Store
	Recipient         RecipientConsumer
	InternalAuthorize func(*http.Request, string) error
}

func (h HTTP) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/discovery/notifications", func(w http.ResponseWriter, r *http.Request) {
		account, e := h.actor(r)
		if e != nil {
			w.WriteHeader(401)
			return
		}
		items, e := h.Store.List(r.Context(), account, 50)
		respond(w, items, e)
	})
	mux.HandleFunc("PATCH /v2/discovery/notifications/{id}", func(w http.ResponseWriter, r *http.Request) {
		account, e := h.actor(r)
		if e != nil {
			w.WriteHeader(401)
			return
		}
		var input struct {
			Read bool `json:"read"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		decoder.DisallowUnknownFields()
		if e = decoder.Decode(&input); e != nil {
			w.WriteHeader(400)
			return
		}
		var extra any
		if e = decoder.Decode(&extra); e != io.EOF {
			w.WriteHeader(400)
			return
		}
		e = h.Store.SetRead(r.Context(), account, r.PathValue("id"), input.Read)
		respond(w, map[string]bool{"read": input.Read}, e)
	})
	mux.HandleFunc("DELETE /v2/discovery/notifications/{id}", func(w http.ResponseWriter, r *http.Request) {
		account, e := h.actor(r)
		if e != nil {
			w.WriteHeader(401)
			return
		}
		e = h.Store.DeleteItem(r.Context(), account, r.PathValue("id"))
		respond(w, map[string]bool{"deleted": e == nil}, e)
	})
	return mux
}
func (h HTTP) actor(r *http.Request) (string, error) {
	if h.Authenticate == nil {
		return "", ErrUnavailable
	}
	a, e := h.Authenticate(r)
	if e != nil || a == "" {
		return "", ErrForbidden
	}
	return a, nil
}
func respond(w http.ResponseWriter, v any, e error) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json")
	if e != nil {
		code := 503
		if errors.Is(e, sql.ErrNoRows) {
			code = 404
		} else if errors.Is(e, ErrInvalid) {
			code = 400
		} else if errors.Is(e, ErrForbidden) {
			code = 403
		}
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "notification_unavailable"})
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

type ChatRequest struct {
	EventID, AccountID, ResourceID string
	CreatedAt                      time.Time
}
type ChatResponse struct {
	NotificationID string
	Created        bool
}
type InternalPolicy struct {
	Credentials                          *Credentials
	TokenRef, DeletePeerDNS, ChatPeerDNS string
}

func (p InternalPolicy) Authorize(r *http.Request, account string) error {
	if account == "" || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || p.Credentials == nil {
		return ErrForbidden
	}
	peer := ""
	if r.Method == "DELETE" {
		peer = p.DeletePeerDNS
	} else if r.Method == "POST" && r.URL.Path == "/internal/v2/notifications/chat" {
		peer = p.ChatPeerDNS
	}
	if peer == "" || len(r.TLS.PeerCertificates) == 0 || r.TLS.PeerCertificates[0].VerifyHostname(peer) != nil {
		return ErrForbidden
	}
	token, e := p.Credentials.Get(p.TokenRef)
	if e != nil {
		return ErrUnavailable
	}
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(header, "Bearer ")), []byte(token)) != 1 {
		return ErrForbidden
	}
	return nil
}
func (h HTTP) InternalHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /internal/v2/accounts/{accountId}/notification-data", func(w http.ResponseWriter, r *http.Request) {
		account := r.PathValue("accountId")
		deletion := r.URL.Query().Get("deletionId")
		uuid := regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
		if !uuid.MatchString(account) || !uuid.MatchString(deletion) {
			w.WriteHeader(400)
			return
		}
		if h.InternalAuthorize == nil {
			w.WriteHeader(503)
			return
		}
		if e := h.InternalAuthorize(r, account); e != nil {
			respond(w, nil, e)
			return
		}
		if h.Discovery.DB == nil {
			w.WriteHeader(503)
			return
		}
		if e := h.Discovery.DeleteAccount(r.Context(), account); e != nil {
			respond(w, nil, e)
			return
		}
		e := h.Store.DeleteAccount(r.Context(), account)
		respond(w, map[string]bool{"deleted": e == nil}, e)
	})
	mux.HandleFunc("POST /internal/v2/notifications/chat", func(w http.ResponseWriter, r *http.Request) {
		var in ChatRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if e := decoder.Decode(&in); e != nil {
			w.WriteHeader(400)
			return
		}
		var extra any
		if e := decoder.Decode(&extra); e != io.EOF {
			w.WriteHeader(400)
			return
		}
		if h.InternalAuthorize == nil {
			w.WriteHeader(503)
			return
		}
		if e := h.InternalAuthorize(r, in.AccountID); e != nil {
			respond(w, nil, e)
			return
		}
		n, created, e := h.Recipient.EnqueueChat(r.Context(), in.EventID, in.AccountID, in.ResourceID, in.CreatedAt)
		respond(w, ChatResponse{NotificationID: n.ID, Created: created}, e)
	})
	return mux
}
