package auth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SessionCookie uses a host-only secure transport. Refresh is never serialized in the public JSON.
func SessionCookie(name, value string, expires time.Time) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, Path: "/", Expires: expires, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode}
}

// AuthenticateRequest never reads token query strings; cookie mutations require CSRF and allowed origin.
func (s *Identity) AuthenticateRequest(ctx context.Context, r *http.Request) (Principal, error) {
	if r == nil || r.URL == nil {
		return Principal{}, unauthorized()
	}
	for k := range r.URL.Query() {
		if strings.EqualFold(k, "token") || strings.EqualFold(k, "accessToken") || strings.EqualFold(k, "refreshToken") {
			return Principal{}, unauthorized()
		}
	}
	if err := validateIdentityTransport(r); err != nil {
		return Principal{}, err
	}
	origin := r.Header.Get("Origin")
	if origin != "" && !s.origins[origin] {
		return Principal{}, identityError("SCOPE_DENIED", 403)
	}
	token := bearer(r.Header.Get("Authorization"))
	if r.Header.Get("Authorization") != "" && token == "" {
		return Principal{}, unauthorized()
	}
	cookieAuth := false
	if token == "" {
		cookie, e := r.Cookie("__Host-kindle_access")
		if e != nil {
			return Principal{}, unauthorized()
		}
		token = cookie.Value
		cookieAuth = true
	}
	var p Principal
	var e error
	if s.tokens != nil {
		p, e = s.AuthenticateJWT(ctx, s.tokens, token)
	} else {
		p, e = s.Authenticate(ctx, token)
	}
	if e != nil {
		return Principal{}, e
	}
	if cookieAuth && r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
		if origin == "" || !s.origins[origin] {
			return Principal{}, identityError("SCOPE_DENIED", 403)
		}
		csrf := r.Header.Get("X-CSRF-Token")
		if len(csrf) != 64 {
			return Principal{}, identityError("SCOPE_DENIED", 403)
		}
		var expected []byte
		e = s.pool.QueryRow(ctx, `SELECT csrf_hash FROM identity_sessions WHERE session_id=$1 AND revoked_at IS NULL`, p.sessionID).Scan(&expected)
		if e != nil {
			return Principal{}, unavailable()
		}
		if subtle.ConstantTimeCompare(expected, digest(csrf)) != 1 {
			return Principal{}, identityError("SCOPE_DENIED", 403)
		}
	}
	return p, nil
}
func (s *Identity) Envelope(data any) Envelope[any] {
	return Envelope[any]{RequestID: uuid.NewString(), Data: data, Quality: "complete", GeneratedAt: s.now().UTC().Truncate(time.Millisecond)}
}

// StripLegacyCredentialQuery lets access logging redact rejected legacy credential URLs.
func StripLegacyCredentialQuery(r *http.Request) {
	q := r.URL.Query()
	for k := range q {
		if strings.EqualFold(k, "token") || strings.EqualFold(k, "accessToken") || strings.EqualFold(k, "refreshToken") {
			q.Set(k, "[REDACTED]")
		}
	}
	r.URL.RawQuery = q.Encode()
	r.RequestURI = r.URL.RequestURI()
}

type IdentityHTTPConfig struct {
	ServiceToken     string
	ServiceVerifiers []sharedauth.ServiceRequestVerifier
	// DevelopmentOwnerReads mounts the owner reads that need no identity event
	// delivery (GET /v2/me/wallet-links) when Events is not configured, and
	// answers the owner writes with a non-retryable 503. Development only.
	DevelopmentOwnerReads bool
}

func (s *Identity) streamPrincipal(p Principal) sharedauth.StreamPrincipal {
	return sharedauth.StreamPrincipal{AccountID: p.accountID, SessionID: p.sessionID, Wallet: p.wallet, AuthEpoch: p.epoch}
}
func (s *Identity) IssueScopedTicket(ctx context.Context, p Principal, binding sharedauth.StreamTicketBinding) (string, error) {
	return s.issueBoundTicket(ctx, p, binding, sharedauth.ValidateStreamBinding)
}
func (s *Identity) issueBoundTicket(ctx context.Context, p Principal, binding sharedauth.StreamTicketBinding, validate func(sharedauth.StreamPrincipal, sharedauth.StreamTicketBinding) error) (string, error) {
	binding.Topics = append([]string(nil), binding.Topics...)
	sort.Strings(binding.Topics)
	if !s.origins[binding.Origin] || validate(s.streamPrincipal(p), binding) != nil {
		return "", identityError("SCOPE_DENIED", 403)
	}
	secret, err := randomSecret()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(binding)
	if err != nil {
		return "", invalid()
	}
	tag, err := s.pool.Exec(ctx, `INSERT INTO identity_tickets(ticket_hash,session_id,origin,endpoint,issued_at,expires_at,binding) SELECT $1,s.session_id,$3,$4,clock_timestamp(),clock_timestamp()+interval '30 seconds',$5::jsonb FROM identity_sessions s JOIN identity_accounts a USING(account_id) WHERE s.session_id=$2 AND s.account_id=$6 AND s.auth_epoch=$7 AND a.auth_epoch=$7 AND s.wallet=$8 AND s.revoked_at IS NULL AND s.access_expires_at>clock_timestamp()`, digest(secret), p.sessionID, binding.Origin, binding.Endpoint, raw, p.accountID, p.epoch, p.wallet)
	if err != nil {
		return "", unavailable()
	}
	if tag.RowsAffected() != 1 {
		return "", unauthorized()
	}
	s.observe("ticket_issued")
	return secret, nil
}
func (s *Identity) ConsumeStreamTicket(ctx context.Context, secret string, binding sharedauth.StreamTicketBinding) (sharedauth.StreamPrincipal, error) {
	return s.consumeBoundTicket(ctx, secret, binding, sharedauth.ValidateStreamBinding)
}
func (s *Identity) consumeBoundTicket(ctx context.Context, secret string, binding sharedauth.StreamTicketBinding, validate func(sharedauth.StreamPrincipal, sharedauth.StreamTicketBinding) error) (sharedauth.StreamPrincipal, error) {
	binding.Topics = append([]string(nil), binding.Topics...)
	sort.Strings(binding.Topics)
	var p Principal
	if len(secret) != 64 || !s.origins[binding.Origin] {
		return sharedauth.StreamPrincipal{}, unauthorized()
	}
	raw, err := json.Marshal(binding)
	if err != nil {
		return sharedauth.StreamPrincipal{}, invalid()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return sharedauth.StreamPrincipal{}, unavailable()
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `UPDATE identity_tickets t SET consumed_at=clock_timestamp() FROM identity_sessions s JOIN identity_accounts a USING(account_id) WHERE t.ticket_hash=$1 AND t.session_id=s.session_id AND t.origin=$2 AND t.endpoint=$3 AND t.binding=$4::jsonb AND t.consumed_at IS NULL AND t.expires_at>clock_timestamp() AND s.revoked_at IS NULL AND s.auth_epoch=a.auth_epoch AND s.access_expires_at>clock_timestamp() RETURNING a.account_id::text,s.session_id::text,s.wallet,s.auth_epoch,s.reauthenticated_at`, digest(secret), binding.Origin, binding.Endpoint, raw).Scan(&p.accountID, &p.sessionID, &p.wallet, &p.epoch, &p.reauthenticatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return sharedauth.StreamPrincipal{}, unauthorized()
	}
	if err != nil {
		return sharedauth.StreamPrincipal{}, unavailable()
	}
	principal := s.streamPrincipal(p)
	if validate(principal, binding) != nil {
		return sharedauth.StreamPrincipal{}, identityError("SCOPE_DENIED", 403)
	}
	if err = s.AuthorizeTx(ctx, tx, p); err != nil {
		return sharedauth.StreamPrincipal{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return sharedauth.StreamPrincipal{}, unavailable()
	}
	s.observe("ticket_consumed")
	return principal, nil
}
func (s *Identity) CheckStreamSession(ctx context.Context, principal sharedauth.StreamPrincipal) error {
	p := Principal{accountID: principal.AccountID, sessionID: principal.SessionID, wallet: principal.Wallet, epoch: principal.AuthEpoch}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable()
	}
	defer tx.Rollback(ctx)
	if err = s.AuthorizeTx(ctx, tx, p); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return unavailable()
	}
	return nil
}
func (s *Identity) writeIdentity(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	envelope := s.Envelope(data)
	if id := w.Header().Get("X-Request-Id"); validUUID(id) {
		envelope.RequestID = id
	}
	json.NewEncoder(w).Encode(envelope)
}
func (s *Identity) writeIdentityError(w http.ResponseWriter, err error) {
	var p *PublicError
	if !errors.As(err, &p) {
		p = unavailable().(*PublicError)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if id := w.Header().Get("X-Request-Id"); validUUID(id) {
		copy := *p
		p = &copy
		p.RequestID = id
	}
	if p.RetryAfterMS != nil {
		seconds := (*p.RetryAfterMS + 999) / 1000
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
	}
	w.WriteHeader(p.Status)
	json.NewEncoder(w).Encode(p)
}
func (s *Identity) cookieCredentials(w http.ResponseWriter, c Credentials) {
	http.SetCookie(w, SessionCookie("__Host-kindle_access", c.AccessToken, c.Session.AccessExpiresAt))
	http.SetCookie(w, SessionCookie("__Host-kindle_refresh", c.RefreshSecret, c.Session.RefreshExpiresAt))
	csrf := SessionCookie("__Host-kindle_csrf", c.CSRFSecret, c.Session.RefreshExpiresAt)
	csrf.HttpOnly = false
	http.SetCookie(w, csrf)
	w.Header().Set("X-CSRF-Token", c.CSRFSecret)
}
func (s *Identity) refreshCSRF(ctx context.Context, r *http.Request, secret string) error {
	if !s.origins[r.Header.Get("Origin")] || len(r.Header.Get("X-CSRF-Token")) != 64 {
		return identityError("SCOPE_DENIED", 403)
	}
	if subtle.ConstantTimeCompare([]byte(s.refreshCSRFSecret(secret)), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
		return identityError("SCOPE_DENIED", 403)
	}
	return nil
}
func (s *Identity) RegisterV2Routes(router chi.Router, cfg IdentityHTTPConfig) error {
	if s.tokens == nil || len(cfg.ServiceToken) < 32 {
		return invalid()
	}
	router = router.With(s.identityRequestBoundary)
	if s.events != nil {
		s.registerOwnerV2Routes(router)
	} else if cfg.DevelopmentOwnerReads {
		s.registerDevelopmentOwnerRoutes(router)
	}
	router.Post("/v2/auth/challenges", func(w http.ResponseWriter, r *http.Request) {
		raw, readErr := io.ReadAll(io.LimitReader(r.Body, 16385))
		if readErr != nil || len(raw) > 16384 {
			s.writeIdentityError(w, invalid())
			return
		}
		var req ChallengeRequest
		if err := DecodeIdentityRequest(bytes.NewReader(raw), &req); err != nil {
			s.writeIdentityError(w, err)
			return
		}
		if req.Origin != r.Header.Get("Origin") {
			s.writeIdentityError(w, identityError("SCOPE_DENIED", 403))
			return
		}
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		challenge, err := s.IssueLoginIdempotent(r.Context(), req, ip, r.Header.Get("Idempotency-Key"), raw)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(s.Envelope(struct {
			Challenge Challenge `json:"challenge"`
		}{challenge}))
	})
	router.Post("/v2/auth/sessions", func(w http.ResponseWriter, r *http.Request) {
		if !s.origins[r.Header.Get("Origin")] {
			s.writeIdentityError(w, identityError("SCOPE_DENIED", 403))
			return
		}
		var req LoginRequest
		if err := DecodeIdentityRequest(r.Body, &req); err != nil {
			s.writeIdentityError(w, err)
			return
		}
		credentials, err := s.Login(r.Context(), req)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		s.cookieCredentials(w, credentials)
		s.writeIdentity(w, credentials.LoginData)
	})
	router.Post("/v2/auth/sessions:refresh", func(w http.ResponseWriter, r *http.Request) {
		var req RefreshRequest
		if err := DecodeIdentityRequest(r.Body, &req); err != nil {
			s.writeIdentityError(w, err)
			return
		}
		cookie, err := r.Cookie("__Host-kindle_refresh")
		if err != nil {
			s.writeIdentityError(w, unauthorized())
			return
		}
		if err = s.refreshCSRF(r.Context(), r, cookie.Value); err != nil {
			s.writeIdentityError(w, err)
			return
		}
		credentials, err := s.Refresh(r.Context(), cookie.Value)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		s.cookieCredentials(w, credentials)
		s.writeIdentity(w, credentials.LoginData)
	})
	router.Get("/v2/auth/sessions", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.AuthenticateRequest(r.Context(), r)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		items, next, err := s.listSessions(r.Context(), p, r)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		envelope := s.Envelope(struct {
			Items []Session `json:"items"`
		}{items})
		envelope.NextCursor = next
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(envelope)
	})
	// GET /v2/auth/session resolves the caller's current session from the
	// bearer or the __Host-kindle_access cookie (a page bootstrap after a
	// reload). It is a read: no CSRF secret is issued or required, and the
	// principal is re-read from the session row so a revoked or rotated
	// session answers AUTH_EXPIRED.
	router.Get("/v2/auth/session", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.AuthenticateRequest(r.Context(), r)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		verified, err := s.verifiedPrincipal(r.Context(), p)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		s.writeIdentity(w, verified)
	})
	router.Delete("/v2/auth/sessions/{sessionId}", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.AuthenticateRequest(r.Context(), r)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		id := chi.URLParam(r, "sessionId")
		if err = s.LogoutSession(r.Context(), p, id); err != nil {
			s.writeIdentityError(w, err)
			return
		}
		if id == p.sessionID {
			for _, name := range []string{"__Host-kindle_refresh", "__Host-kindle_csrf", "__Host-kindle_access"} {
				cookie := SessionCookie(name, "", time.Unix(0, 0))
				cookie.MaxAge = -1
				http.SetCookie(w, cookie)
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	})
	router.Post("/v2/stream/tickets", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.AuthenticateRequest(r.Context(), r)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		var binding sharedauth.StreamTicketBinding
		raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
		if err != nil || len(raw) > 16384 || strictJSON(raw, &binding) != nil {
			s.writeIdentityError(w, invalid())
			return
		}
		if binding.Origin != r.Header.Get("Origin") {
			s.writeIdentityError(w, identityError("SCOPE_DENIED", 403))
			return
		}
		var ticket string
		if chatBinding(binding) {
			ticket, err = s.IssueChatStreamTicket(r.Context(), p, binding)
		} else {
			ticket, err = s.IssueScopedTicket(r.Context(), p, binding)
		}
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		s.writeIdentity(w, struct {
			Ticket      string `json:"ticket"`
			ExpiresAt   string `json:"expiresAt"`
			Subprotocol string `json:"subprotocol"`
		}{ticket, wireTime(s.now().Add(30 * time.Second)), "sidiora.v2"})
	})
	internal := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if err := authenticateIdentityService(r, cfg); err != nil {
				s.writeIdentityError(w, unauthorized())
				return
			}
			next(w, r)
		}
	}
	s.registerChatTicketRoute(router, internal)
	router.Post("/internal/v2/identity/ticket:consume", internal(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Ticket  string                         `json:"ticket"`
			Binding sharedauth.StreamTicketBinding `json:"binding"`
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
		if err != nil || len(raw) > 16384 || strictJSON(raw, &req) != nil {
			s.writeIdentityError(w, invalid())
			return
		}
		p, err := s.ConsumeStreamTicket(r.Context(), req.Ticket, req.Binding)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(p)
	}))
	router.Post("/internal/v2/identity/request:authenticate", internal(func(w http.ResponseWriter, r *http.Request) {
		var request sharedauth.IdentityAuthenticationRequest
		raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
		if err != nil || len(raw) > 16384 || strictJSON(raw, &request) != nil || sharedauth.ValidateIdentityAuthenticationRequest(request) != nil {
			s.writeIdentityError(w, invalid())
			return
		}
		forwarded, err := http.NewRequestWithContext(r.Context(), request.Method, "https://identity.invalid/", nil)
		if err != nil {
			s.writeIdentityError(w, invalid())
			return
		}
		forwarded.Header.Set("Authorization", request.Authorization)
		forwarded.Header.Set("Origin", request.Origin)
		forwarded.Header.Set("X-CSRF-Token", request.CSRFToken)
		if request.AccessCookie != "" {
			forwarded.AddCookie(&http.Cookie{Name: "__Host-kindle_access", Value: request.AccessCookie})
		}
		p, err := s.AuthenticateRequest(r.Context(), forwarded)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		verified, err := s.verifiedPrincipal(r.Context(), p)
		if err != nil {
			s.writeIdentityError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(verified)
	}))
	router.Post("/internal/v2/identity/session:check", internal(func(w http.ResponseWriter, r *http.Request) {
		var p sharedauth.StreamPrincipal
		raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
		if err != nil || len(raw) > 16384 || strictJSON(raw, &p) != nil {
			s.writeIdentityError(w, invalid())
			return
		}
		if err = s.CheckStreamSession(r.Context(), p); err != nil {
			s.writeIdentityError(w, err)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	return nil
}

type identitySessionCursor struct {
	AccountID string    `json:"accountId"`
	Wallet    string    `json:"wallet"`
	SessionID string    `json:"sessionId"`
	Snapshot  time.Time `json:"snapshot"`
	Before    time.Time `json:"before"`
	BeforeID  string    `json:"beforeId"`
	Limit     int       `json:"limit"`
	ExpiresAt int64     `json:"expiresAt"`
}

func (s *Identity) sessionCursorMAC(raw []byte) []byte {
	mac := hmac.New(sha256.New, s.limiterKey)
	mac.Write([]byte("identity-session-page-v2\x00"))
	mac.Write(raw)
	return mac.Sum(nil)
}
func (s *Identity) listSessions(ctx context.Context, p Principal, r *http.Request) ([]Session, *string, error) {
	limit := 50
	query := r.URL.Query()
	for key := range query {
		if key != "cursor" && key != "limit" {
			return nil, nil, invalid()
		}
	}
	if values := query["limit"]; len(values) > 1 {
		return nil, nil, invalid()
	}
	if raw := query.Get("limit"); raw != "" {
		number, err := strconv.Atoi(raw)
		if err != nil || number < 1 || number > 100 || strconv.Itoa(number) != raw {
			return nil, nil, invalid()
		}
		limit = number
	}
	var dbNow time.Time
	if err := s.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&dbNow); err != nil {
		return nil, nil, unavailable()
	}
	cursor := identitySessionCursor{AccountID: p.accountID, Wallet: p.wallet, SessionID: p.sessionID, Snapshot: dbNow, Limit: limit, ExpiresAt: dbNow.Add(15 * time.Minute).Unix()}
	if values := query["cursor"]; len(values) > 1 {
		return nil, nil, identityError("CURSOR_INVALID", 400)
	}
	if encoded := query.Get("cursor"); encoded != "" {
		if len(encoded) > 2048 {
			return nil, nil, identityError("CURSOR_INVALID", 400)
		}
		parts := strings.Split(encoded, ".")
		if len(parts) != 2 {
			return nil, nil, identityError("CURSOR_INVALID", 400)
		}
		raw, err := b64.DecodeString(parts[0])
		if err != nil {
			return nil, nil, identityError("CURSOR_INVALID", 400)
		}
		mac, err := hex.DecodeString(parts[1])
		if err != nil || subtle.ConstantTimeCompare(mac, s.sessionCursorMAC(raw)) != 1 || strictJSON(raw, &cursor) != nil {
			return nil, nil, identityError("CURSOR_INVALID", 400)
		}
		if cursor.AccountID != p.accountID || cursor.Wallet != p.wallet || cursor.SessionID != p.sessionID || cursor.Limit != limit || cursor.ExpiresAt <= dbNow.Unix() || cursor.Snapshot.After(dbNow) || !validUUID(cursor.BeforeID) || cursor.Before.After(cursor.Snapshot) {
			return nil, nil, identityError("CURSOR_INVALID", 400)
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT session_id::text,wallet,access_expires_at,refresh_expires_at,device_label,revoked_at,issued_at FROM identity_sessions WHERE account_id=$1 AND wallet=$2 AND issued_at<=$3 AND ($4::uuid IS NULL OR (issued_at,session_id)<($5,$4::uuid)) ORDER BY issued_at DESC,session_id DESC LIMIT $6`, p.accountID, p.wallet, cursor.Snapshot, nullableID(cursor.BeforeID), cursor.Before, limit+1)
	if err != nil {
		return nil, nil, unavailable()
	}
	defer rows.Close()
	items := []Session{}
	var last time.Time
	var next *string
	for rows.Next() {
		var session Session
		var issued time.Time
		if err = rows.Scan(&session.SessionID, &session.Wallet, &session.AccessExpiresAt, &session.RefreshExpiresAt, &session.DeviceLabel, &session.RevokedAt, &issued); err != nil {
			return nil, nil, unavailable()
		}
		if len(items) == limit {
			cursor.Before = last
			cursor.BeforeID = items[len(items)-1].SessionID
			raw, err := json.Marshal(cursor)
			if err != nil {
				return nil, nil, unavailable()
			}
			encoded := b64.EncodeToString(raw) + "." + hex.EncodeToString(s.sessionCursorMAC(raw))
			next = &encoded
			break
		}
		session.Scopes = []string{"account:owner"}
		items = append(items, session)
		last = issued
	}
	if rows.Err() != nil {
		return nil, nil, unavailable()
	}
	return items, next, nil
}

func validateIdentityTransport(r *http.Request) error {
	if r == nil || r.URL == nil || len(r.Header.Values("Authorization")) > 1 || len(r.Header.Values("Origin")) > 1 || len(r.Header.Values("X-CSRF-Token")) > 1 {
		return unauthorized()
	}
	counts := map[string]int{}
	for _, cookie := range r.Cookies() {
		if cookie.Name == "__Host-kindle_access" || cookie.Name == "__Host-kindle_csrf" || cookie.Name == "__Host-kindle_refresh" {
			counts[cookie.Name]++
			if counts[cookie.Name] > 1 {
				return unauthorized()
			}
		}
	}
	return nil
}

// verifiedPrincipal re-reads an authenticated principal from its live session
// row, bound to the exact access credential that authenticated it.
func (s *Identity) verifiedPrincipal(ctx context.Context, p Principal) (sharedauth.IdentityPrincipal, error) {
	var verified sharedauth.IdentityPrincipal
	err := s.pool.QueryRow(ctx, `SELECT a.account_id::text,s.session_id::text,s.wallet,s.auth_epoch,s.reauthenticated_at,s.access_expires_at FROM identity_sessions s JOIN identity_accounts a USING(account_id) JOIN identity_wallet_links w ON w.account_id=a.account_id AND w.wallet=s.wallet AND w.state='verified' WHERE a.account_id=$1 AND s.session_id=$2 AND a.auth_epoch=$3 AND s.auth_epoch=$3 AND s.wallet=$4 AND s.revoked_at IS NULL AND s.access_expires_at>clock_timestamp() AND s.access_hash=$5`, p.accountID, p.sessionID, p.epoch, p.wallet, digest(p.accessID)).Scan(&verified.AccountID, &verified.SessionID, &verified.Wallet, &verified.AuthEpoch, &verified.ProofAt, &verified.AccessExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return sharedauth.IdentityPrincipal{}, unauthorized()
	}
	if err != nil {
		return sharedauth.IdentityPrincipal{}, unavailable()
	}
	return verified, nil
}
