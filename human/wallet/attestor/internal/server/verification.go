package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	gethcrypto "github.com/ethereum/go-ethereum/crypto"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/policy"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/tss/dealer"
	tssecdsa "github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/tss/ecdsa"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/tss/eddsa"
)

const (
	KindOperatorVerification = policy.KindOperatorVerification
	VerificationDomain       = policy.VerificationDomain

	verificationAuditKind = "ceremony.verification"
	verificationSubject   = "operator"
)

type VerificationRequest struct {
	RecoveryEvidence  *SignResponse `json:"recovery_evidence,omitempty"`
	CeremonyID        string        `json:"ceremony_id"`
	ExpectedEpoch     *uint64       `json:"expected_epoch"`
	RecoverySessionID string        `json:"recovery_session_id,omitempty"`
	SessionID         string        `json:"session_id"`
	KeyID             string        `json:"key_id"`
	Kind              string        `json:"kind"`
	Signers           []string      `json:"signers"`
	ImportSessionID   string        `json:"import_session_id"`
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

type verificationState struct {
	CeremonyID      string        `json:"ceremony_id"`
	ImportSessionID string        `json:"import_session_id"`
	SessionID       string        `json:"session_id"`
	Epoch           uint64        `json:"epoch"`
	Signers         []string      `json:"signers"`
	State           string        `json:"state"`
	ErrorCode       string        `json:"error_code,omitempty"`
	Response        *SignResponse `json:"response,omitempty"`
}

func (s *Server) loadVerification(keyID string) (verificationState, error) {
	var state verificationState
	err := s.opts.Store.WithRecord(store.RecordVerification, keyID, func(raw []byte) error { return json.Unmarshal(raw, &state) })
	return state, err
}
func (s *Server) saveVerification(keyID string, state verificationState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.opts.Store.UpdateRecord(store.RecordVerification, keyID, func([]byte) ([]byte, error) { return append([]byte(nil), raw...), nil })
}
func (s *Server) finishVerification(keyID string, state verificationState) (SignResponse, *Error) {
	if state.Response == nil || state.Response.Signature == "" {
		return SignResponse{}, newError(CodeStoreFailed, "durable verification evidence is missing")
	}
	if state.State == "complete" {
		if state.Response.AuditSequence == 0 {
			return SignResponse{}, newError(CodeStoreFailed, "completed verification has no audit evidence")
		}
		if err := s.opts.Audit.Verify(); err != nil {
			return SignResponse{}, newError(CodeStoreAuditFailed, "%v", err)
		}
		head, _ := s.opts.Audit.Head()
		if head < state.Response.AuditSequence {
			return SignResponse{}, newError(CodeStoreAuditFailed, "verification audit sequence is absent")
		}
		return *state.Response, nil
	}
	if state.State != "signed_pending_audit" {
		return SignResponse{}, newError(CodeStoreFailed, "verification has no retained signature")
	}
	seq, e := s.audit(verificationAuditKind, keyID, verificationSubject, "allowed", "verification granted in window "+state.CeremonyID+" for import "+state.ImportSessionID, state.SessionID)
	if e != nil {
		return SignResponse{}, e
	}
	state.Response.AuditSequence = seq
	state.State = "complete"
	state.ErrorCode = ""
	if err := s.saveVerification(keyID, state); err != nil {
		return SignResponse{}, newError(CodeStoreFailed, "%v", err)
	}
	return *state.Response, nil
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
		return refuse(newError(CodeSessionKind, "unsupported verification kind"))
	}
	if r.Header.Get("Authorization") != "" || r.Header.Get(HeaderAgentKey) != "" {
		return refuse(newError(CodeSessionBadRequest, "operator verification carries no bearer token or agent signature"))
	}
	if !s.opts.Ceremony {
		return refuse(newError(CodeVerificationWindow, "operator verification requires the explicit ceremony window"))
	}
	if e := requireIDs(req.ImportSessionID, req.KeyID); e != nil {
		return refuse(e)
	}
	if e := requireIDs(req.CeremonyID, req.KeyID); e != nil {
		return refuse(e)
	}
	if req.ExpectedEpoch == nil {
		return refuse(newError(CodeSessionBadRequest, "expected_epoch is required"))
	}
	unlock := s.lockKey(req.KeyID)
	defer unlock()
	receipt, err := s.opts.Store.ImportReceipt(req.KeyID)
	if err != nil || receipt.Identity.CeremonyID != req.CeremonyID || receipt.Identity.SessionID != req.ImportSessionID || receipt.AuditSequence == 0 {
		return refuse(newError(CodeVerificationNotImported, "durable audited import does not match ceremony identity"))
	}
	rec, payload, e := s.loadShare(req.KeyID)
	if e != nil {
		return refuse(e)
	}
	if rec.Epoch != *req.ExpectedEpoch || rec.Curve != receipt.Identity.Curve || !bytes.Equal(rec.PublicKey, receipt.Identity.PublicKey) || !sameIDs(rec.Participants, receipt.Identity.Participants) {
		return refuse(newError(CodeKeyInvalidShare, "held key differs from durable ceremony identity or expected epoch"))
	}
	refreshed, refreshErr := s.opts.Store.CeremonyRefresh(req.KeyID)
	if refreshErr != nil || refreshed.ExistingKey || refreshed.CeremonyID != req.CeremonyID || refreshed.ImportSessionID != req.ImportSessionID || refreshed.Decision != PhaseCommit || refreshed.State != "complete" || refreshed.BaseEpoch+1 != rec.Epoch || rec.Epoch <= receipt.Identity.Epoch {
		return refuse(newError(CodeKeyNotRefreshed, "verification requires the durably completed original-key refresh"))
	}
	if e := s.requireAuditSequence(refreshed.AuditSequence); e != nil {
		return refuse(e)
	}
	if err := s.opts.Inventory.Check(req.KeyID, "sign", payload.Owner, rec.Curve, &rec); err != nil {
		return refuse(newError(CodeKeyInvalidShare, "approved inventory does not admit verification of held epoch"))
	}
	signers, e := s.verificationSigners(req.Signers, rec.Participants)
	if e != nil {
		return refuse(e)
	}
	msg := policy.VerificationMessage(req.KeyID, rec.PublicKey, req.ImportSessionID)
	decision := s.opts.Policy.Evaluate(payload.Account, policy.Request{Kind: KindOperatorVerification, View: &policy.Verification{KeyID: req.KeyID, PublicKey: append([]byte(nil), rec.PublicKey...), ImportSession: req.ImportSessionID, Message: msg}}, s.opts.Ledger)
	if !decision.Allowed {
		return refuse(policyError(decision.Code, decision.Reason))
	}
	state, err := s.loadVerification(req.KeyID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return refuse(newError(CodeStoreFailed, "%v", err))
	}
	if err == nil {
		if state.CeremonyID != req.CeremonyID || state.ImportSessionID != req.ImportSessionID || state.Epoch != rec.Epoch {
			return refuse(newError(CodeVerificationUsed, "verification grant belongs to another ceremony or epoch"))
		}
		if state.State == "complete" || state.State == "signed_pending_audit" {
			if state.SessionID != req.SessionID || !sameIDs(state.Signers, signers) {
				return refuse(newError(CodeVerificationUsed, "verification grant already has retained evidence for another session"))
			}
			return s.finishVerification(req.KeyID, state)
		}
		if req.RecoveryEvidence != nil {
			if (state.State != "failed" && state.State != "signing") || req.SessionID != state.SessionID || req.RecoverySessionID != "" || !sameIDs(state.Signers, signers) {
				return refuse(newError(CodeVerificationUsed, "retained evidence recovery requires the exact interrupted session and signers"))
			}
			response, e := s.admitVerificationEvidence(req.KeyID, rec, req.ImportSessionID, signers, *req.RecoveryEvidence)
			if e != nil {
				return refuse(e)
			}
			state.Response = &response
			state.State = "signed_pending_audit"
			state.ErrorCode = ""
			if err := s.saveVerification(req.KeyID, state); err != nil {
				return SignResponse{}, newError(CodeStoreFailed, "%v", err)
			}
			return s.finishVerification(req.KeyID, state)
		}
		if (state.State != "failed" && state.State != "signing") || req.RecoverySessionID != state.SessionID || req.SessionID == state.SessionID {
			return refuse(newError(CodeVerificationUsed, "recovery requires a new session naming the previous failed or interrupted session"))
		}
	} else if req.RecoverySessionID != "" || req.RecoveryEvidence != nil {
		return refuse(newError(CodeVerificationNotImported, "there is no previous verification attempt to recover"))
	}
	b, share, e := payload.bundle()
	if e != nil {
		return refuse(e)
	}
	defer dealer.Wipe(b.Share)
	signed := msg
	if b.Curve == dealer.Secp256k1 {
		if share == nil {
			return refuse(newError(CodeKeyNotRefreshed, "key requires refresh before verification"))
		}
		signed = gethcrypto.Keccak256(msg)
	}
	state = verificationState{CeremonyID: req.CeremonyID, ImportSessionID: req.ImportSessionID, SessionID: req.SessionID, Epoch: rec.Epoch, Signers: signers, State: "signing"}
	if err := s.saveVerification(req.KeyID, state); err != nil {
		return SignResponse{}, newError(CodeStoreFailed, "%v", err)
	}
	failed := func(e *Error) (SignResponse, *Error) {
		state.State = "failed"
		state.ErrorCode = e.Code
		if err := s.saveVerification(req.KeyID, state); err != nil {
			return SignResponse{}, newError(CodeStoreFailed, "failed verification needs durable recovery: %v", err)
		}
		return refuse(e)
	}
	resp := SignResponse{NodeID: s.opts.NodeID, KeyID: req.KeyID, Kind: KindOperatorVerification, Message: hex.EncodeToString(msg), SignedBytes: hex.EncodeToString(signed)}
	switch b.Curve {
	case dealer.Secp256k1:
		var sig tssecdsa.EthereumSignature
		e = s.runBoundSession(r.Context(), req.SessionID, "sign", protocolSignSecp, epochBinding(rec.Epoch), signers, func(ctx context.Context, ps *peerSession) error {
			var err error
			sig, err = tssecdsa.Sign(ctx, share, ecdsaNet{ps}, signers, signed)
			return err
		})
		signature := sig.Bytes()
		pub, err := gethcrypto.SigToPub(signed, signature)
		if err != nil || !bytes.Equal(gethcrypto.FromECDSAPub(pub), rec.PublicKey) {
			if e != nil {
				return failed(e)
			}
			return failed(newError(CodeKeyInvalidShare, "verification signature differs from original public identity"))
		}
		v := sig.V
		resp.Signature = hex.EncodeToString(signature)
		resp.RecoveryID = &v
	case dealer.Ed25519:
		k, err := eddsa.NewKeyShare(b.ParticipantID, b.Threshold, b.PublicKey, b.Share, b.Bks, b.PartialPublicKeys)
		if err != nil {
			return failed(newError(CodeKeyInvalidShare, "%v", err))
		}
		var signature [64]byte
		e = s.runBoundSession(r.Context(), req.SessionID, "sign", protocolSignEd, epochBinding(rec.Epoch), signers, func(ctx context.Context, ps *peerSession) error {
			var err error
			signature, err = eddsa.Sign(ctx, k, eddsaNet{ps}, signers, signed)
			return err
		})
		if !ed25519.Verify(ed25519.PublicKey(rec.PublicKey), signed, signature[:]) {
			if e != nil {
				return failed(e)
			}
			return failed(newError(CodeKeyInvalidShare, "verification signature differs from original public identity"))
		}
		resp.Signature = hex.EncodeToString(signature[:])
	default:
		return failed(newError(CodeKeyCurve, "unsupported verification curve"))
	}
	state.Response = &resp
	state.State = "signed_pending_audit"
	if err := s.saveVerification(req.KeyID, state); err != nil {
		return SignResponse{}, newError(CodeStoreFailed, "verification evidence retention failed: %v", err)
	}
	return s.finishVerification(req.KeyID, state)
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
	receipt, err := s.opts.Store.ImportReceipt(keyID)
	if err != nil {
		return false
	}
	msg := policy.VerificationMessage(keyID, pubBytes, receipt.Identity.SessionID)
	return bytes.Equal(signed, msg) || bytes.Equal(signed, gethcrypto.Keccak256(msg))
}

func (s *Server) admitVerificationEvidence(keyID string, rec store.ShareRecord, importSession string, signers []string, evidence SignResponse) (SignResponse, *Error) {
	message := policy.VerificationMessage(keyID, rec.PublicKey, importSession)
	signed := message
	if rec.Curve == store.CurveSecp256k1 {
		signed = gethcrypto.Keccak256(message)
	}
	if evidence.KeyID != keyID || evidence.Kind != KindOperatorVerification || !contains(signers, evidence.NodeID) || evidence.AuditSequence == 0 || evidence.Message != hex.EncodeToString(message) || evidence.SignedBytes != hex.EncodeToString(signed) {
		return SignResponse{}, newError(CodeVerificationUsed, "retained evidence does not match the original verification grant")
	}
	signature, err := hex.DecodeString(evidence.Signature)
	if err != nil {
		return SignResponse{}, newError(CodeKeyInvalidShare, "invalid verification evidence encoding")
	}
	switch rec.Curve {
	case store.CurveSecp256k1:
		if len(signature) != 65 || evidence.RecoveryID == nil || *evidence.RecoveryID != signature[64] {
			return SignResponse{}, newError(CodeKeyInvalidShare, "invalid verification recovery byte")
		}
		public, err := gethcrypto.SigToPub(signed, signature)
		if err != nil || !bytes.Equal(gethcrypto.FromECDSAPub(public), rec.PublicKey) {
			return SignResponse{}, newError(CodeKeyInvalidShare, "retained evidence is not signed by the original key")
		}
	case store.CurveEd25519:
		if evidence.RecoveryID != nil || !ed25519.Verify(ed25519.PublicKey(rec.PublicKey), signed, signature) {
			return SignResponse{}, newError(CodeKeyInvalidShare, "retained evidence is not signed by the original key")
		}
	default:
		return SignResponse{}, newError(CodeKeyCurve, "invalid verification curve")
	}
	evidence.NodeID = s.opts.NodeID
	evidence.AuditSequence = 0
	return evidence, nil
}
