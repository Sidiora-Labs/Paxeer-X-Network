package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

const DeliverPath = "/v1/peer/deliver"

type Peer struct {
	ID         string
	Address    string
	SPKISHA256 string
}

type Limits struct {
	MaxQueue           int
	MaxPendingSessions int
	PendingTTL         time.Duration
	MaxBodyBytes       int64
	RetryBackoff       time.Duration
	MaxRetryBackoff    time.Duration
	RequestTimeout     time.Duration
}

func DefaultLimits() Limits {
	return Limits{
		MaxQueue:           256,
		MaxPendingSessions: 64,
		PendingTTL:         30 * time.Second,
		MaxBodyBytes:       4 << 20,
		RetryBackoff:       10 * time.Millisecond,
		MaxRetryBackoff:    500 * time.Millisecond,
		RequestTimeout:     10 * time.Second,
	}
}

type Config struct {
	SelfID             string
	ListenAddr         string
	CertFile           string
	KeyFile            string
	CAFile             string
	Peers              []Peer
	OperatorSPKISHA256 string
	OperatorCAFile     string
	Limits             Limits
}

type Envelope struct {
	Session  string `json:"session"`
	Protocol string `json:"protocol"`
	Sender   string `json:"sender"`
	Round    int32  `json:"round"`
	Seq      uint64 `json:"seq"`
	Type     string `json:"type"`
	Payload  []byte `json:"payload"`
	Relay    bool   `json:"relay,omitempty"`
	Origin   string `json:"origin,omitempty"`
}

type peerEntry struct {
	id      string
	address string
	spki    [32]byte
}

type replayKey struct {
	sender string
	round  int32
	seq    uint64
}

type bufferedEnvelope struct {
	from string
	env  Envelope
}

type pendingSession struct {
	created   time.Time
	creator   string
	envs      []bufferedEnvelope
	seen      map[replayKey]struct{}
	perSender map[string]int
}

type Transport struct {
	self       string
	listenAddr string
	limits     Limits
	peers      map[string]peerEntry
	bySPKI     map[[32]byte]string
	operator   operatorPin
	cert       tls.Certificate
	roots      *x509.CertPool
	clients    map[string]*http.Client
	mux        *http.ServeMux
	server     *http.Server

	mu       sync.Mutex
	sessions map[string]*Session
	pending  map[string]*pendingSession
	closed   map[string]time.Time
	shut     bool
}

func New(cfg Config) (*Transport, error) {
	if cfg.SelfID == "" {
		return nil, fmt.Errorf("%w: self id is required", ErrConfig)
	}
	limits := cfg.Limits
	if limits == (Limits{}) {
		limits = DefaultLimits()
	}
	if limits.MaxQueue <= 0 || limits.MaxPendingSessions <= 0 || limits.PendingTTL <= 0 || limits.MaxBodyBytes <= 0 || limits.RetryBackoff <= 0 || limits.MaxRetryBackoff < limits.RetryBackoff || limits.RequestTimeout <= 0 {
		return nil, fmt.Errorf("%w: every limit must be positive", ErrConfig)
	}
	cert, err := loadKeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, err
	}
	if cfg.CAFile == "" {
		return nil, fmt.Errorf("%w: peer CA path is required", ErrConfig)
	}
	roots, err := loadCertPool(cfg.CAFile)
	if err != nil {
		return nil, err
	}
	t := &Transport{
		self:       cfg.SelfID,
		listenAddr: cfg.ListenAddr,
		limits:     limits,
		peers:      make(map[string]peerEntry, len(cfg.Peers)),
		bySPKI:     make(map[[32]byte]string, len(cfg.Peers)),
		cert:       cert,
		roots:      roots,
		clients:    make(map[string]*http.Client, len(cfg.Peers)),
		mux:        http.NewServeMux(),
		sessions:   make(map[string]*Session),
		pending:    make(map[string]*pendingSession),
		closed:     make(map[string]time.Time),
	}
	for _, p := range cfg.Peers {
		if p.ID == "" || p.Address == "" {
			return nil, fmt.Errorf("%w: peer id and address are required", ErrConfig)
		}
		if _, dup := t.peers[p.ID]; dup {
			return nil, fmt.Errorf("%w: duplicate peer id %s", ErrConfig, p.ID)
		}
		pin, err := ParseSPKIHash(p.SPKISHA256)
		if err != nil {
			return nil, fmt.Errorf("peer %s: %w", p.ID, err)
		}
		if _, dup := t.bySPKI[pin]; dup {
			return nil, fmt.Errorf("%w: duplicate SPKI pin for peer %s", ErrConfig, p.ID)
		}
		t.peers[p.ID] = peerEntry{id: p.ID, address: p.Address, spki: pin}
		t.bySPKI[pin] = p.ID
	}
	selfEntry, ok := t.peers[t.self]
	if !ok {
		return nil, fmt.Errorf("%w: self id %s is not a configured peer", ErrConfig, t.self)
	}
	if SPKIHash(cert.Leaf) != selfEntry.spki {
		return nil, fmt.Errorf("%w: own certificate does not match own pin", ErrConfig)
	}
	if cfg.OperatorSPKISHA256 != "" {
		pin, err := ParseSPKIHash(cfg.OperatorSPKISHA256)
		if err != nil {
			return nil, fmt.Errorf("operator: %w", err)
		}
		if _, clash := t.bySPKI[pin]; clash {
			return nil, fmt.Errorf("%w: operator pin equals a peer pin", ErrConfig)
		}
		t.operator.spki = pin
		t.operator.hasSPKI = true
	}
	clientCAs := roots
	if cfg.OperatorCAFile != "" {
		opPool, err := loadCertPool(cfg.OperatorCAFile)
		if err != nil {
			return nil, err
		}
		t.operator.pool = opPool
		clientCAs, err = loadCertPool(cfg.CAFile, cfg.OperatorCAFile)
		if err != nil {
			return nil, err
		}
	}
	for id, p := range t.peers {
		if id == t.self {
			continue
		}
		t.clients[id] = &http.Client{
			Timeout: limits.RequestTimeout,
			Transport: &http.Transport{
				TLSClientConfig:     t.clientTLSConfig(p.spki),
				ForceAttemptHTTP2:   true,
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: limits.RequestTimeout,
			},
		}
	}
	t.mux.HandleFunc(DeliverPath, t.handleDeliver)
	t.server = &http.Server{
		Handler:           t.mux,
		TLSConfig:         t.serverTLSConfig(clientCAs),
		ReadHeaderTimeout: limits.RequestTimeout,
		ReadTimeout:       limits.RequestTimeout,
		WriteTimeout:      limits.RequestTimeout,
		IdleTimeout:       120 * time.Second,
	}
	return t, nil
}

func (t *Transport) SelfID() string {
	return t.self
}

func (t *Transport) Handle(pattern string, h http.Handler) {
	t.mux.Handle(pattern, h)
}

func (t *Transport) Serve(l net.Listener) error {
	err := t.server.ServeTLS(l, "", "")
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (t *Transport) ListenAndServe() error {
	if t.listenAddr == "" {
		return fmt.Errorf("%w: listen address is required", ErrConfig)
	}
	l, err := net.Listen("tcp", t.listenAddr)
	if err != nil {
		return err
	}
	return t.Serve(l)
}

func (t *Transport) Close() error {
	t.mu.Lock()
	if t.shut {
		t.mu.Unlock()
		return nil
	}
	t.shut = true
	open := make([]*Session, 0, len(t.sessions))
	for _, s := range t.sessions {
		open = append(open, s)
	}
	t.mu.Unlock()
	for _, s := range open {
		s.Close()
	}
	err := t.server.Close()
	for _, c := range t.clients {
		c.CloseIdleConnections()
	}
	return err
}

func (t *Transport) handleDeliver(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := t.RequestIdentity(r)
	if err != nil || id.Kind != IdentityPeer {
		http.Error(w, ErrNotParticipant.Error(), http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, t.limits.MaxBodyBytes)
	var env Envelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		http.Error(w, fmt.Sprintf("%v: %v", ErrBadEnvelope, err), http.StatusBadRequest)
		return
	}
	if err := t.Deliver(id.ID, &env); err != nil {
		http.Error(w, err.Error(), statusFor(err))
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, ErrReplay):
		return http.StatusConflict
	case errors.Is(err, ErrNotParticipant), errors.Is(err, ErrSenderMismatch):
		return http.StatusForbidden
	case errors.Is(err, ErrQueueFull), errors.Is(err, ErrClosed), errors.Is(err, ErrPeerQuota):
		return http.StatusServiceUnavailable
	case errors.Is(err, ErrSessionClosed):
		return http.StatusGone
	}
	return http.StatusBadRequest
}

func (t *Transport) Deliver(from string, env *Envelope) error {
	if env == nil || env.Session == "" || env.Protocol == "" || env.Seq == 0 || env.Type == "" {
		return ErrBadEnvelope
	}
	if env.Sender != from {
		return ErrSenderMismatch
	}
	if _, ok := t.peers[from]; !ok || from == t.self {
		return ErrNotParticipant
	}
	t.mu.Lock()
	if t.shut {
		t.mu.Unlock()
		return ErrClosed
	}
	s := t.sessions[env.Session]
	if s == nil {
		err := t.bufferLocked(from, env)
		t.mu.Unlock()
		return err
	}
	t.mu.Unlock()
	return s.deliver(env)
}

func (t *Transport) purgeLocked(now time.Time) {
	for id, ps := range t.pending {
		if now.Sub(ps.created) > t.limits.PendingTTL {
			delete(t.pending, id)
		}
	}
	for id, at := range t.closed {
		if now.Sub(at) > t.limits.PendingTTL {
			delete(t.closed, id)
		}
	}
}

func (t *Transport) bufferLocked(from string, env *Envelope) error {
	now := time.Now()
	t.purgeLocked(now)
	if _, ok := t.closed[env.Session]; ok {
		return ErrSessionClosed
	}
	ps := t.pending[env.Session]
	if ps == nil {
		if len(t.pending) >= t.limits.MaxPendingSessions {
			return ErrQueueFull
		}
		created := 0
		for _, other := range t.pending {
			if other.creator == from {
				created++
			}
		}
		if created >= t.pendingSessionsPerPeer() {
			return fmt.Errorf("%w: peer %s holds %d pending sessions", ErrPeerQuota, from, created)
		}
		ps = &pendingSession{created: now, creator: from, seen: make(map[replayKey]struct{}), perSender: make(map[string]int)}
		t.pending[env.Session] = ps
	}
	key := replayKey{sender: from, round: env.Round, seq: env.Seq}
	if _, dup := ps.seen[key]; dup {
		return ErrReplay
	}
	if len(ps.envs) >= t.limits.MaxQueue {
		return ErrQueueFull
	}
	if ps.perSender[from] >= t.pendingMessagesPerPeer() {
		return fmt.Errorf("%w: peer %s holds %d pending messages in session %s", ErrPeerQuota, from, ps.perSender[from], env.Session)
	}
	ps.seen[key] = struct{}{}
	ps.perSender[from]++
	ps.envs = append(ps.envs, bufferedEnvelope{from: from, env: *env})
	return nil
}

func (t *Transport) otherPeers() int {
	if n := len(t.peers) - 1; n > 0 {
		return n
	}
	return 1
}

func (t *Transport) pendingSessionsPerPeer() int {
	return max(1, t.limits.MaxPendingSessions/t.otherPeers())
}

func (t *Transport) pendingMessagesPerPeer() int {
	return max(1, t.limits.MaxQueue/t.otherPeers())
}

func (t *Transport) Request(ctx context.Context, peerID, path string, body []byte) (int, []byte, error) {
	c, ok := t.clients[peerID]
	if !ok {
		return 0, nil, ErrNotParticipant
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+t.peers[peerID].address+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, t.limits.MaxBodyBytes))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, out, nil
}

func (t *Transport) post(ctx context.Context, peerID string, env *Envelope) (int, error) {
	c, ok := t.clients[peerID]
	if !ok {
		return 0, ErrNotParticipant
	}
	body, err := json.Marshal(env)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+t.peers[peerID].address+DeliverPath, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

func (t *Transport) send(s *Session, peerID string, env *Envelope) {
	backoff := t.limits.RetryBackoff
	for attempt := 0; ; attempt++ {
		status, err := t.post(s.ctx, peerID, env)
		switch {
		case err == nil && status == http.StatusAccepted:
			return
		case err == nil && status == http.StatusConflict && attempt > 0:
			return
		case err != nil || status == http.StatusServiceUnavailable:
			if s.ctx.Err() != nil {
				return
			}
		default:
			s.setErr(fmt.Errorf("%w: peer %s answered %d for seq %d", ErrPeerRefused, peerID, status, env.Seq))
			return
		}
		timer := time.NewTimer(backoff)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff *= 2
		if backoff > t.limits.MaxRetryBackoff {
			backoff = t.limits.MaxRetryBackoff
		}
	}
}

var (
	ErrBadEnvelope        = errors.New("transport: malformed envelope")
	ErrSenderMismatch     = errors.New("transport: sender does not match the authenticated peer")
	ErrNotParticipant     = errors.New("transport: sender is not a participant")
	ErrReplay             = errors.New("transport: replayed message")
	ErrQueueFull          = errors.New("transport: queue full")
	ErrClosed             = errors.New("transport: closed")
	ErrSessionClosed      = errors.New("transport: session closed")
	ErrSessionExists      = errors.New("transport: session already open")
	ErrProtocolMismatch   = errors.New("transport: protocol does not match the session")
	ErrRoundMismatch      = errors.New("transport: round does not match the message")
	ErrUnsupportedMessage = errors.New("transport: unsupported message")
	ErrPeerRefused        = errors.New("transport: peer refused message")
	ErrReceiver           = errors.New("transport: receiver rejected message")
	ErrAlreadyAttached    = errors.New("transport: session already has a receiver")
	ErrPeerQuota          = errors.New("transport: peer exceeds its pending share")
	ErrRoundDeadline      = errors.New("transport: no message arrived within the round deadline")
)
