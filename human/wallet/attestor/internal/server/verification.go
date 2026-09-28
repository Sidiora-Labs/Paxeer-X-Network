package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	gethcrypto "github.com/ethereum/go-ethereum/crypto"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/dealer"
	tssecdsa "github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/ecdsa"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/eddsa"
)

const (
	KindOperatorVerification = policy.KindOperatorVerification
	VerificationDomain       = policy.VerificationDomain

	verificationAuditKind = "ceremony.verification"
	verificationSubject   = "operator"
)

type VerificationRequest struct {
	SessionID       string   `json:"session_id"`
	KeyID           string   `json:"key_id"`
	Kind            string   `json:"kind"`
	Signers         []string `json:"signers"`
	ImportSessionID string   `json:"import_session_id"`
}

type ceremonyImport struct {
	session string
	window  string
	curve   dealer.Curve
	used    bool
}

type ceremonyImports struct {
	window string
	mu     sync.Mutex
	keys   map[string]*ceremonyImport
}

func newCeremonyImports(enabled bool) (*ceremonyImports, error) {
	c := &ceremonyImports{keys: map[string]*ceremonyImport{}}
	if !enabled {
		return c, nil
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, fmt.Errorf("server: ceremony window: %w", err)
	}
	c.window = hex.EncodeToString(id[:])
	return c, nil
}

func (c *ceremonyImports) record(keyID, session string, curve dealer.Curve) {
	if c == nil || c.window == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys[keyID] = &ceremonyImport{session: session, window: c.window, curve: curve}
}

func (c *ceremonyImports) lookup(keyID string) (ceremonyImport, bool) {
	if c == nil {
		return ceremonyImport{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	imp, ok := c.keys[keyID]
	if !ok {
		return ceremonyImport{}, false
	}
	return *imp, true
}

func (c *ceremonyImports) claim(keyID, session string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	imp, ok := c.keys[keyID]
	if !ok || imp.used || imp.session != session || imp.window != c.window {
		return false
	}
	imp.used = true
	return true
}

func (s *Server) signRoute() http.HandlerFunc {
	sign := s.gatewayOnly("sign", s.HandleSign)
	verify := s.operatorOnly(verificationAuditKind, s.HandleVerification)
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes+1))
		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), r.Body))
		if peekIDs(body).Kind == KindOperatorVerification {
			verify(w, r)
			return
		}
		sign(w, r)
	}
}

func (s *Server) HandleVerification(w http.ResponseWriter, r *http.Request) {
	body, e := readBody(r)
	var resp SignResponse
	if e == nil {
		resp, e = s.doVerification(r, body)
	}
	s.finish(w, verificationAuditKind, body, resp, e)
}

func (s *Server) doVerification(r *http.Request, body []byte) (SignResponse, *Error) {
	var req VerificationRequest
	if e := decodeRequest(body, &req); e != nil {
		return SignResponse{}, e
	}
	if e := requireIDs(req.SessionID, req.KeyID); e != nil {
		return SignResponse{}, e
	}
	refuse := func(e *Error) (SignResponse, *Error) {
		return SignResponse{}, s.deny(verificationAuditKind, req.KeyID, verificationSubject, "denied", req.SessionID, e)
	}
	if req.Kind != KindOperatorVerification {
		return refuse(newError(CodeSessionKind, "kind %q is not %s", req.Kind, KindOperatorVerification))
	}
	if r.Header.Get("Authorization") != "" || r.Header.Get(HeaderAgentKey) != "" {
		return refuse(newError(CodeSessionBadRequest, "operator verification carries no bearer token or agent signature"))
	}
	if req.ImportSessionID == "" || len(req.ImportSessionID) > 128 {
		return refuse(newError(CodeSessionBadRequest, "import_session_id must be 1 to 128 characters"))
	}
	if !s.opts.Ceremony {
		return refuse(newError(CodeVerificationWindow, "operator verification is granted only in the ceremony window that imported the key"))
	}
	unlock := s.lockKey(req.KeyID)
	defer unlock()
	imp, ok := s.ceremony.lookup(req.KeyID)
	if !ok || imp.window != s.ceremony.window {
		return refuse(newError(CodeVerificationNotImported, "key %q was not imported in this ceremony window", req.KeyID))
	}
	if imp.session != req.ImportSessionID {
		return refuse(newError(CodeVerificationNotImported, "key %q was not imported under session %q", req.KeyID, req.ImportSessionID))
	}
	if imp.used {
		return refuse(newError(CodeVerificationUsed, "key %q already received its verification signature", req.KeyID))
	}
	rec, payload, e := s.loadShare(req.KeyID)
	if e != nil {
		return refuse(e)
	}
	if c, _ := parseCurve(rec.Curve); c != imp.curve {
		return refuse(newError(CodeKeyCurve, "key %q no longer has the imported curve", req.KeyID))
	}
	signers, e := s.verificationSigners(req.Signers, rec.Participants)
	if e != nil {
		return refuse(e)
	}
	msg := policy.VerificationMessage(req.KeyID, rec.PublicKey, imp.session)
	view := &policy.Verification{KeyID: req.KeyID, PublicKey: append([]byte(nil), rec.PublicKey...), ImportSession: imp.session, Message: msg}
	decision := s.opts.Policy.Evaluate(payload.Account, policy.Request{Kind: KindOperatorVerification, View: view}, s.opts.Ledger)
	if !decision.Allowed {
		return refuse(policyError(decision.Code, decision.Reason))
	}
	b, share, e := payload.bundle()
	if e != nil {
		return refuse(e)
	}
	defer dealer.Wipe(b.Share)
	signed := msg
	if imp.curve == dealer.Secp256k1 {
		if share == nil {
			return refuse(newError(CodeKeyNotRefreshed, "key %q needs a refresh before it can sign", req.KeyID))
		}
		signed = gethcrypto.Keccak256(msg)
	}
	if !s.ceremony.claim(req.KeyID, imp.session) {
		return refuse(newError(CodeVerificationUsed, "key %q already received its verification signature", req.KeyID))
	}
	seq, e := s.audit(verificationAuditKind, req.KeyID, verificationSubject, "allowed", "verification granted in window "+imp.window+" for import "+imp.session, req.SessionID)
	if e != nil {
		return SignResponse{}, e
	}
	resp := SignResponse{NodeID: s.opts.NodeID, KeyID: req.KeyID, Kind: KindOperatorVerification, Message: hex.EncodeToString(msg), SignedBytes: hex.EncodeToString(signed), AuditSequence: seq}
	switch imp.curve {
	case dealer.Secp256k1:
		var sig tssecdsa.EthereumSignature
		e = s.runBoundSession(r.Context(), req.SessionID, "sign", protocolSignSecp, epochBinding(rec.Epoch), signers, func(ctx context.Context, ps *peerSession) error {
			var err error
			sig, err = tssecdsa.Sign(ctx, share, ecdsaNet{ps}, signers, signed)
			return err
		})
		if e != nil {
			return SignResponse{}, e
		}
		v := sig.V
		resp.Signature, resp.RecoveryID = hex.EncodeToString(sig.Bytes()), &v
	case dealer.Ed25519:
		k, err := eddsa.NewKeyShare(b.ParticipantID, b.Threshold, b.PublicKey, b.Share, b.Bks, b.PartialPublicKeys)
		if err != nil {
			return SignResponse{}, newError(CodeKeyInvalidShare, "%v", err)
		}
		var sig [64]byte
		e = s.runBoundSession(r.Context(), req.SessionID, "sign", protocolSignEd, epochBinding(rec.Epoch), signers, func(ctx context.Context, ps *peerSession) error {
			var err error
			sig, err = eddsa.Sign(ctx, k, eddsaNet{ps}, signers, signed)
			return err
		})
		if e != nil {
			return SignResponse{}, e
		}
		resp.Signature = hex.EncodeToString(sig[:])
	default:
		return SignResponse{}, newError(CodeKeyCurve, "key %q has no signing curve", req.KeyID)
	}
	return resp, nil
}

func (s *Server) verificationSigners(requested, participants []string) ([]string, *Error) {
	signers, ok := sortedUnique(requested)
	if !ok {
		return nil, newError(CodeSessionBadRequest, "signers must be distinct non-empty ids")
	}
	if len(signers) < int(dealer.Threshold) {
		return nil, newError(CodeQuorumTooFew, "at least %d signers are required", dealer.Threshold)
	}
	members := make(map[string]bool, len(participants))
	for _, p := range participants {
		members[p] = true
	}
	self := false
	for _, id := range signers {
		if !members[id] {
			return nil, newError(CodeQuorumNotMember, "signer %q does not hold a share of the key", id)
		}
		self = self || id == s.opts.NodeID
	}
	if !self {
		return nil, newError(CodeQuorumSelfMissing, "this node is not among the signers")
	}
	return signers, nil
}

func requestCarriesVerification(req SignRequest) bool {
	if strings.Contains(req.TypedData, VerificationDomain) {
		return true
	}
	domain := []byte(VerificationDomain)
	for _, field := range []string{req.Transaction, req.Message, req.Activity} {
		if raw, err := hex.DecodeString(strings.TrimPrefix(field, "0x")); err == nil && bytes.Contains(raw, domain) {
			return true
		}
	}
	return false
}

func (s *Server) signsVerification(keyID string, pubBytes, signed []byte, view any) bool {
	if bytes.Contains(signed, []byte(VerificationDomain)) || policy.CarriesVerification(view) {
		return true
	}
	imp, ok := s.ceremony.lookup(keyID)
	if !ok {
		return false
	}
	msg := policy.VerificationMessage(keyID, pubBytes, imp.session)
	return bytes.Equal(signed, msg) || bytes.Equal(signed, gethcrypto.Keccak256(msg))
}
