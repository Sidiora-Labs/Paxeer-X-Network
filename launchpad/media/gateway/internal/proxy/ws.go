package proxy

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/auth"
)

// WS is a WebSocket reverse tunnel fronting media/social's /ws hub. It upgrades
// the public client, dials the upstream with the injected X-Actor-Wallet header,
// and pumps frames in both directions. Each direction is a synchronous
// read->write loop, so a slow peer back-pressures its source rather than growing
// an unbounded buffer (invariant i11); per-connection read limits cap memory.
type WS struct {
	target      *url.URL
	upgrader    websocket.Upgrader
	dialer      *websocket.Dialer
	readLimit   int64
	writeWait   time.Duration
	dialTimeout time.Duration
	logger      *slog.Logger
}

// WSDeps configures NewWS.
type WSDeps struct {
	// TargetBaseURL is the media/social WS base (e.g. ws://social:3000); the
	// "/ws" path is appended when dialing.
	TargetBaseURL string
	ReadLimit     int64
	WriteWait     time.Duration
	DialTimeout   time.Duration
	// CheckOrigin overrides the upgrader origin policy (defaults to allow-all;
	// origin is enforced at the CDN/load-balancer in front of the edge).
	CheckOrigin func(*http.Request) bool
	Logger      *slog.Logger
}

// NewWS builds a WS tunnel. It returns an error if the target URL is invalid.
func NewWS(d WSDeps) (*WS, error) {
	base := strings.TrimRight(d.TargetBaseURL, "/")
	target, err := url.Parse(base + "/ws")
	if err != nil {
		return nil, err
	}
	if target.Host == "" || target.User != nil || target.RawQuery != "" || target.Fragment != "" || (target.Scheme != "ws" && target.Scheme != "wss") {
		return nil, errors.New("invalid websocket upstream")
	}
	readLimit := d.ReadLimit
	if readLimit <= 0 || readLimit > 64<<10 {
		readLimit = 4096
	}
	writeWait := d.WriteWait
	if writeWait <= 0 || writeWait > 10*time.Second {
		writeWait = 10 * time.Second
	}
	dialTimeout := d.DialTimeout
	if dialTimeout <= 0 || dialTimeout > 3*time.Second {
		dialTimeout = 3 * time.Second
	}
	checkOrigin := d.CheckOrigin
	if checkOrigin == nil {
		checkOrigin = func(r *http.Request) bool {
			v := r.Header.Get("Origin")
			if v == "" {
				return true
			}
			u, e := url.Parse(v)
			return e == nil && u.Host == r.Host && (u.Scheme == "http" || u.Scheme == "https")
		}
	}
	return &WS{
		target:      target,
		upgrader:    websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024, CheckOrigin: checkOrigin},
		dialer:      &websocket.Dialer{HandshakeTimeout: dialTimeout},
		readLimit:   readLimit,
		writeWait:   writeWait,
		dialTimeout: dialTimeout,
		logger:      d.Logger,
	}, nil
}

// ServeHTTP upgrades the client and tunnels to media/social. It must be mounted
// behind auth.RequireSession so the actor wallet is present.
func (p *WS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	wallet := auth.Actor(r.Context())
	if wallet == "" {
		sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "missing session")
		return
	}

	// Dial upstream BEFORE upgrading the client, so an upstream failure is a
	// clean HTTP error instead of a half-open client socket.
	dialCtx, cancel := context.WithTimeout(r.Context(), p.dialTimeout)
	defer cancel()
	upstream, resp, err := p.dialer.DialContext(dialCtx, p.target.String(), http.Header{actorHeader: {wallet}})
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		p.logErr("dial social ws", err)
		sharedhttp.WriteError(w, http.StatusBadGateway, "Bad Gateway", "social realtime upstream unavailable")
		return
	}
	defer upstream.Close()

	client, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // upgrader already wrote the error
	}
	defer client.Close()

	client.SetReadLimit(p.readLimit)
	upstream.SetReadLimit(p.readLimit)

	errc := make(chan error, 2)
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = client.Close()
			_ = upstream.Close()
		})
	}
	go p.pump(upstream, client, errc, closeBoth) // client -> upstream
	go p.pump(client, upstream, errc, closeBoth) // upstream -> client
	select {
	case <-errc:
	case <-r.Context().Done():
	}
	closeBoth()
}

// pump copies one message at a time from src to dst until either side errors.
// The synchronous loop is the back-pressure mechanism: a slow dst stalls reads
// from src rather than buffering.
func (p *WS) pump(dst, src *websocket.Conn, errc chan<- error, closeBoth func()) {
	for {
		mt, msg, err := src.ReadMessage()
		if err != nil {
			errc <- err
			closeBoth()
			return
		}
		if err := dst.SetWriteDeadline(time.Now().Add(p.writeWait)); err != nil {
			errc <- err
			closeBoth()
			return
		}
		if err := dst.WriteMessage(mt, msg); err != nil {
			errc <- err
			closeBoth()
			return
		}
	}
}

func (p *WS) logErr(op string, err error) {
	if p.logger != nil {
		p.logger.Error("ws tunnel error", slog.String("op", op), slog.String("reason", "realtime_dependency_unavailable"))
	}
}
