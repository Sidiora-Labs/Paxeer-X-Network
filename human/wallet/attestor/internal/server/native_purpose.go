package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy"
	nativepolicy "github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy/native"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/store"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/dealer"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/eddsa"
)

const PathNativeSign = "/v2/sign/native"
const NativeProfile = 2
const maxNativeRecordBytes = 1 << 20
const maxNativeRequestBytes = (4 << 20) + 4096
const NativePreparationPurpose = nativepolicy.PreparationPurpose
const NativeLocalGrantConsent = nativepolicy.LocalGrantConsent
const NativeSendPurpose = nativepolicy.SendPurposeKind
const nativeRecordKind store.RecordKind = "native-consent-v2"

type NativeSignRequest struct {
	Profile       uint8    `json:"profile"`
	SessionID     string   `json:"session_id"`
	KeyID         string   `json:"key_id"`
	Operation     string   `json:"operation"`
	Signers       []string `json:"signers"`
	Purpose       string   `json:"purpose,omitempty"`
	Capability    string   `json:"capability,omitempty"`
	NativeSession string   `json:"native_session,omitempty"`
	ExpiryMS      string   `json:"expiry_ms,omitempty"`
}
type NativeSignResponse struct {
	Profile       uint8  `json:"profile"`
	NodeID        string `json:"node_id"`
	KeyID         string `json:"key_id"`
	Operation     string `json:"operation"`
	SignedBytes   string `json:"signed_bytes"`
	Signature     string `json:"signature"`
	AuditSequence uint64 `json:"audit_sequence"`
}
type nativeConsentState struct {
	RequestDigest string              `json:"request_digest"`
	Epoch         uint64              `json:"epoch"`
	Response      *NativeSignResponse `json:"response,omitempty"`
}

func decodeNativeRequest(body []byte) (NativeSignRequest, *Error) {
	var req NativeSignRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return req, newError(CodeSessionBadRequest, "native request must be an object")
	}
	seen := map[string]bool{}
	for dec.More() {
		token, err = dec.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return req, newError(CodeSessionBadRequest, "duplicate native request field")
		}
		seen[key] = true
		var value json.RawMessage
		if dec.Decode(&value) != nil {
			return req, newError(CodeSessionBadRequest, "malformed native request")
		}
	}
	if _, err = dec.Token(); err != nil {
		return req, newError(CodeSessionBadRequest, "malformed native request")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return req, newError(CodeSessionBadRequest, "trailing native request data")
	}
	dec = json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&req); err != nil {
		return req, newError(CodeSessionBadRequest, "malformed native request")
	}
	if req.Profile != NativeProfile {
		return req, newError(CodeSessionBadRequest, "native request requires profile 2")
	}
	for _, key := range []string{"profile", "session_id", "key_id", "operation", "signers"} {
		if !seen[key] {
			return req, newError(CodeSessionBadRequest, "missing native request field %s", key)
		}
	}
	switch req.Operation {
	case NativePreparationPurpose, NativeSendPurpose:
		if !seen["purpose"] || seen["capability"] || seen["native_session"] || seen["expiry_ms"] {
			return req, newError(CodeSessionBadRequest, "purpose operation carries only a canonical purpose")
		}
	case NativeLocalGrantConsent:
		if seen["purpose"] || !seen["capability"] || !seen["native_session"] || !seen["expiry_ms"] {
			return req, newError(CodeSessionBadRequest, "grant consent requires canonical capability, native session and expiry")
		}
	default:
		return req, newError(CodeSessionKind, "unsupported native operation")
	}
	return req, nil
}
func readNativeBody(r *http.Request) ([]byte, *Error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxNativeRequestBytes+1))
	if err != nil {
		return nil, newError(CodeSessionBadRequest, "native request body unreadable")
	}
	if len(body) > maxNativeRequestBytes {
		return nil, newError(CodeSessionBadRequest, "native request body exceeds closed profile bound")
	}
	return body, nil
}
func (s *Server) HandleNativeSign(w http.ResponseWriter, r *http.Request) {
	body, e := readNativeBody(r)
	var resp NativeSignResponse
	if e == nil {
		resp, e = s.doNativeSign(r, body)
	}
	s.finish(w, "sign.native-v2", body, resp, e)
}
func (s *Server) doNativeSign(r *http.Request, body []byte) (NativeSignResponse, *Error) {
	var zero NativeSignResponse
	req, e := decodeNativeRequest(body)
	if e != nil {
		return zero, e
	}
	if e = requireIDs(req.SessionID, req.KeyID); e != nil {
		return zero, e
	}
	if r.Header.Get("Authorization") == "" || r.Header.Get(HeaderAgentKey) != "" {
		return zero, newError(CodeTokenMissing, "native consent requires the original owner bearer authorization")
	}
	unlock := s.lockKey("native-v2\x00" + req.KeyID)
	defer unlock()
	rec, payload, e := s.loadShare(req.KeyID)
	if e != nil {
		return zero, e
	}
	if strings.HasPrefix(payload.Owner, "agent:") {
		return zero, newError(CodeTokenNotOwner, "native consent requires the original wallet owner")
	}
	subject, e := s.authenticateAt(r, PathNativeSign, req.KeyID, body, payload.Owner)
	if e != nil {
		return zero, s.deny("sign.native-v2", req.KeyID, "", "denied", req.SessionID, e)
	}
	refuse := func(e *Error) (NativeSignResponse, *Error) {
		return zero, s.deny("sign.native-v2."+req.Operation, req.KeyID, subject, "denied", req.SessionID, e)
	}
	if subject != payload.Owner {
		return refuse(newError(CodeTokenNotOwner, "native consent requires the actual key owner"))
	}
	curve, ok := parseCurve(rec.Curve)
	if !ok || curve != dealer.Ed25519 || len(rec.PublicKey) != 32 {
		return refuse(newError(CodeKeyCurve, "native consent requires the stored Ed25519 owner key"))
	}
	if s.opts.Inventory == nil {
		return refuse(newError(CodeTokenUnavailable, "owner-approved key inventory unavailable"))
	}
	if err := s.opts.Inventory.Check(req.KeyID, "sign", payload.Owner, rec.Curve, &rec); err != nil {
		return refuse(newError(CodeKeyInvalidShare, "owner-approved inventory differs from stored share"))
	}
	if !common.IsHexAddress(payload.Account) {
		return refuse(newError(CodeKeyInvalidShare, "held wallet account is invalid"))
	}
	signers, ok := sortedUnique(req.Signers)
	if !ok {
		return refuse(newError(CodeSessionBadRequest, "native signers must be distinct non-empty ids"))
	}
	if len(signers) < int(dealer.Threshold) {
		return refuse(newError(CodeQuorumTooFew, "native signing requires a threshold quorum"))
	}
	self := false
	for _, id := range signers {
		if !contains(rec.Participants, id) {
			return refuse(newError(CodeQuorumNotMember, "native signer is not a held-share participant"))
		}
		self = self || id == s.opts.NodeID
	}
	if !self {
		return refuse(newError(CodeQuorumSelfMissing, "this node is absent from native quorum"))
	}
	var digest [32]byte
	var coords nativepolicy.Coordinates
	var err error
	switch req.Operation {
	case NativePreparationPurpose:
		raw, decodeErr := nativepolicy.Hex(req.Purpose, 0)
		if decodeErr != nil {
			return refuse(newError(CodeSessionBadRequest, "purpose must be canonical hex"))
		}
		digest, coords, err = nativepolicy.PurposeDigest(raw)
	case NativeSendPurpose:
		raw, decodeErr := nativepolicy.Hex(req.Purpose, 0)
		if decodeErr != nil {
			return refuse(newError(CodeSessionBadRequest, "send purpose must be canonical hex"))
		}
		digest, coords, err = nativepolicy.SendPurposeDigest(raw, rec.PublicKey)
	case NativeLocalGrantConsent:
		capability, decodeErr := nativepolicy.Hex(req.Capability, 0)
		if decodeErr != nil || len(capability) > maxNativeRecordBytes {
			return refuse(newError(CodeSessionBadRequest, "capability must be canonical hex"))
		}
		session, decodeErr := nativepolicy.Hex(req.NativeSession, 0)
		if decodeErr != nil || len(session) > maxNativeRecordBytes {
			return refuse(newError(CodeSessionBadRequest, "native session must be canonical hex"))
		}
		expiry, decodeErr := strconv.ParseUint(req.ExpiryMS, 10, 64)
		if decodeErr != nil || strconv.FormatUint(expiry, 10) != req.ExpiryMS {
			return refuse(newError(CodeSessionBadRequest, "expiry_ms must be canonical unsigned decimal"))
		}
		digest, coords, err = nativepolicy.GrantDigest(capability, session, expiry, rec.PublicKey)
	}
	if err != nil {
		return refuse(newError(CodeSessionBadRequest, "malformed canonical native consent records"))
	}
	if s.opts.NativePolicy == nil || s.spends == nil {
		return refuse(policyError(policy.CodeDestinationDenied, "native-v2 signing policy and durable ledger are required"))
	}
	requestDigest := sha256.Sum256(body)
	requestHex := hex.EncodeToString(requestDigest[:])
	recordID := req.KeyID + "/" + req.SessionID
	var state nativeConsentState
	err = s.opts.Store.WithRecord(nativeRecordKind, recordID, func(raw []byte) error { return json.Unmarshal(raw, &state) })
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return refuse(newError(CodeStoreFailed, "native replay record unavailable"))
	}
	if err == nil && (state.RequestDigest != requestHex || state.Epoch != rec.Epoch) {
		return refuse(newError(CodeSessionBadRequest, "native session already binds different consent or key epoch"))
	}
	ledgerSession := "native-v2-" + hex.EncodeToString(sha256Digest([]byte(req.SessionID)))
	unlockAccount := s.lockKey("ledger\x00" + policy.AccountKey(payload.Account))
	decision := s.opts.NativePolicy.Evaluate(req.KeyID, subject, req.Operation, payload.Account, rec.PublicKey, coords, s.spends.ForRequest(requestID(req.KeyID, ledgerSession)))
	unlockAccount()
	if !decision.Allowed {
		return refuse(policyError(decision.Code, decision.Reason))
	}
	if state.Response != nil {
		resp := *state.Response
		signature, sigErr := hex.DecodeString(resp.Signature)
		if resp.Profile != NativeProfile || resp.KeyID != req.KeyID || resp.NodeID != s.opts.NodeID || resp.Operation != req.Operation || resp.SignedBytes != hex.EncodeToString(digest[:]) || sigErr != nil || !ed25519.Verify(rec.PublicKey, digest[:], signature) {
			return refuse(newError(CodeStoreFailed, "native replay signature binding is invalid"))
		}
		return resp, nil
	}
	err = s.opts.Store.UpdateRecord(nativeRecordKind, recordID, func(prev []byte) ([]byte, error) {
		if prev != nil {
			var prior nativeConsentState
			if json.Unmarshal(prev, &prior) != nil || prior.RequestDigest != requestHex || prior.Epoch != rec.Epoch {
				return nil, nativepolicy.ErrMalformed
			}
			return prev, nil
		}
		return json.Marshal(nativeConsentState{RequestDigest: requestHex, Epoch: rec.Epoch})
	})
	if err != nil {
		return refuse(newError(CodeStoreFailed, "cannot persist native consent binding"))
	}
	if acked, _ := s.Announce(r.Context(), req.KeyID, ledgerSession, payload.Account, rec.Participants, nil); acked < int(dealer.Threshold) {
		return refuse(newError(CodeQuorumTooFew, "native consent was not durably recorded by a quorum"))
	}
	now, err := s.spends.Now()
	if err != nil {
		return refuse(newError(CodeStoreFailed, "native consent ledger clock unavailable"))
	}
	if err = s.spends.Apply(policy.AccountKey(payload.Account), requestID(req.KeyID, ledgerSession), nil, now); err != nil {
		return refuse(newError(CodeStoreFailed, "native consent rate record unavailable"))
	}
	seq, e := s.audit("sign.native-v2."+req.Operation, req.KeyID, subject, "allowed", "explicit owner consent only", req.SessionID)
	if e != nil {
		return zero, e
	}
	bundle, _, e := payload.bundle()
	if e != nil {
		return zero, e
	}
	defer dealer.Wipe(bundle.Share)
	key, err := eddsa.NewKeyShare(bundle.ParticipantID, bundle.Threshold, bundle.PublicKey, bundle.Share, bundle.Bks, bundle.PartialPublicKeys)
	if err != nil {
		return refuse(newError(CodeKeyInvalidShare, "invalid native Ed25519 key share"))
	}
	var signature [64]byte
	tssSession := "native-v2-" + hex.EncodeToString(sha256Digest(append(append([]byte(req.SessionID), 0), digest[:]...)))
	e = s.runBoundSession(r.Context(), tssSession, "sign", protocolSignEd, epochBinding(rec.Epoch), signers, func(ctx context.Context, ps *peerSession) error {
		var err error
		signature, err = eddsa.Sign(ctx, key, eddsaNet{ps}, signers, digest[:])
		return err
	})
	if e != nil {
		return zero, e
	}
	if !ed25519.Verify(rec.PublicKey, digest[:], signature[:]) {
		return refuse(newError(CodeKeyInvalidShare, "native threshold signature failed owner verification"))
	}
	resp := NativeSignResponse{Profile: NativeProfile, NodeID: s.opts.NodeID, KeyID: req.KeyID, Operation: req.Operation, SignedBytes: hex.EncodeToString(digest[:]), Signature: hex.EncodeToString(signature[:]), AuditSequence: seq}
	err = s.opts.Store.UpdateRecord(nativeRecordKind, recordID, func(prev []byte) ([]byte, error) {
		var bound nativeConsentState
		if json.Unmarshal(prev, &bound) != nil || bound.RequestDigest != requestHex || bound.Epoch != rec.Epoch {
			return nil, nativepolicy.ErrMalformed
		}
		bound.Response = &resp
		return json.Marshal(bound)
	})
	if err != nil {
		return refuse(newError(CodeStoreFailed, "cannot persist completed native consent"))
	}
	s.snapshotAfter("sign.native-v2")
	return resp, nil
}
func sha256Digest(raw []byte) []byte { sum := sha256.Sum256(raw); return sum[:] }
