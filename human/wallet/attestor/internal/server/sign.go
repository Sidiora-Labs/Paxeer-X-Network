package server

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/auth/agent"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/auth/jwt"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/lxwire"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy/evm"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy/lx"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/dealer"
	tssecdsa "github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/ecdsa"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/eddsa"
)

const (
	KindEVMTransaction  = "evm_tx"
	KindTypedData       = "eip712"
	KindPersonalMessage = "personal_message"
	KindEthSignDigest   = "eth_sign_digest"
	KindLXActivity      = "lx_activity"
	KindLXBind          = "lx_bind"
	KindLXGrant         = "lx_grant"

	HeaderAgentKey       = "X-Agent-Key"
	HeaderAgentNonce     = "X-Agent-Nonce"
	HeaderAgentExpiry    = "X-Agent-Expiry"
	HeaderAgentSignature = "X-Agent-Signature"
)

var kindCurves = map[string]dealer.Curve{
	KindEVMTransaction:  dealer.Secp256k1,
	KindTypedData:       dealer.Secp256k1,
	KindPersonalMessage: dealer.Secp256k1,
	KindEthSignDigest:   dealer.Secp256k1,
	KindLXActivity:      dealer.Ed25519,
	KindLXBind:          dealer.Ed25519,
	KindLXGrant:         dealer.Ed25519,
}

func SignKinds() []string {
	return []string{KindEVMTransaction, KindTypedData, KindPersonalMessage, KindEthSignDigest, KindLXActivity, KindLXBind, KindLXGrant}
}

type GrantJSON struct {
	From               string `json:"from"`
	Recipient          string `json:"recipient"`
	Asset              string `json:"asset"`
	PerDrawMaximum     string `json:"per_draw_maximum"`
	Allowance          string `json:"allowance"`
	Recurring          bool   `json:"recurring"`
	WindowLength       uint64 `json:"window_length"`
	Expiration         uint64 `json:"expiration"`
	PurposeHash        string `json:"purpose_hash"`
	HasReference       bool   `json:"has_reference"`
	ReferenceHash      string `json:"reference_hash"`
	RevocationSequence uint64 `json:"revocation_sequence"`
}

type SignRequest struct {
	SessionID    string            `json:"session_id"`
	KeyID        string            `json:"key_id"`
	Kind         string            `json:"kind"`
	Signers      []string          `json:"signers"`
	Transaction  string            `json:"transaction,omitempty"`
	TypedData    string            `json:"typed_data,omitempty"`
	Message      string            `json:"message,omitempty"`
	Digest       string            `json:"digest,omitempty"`
	Activity     string            `json:"activity,omitempty"`
	Grant        *GrantJSON        `json:"grant,omitempty"`
	Construction *ConstructionJSON `json:"construction,omitempty"`
}

type BatchCallJSON struct {
	To    string `json:"to"`
	Value string `json:"value"`
	Data  string `json:"data"`
}

type QuoteJSON struct {
	Sponsor        string `json:"sponsor"`
	Token          string `json:"token"`
	MaxTokenAmount string `json:"maxTokenAmount"`
	TokenAmount    string `json:"tokenAmount"`
	Deadline       string `json:"deadline"`
	QuoteNonce     string `json:"quoteNonce"`
	GasCost        string `json:"gasCost"`
}

type ConstructionJSON struct {
	Kind    string          `json:"kind"`
	ChainID string          `json:"chainId"`
	Account string          `json:"account,omitempty"`
	Address string          `json:"address,omitempty"`
	Nonce   string          `json:"nonce"`
	Calls   []BatchCallJSON `json:"calls,omitempty"`
	Quote   *QuoteJSON      `json:"quote,omitempty"`
}

type SignResponse struct {
	NodeID        string `json:"node_id"`
	KeyID         string `json:"key_id"`
	Kind          string `json:"kind"`
	SignedBytes   string `json:"signed_bytes"`
	Signature     string `json:"signature"`
	RecoveryID    *uint8 `json:"recovery_id,omitempty"`
	AuditSequence uint64 `json:"audit_sequence"`
}

func decodeHex(field, s string) ([]byte, *Error) {
	raw, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil || len(raw) == 0 {
		return nil, newError(CodeSessionBadRequest, "%s must be non-empty hex", field)
	}
	return raw, nil
}

func hex32(field, s string) ([32]byte, *Error) {
	var out [32]byte
	raw, e := decodeHex(field, s)
	if e != nil {
		return out, e
	}
	if len(raw) != 32 {
		return out, newError(CodeSessionBadRequest, "%s must be 32 bytes", field)
	}
	copy(out[:], raw)
	return out, nil
}

func parseUint128(field, s string) (lxwire.Uint128, *Error) {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok || v.Sign() < 0 || v.BitLen() > 128 {
		return lxwire.Uint128{}, newError(CodeSessionBadRequest, "%s must be a decimal 128-bit amount", field)
	}
	lo := new(big.Int).And(v, new(big.Int).SetUint64(^uint64(0))).Uint64()
	hi := new(big.Int).Rsh(v, 64).Uint64()
	return lxwire.Uint128{Hi: hi, Lo: lo}, nil
}

func (g *GrantJSON) decode(pub [32]byte) (lxwire.Grant, *Error) {
	var out lxwire.Grant
	var e *Error
	if out.From, e = hex32("grant.from", g.From); e != nil {
		return out, e
	}
	if out.Recipient, e = hex32("grant.recipient", g.Recipient); e != nil {
		return out, e
	}
	if out.Asset, e = hex32("grant.asset", g.Asset); e != nil {
		return out, e
	}
	if out.PerDrawMaximum, e = parseUint128("grant.per_draw_maximum", g.PerDrawMaximum); e != nil {
		return out, e
	}
	if out.Allowance, e = parseUint128("grant.allowance", g.Allowance); e != nil {
		return out, e
	}
	if out.PurposeHash, e = hex32("grant.purpose_hash", g.PurposeHash); e != nil {
		return out, e
	}
	if g.HasReference {
		if out.ReferenceHash, e = hex32("grant.reference_hash", g.ReferenceHash); e != nil {
			return out, e
		}
	}
	out.Recurring, out.WindowLength, out.Expiration = g.Recurring, g.WindowLength, g.Expiration
	out.HasReference, out.RevocationSequence, out.PublicKey = g.HasReference, g.RevocationSequence, pub
	return out, nil
}

func parseUint256(field, s string) (*big.Int, *Error) {
	base := 10
	digits := s
	if rest, ok := strings.CutPrefix(s, "0x"); ok {
		base, digits = 16, rest
	}
	v, ok := new(big.Int).SetString(digits, base)
	if digits == "" || !ok || v.Sign() < 0 || v.BitLen() > 256 {
		return nil, newError(CodeSessionBadRequest, "%s must be an unsigned 256-bit integer", field)
	}
	return v, nil
}

func parseAddress(field, s string) (common.Address, *Error) {
	if !common.IsHexAddress(s) || !strings.HasPrefix(s, "0x") {
		return common.Address{}, newError(CodeSessionBadRequest, "%s must be a 0x-prefixed address", field)
	}
	return common.HexToAddress(s), nil
}

func (c *ConstructionJSON) claim(digest common.Hash) (any, string, *Error) {
	chainID, e := parseUint256("construction.chainId", c.ChainID)
	if e != nil {
		return nil, "", e
	}
	switch c.Kind {
	case policy.KindAuthorization:
		if c.Account != "" || len(c.Calls) > 0 || c.Quote != nil {
			return nil, "", newError(CodeSessionBadRequest, "an authorization construction carries only chainId, address and nonce")
		}
		address, e := parseAddress("construction.address", c.Address)
		if e != nil {
			return nil, "", e
		}
		nonce, e := parseUint256("construction.nonce", c.Nonce)
		if e != nil {
			return nil, "", e
		}
		if !nonce.IsUint64() {
			return nil, "", newError(CodeSessionBadRequest, "construction.nonce must fit in 64 bits")
		}
		return &evm.AuthorizationClaim{ChainID: chainID, Address: address, Nonce: nonce.Uint64(), ClaimedDigest: digest}, policy.KindAuthorization, nil
	case policy.KindSponsoredBatch:
		if c.Address != "" || c.Quote == nil {
			return nil, "", newError(CodeSessionBadRequest, "a sponsored batch construction carries account, nonce, calls and quote")
		}
		batch := evm.SponsoredBatch{ChainID: chainID}
		var e *Error
		if batch.Account, e = parseAddress("construction.account", c.Account); e != nil {
			return nil, "", e
		}
		if batch.Nonce, e = parseUint256("construction.nonce", c.Nonce); e != nil {
			return nil, "", e
		}
		for i, call := range c.Calls {
			var out evm.BatchCall
			if out.To, e = parseAddress("construction.calls.to", call.To); e != nil {
				return nil, "", e
			}
			if out.Value, e = parseUint256("construction.calls.value", call.Value); e != nil {
				return nil, "", e
			}
			data, err := hex.DecodeString(strings.TrimPrefix(call.Data, "0x"))
			if err != nil || !strings.HasPrefix(call.Data, "0x") {
				return nil, "", newError(CodeSessionBadRequest, "construction.calls[%d].data must be 0x-prefixed hex", i)
			}
			out.Data = data
			batch.Calls = append(batch.Calls, out)
		}
		q := c.Quote
		if batch.Quote.Sponsor, e = parseAddress("construction.quote.sponsor", q.Sponsor); e != nil {
			return nil, "", e
		}
		if batch.Quote.Token, e = parseAddress("construction.quote.token", q.Token); e != nil {
			return nil, "", e
		}
		for _, f := range []struct {
			name string
			raw  string
			dst  **big.Int
		}{
			{"construction.quote.maxTokenAmount", q.MaxTokenAmount, &batch.Quote.MaxTokenAmount},
			{"construction.quote.tokenAmount", q.TokenAmount, &batch.Quote.TokenAmount},
			{"construction.quote.deadline", q.Deadline, &batch.Quote.Deadline},
			{"construction.quote.quoteNonce", q.QuoteNonce, &batch.Quote.QuoteNonce},
			{"construction.quote.gasCost", q.GasCost, &batch.Quote.GasCost},
		} {
			if *f.dst, e = parseUint256(f.name, f.raw); e != nil {
				return nil, "", e
			}
		}
		return &evm.SponsoredBatchClaim{Batch: batch, ClaimedDigest: digest}, policy.KindSponsoredBatch, nil
	}
	return nil, "", newError(CodeSessionBadRequest, "construction kind %q is neither %s nor %s", c.Kind, policy.KindSponsoredBatch, policy.KindAuthorization)
}

func (s *Server) HandleSign(w http.ResponseWriter, r *http.Request) {
	body, e := readBody(r)
	var resp SignResponse
	if e == nil {
		resp, e = s.doSign(r, body)
	}
	s.finish(w, "sign", body, resp, e)
}

func (s *Server) authenticate(r *http.Request, keyID string, body []byte, owner string) (string, *Error) {
	if auth := r.Header.Get("Authorization"); auth != "" {
		token, ok := strings.CutPrefix(auth, "Bearer ")
		if !ok || token == "" {
			return "", newError(CodeTokenInvalid, "authorization must be a bearer token")
		}
		if s.opts.Tokens == nil {
			return "", newError(CodeTokenUnavailable, "no token verifier is configured")
		}
		subject, err := s.opts.Tokens.Verify(r.Context(), token, keyID, func(subject, id string) (bool, error) {
			return id == keyID && subject == owner, nil
		})
		if errors.Is(err, jwt.ErrNotOwner) {
			return "", newError(CodeTokenNotOwner, "token subject does not own the key")
		}
		if err != nil {
			return "", newError(CodeTokenInvalid, "%v", err)
		}
		return subject, nil
	}
	if r.Header.Get(HeaderAgentKey) != "" {
		if s.opts.Agents == nil {
			return "", newError(CodeTokenUnavailable, "no agent verifier is configured")
		}
		req := agent.Request{Method: PathSign, KeyID: keyID, Body: body}
		pub, e := hex32(HeaderAgentKey, r.Header.Get(HeaderAgentKey))
		if e != nil {
			return "", newError(CodeAgentInvalid, "%s", e.Message)
		}
		nonce, err := hex.DecodeString(r.Header.Get(HeaderAgentNonce))
		sig, err2 := hex.DecodeString(r.Header.Get(HeaderAgentSignature))
		expiry, err3 := strconv.ParseUint(r.Header.Get(HeaderAgentExpiry), 10, 64)
		if err != nil || err2 != nil || err3 != nil || len(nonce) != 16 || len(sig) != 64 {
			return "", newError(CodeAgentInvalid, "agent headers are malformed")
		}
		req.PublicKey = pub
		copy(req.Nonce[:], nonce)
		copy(req.Signature[:], sig)
		req.Expiry = expiry
		if _, err := s.opts.Agents.Verify(r.Context(), req); err != nil {
			return "", newError(CodeAgentInvalid, "%v", err)
		}
		return "agent:" + hex.EncodeToString(pub[:]), nil
	}
	return "", newError(CodeTokenMissing, "a bearer token or agent signature is required")
}

func (s *Server) doSign(r *http.Request, body []byte) (SignResponse, *Error) {
	var req SignRequest
	if e := decodeRequest(body, &req); e != nil {
		return SignResponse{}, e
	}
	if e := requireIDs(req.SessionID, req.KeyID); e != nil {
		return SignResponse{}, e
	}
	curve, ok := kindCurves[req.Kind]
	if !ok {
		return SignResponse{}, newError(CodeSessionKind, "kind %q is not supported", req.Kind)
	}
	rec, payload, e := s.loadShare(req.KeyID)
	if e != nil {
		return SignResponse{}, e
	}
	subject, e := s.authenticate(r, req.KeyID, body, payload.Owner)
	if e != nil {
		return SignResponse{}, s.deny("sign."+req.Kind, req.KeyID, "", "denied", req.SessionID, e)
	}
	refuse := func(e *Error) (SignResponse, *Error) {
		return SignResponse{}, s.deny("sign."+req.Kind, req.KeyID, subject, "denied", req.SessionID, e)
	}
	if c, _ := parseCurve(rec.Curve); c != curve {
		return refuse(newError(CodeKeyCurve, "kind %q needs a %s key", req.Kind, curveName(curve)))
	}
	signers, ok := sortedUnique(req.Signers)
	if !ok {
		return refuse(newError(CodeSessionBadRequest, "signers must be distinct non-empty ids"))
	}
	if len(signers) < int(dealer.Threshold) {
		return refuse(newError(CodeQuorumTooFew, "at least %d signers are required", dealer.Threshold))
	}
	members := make(map[string]bool, len(rec.Participants))
	for _, p := range rec.Participants {
		members[p] = true
	}
	self := false
	for _, id := range signers {
		if !members[id] {
			return refuse(newError(CodeQuorumNotMember, "signer %q does not hold a share of the key", id))
		}
		self = self || id == s.opts.NodeID
	}
	if !self {
		return refuse(newError(CodeQuorumSelfMissing, "this node is not among the signers"))
	}

	signed, view, policyKind, e := s.prepare(req, rec.PublicKey)
	if e != nil {
		return refuse(e)
	}
	unlockAccount := s.lockKey("ledger\x00" + policy.AccountKey(payload.Account))
	decision := s.evaluate(payload.Account, policyKind, view, s.spends.ForRequest(requestID(req.KeyID, req.SessionID)))
	unlockAccount()
	if !decision.Allowed {
		return refuse(policyError(decision.Code, decision.Reason))
	}
	if acked, silent := s.Announce(r.Context(), req.KeyID, req.SessionID, payload.Account, rec.Participants, decision.Spends); acked < int(dealer.Threshold) {
		if _, ae := s.audit("sign."+req.Kind, req.KeyID, subject, "denied", "announcement not acknowledged by a quorum", req.SessionID); ae != nil {
			return SignResponse{}, ae
		}
		e := newError(CodeQuorumTooFew, "%d of %d participants recorded the request; %d are required; silent: %s", acked, len(rec.Participants), dealer.Threshold, strings.Join(silent, ","))
		e.audited = true
		return SignResponse{}, e
	}
	seq, e := s.audit("sign."+req.Kind, req.KeyID, subject, "allowed", decision.Code, req.SessionID)
	if e != nil {
		return SignResponse{}, e
	}
	resp := SignResponse{NodeID: s.opts.NodeID, KeyID: req.KeyID, Kind: req.Kind, SignedBytes: hex.EncodeToString(signed), AuditSequence: seq}
	b, share, e := payload.bundle()
	if e != nil {
		return SignResponse{}, e
	}
	defer dealer.Wipe(b.Share)
	switch curve {
	case dealer.Secp256k1:
		if share == nil {
			return SignResponse{}, newError(CodeKeyNotRefreshed, "key %q needs a refresh before it can sign", req.KeyID)
		}
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
	}
	return resp, nil
}

func (s *Server) evaluate(account, policyKind string, view any, ledger policy.Ledger) policy.Decision {
	switch v := view.(type) {
	case *lx.ActivityRequest:
		return s.opts.Kernel.EvaluateActivity(common.HexToAddress(account), v)
	case *lx.BindRequest:
		return s.opts.Kernel.EvaluateBind(common.HexToAddress(account), v)
	case *lx.GrantRequest:
		return s.opts.Kernel.EvaluateGrant(common.HexToAddress(account), v)
	}
	switch policyKind {
	case policy.KindLXActivity, policy.KindLXBind, policy.KindLXGrant:
		return policy.Decision{Allowed: false, Code: policy.CodeDecodeError, Reason: "kernel request has no kernel view"}
	}
	return s.opts.Policy.Evaluate(account, policy.Request{Kind: policyKind, View: view}, ledger)
}

func activityDisclosure(a *lxwire.Activity) (lx.Disclosure, *Error) {
	effect, err := lx.DecodeEffect(a)
	if err != nil {
		return lx.Disclosure{}, policyError(policy.CodeDecodeError, err.Error())
	}
	module, ok := lx.ModuleName(a.Type.Module())
	if !ok {
		return lx.Disclosure{}, policyError(lx.CodeUnknownModule, "activity names an unknown module")
	}
	return lx.Disclosure{
		Account:      effect.Account,
		Module:       module,
		Operation:    a.Type.Ordinal(),
		Amounts:      effect.Amounts,
		Destinations: effect.Destinations,
		Sequence:     a.AccountSequence,
		NotBefore:    a.NotBefore,
		NotAfter:     a.NotAfter,
	}, nil
}

func (s *Server) prepare(req SignRequest, pubBytes []byte) ([]byte, any, string, *Error) {
	switch req.Kind {
	case KindEVMTransaction:
		raw, e := decodeHex("transaction", req.Transaction)
		if e != nil {
			return nil, nil, "", e
		}
		tx, err := evm.DecodeTransaction(raw, new(big.Int).SetUint64(s.opts.ChainID))
		if err != nil {
			return nil, nil, "", policyError(policy.CodeDecodeError, err.Error())
		}
		return tx.SigningDigest.Bytes(), tx, policy.KindEVMTransaction, nil
	case KindTypedData:
		if req.TypedData == "" {
			return nil, nil, "", newError(CodeSessionBadRequest, "typed_data is required")
		}
		td, err := evm.DecodeTypedData([]byte(req.TypedData))
		if err != nil {
			return nil, nil, "", policyError(policy.CodeDecodeError, err.Error())
		}
		return td.Digest.Bytes(), td, policy.KindTypedData, nil
	case KindPersonalMessage:
		raw, e := decodeHex("message", req.Message)
		if e != nil {
			return nil, nil, "", e
		}
		pm := evm.DecodePersonalMessage(raw)
		return pm.Digest.Bytes(), pm, policy.KindPersonalMessage, nil
	case KindEthSignDigest:
		d, e := hex32("digest", req.Digest)
		if e != nil {
			return nil, nil, "", e
		}
		if req.Construction == nil {
			return nil, nil, "", newError(CodeSessionBadRequest, "eth_sign_digest needs the construction its digest is recomputed from")
		}
		view, policyKind, e := req.Construction.claim(common.Hash(d))
		if e != nil {
			return nil, nil, "", e
		}
		return d[:], view, policyKind, nil
	case KindLXActivity:
		raw, e := decodeHex("activity", req.Activity)
		if e != nil {
			return nil, nil, "", e
		}
		a, err := lxwire.DecodeUnsignedActivity(raw, s.opts.Activities)
		if err != nil {
			return nil, nil, "", policyError(policy.CodeDecodeError, err.Error())
		}
		pre, err := lxwire.SignaturePreimage(a)
		if err != nil {
			return nil, nil, "", policyError(policy.CodeDecodeError, err.Error())
		}
		disclosure, e := activityDisclosure(a)
		if e != nil {
			return nil, nil, "", e
		}
		var pub [32]byte
		copy(pub[:], pubBytes)
		return pre[:], &lx.ActivityRequest{Envelope: raw, Digest: pre, PublicKey: pub, Disclosure: disclosure}, policy.KindLXActivity, nil
	case KindLXBind:
		raw, e := decodeHex("message", req.Message)
		if e != nil {
			return nil, nil, "", e
		}
		if _, err := lxwire.ParseBindMessage(raw); err != nil {
			return nil, nil, "", policyError(policy.CodeDecodeError, err.Error())
		}
		return raw, &lx.BindRequest{Message: raw}, policy.KindLXBind, nil
	case KindLXGrant:
		if req.Grant == nil {
			return nil, nil, "", newError(CodeSessionBadRequest, "grant is required")
		}
		var pub [32]byte
		copy(pub[:], pubBytes)
		g, e := req.Grant.decode(pub)
		if e != nil {
			return nil, nil, "", e
		}
		pre, err := lxwire.GrantPreimage(g)
		if err != nil {
			return nil, nil, "", policyError(policy.CodeDecodeError, err.Error())
		}
		return pre[:], &lx.GrantRequest{PublicKey: pub, Grant: &g, Digest: pre}, policy.KindLXGrant, nil
	}
	return nil, nil, "", newError(CodeSessionKind, "kind %q is not supported", req.Kind)
}
