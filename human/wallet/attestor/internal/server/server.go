package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/audit"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/auth/agent"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/auth/jwt"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/backup"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/health"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/lxwire"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy/lx"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/store"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/transport"
)

const (
	PathGenerate = "/v1/keys/generate"
	PathImport   = "/v1/keys/import"
	PathRefresh  = "/v1/keys/refresh"
	PathAddShare = "/v1/keys/addshare"
	PathSign     = "/v1/sign"
	PathHealth   = "/health"

	DefaultProtocolTimeout = 5 * time.Minute
	maxRequestBytes        = 2 << 20
)

type Options struct {
	NodeID          string
	Region          string
	ChainID         uint64
	Ceremony        bool
	Participants    []string
	Store           *store.Store
	Audit           *audit.Log
	Transport       *transport.Transport
	Policy          *policy.Policy
	Kernel          *lx.Evaluator
	Ledger          policy.Ledger
	Clients         *ClientAuthorities
	Tokens          *jwt.TokenVerifier
	Agents          *agent.AgentVerifier
	Activities      *lxwire.Registry
	PeerProbe       func(ctx context.Context) map[string]health.PeerState
	Replica         func() health.ReplicaState
	Snapshots       *backup.Writer
	ProtocolTimeout time.Duration
	PeerTimeout     time.Duration
	RoundTimeout    time.Duration
}

type Server struct {
	opts       Options
	mux        *http.ServeMux
	reporter   *health.Reporter
	keyMu      sync.Mutex
	keyLocks   map[string]*sync.Mutex
	spends     *policy.SpendLedger
	votesMu    sync.Mutex
	votes      map[string]*refreshVote
	afterStage func(keyID string)
	ceremony   *ceremonyImports
}

func New(opts Options) (*Server, error) {
	if opts.NodeID == "" || opts.ChainID == 0 || opts.Store == nil || opts.Audit == nil || opts.Transport == nil || opts.Policy == nil || opts.Ledger == nil || opts.Clients == nil {
		return nil, errors.New("server: node id, chain id, store, audit log, transport, policy, ledger and client authorities are required")
	}
	participants, ok := sortedUnique(opts.Participants)
	if !ok || len(participants) < 2 {
		return nil, errors.New("server: participants must be distinct non-empty ids")
	}
	self := false
	for _, p := range participants {
		self = self || p == opts.NodeID
	}
	if !self {
		return nil, errors.New("server: node id is not among the participants")
	}
	opts.Participants = participants
	if opts.ProtocolTimeout <= 0 {
		opts.ProtocolTimeout = DefaultProtocolTimeout
	}
	if opts.PeerProbe == nil {
		opts.PeerProbe = func(context.Context) map[string]health.PeerState { return map[string]health.PeerState{} }
	}
	if err := registerMessages(); err != nil {
		return nil, err
	}
	s := &Server{opts: opts, mux: http.NewServeMux(), keyLocks: make(map[string]*sync.Mutex)}
	if err := s.initPeer(); err != nil {
		return nil, err
	}
	ceremony, err := newCeremonyImports(opts.Ceremony)
	if err != nil {
		return nil, err
	}
	s.ceremony = ceremony
	providers := health.Providers{
		Shares:    s.shareStats,
		AuditHead: opts.Audit.Head,
		Peers:     opts.PeerProbe,
		Readiness: s.readiness,
		Replica:   opts.Replica,
	}
	if opts.Snapshots != nil {
		providers.Snapshot = opts.Snapshots.State
	}
	reporter, err := health.NewReporter(opts.NodeID, opts.Region, providers)
	if err != nil {
		return nil, err
	}
	s.reporter = reporter
	s.mux.HandleFunc(PathGenerate, s.post(s.gatewayOnly("keys.generate", s.HandleGenerate)))
	s.mux.HandleFunc(PathImport, s.post(s.operatorOnly("keys.import", s.HandleImport)))
	s.mux.HandleFunc(PathRefresh, s.post(s.operatorOnly("keys.refresh", s.HandleRefresh)))
	s.mux.HandleFunc(PathAddShare, s.post(s.operatorOnly("keys.addshare", s.HandleAddShare)))
	s.mux.HandleFunc(PathSign, s.post(s.signRoute()))
	s.mux.HandleFunc(PathHealth, s.HandleHealth)
	return s, nil
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) post(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, newError(CodeSessionBadRequest, "method %s not allowed", r.Method))
			return
		}
		if !verifiedClient(r) {
			writeError(w, newError(CodeOperatorRequired, "a verified client certificate is required"))
			return
		}
		h(w, r)
	}
}

func verifiedClient(r *http.Request) bool {
	return r.TLS != nil && len(r.TLS.VerifiedChains) > 0 && len(r.TLS.VerifiedChains[0]) > 0
}

type Authority int

const (
	AuthorityGateway Authority = 1 << iota
	AuthorityOperator
)

type ClientAuthorities struct {
	pool     *x509.CertPool
	gateway  []*x509.Certificate
	operator []*x509.Certificate
}

func parseCAFile(label, path string) ([]*x509.Certificate, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("server: %s CA: %w", label, err)
	}
	var out []*x509.Certificate
	for len(raw) > 0 {
		var block *pem.Block
		block, raw = pem.Decode(raw)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("server: %s CA: %w", label, err)
		}
		out = append(out, cert)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("server: %s CA holds no certificate", label)
	}
	return out, nil
}

func LoadClientAuthorities(gatewayCAFile, operatorCAFile string) (*ClientAuthorities, error) {
	gateway, err := parseCAFile("gateway", gatewayCAFile)
	if err != nil {
		return nil, err
	}
	operator, err := parseCAFile("operator", operatorCAFile)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	for _, c := range append(append([]*x509.Certificate{}, gateway...), operator...) {
		pool.AddCert(c)
	}
	return &ClientAuthorities{pool: pool, gateway: gateway, operator: operator}, nil
}

func (c *ClientAuthorities) Of(state *tls.ConnectionState) Authority {
	var out Authority
	if c == nil || state == nil {
		return out
	}
	for _, chain := range state.VerifiedChains {
		if len(chain) == 0 {
			continue
		}
		root := chain[len(chain)-1]
		for _, g := range c.gateway {
			if root.Equal(g) {
				out |= AuthorityGateway
			}
		}
		for _, o := range c.operator {
			if root.Equal(o) {
				out |= AuthorityOperator
			}
		}
	}
	return out
}

func (s *Server) requireAuthority(kind string, want Authority, name string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.opts.Clients.Of(r.TLS)&want == 0 {
			writeError(w, s.deny(kind, "", "", "denied", "", newError(CodeOperatorRequired, "%s needs a client certificate issued by the %s CA", kind, name)))
			return
		}
		h(w, r)
	}
}

func (s *Server) operatorOnly(kind string, h http.HandlerFunc) http.HandlerFunc {
	return s.requireAuthority(kind, AuthorityOperator, "operator", h)
}

func (s *Server) gatewayOnly(kind string, h http.HandlerFunc) http.HandlerFunc {
	return s.requireAuthority(kind, AuthorityGateway, "gateway", h)
}

func readBody(r *http.Request) ([]byte, *Error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes+1))
	if err != nil {
		return nil, newError(CodeSessionBadRequest, "request body unreadable")
	}
	if len(body) > maxRequestBytes {
		return nil, newError(CodeSessionBadRequest, "request body exceeds %d bytes", maxRequestBytes)
	}
	return body, nil
}

func (s *Server) lockKey(keyID string) func() {
	s.keyMu.Lock()
	m, ok := s.keyLocks[keyID]
	if !ok {
		m = &sync.Mutex{}
		s.keyLocks[keyID] = m
	}
	s.keyMu.Unlock()
	m.Lock()
	return m.Unlock
}

func (s *Server) shareStats() (uint64, uint64, error) {
	recs, err := s.opts.Store.List()
	if err != nil {
		return 0, 0, err
	}
	var epoch uint64
	for _, r := range recs {
		if r.Epoch > epoch {
			epoch = r.Epoch
		}
	}
	return uint64(len(recs)), epoch, nil
}

func (s *Server) readiness() error {
	if _, err := s.opts.Store.List(); err != nil {
		return err
	}
	return s.opts.Audit.Verify()
}

func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, newError(CodeSessionBadRequest, "method %s not allowed", r.Method))
		return
	}
	report := s.reporter.Report(r.Context())
	status := http.StatusOK
	if !report.Ready {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, report)
}

func (s *Server) snapshotAfter(operation string) {
	if s.opts.Snapshots != nil {
		_, _ = s.opts.Snapshots.WriteSnapshot(operation)
	}
}

func (s *Server) audit(kind, keyID, subject, decision, reason, sessionID string) (uint64, *Error) {
	rec, err := s.opts.Audit.Append(audit.Entry{Kind: kind, KeyID: keyID, Subject: []byte(subject), Decision: decision, Reason: reason, SessionID: sessionID})
	if err != nil {
		e := newError(CodeStoreAuditFailed, "%v", err)
		e.audited = true
		return 0, e
	}
	return rec.Sequence, nil
}

func (s *Server) deny(kind, keyID, subject, decision, sessionID string, e *Error) *Error {
	if e == nil || e.audited {
		return e
	}
	if _, ae := s.audit(kind, keyID, subject, decision, e.Code, sessionID); ae != nil {
		return ae
	}
	e.audited = true
	return e
}

type requestIDs struct {
	SessionID string `json:"session_id"`
	KeyID     string `json:"key_id"`
	Kind      string `json:"kind"`
}

func peekIDs(body []byte) requestIDs {
	var ids requestIDs
	if json.Unmarshal(body, &ids) != nil {
		return requestIDs{}
	}
	return ids
}

func (s *Server) finish(w http.ResponseWriter, kind string, body []byte, v any, e *Error) {
	if e != nil && !e.audited {
		ids := peekIDs(body)
		if kind == "sign" && ids.Kind != "" {
			kind = "sign." + ids.Kind
		}
		e = s.deny(kind, ids.KeyID, "", "failed", ids.SessionID, e)
	}
	respond(w, v, e)
}

func APITLSConfig(certFile, keyFile string, clients *ClientAuthorities) (*tls.Config, error) {
	if clients == nil {
		return nil, errors.New("server: api client authorities are required")
	}
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("server: api certificate: %w", err)
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{pair},
		ClientCAs:    clients.pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}, nil
}

func (s *Server) Serve(l net.Listener, cfg *tls.Config) *http.Server {
	srv := &http.Server{Handler: s.mux, TLSConfig: cfg, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(tls.NewListener(l, cfg)) }()
	return srv
}

func TCPPeerProbe(peers map[string]string) func(ctx context.Context) map[string]health.PeerState {
	ids := make([]string, 0, len(peers))
	for id := range peers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return func(ctx context.Context) map[string]health.PeerState {
		out := make(map[string]health.PeerState, len(ids))
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, id := range ids {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				dialCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				defer cancel()
				start := time.Now()
				conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", peers[id])
				state := health.PeerState{}
				if err == nil {
					state = health.PeerState{Reachable: true, RTT: time.Since(start)}
					_ = conn.Close()
				}
				mu.Lock()
				out[id] = state
				mu.Unlock()
			}(id)
		}
		wg.Wait()
		return out
	}
}
