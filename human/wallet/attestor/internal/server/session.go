package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"sort"
	"sync"

	"github.com/getamis/alice/types"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/transport"
	tssecdsa "github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/ecdsa"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/refresh"
)

const (
	protocolRefreshSecp = "attestor/refresh/secp256k1"
	protocolRefreshEd   = "attestor/refresh/ed25519"
	protocolAddShare    = "attestor/addshare"
	protocolKeygenSecp  = "attestor/keygen/secp256k1"
	protocolKeygenEd    = "attestor/keygen/ed25519"
	protocolSignSecp    = "attestor/sign/secp256k1"
	protocolSignEd      = "attestor/sign/ed25519"
)

var protocolKinds = map[string]transport.SessionKind{
	protocolRefreshSecp: transport.KindRefreshSecp256k1,
	protocolRefreshEd:   transport.KindRefreshEd25519,
	protocolAddShare:    transport.KindAddShare,
	protocolKeygenSecp:  transport.KindKeygenSecp256k1,
	protocolKeygenEd:    transport.KindKeygenEd25519,
	protocolSignSecp:    transport.KindSignSecp256k1,
	protocolSignEd:      transport.KindSignEd25519,
}

var registerOnce sync.Once

func registerMessages() error {
	var err error
	registerOnce.Do(func() {
		if e := transport.RegisterJSONMessage("attestor.refresh.ShareProofMessage", func() types.Message { return &refresh.ShareProofMessage{} }); e != nil {
			err = e
			return
		}
		err = transport.RegisterJSONMessage("attestor.refresh.EdDSARefreshMessage", func() types.Message { return &refresh.EdDSARefreshMessage{} })
	})
	return err
}

type peerSession struct {
	*transport.Session
	mu      sync.Mutex
	bindErr error
}

func (p *peerSession) attach(r transport.Receiver) {
	if err := p.Session.Attach(r); err != nil {
		p.mu.Lock()
		if p.bindErr == nil {
			p.bindErr = err
		}
		p.mu.Unlock()
	}
}

func (p *peerSession) err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bindErr != nil {
		return p.bindErr
	}
	return p.Session.Err()
}

type ecdsaNet struct{ *peerSession }

func (n ecdsaNet) Bind(r tssecdsa.Receiver) { n.attach(r) }

type refreshNet struct{ *peerSession }

func (n refreshNet) Bind(r refresh.Receiver) { n.attach(r) }

type eddsaNet struct{ *peerSession }

func (n eddsaNet) Register(m types.MessageMain) { n.attach(m) }

func sessionName(requestSession, phase string) string {
	return requestSession + "/" + phase
}

func deriveSSID(domain, requestSession, keyID string) []byte {
	h := sha256.New()
	h.Write([]byte("paxeer-x-attestor/" + domain))
	h.Write([]byte{0})
	h.Write([]byte(requestSession))
	h.Write([]byte{0})
	h.Write([]byte(keyID))
	return h.Sum(nil)
}

func sortedUnique(ids []string) ([]string, bool) {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	for i := range out {
		if out[i] == "" || (i > 0 && out[i-1] == out[i]) {
			return nil, false
		}
	}
	return out, true
}

func (s *Server) runSession(ctx context.Context, requestSession, phase, protocol string, participants []string, fn func(context.Context, *peerSession) error) *Error {
	kind, known := protocolKinds[protocol]
	if !known {
		return newError(CodeSessionOpen, "unknown protocol %s", protocol)
	}
	ts, err := s.opts.Transport.OpenKind(sessionName(requestSession, phase), participants, protocol, kind)
	if err != nil {
		return newError(CodeSessionOpen, "%v", err)
	}
	ps := &peerSession{Session: ts}
	defer ts.Close()
	runCtx, cancel := context.WithTimeout(ctx, s.opts.ProtocolTimeout)
	defer cancel()
	runErr := fn(runCtx, ps)
	if runErr == nil {
		runErr = ts.Flush(runCtx)
	}
	if runErr != nil {
		if sessErr := ps.err(); sessErr != nil {
			return newError(CodeSessionFailed, "%v: %v", runErr, sessErr)
		}
		if errors.Is(runErr, context.DeadlineExceeded) || errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return newError(CodeSessionTimeout, "%v", runErr)
		}
		return newError(CodeSessionFailed, "%v", runErr)
	}
	return nil
}
