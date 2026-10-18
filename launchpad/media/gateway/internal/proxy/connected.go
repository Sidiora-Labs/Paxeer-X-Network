package proxy

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/google/uuid"
)

type ConnectedRoute struct {
	Method       string
	Template     string
	Authenticate bool
	Optional     bool
	Upload       bool
}
type RequestIdentityAuthority interface {
	AuthenticateRequest(context.Context, *http.Request) (sharedauth.IdentityPrincipal, error)
}
type ConnectedDeps struct {
	Name       string
	URL        string
	Routes     []ConnectedRoute
	Identity   RequestIdentityAuthority
	SigningKey ed25519.PrivateKey
	TLS        *tls.Config
}
type Connected struct {
	target   *url.URL
	routes   []ConnectedRoute
	identity RequestIdentityAuthority
	key      ed25519.PrivateKey
	audience string
	client   *http.Client
}

func NewConnected(d ConnectedDeps) (*Connected, error) {
	if d.Name != "metadata" && d.Name != "user" && d.Name != "social" && d.Name != "livestream" {
		return nil, errors.New("unknown media upstream")
	}
	u, e := url.Parse(d.URL)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil || len(d.SigningKey) != ed25519.PrivateKeySize || d.TLS == nil || d.TLS.InsecureSkipVerify || d.TLS.RootCAs == nil || len(d.TLS.Certificates) == 0 || len(d.Routes) == 0 || len(d.Routes) > 256 {
		return nil, errors.New("private media channel unavailable")
	}
	if d.Identity == nil {
		return nil, errors.New("identity authority unavailable")
	}
	routes := append([]ConnectedRoute(nil), d.Routes...)
	seen := map[string]bool{}
	for _, r := range routes {
		if !allowedConnectedRoute(d.Name, r) || seen[r.Method+" "+r.Template] {
			return nil, errors.New("unknown media route")
		}
		seen[r.Method+" "+r.Template] = true
	}
	tlsConfig := d.TLS.Clone()
	if tlsConfig.MinVersion < tls.VersionTLS12 {
		tlsConfig.MinVersion = tls.VersionTLS12
	}
	// The response-header wait matches the longest per-route deadline (10s:
	// uploads and metadata publication admission); every request also runs
	// under its route's context deadline (3s by default), which is what
	// bounds the ordinary routes.
	transport := &http.Transport{TLSClientConfig: tlsConfig, Proxy: nil, MaxIdleConns: 128, MaxIdleConnsPerHost: 32, IdleConnTimeout: 90 * time.Second, ResponseHeaderTimeout: 10 * time.Second, DisableCompression: true}
	return &Connected{u, routes, d.Identity, append(ed25519.PrivateKey(nil), d.SigningKey...), d.Name, &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func allowedConnectedRoute(name string, r ConnectedRoute) bool {
	routes := map[string]map[string]string{
		"metadata":   {"/v2/tokens/{tokenAddress}/metadata": "GET PATCH", "/v2/tokens/{tokenAddress}/media": "GET", "/v2/tokens/{tokenAddress}/assets/{assetId}": "GET PUT", "/v2/accounts/{wallet}/assets/{assetId}": "GET PUT", "/v2/assets/uploads": "POST", "/v2/assets/{assetId}": "GET", "/v2/assets/{assetId}:complete": "POST"},
		"user":       {"/v2/profiles/{wallet}": "GET PATCH", "/v2/profiles:batch": "GET", "/v2/me/watchlist": "GET", "/v2/me/watchlist/{poolAddress}": "PUT DELETE"},
		"livestream": {"/v2/streams": "GET POST", "/v2/streams/operations/{operationId}": "GET", "/v2/streams/{streamId}:start": "POST", "/v2/streams/{streamId}:end": "POST", "/v2/streams/{streamId}/viewers:heartbeat": "POST", "/v2/streams/{streamId}/ingest-credential": "POST"},
		"social":     {"/v2/me/following/{wallet}": "PUT DELETE", "/v2/profiles/{wallet}/followers": "GET", "/v2/profiles/{wallet}/follow-stats": "GET", "/v2/pools/{poolAddress}/comments": "GET POST", "/v2/comments/{commentId}": "PATCH DELETE", "/v2/comments/{commentId}/likes/me": "PUT DELETE", "/v2/reports": "POST", "/v2/callouts": "POST", "/v2/pools/{poolAddress}/callouts": "GET", "/v2/bounties": "GET POST", "/v2/bounties/stats": "GET", "/v2/bounties/{bountyId}": "GET", "/v2/bounties/{bountyId}/submissions": "GET POST", "/v2/bounties/{bountyId}/submissions/{submissionId}:accept": "POST", "/v2/rooms/{roomId}/messages": "GET POST"},
	}
	if r.Optional && (r.Authenticate || r.Method != "GET") {
		return false
	}
	if name == "user" {
		for _, route := range UserConnectedRoutes() {
			if route.Template == r.Template && route.Method == r.Method {
				return r.Authenticate == route.Authenticate && r.Optional == route.Optional && !r.Upload
			}
		}
	}
	if name == "metadata" {
		for _, route := range MetadataConnectedRoutes() {
			if route.Template == r.Template && route.Method == r.Method {
				return r.Authenticate == route.Authenticate && r.Optional == route.Optional && !r.Upload
			}
		}
	}
	if name == "livestream" {
		for _, route := range LivestreamConnectedRoutes() {
			if route.Template == r.Template && route.Method == r.Method {
				return r.Authenticate == route.Authenticate && r.Optional == route.Optional && !r.Upload
			}
		}
	}
	if name == "social" {
		for _, route := range SocialConnectedRoutes() {
			if route.Template == r.Template && route.Method == r.Method {
				return r.Authenticate == route.Authenticate && r.Optional == route.Optional && !r.Upload
			}
		}
	}
	methods, ok := routes[name][r.Template]
	if !ok || !strings.Contains(" "+methods+" ", " "+r.Method+" ") {
		return false
	}
	if r.Method != "GET" && !r.Authenticate {
		return false
	}
	if strings.HasPrefix(r.Template, "/v2/me/") && !r.Authenticate {
		return false
	}
	if strings.Contains(r.Template, "/assets/") && !r.Authenticate {
		return false
	}
	if strings.Contains(r.Template, "/operations/") && !r.Authenticate {
		return false
	}
	if name == "metadata" && r.Method == "PUT" && strings.Contains(r.Template, "/assets/") && !r.Upload {
		return false
	}
	return !r.Upload || (name == "metadata" && (r.Method == "POST" || r.Method == "PUT") && strings.Contains(r.Template, "/assets/"))
}
func connectedMatch(template, path string) bool {
	a, b := strings.Split(template, "/"), strings.Split(path, "/")
	if len(a) != len(b) {
		return false
	}
	for i, part := range a {
		if strings.HasPrefix(part, "{") {
			end := strings.IndexByte(part, '}')
			if end < 2 {
				return false
			}
			suffix := part[end+1:]
			if !strings.HasSuffix(b[i], suffix) {
				return false
			}
			value := strings.TrimSuffix(b[i], suffix)
			if value == "" || len(value) > 128 || strings.ContainsAny(value, "%\\: ") || strings.IndexFunc(value, func(c rune) bool { return c < 0x21 || c == 0x7f }) >= 0 {
				return false
			}
		} else if part != b[i] {
			return false
		}
	}
	return true
}
func (p *Connected) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL == nil || r.URL.RawPath != "" || strings.ContainsAny(r.URL.Path, "%\\") || strings.Contains(r.URL.Path, "..") || len(r.URL.RequestURI()) > 8192 {
		http.NotFound(w, r)
		return
	}
	var selected *ConnectedRoute
	for i := range p.routes {
		if p.routes[i].Method == r.Method && connectedMatch(p.routes[i].Template, r.URL.Path) {
			selected = &p.routes[i]
			break
		}
	}
	if selected == nil {
		http.NotFound(w, r)
		return
	}
	timeout := 3 * time.Second
	if selected.Upload || p.slowRoute(selected) {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	r = r.WithContext(ctx)
	var principal sharedauth.IdentityPrincipal
	recovery := p.audience == "user" && selected.Template == "/v1/me/deletion-requests/{jobId}" && r.Method == http.MethodGet && r.Header.Get("X-Recovery-Receipt") != ""
	if recovery && (len(r.Header.Values("X-Recovery-Receipt")) != 1 || len(r.Header.Get("X-Recovery-Receipt")) > 4096) {
		connectedError(w, 400, "recovery_receipt_invalid")
		return
	}
	if (selected.Authenticate || (selected.Optional && sessionCredentials(r))) && !recovery {
		var e error
		principal, e = p.identity.AuthenticateRequest(r.Context(), r)
		if e == nil {
			e = principal.Validate(time.Now())
		}
		if e != nil {
			status := http.StatusServiceUnavailable
			var authority *sharedauth.IdentityAuthorityError
			if errors.As(e, &authority) {
				status = authority.StatusCode()
			} else if errors.Is(e, sharedauth.ErrScopeDenied) {
				status = http.StatusForbidden
			}
			connectedError(w, status, "identity_unavailable")
			return
		}
	}
	var body []byte
	if r.Body != nil {
		controller := http.NewResponseController(w)
		deadline, _ := ctx.Deadline()
		_ = controller.SetReadDeadline(deadline)
		defer controller.SetReadDeadline(time.Time{})
		var e error
		limit := int64(1 << 20)
		if selected.Upload && r.Method == "PUT" {
			limit = 6 << 20
		}
		if p.audience == "metadata" && selected.Template == metadataPublications && r.Method == http.MethodPost {
			limit = MetadataPublicationBodyLimit
		}
		body, e = io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
		if e != nil {
			var oversized *http.MaxBytesError
			if errors.As(e, &oversized) {
				connectedError(w, 413, "request_too_large")
			} else if errors.Is(e, context.DeadlineExceeded) || ctx.Err() != nil {
				connectedError(w, 408, "request_timeout")
			} else {
				w.Header().Set("Retry-After", "1")
				connectedError(w, 503, "request_ingress_unavailable")
			}
			return
		}
	}
	target := *p.target
	target.Path = r.URL.Path
	target.RawQuery = r.URL.RawQuery
	req, e := http.NewRequestWithContext(ctx, r.Method, target.String(), bytes.NewReader(body))
	if e != nil {
		connectedError(w, 503, "upstream_unavailable")
		return
	}
	for _, name := range []string{"Content-Type", "Accept", "If-None-Match", "If-Match", "Idempotency-Key", "X-Action-Signature", "X-Action-Nonce", "X-Action-Expires", "X-Action-Audience", "X-Kindle-Operation", "X-Kindle-Operation-Signature", "X-Request-ID", "Origin", "X-CSRF-Token", "X-Recovery-Receipt"} {
		if values := r.Header.Values(name); len(values) == 1 {
			req.Header.Set(name, values[0])
		} else if len(values) > 1 {
			connectedError(w, 400, "ambiguous_header")
			return
		}
	}
	if principal.Wallet != "" {
		if authorization := r.Header.Get("Authorization"); authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		if cookie, e := r.Cookie("__Host-kindle_access"); e == nil {
			req.AddCookie(cookie)
		}
		if supplied := r.Header.Get("X-Action-Wallet"); supplied != "" && supplied != principal.Wallet {
			connectedError(w, 403, "actor_mismatch")
			return
		}
		req.Header.Set("X-Action-Wallet", principal.Wallet)
		requestID := req.Header.Get("X-Request-ID")
		if _, err := uuid.Parse(requestID); err != nil {
			requestID = uuid.NewString()
			req.Header.Set("X-Request-ID", requestID)
		}
		audience := p.audience
		if p.audience == "user" && (strings.HasPrefix(selected.Template, "/v1/") || strings.HasPrefix(selected.Template, "/v3/") || selected.Template == "/v2/me/preferences" || selected.Template == "/v2/me/exports/{jobId}/download") {
			audience = "account"
		}
		signer := sharedauth.ServiceAssertionIssuer{Issuer: "gateway", Audience: audience, Key: p.key, Transport: actorAssertionTransport(p.audience)}
		if e = signer.SignRequest(req, principal, []string{"owner"}, body); e != nil {
			connectedError(w, 403, "actor_assertion_denied")
			return
		}
	}
	if recovery {
		requestID := req.Header.Get("X-Request-ID")
		if _, e := uuid.Parse(requestID); e != nil {
			req.Header.Set("X-Request-ID", uuid.NewString())
		}
		signer := sharedauth.ServiceRequestIssuer{Issuer: "gateway", Audience: "account", Key: p.key, Transport: sharedauth.AssertionTransport{AssertionHeader: sharedauth.IdentityAssertionHeader, SignatureHeader: sharedauth.IdentitySignatureHeader}}
		if e = signer.SignRequest(req, nil); e != nil {
			connectedError(w, 403, "recovery_assertion_denied")
			return
		}
	}
	resp, e := p.client.Do(req)
	if e != nil {
		connectedError(w, 503, "upstream_unavailable")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 && resp.StatusCode != 304 {
		connectedError(w, 502, "upstream_redirect_denied")
		return
	}
	if resp.StatusCode >= 500 {
		upstreamUnavailable(w, resp)
		return
	}
	if resp.StatusCode == 304 || resp.StatusCode == 204 {
		if etag := resp.Header.Get("ETag"); len(etag) <= 128 && !strings.ContainsAny(etag, "\r\n") {
			w.Header().Set("ETag", etag)
		}
		w.WriteHeader(resp.StatusCode)
		return
	}
	if p.audience == "social" && selected.Template == "/v2/voice-rooms/{roomId}/recording/object" && resp.StatusCode == 200 {
		if resp.Header.Get("Content-Type") != "audio/ogg" {
			connectedError(w, 502, "upstream_response_invalid")
			return
		}
		w.Header().Set("Content-Type", "audio/ogg")
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = io.Copy(w, io.LimitReader(resp.Body, 64<<20))
		return
	}
	if p.audience == "metadata" && selected.Template == metadataLogo && resp.StatusCode == 200 {
		image, e := io.ReadAll(io.LimitReader(resp.Body, metadataLogoLimit+1))
		if e != nil || len(image) > metadataLogoLimit || resp.Header.Get("Content-Type") != "image/png" || len(image) < 8 || !bytes.Equal(image[:8], []byte("\x89PNG\r\n\x1a\n")) {
			connectedError(w, 502, "upstream_response_invalid")
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(image)
		return
	}
	result, e := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if e != nil || len(result) > 1<<20 || !json.Valid(result) {
		connectedError(w, 502, "upstream_response_invalid")
		return
	}
	if containsSecret(result) {
		connectedError(w, 502, "upstream_response_restricted")
		return
	}
	for _, name := range []string{"ETag", "Retry-After"} {
		if v := resp.Header.Get(name); len(v) <= 128 && !strings.ContainsAny(v, "\r\n") {
			w.Header().Set(name, v)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(result)
}
func containsSecret(raw []byte) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return true
	}
	var inspect func(any) bool
	inspect = func(v any) bool {
		switch x := v.(type) {
		case map[string]any:
			for k, v := range x {
				switch strings.ToLower(k) {
				case "secretaccesskey", "accesskeyid", "servicetoken", "providertoken", "providersecret", "streamkey", "ingestkey", "privatekey", "authorization":
					return true
				}
				if inspect(v) {
					return true
				}
			}
		case []any:
			for _, v := range x {
				if inspect(v) {
					return true
				}
			}
		case string:
			lower := strings.ToLower(x)
			if strings.Contains(lower, "x-amz-credential=") || strings.Contains(lower, "x-amz-signature=") {
				return true
			}
		}
		return false
	}
	return inspect(value)
}

// sessionCredentials reports whether the caller presented a session: an
// Optional route authenticates only then, so an anonymous read stays anonymous
// while a stale credential is rejected instead of silently downgraded.
func sessionCredentials(r *http.Request) bool {
	if r.Header.Get("Authorization") != "" {
		return true
	}
	_, e := r.Cookie("__Host-kindle_access")
	return e == nil
}

// SocialContractRoutes are the frozen v2 follow-graph paths served by the
// social upstream, which owns the follow store. The reads are public and
// viewer-aware (Optional), the writes require a session.
func SocialContractRoutes() []ConnectedRoute {
	return []ConnectedRoute{
		{Method: http.MethodPut, Template: "/v2/me/following/{wallet}", Authenticate: true},
		{Method: http.MethodDelete, Template: "/v2/me/following/{wallet}", Authenticate: true},
		{Method: http.MethodGet, Template: "/v2/profiles/{wallet}/followers", Optional: true},
		{Method: http.MethodGet, Template: "/v2/profiles/{wallet}/follow-stats", Optional: true},
	}
}

// upstreamUnavailableMessage is the only message relayed for an upstream 5xx;
// upstream wording and details (dependency and capability names) stay inside.
const upstreamUnavailableMessage = "Upstream service unavailable"

// upstreamUnavailable collapses every upstream 5xx into a 503. When the
// upstream answered with a valid v2 Error envelope, the 503 body is a v2 Error
// envelope carrying the upstream's stable code, retry hints and request id, so
// a client can tell a coded upstream condition (DEPENDENCY_UNAVAILABLE, ...)
// from a dead upstream; the upstream message and details are replaced. Any
// other 5xx (no envelope, oversized, invalid) answers the gateway's own
// {"code":"upstream_unavailable"}. Upstream 4xx envelopes are relayed
// verbatim by ServeHTTP, so in every case the client reads the top-level
// "code": UPPER_SNAKE codes come from the upstream v2 contract, lower_snake
// codes from the gateway itself.
func upstreamUnavailable(w http.ResponseWriter, resp *http.Response) {
	raw, e := io.ReadAll(io.LimitReader(resp.Body, (16<<10)+1))
	var upstream v2.Error
	if e != nil || len(raw) > 16<<10 || !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") || v2.ValidateJSON("Error", raw) != nil || json.Unmarshal(raw, &upstream) != nil || !upstreamCode.MatchString(upstream.Code) {
		connectedError(w, 503, "upstream_unavailable")
		return
	}
	out := v2.Error{Code: upstream.Code, Message: upstreamUnavailableMessage, RequestId: upstream.RequestId, Retryable: upstream.Retryable, RetryAfterMs: upstream.RetryAfterMs, FieldErrors: []v2.FieldError{}}
	if !out.Retryable {
		out.RetryAfterMs = nil
	}
	body, e := json.Marshal(out)
	if e != nil || v2.ValidateJSON("Error", body) != nil {
		connectedError(w, 503, "upstream_unavailable")
		return
	}
	if retry := resp.Header.Get("Retry-After"); out.Retryable && retryAfterSeconds.MatchString(retry) {
		w.Header().Set("Retry-After", retry)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write(body)
}

var (
	upstreamCode      = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	retryAfterSeconds = regexp.MustCompile(`^[0-9]{1,3}$`)
)

func connectedError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code})
}

func SocialConnectedRoutes() []ConnectedRoute {
	methods := map[string]string{
		"/v2/chat/history": "GET", "/v2/chat/conversations": "GET", "/v2/chat/read": "POST", "/v2/chat/revise": "POST",
		"/v2/social/users/{wallet}/followers": "GET", "/v2/social/users/{wallet}/following": "GET", "/v2/social/users/{wallet}/follow-stats": "GET",
		"/v2/social/follow/{followee}": "PUT DELETE", "/v2/social/pool/{poolAddress}/comments": "GET POST", "/v2/social/comments/{id}": "PATCH DELETE", "/v2/social/comments/{id}/hide": "POST", "/v2/social/comments/{id}/like": "PUT DELETE", "/v2/social/callouts/{id}/references": "POST", "/v2/social/me/social/privacy": "GET PATCH", "/v2/social/me/social/blocks/{wallet}": "PUT DELETE",
		"/v2/safety/reports": "POST", "/v2/safety/cases": "GET", "/v2/safety/cases/{caseId}": "PATCH", "/v2/safety/cases/{caseId}/bans": "POST", "/v2/safety/cases/{caseId}/restore": "POST", "/v2/safety/appeals": "POST", "/v2/safety/appeals/{appealId}": "GET", "/v2/safety/appeals/{appealId}/reverse-ban": "POST",
		"/v2/voice-rooms": "POST", "/v2/voice-rooms/{roomId}/start": "POST", "/v2/voice-rooms/{roomId}/end": "POST", "/v2/voice-rooms/{roomId}/moderation-actions": "POST", "/v2/voice-rooms/{roomId}/prompt": "GET", "/v2/voice-rooms/{roomId}/tickets": "POST", "/v2/voice-rooms/{roomId}/recording": "GET POST", "/v2/voice-rooms/{roomId}/recording/object": "GET", "/v2/voice-rooms/{roomId}/recording/consent": "POST", "/v2/voice-rooms/{roomId}/recording/start": "POST", "/v2/voice-rooms/{roomId}/recording/stop": "POST", "/v2/voice-rooms/{roomId}/speaker-requests": "POST", "/v2/voice-rooms/{roomId}/speakers": "POST", "/v2/voice-rooms/{roomId}/leave": "POST",
	}
	paths := make([]string, 0, len(methods))
	for path := range methods {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	routes := SocialContractRoutes()
	for _, path := range paths {
		for _, method := range strings.Fields(methods[path]) {
			routes = append(routes, ConnectedRoute{Method: method, Template: path, Authenticate: true})
		}
	}
	return routes
}

// LivestreamConnectedRoutes is the media/livestream v2 surface the web calls
// (internal/streams RegisterRoutes /v2 group). The directory, pool listing and
// stream document are public reads; viewer sessions and heartbeats are
// anonymous viewer writes authorised by the viewer-session token in their
// body (no wallet session); every broadcaster action requires a session.
func LivestreamConnectedRoutes() []ConnectedRoute {
	return []ConnectedRoute{
		{Method: http.MethodGet, Template: "/v2/streams/live"},
		{Method: http.MethodGet, Template: "/v2/streams/pool/{poolAddress}"},
		{Method: http.MethodGet, Template: "/v2/streams/operations/{operationId}", Authenticate: true},
		{Method: http.MethodGet, Template: "/v2/streams/{streamId}"},
		{Method: http.MethodPost, Template: "/v2/streams/{streamId}/viewer-session"},
		{Method: http.MethodPost, Template: "/v2/streams/{streamId}/heartbeat"},
		{Method: http.MethodPost, Template: "/v2/streams", Authenticate: true},
		{Method: http.MethodPatch, Template: "/v2/streams/{streamId}", Authenticate: true},
		{Method: http.MethodPost, Template: "/v2/streams/{streamId}/go-live", Authenticate: true},
		{Method: http.MethodPost, Template: "/v2/streams/{streamId}/end", Authenticate: true},
		{Method: http.MethodPost, Template: "/v2/streams/{streamId}/rotate-key", Authenticate: true},
		{Method: http.MethodPost, Template: "/v2/streams/{streamId}/restore", Authenticate: true},
		{Method: http.MethodGet, Template: "/v2/streams/{streamId}/key-rotation", Authenticate: true},
		{Method: http.MethodGet, Template: "/v2/streams/{streamId}/private-playback", Authenticate: true},
		{Method: http.MethodPost, Template: "/v2/streams/{streamId}/ingest-credential", Authenticate: true},
		{Method: http.MethodPost, Template: "/v2/streams/{streamId}/ingest-key", Authenticate: true},
	}
}

func UserConnectedRoutes() []ConnectedRoute {
	methods := map[string]string{
		"/v2/profiles/{wallet}": "GET", "/v2/profiles:batch": "GET", "/v3/profiles/{wallet}": "GET PATCH", "/v3/me/preferences": "GET PATCH", "/v1/me/preferences": "GET PATCH", "/v2/me/preferences": "GET PATCH", "/v1/me/devices": "GET", "/v1/me/devices/{deviceId}": "PUT DELETE", "/v1/me/exports": "POST", "/v1/me/exports/{jobId}": "GET", "/v2/me/exports/{jobId}/download": "GET", "/v1/me/deletion-requests": "POST", "/v1/me/deletion-requests/{jobId}": "GET", "/v1/profiles/{wallet}": "GET PATCH",
		"/v1/me/watchlist": "GET", "/v1/me/watchlist/{poolAddress}": "PUT DELETE",
	}
	paths := make([]string, 0, len(methods))
	for path := range methods {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	routes := []ConnectedRoute{}
	for _, path := range paths {
		for _, method := range strings.Fields(methods[path]) {
			public := method == http.MethodGet && (path == "/v3/profiles/{wallet}" || path == "/v2/profiles/{wallet}" || path == "/v2/profiles:batch")
			routes = append(routes, ConnectedRoute{Method: method, Template: path, Authenticate: !public})
		}
	}
	return routes
}

// DevelopmentSocialRoutes is the social surface the development gateway
// forwards verbatim to the social upstream: the production social allowlist
// (SocialConnectedRoutes, which includes SocialContractRoutes) plus the
// social upstream routes of the production gateway runtime document
// (comments, reports, callouts, bounties, rooms) and the bounty reads the web
// issues. GET routes are anonymous-capable (Authenticate false: the actor is
// attached when a session is present); every other method requires a session.
func DevelopmentSocialRoutes() []ConnectedRoute {
	extra := map[string]string{
		"/v2/pools/{poolAddress}/comments": "GET POST", "/v2/comments/{commentId}": "PATCH DELETE", "/v2/comments/{commentId}/likes/me": "PUT DELETE",
		"/v2/reports": "POST", "/v2/callouts": "POST", "/v2/pools/{poolAddress}/callouts": "GET",
		"/v2/bounties": "GET POST", "/v2/bounties/stats": "GET", "/v2/bounties/{bountyId}": "GET", "/v2/bounties/{bountyId}/submissions": "GET POST", "/v2/bounties/{bountyId}/submissions/{submissionId}:accept": "POST",
		"/v2/rooms/{roomId}/messages": "GET POST",
	}
	routes := []ConnectedRoute{}
	seen := map[string]bool{}
	add := func(method, template string) {
		if seen[method+" "+template] {
			return
		}
		seen[method+" "+template] = true
		routes = append(routes, ConnectedRoute{Method: method, Template: template, Authenticate: method != http.MethodGet, Optional: method == http.MethodGet})
	}
	for _, route := range SocialConnectedRoutes() {
		add(route.Method, route.Template)
	}
	paths := make([]string, 0, len(extra))
	for path := range extra {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		for _, method := range strings.Fields(extra[path]) {
			add(method, path)
		}
	}
	return routes
}

// DevelopmentUserRoutes are the account owner writes the development gateway
// forwards verbatim to media/user, which serves them outside production over
// its legacy profile store: the web profile editor's
// PATCH /v3/profiles/{wallet}. They require a session here, and media/user
// re-authenticates the forwarded bearer against the identity authority.
func DevelopmentUserRoutes() []ConnectedRoute {
	return []ConnectedRoute{{Method: http.MethodPatch, Template: "/v3/profiles/{wallet}", Authenticate: true}}
}

// The media/metadata routes behind the gateway beyond the frozen v2 media
// surface: the /v3 metadata publication the token-create flow drives
// (PublicationService.RegisterRoutes) and the legacy public reads
// (ProductionLegacyReads.RegisterRoutes) the web's metadata client issues
// against NEXT_PUBLIC_METADATA_API_URL, which is the gateway origin because
// metadata's production listener is mTLS-only.
const (
	metadataPublicationAuthority = "/v3/metadata-publication-authority"
	metadataPublications         = "/v3/tokens/{tokenAddress}/metadata-publications"
	metadataPublication          = "/v3/tokens/{tokenAddress}/metadata-publications/{operationId}"
	metadataLogo                 = "/logo/{file}"
	// MetadataPublicationBodyLimit is media/metadata's PublicationBodyLimit
	// (V2BodyLimit 32 KiB + 4 KiB): the edge refuses a larger publication
	// body before it crosses the private channel.
	MetadataPublicationBodyLimit = 32<<10 + 4096
	// metadataLogoLimit bounds a relayed legacy logo (the approved 512px PNG
	// variant; metadata's upload ceiling is 6 MiB).
	metadataLogoLimit = 6 << 20
)

// MetadataConnectedRoutes is the media/metadata surface the gateway adds to
// the metadata upstream: publication authority, enqueue and status require
// the creator's session (metadata re-authenticates the forwarded bearer and,
// for the enqueue, verifies the gateway's actor assertion and the creator's
// operation envelope); the legacy metadata/logo/banner reads are public.
func MetadataConnectedRoutes() []ConnectedRoute {
	return []ConnectedRoute{
		{Method: http.MethodGet, Template: metadataPublicationAuthority, Authenticate: true},
		{Method: http.MethodPost, Template: metadataPublications, Authenticate: true},
		{Method: http.MethodGet, Template: metadataPublication, Authenticate: true},
		{Method: http.MethodGet, Template: "/metadata/batch"},
		{Method: http.MethodGet, Template: "/metadata/{tokenAddress}"},
		{Method: http.MethodGet, Template: metadataLogo},
		{Method: http.MethodGet, Template: "/banner/{file}"},
	}
}

// slowRoute is a route whose upstream work is bounded by more than the
// default 3s: metadata publication admission authenticates the session,
// confirms the identity and verifies the creator's committed operation
// against the chain inside one transaction.
func (p *Connected) slowRoute(route *ConnectedRoute) bool {
	return p.audience == "metadata" && route.Method == http.MethodPost && route.Template == metadataPublications
}

// MetadataActorTransport is the header pair media/metadata's private channel
// (internal/app metadataPrivateChannel) reads the gateway's actor assertion
// from. Every other upstream reads the shared actor headers.
var MetadataActorTransport = sharedauth.AssertionTransport{AssertionHeader: "X-Internal-Assertion", SignatureHeader: "X-Internal-Signature"}

func actorAssertionTransport(upstream string) sharedauth.AssertionTransport {
	if upstream == "metadata" {
		return MetadataActorTransport
	}
	return sharedauth.AssertionTransport{AssertionHeader: sharedauth.ActorAssertionHeader, SignatureHeader: sharedauth.ActorSignatureHeader}
}
