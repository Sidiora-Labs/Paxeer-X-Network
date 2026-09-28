package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/health"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/lxwire"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy"
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
	Ledger          policy.Ledger
	Tokens          *jwt.TokenVerifier
	Agents          *agent.AgentVerifier
	Activities      *lxwire.Registry
	PeerProbe       func(ctx context.Context) map[string]health.PeerState
	ProtocolTimeout time.Duration
}

type Server struct {
	opts     Options
	mux      *http.ServeMux
	reporter *health.Reporter
	keyMu    sync.Mutex
	keyLocks map[string]*sync.Mutex
}

func New(opts Options) (*Server, error) {
	if opts.NodeID == "" || opts.ChainID == 0 || opts.Store == nil || opts.Audit == nil || opts.Transport == nil || opts.Policy == nil || opts.Ledger == nil {
		return nil, errors.New("server: node id, chain id, store, audit log, transport, policy and ledger are required")
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
	if err := registerKernelKinds(opts.Policy); err != nil {
		return nil, err
	}
	s := &Server{opts: opts, mux: http.NewServeMux(), keyLocks: make(map[string]*sync.Mutex)}
	reporter, err := health.NewReporter(opts.NodeID, opts.Region, health.Providers{
		Shares:    s.shareStats,
		AuditHead: opts.Audit.Head,
		Peers:     opts.PeerProbe,
		Readiness: s.readiness,
	})
	if err != nil {
		return nil, err
	}
	s.reporter = reporter
	s.mux.HandleFunc(PathGenerate, s.post(s.HandleGenerate))
	s.mux.HandleFunc(PathImport, s.post(s.HandleImport))
	s.mux.HandleFunc(PathRefresh, s.post(s.HandleRefresh))
	s.mux.HandleFunc(PathAddShare, s.post(s.HandleAddShare))
	s.mux.HandleFunc(PathSign, s.post(s.HandleSign))
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

func (s *Server) audit(kind, keyID, subject, decision, reason, sessionID string) (uint64, *Error) {
	rec, err := s.opts.Audit.Append(audit.Entry{Kind: kind, KeyID: keyID, Subject: []byte(subject), Decision: decision, Reason: reason, SessionID: sessionID})
	if err != nil {
		return 0, newError(CodeStoreAuditFailed, "%v", err)
	}
	return rec.Sequence, nil
}

func APITLSConfig(certFile, keyFile, clientCAFile string) (*tls.Config, error) {
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("server: api certificate: %w", err)
	}
	pem, err := os.ReadFile(clientCAFile)
	if err != nil {
		return nil, fmt.Errorf("server: api client CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("server: api client CA holds no certificate")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{pair},
		ClientCAs:    pool,
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
