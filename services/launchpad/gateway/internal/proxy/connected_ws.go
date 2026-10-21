package proxy

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/hex"
	"errors"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type SocketIdentityAuthority interface {
	RequestIdentityAuthority
	CheckStreamSession(context.Context, sharedauth.StreamPrincipal) error
}
type ConnectedWSDeps struct {
	URL        string
	Identity   SocketIdentityAuthority
	SigningKey ed25519.PrivateKey
	TLS        *tls.Config
	Origins    []string
}
type ConnectedWS struct {
	target   *url.URL
	identity SocketIdentityAuthority
	signer   sharedauth.ServiceAssertionIssuer
	dialer   *websocket.Dialer
	upgrader websocket.Upgrader
	origins  map[string]bool
	slots    chan struct{}
}

func NewConnectedWS(d ConnectedWSDeps) (*ConnectedWS, error) {
	target, err := url.Parse(d.URL)
	if err != nil || target.Scheme != "https" || target.Host == "" || target.Path != "" || target.User != nil || target.RawQuery != "" || target.Fragment != "" || d.Identity == nil || len(d.SigningKey) != ed25519.PrivateKeySize || d.TLS == nil || d.TLS.InsecureSkipVerify || d.TLS.RootCAs == nil || len(d.TLS.Certificates) == 0 || d.TLS.MinVersion < tls.VersionTLS13 {
		return nil, errors.New("authenticated social socket binding unavailable")
	}
	origins := map[string]bool{}
	for _, value := range d.Origins {
		origin, err := url.Parse(value)
		if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.Path != "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || strings.Contains(value, "*") {
			return nil, errors.New("explicit socket origin required")
		}
		origins[value] = true
	}
	if len(origins) == 0 {
		return nil, errors.New("socket origins unavailable")
	}
	target.Scheme = "wss"
	target.Path = "/v2/stream"
	transport := sharedauth.AssertionTransport{AssertionHeader: "X-Internal-Assertion", SignatureHeader: "X-Internal-Signature"}
	return &ConnectedWS{target: target, identity: d.Identity, signer: sharedauth.ServiceAssertionIssuer{Issuer: "gateway", Audience: "social", Key: append(ed25519.PrivateKey(nil), d.SigningKey...), Transport: transport}, dialer: &websocket.Dialer{TLSClientConfig: d.TLS.Clone(), Proxy: nil, HandshakeTimeout: 3 * time.Second, ReadBufferSize: 4096, WriteBufferSize: 4096}, upgrader: websocket.Upgrader{Subprotocols: []string{"sidiora.v2"}, ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: func(r *http.Request) bool { return origins[r.Header.Get("Origin")] }}, origins: origins, slots: make(chan struct{}, 10000)}, nil
}
func (p *ConnectedWS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet || r.URL == nil || r.URL.Path != "/v2/stream" || r.URL.RawPath != "" || !websocket.IsWebSocketUpgrade(r) {
		connectedError(w, 400, "socket_request_invalid")
		return
	}
	if len(r.Header.Values("Origin")) != 1 || !p.origins[r.Header.Get("Origin")] {
		connectedError(w, 403, "origin_denied")
		return
	}
	for key, values := range r.URL.Query() {
		if key != "topics" || len(values) != 1 {
			connectedError(w, 401, "socket_credentials_invalid")
			return
		}
	}
	topics := strings.Split(r.URL.Query().Get("topics"), ",")
	ticket := ""
	protocol := false
	for _, value := range websocket.Subprotocols(r) {
		switch {
		case value == "sidiora.v2":
			if protocol {
				connectedError(w, 400, "socket_protocol_invalid")
				return
			}
			protocol = true
		case strings.HasPrefix(value, "ticket."):
			if ticket != "" {
				connectedError(w, 401, "socket_ticket_invalid")
				return
			}
			ticket = strings.TrimPrefix(value, "ticket.")
		default:
			connectedError(w, 400, "socket_protocol_invalid")
			return
		}
	}
	decoded, err := hex.DecodeString(ticket)
	if !protocol || err != nil || len(decoded) != 32 || strings.ToLower(ticket) != ticket {
		connectedError(w, 401, "socket_ticket_invalid")
		return
	}
	select {
	case p.slots <- struct{}{}:
		defer func() { <-p.slots }()
	default:
		w.Header().Set("Retry-After", "1")
		connectedError(w, 503, "socket_capacity_unavailable")
		return
	}
	// The upgrade credential. A browser WebSocket opened cross-site to the
	// gateway host can carry neither an Authorization header nor the
	// host-only SameSite=Lax access cookie, so the single-use chat ticket
	// (POST /v2/stream/tickets: bound to the session, the exact topics and
	// filter digest, the https origin and /v2/stream) is a complete
	// credential on its own. It is forwarded unconsumed: the social upstream
	// redeems it exactly once at the identity authority
	// (chat-ticket:consume over its mTLS identity client), which resolves the
	// session and rejects any binding mismatch, and then re-checks the
	// session while the stream is open. When the browser does present a
	// session credential (same-site), the gateway authenticates it first and
	// social additionally requires the ticket to belong to that session.
	_, cookieErr := r.Cookie("__Host-kindle_access")
	credentialed := len(r.Header.Values("Authorization")) > 0 || cookieErr == nil
	var principal sharedauth.IdentityPrincipal
	if credentialed {
		principal, err = p.identity.AuthenticateRequest(r.Context(), r)
		if err != nil {
			status := 503
			var failure *sharedauth.IdentityAuthorityError
			if errors.As(err, &failure) {
				status = failure.StatusCode()
			} else if errors.Is(err, sharedauth.ErrScopeDenied) {
				status = 403
			}
			connectedError(w, status, "socket_identity_unavailable")
			return
		}
	}
	binding := sharedauth.StreamTicketBinding{SchemaMajor: 2, Topics: topics, FilterDigest: sharedauth.StreamFilterDigest(topics), Origin: r.Header.Get("Origin"), Endpoint: "/v2/stream"}
	if credentialed && sharedauth.ValidateChatStreamBinding(principal.StreamPrincipal(), binding) != nil {
		connectedError(w, 401, "socket_scope_denied")
		return
	}
	target := *p.target
	target.RawQuery = r.URL.RawQuery
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		connectedError(w, 503, "socket_upstream_unavailable")
		return
	}
	for _, name := range []string{"Authorization", "Origin"} {
		if values := r.Header.Values(name); len(values) > 1 {
			connectedError(w, 400, "ambiguous_header")
			return
		} else if len(values) == 1 {
			request.Header.Set(name, values[0])
		}
	}
	if cookie, err := r.Cookie("__Host-kindle_access"); err == nil {
		request.AddCookie(cookie)
	}
	request.Header.Set("X-Request-ID", uuid.NewString())
	if credentialed {
		if err = p.signer.SignRequest(request, principal, []string{"owner"}, nil); err != nil {
			connectedError(w, 403, "socket_assertion_invalid")
			return
		}
	}
	dialer := *p.dialer
	dialer.Subprotocols = []string{"sidiora.v2", "ticket." + ticket}
	upstream, response, err := dialer.DialContext(r.Context(), target.String(), request.Header)
	if err != nil {
		status := 503
		if response != nil {
			_ = response.Body.Close()
			switch response.StatusCode {
			case 400, 401, 403, 429:
				status = response.StatusCode
			}
		}
		connectedError(w, status, "socket_upstream_unavailable")
		return
	}
	defer upstream.Close()
	if upstream.Subprotocol() != "sidiora.v2" {
		connectedError(w, 502, "socket_protocol_invalid")
		return
	}
	client, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer client.Close()
	client.SetReadLimit(64 << 10)
	upstream.SetReadLimit(64 << 10)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	var once sync.Once
	closeBoth := func() { once.Do(func() { cancel(); _ = client.Close(); _ = upstream.Close() }) }
	defer closeBoth()
	failures := make(chan error, 2)
	pump := func(destination, source *websocket.Conn) {
		for {
			kind, data, err := source.ReadMessage()
			if err == nil {
				err = destination.SetWriteDeadline(time.Now().Add(10 * time.Second))
			}
			if err == nil {
				err = destination.WriteMessage(kind, data)
			}
			if err != nil {
				failures <- err
				closeBoth()
				return
			}
		}
	}
	go pump(upstream, client)
	go pump(client, upstream)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-failures:
			return
		case <-ticker.C:
			if !credentialed {
				// Ticket-only streams: social re-checks the redeemed
				// session at the identity authority and closes the stream.
				continue
			}
			if !principal.AccessExpiresAt.After(time.Now()) {
				return
			}
			check, cancelCheck := context.WithTimeout(ctx, 2*time.Second)
			err := p.identity.CheckStreamSession(check, principal.StreamPrincipal())
			cancelCheck()
			if err != nil {
				return
			}
		}
	}
}
