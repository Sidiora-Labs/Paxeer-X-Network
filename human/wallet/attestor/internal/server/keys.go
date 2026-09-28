package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strings"

	"filippo.io/edwards25519"
	"filippo.io/edwards25519/field"
	"github.com/ethereum/go-ethereum/common"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/getamis/alice/crypto/birkhoffinterpolation"
	pt "github.com/getamis/alice/crypto/ecpointgrouplaw"
	"github.com/getamis/alice/crypto/elliptic"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/lxwire"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/store"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/dealer"
	tssecdsa "github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/ecdsa"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/eddsa"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/refresh"
)

type PointJSON struct {
	X string `json:"x"`
	Y string `json:"y"`
}

type BkJSON struct {
	X    string `json:"x"`
	Rank uint32 `json:"rank"`
}

type ShareBundleJSON struct {
	Curve             string               `json:"curve"`
	ParticipantID     string               `json:"participant_id"`
	Share             string               `json:"share"`
	PublicKey         PointJSON            `json:"public_key"`
	PartialPublicKeys map[string]PointJSON `json:"partial_public_keys"`
	Bks               map[string]BkJSON    `json:"bks"`
	Threshold         uint32               `json:"threshold"`
}

type GenerateRequest struct {
	SessionID string `json:"session_id"`
	KeyID     string `json:"key_id"`
	Curve     string `json:"curve"`
	Owner     string `json:"owner"`
	Account   string `json:"account,omitempty"`
}

type ImportRequest struct {
	SessionID string          `json:"session_id"`
	KeyID     string          `json:"key_id"`
	Owner     string          `json:"owner"`
	Account   string          `json:"account,omitempty"`
	Share     ShareBundleJSON `json:"share"`
}

type RefreshRequest struct {
	SessionID string `json:"session_id"`
	KeyID     string `json:"key_id"`
}

type AddShareRequest struct {
	SessionID        string   `json:"session_id"`
	KeyID            string   `json:"key_id"`
	Curve            string   `json:"curve"`
	PublicKey        string   `json:"public_key"`
	Owner            string   `json:"owner"`
	Account          string   `json:"account,omitempty"`
	NewParticipantID string   `json:"new_participant_id"`
	Quorum           []string `json:"quorum"`
}

type KeyResponse struct {
	NodeID        string   `json:"node_id"`
	KeyID         string   `json:"key_id"`
	Curve         string   `json:"curve"`
	PublicKey     string   `json:"public_key"`
	Address       string   `json:"address,omitempty"`
	DID           string   `json:"did,omitempty"`
	Epoch         uint64   `json:"epoch"`
	Participants  []string `json:"participants"`
	Refreshed     bool     `json:"refreshed"`
	AuditSequence uint64   `json:"audit_sequence"`
}

type storedShare struct {
	Owner   string           `json:"owner"`
	Account string           `json:"account"`
	Bundle  *ShareBundleJSON `json:"bundle,omitempty"`
	ECDSA   json.RawMessage  `json:"ecdsa,omitempty"`
}

func hexInt(v *big.Int) string { return hex.EncodeToString(v.Bytes()) }

func parseHexInt(s string) (*big.Int, error) {
	raw, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil || len(raw) == 0 || len(raw) > 64 {
		return nil, errors.New("malformed integer")
	}
	return new(big.Int).SetBytes(raw), nil
}

func curveName(c dealer.Curve) string {
	switch c {
	case dealer.Secp256k1:
		return store.CurveSecp256k1
	case dealer.Ed25519:
		return store.CurveEd25519
	}
	return ""
}

func parseCurve(name string) (dealer.Curve, bool) {
	switch name {
	case store.CurveSecp256k1:
		return dealer.Secp256k1, true
	case store.CurveEd25519:
		return dealer.Ed25519, true
	}
	return 0, false
}

func pointJSON(p *pt.ECPoint) PointJSON {
	return PointJSON{X: hexInt(p.GetX()), Y: hexInt(p.GetY())}
}

func parsePoint(c dealer.Curve, p PointJSON) (*pt.ECPoint, error) {
	curve, err := c.Elliptic()
	if err != nil {
		return nil, err
	}
	x, err := parseHexInt(p.X)
	if err != nil {
		return nil, err
	}
	y, err := parseHexInt(p.Y)
	if err != nil {
		return nil, err
	}
	return pt.NewECPoint(curve, x, y)
}

func EncodeBundle(b dealer.ShareBundle) ShareBundleJSON {
	out := ShareBundleJSON{
		Curve:             curveName(b.Curve),
		ParticipantID:     b.ParticipantID,
		Share:             hexInt(b.Share),
		PublicKey:         pointJSON(b.PublicKey),
		PartialPublicKeys: make(map[string]PointJSON, len(b.PartialPublicKeys)),
		Bks:               make(map[string]BkJSON, len(b.Bks)),
		Threshold:         b.Threshold,
	}
	for id, p := range b.PartialPublicKeys {
		out.PartialPublicKeys[id] = pointJSON(p)
	}
	for id, bk := range b.Bks {
		out.Bks[id] = BkJSON{X: hexInt(bk.GetX()), Rank: bk.GetRank()}
	}
	return out
}

func DecodeBundle(in ShareBundleJSON) (dealer.ShareBundle, error) {
	c, ok := parseCurve(in.Curve)
	if !ok {
		return dealer.ShareBundle{}, dealer.ErrUnknownCurve
	}
	share, err := parseHexInt(in.Share)
	if err != nil {
		return dealer.ShareBundle{}, err
	}
	pub, err := parsePoint(c, in.PublicKey)
	if err != nil {
		return dealer.ShareBundle{}, err
	}
	b := dealer.ShareBundle{
		Curve:             c,
		ParticipantID:     in.ParticipantID,
		Share:             share,
		PublicKey:         pub,
		PartialPublicKeys: make(map[string]*pt.ECPoint, len(in.PartialPublicKeys)),
		Bks:               make(map[string]*birkhoffinterpolation.BkParameter, len(in.Bks)),
		Threshold:         in.Threshold,
	}
	for id, p := range in.PartialPublicKeys {
		if b.PartialPublicKeys[id], err = parsePoint(c, p); err != nil {
			return dealer.ShareBundle{}, err
		}
	}
	for id, bk := range in.Bks {
		x, err := parseHexInt(bk.X)
		if err != nil {
			return dealer.ShareBundle{}, err
		}
		b.Bks[id] = birkhoffinterpolation.NewBkParameter(x, bk.Rank)
	}
	if err := b.Validate(); err != nil {
		return dealer.ShareBundle{}, err
	}
	return b, nil
}

func publicKeyBytes(c dealer.Curve, p *pt.ECPoint) ([]byte, error) {
	if c == dealer.Ed25519 {
		return dealer.EncodeEd25519(p)
	}
	out := make([]byte, 65)
	out[0] = 4
	p.GetX().FillBytes(out[1:33])
	p.GetY().FillBytes(out[33:])
	return out, nil
}

func parsePublicKey(c dealer.Curve, s string) (*pt.ECPoint, error) {
	raw, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil {
		return nil, err
	}
	if c == dealer.Secp256k1 {
		var x, y *big.Int
		switch len(raw) {
		case 65:
			pub, err := gethcrypto.UnmarshalPubkey(raw)
			if err != nil {
				return nil, err
			}
			x, y = pub.X, pub.Y
		case 33:
			pub, err := gethcrypto.DecompressPubkey(raw)
			if err != nil {
				return nil, err
			}
			x, y = pub.X, pub.Y
		default:
			return nil, errors.New("secp256k1 public key must be 33 or 65 bytes")
		}
		return pt.NewECPoint(elliptic.Secp256k1(), x, y)
	}
	if len(raw) != 32 {
		return nil, errors.New("ed25519 public key must be 32 bytes")
	}
	point, err := new(edwards25519.Point).SetBytes(raw)
	if err != nil || !bytes.Equal(point.Bytes(), raw) || point.Equal(edwards25519.NewIdentityPoint()) == 1 {
		return nil, errors.New("ed25519 public key is not a canonical point")
	}
	X, Y, Z, _ := point.ExtendedCoordinates()
	zInv := new(field.Element).Invert(Z)
	return pt.NewECPoint(elliptic.Ed25519(), fieldInt(new(field.Element).Multiply(X, zInv)), fieldInt(new(field.Element).Multiply(Y, zInv)))
}

func fieldInt(v *field.Element) *big.Int {
	le := v.Bytes()
	for i, j := 0, len(le)-1; i < j; i, j = i+1, j-1 {
		le[i], le[j] = le[j], le[i]
	}
	return new(big.Int).SetBytes(le)
}

func secpAddress(p *pt.ECPoint) string {
	raw, _ := publicKeyBytes(dealer.Secp256k1, p)
	return common.BytesToAddress(gethcrypto.Keccak256(raw[1:])[12:]).Hex()
}

func (s *Server) accountFor(c dealer.Curve, pub *pt.ECPoint, requested string) (string, *Error) {
	if c == dealer.Secp256k1 {
		addr := secpAddress(pub)
		if requested != "" && !strings.EqualFold(requested, addr) {
			return "", newError(CodeSessionBadRequest, "account does not match the key address")
		}
		return addr, nil
	}
	if !common.IsHexAddress(requested) {
		return "", newError(CodeSessionBadRequest, "ed25519 keys need an account address for policy")
	}
	return common.HexToAddress(requested).Hex(), nil
}

func (s *Server) loadShare(keyID string) (store.ShareRecord, storedShare, *Error) {
	rec, err := s.opts.Store.Get(keyID)
	if errors.Is(err, store.ErrNotFound) {
		return rec, storedShare{}, newError(CodeKeyNotFound, "key %q is not held by this node", keyID)
	}
	if err != nil {
		return rec, storedShare{}, newError(CodeStoreFailed, "%v", err)
	}
	var payload storedShare
	err = s.opts.Store.WithShare(keyID, func(plain []byte) error { return json.Unmarshal(plain, &payload) })
	if err != nil {
		return rec, storedShare{}, newError(CodeStoreFailed, "%v", err)
	}
	return rec, payload, nil
}

func (s *Server) saveShare(keyID string, c dealer.Curve, pub *pt.ECPoint, epoch uint64, participants []string, payload storedShare) *Error {
	pubBytes, err := publicKeyBytes(c, pub)
	if err != nil {
		return newError(CodeKeyInvalidShare, "%v", err)
	}
	plain, err := json.Marshal(payload)
	if err != nil {
		return newError(CodeStoreFailed, "%v", err)
	}
	defer func() {
		for i := range plain {
			plain[i] = 0
		}
	}()
	rec := store.ShareRecord{KeyID: keyID, Curve: curveName(c), PublicKey: pubBytes, Epoch: epoch, Participants: participants}
	if err := s.opts.Store.Put(rec, plain); err != nil {
		return newError(CodeStoreFailed, "%v", err)
	}
	return nil
}

func (s *Server) keyExists(keyID string) (bool, *Error) {
	_, err := s.opts.Store.Get(keyID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, newError(CodeStoreFailed, "%v", err)
	}
	return true, nil
}

func (payload storedShare) bundle() (dealer.ShareBundle, *tssecdsa.KeyShare, *Error) {
	if len(payload.ECDSA) > 0 {
		k, err := tssecdsa.LoadKeyShare(payload.ECDSA)
		if err != nil {
			return dealer.ShareBundle{}, nil, newError(CodeKeyInvalidShare, "%v", err)
		}
		return dealer.ShareBundle{
			Curve:             dealer.Secp256k1,
			ParticipantID:     k.SelfID,
			Share:             new(big.Int).Set(k.Share),
			PublicKey:         k.PublicKey.Copy(),
			PartialPublicKeys: k.PartialPublicKeys,
			Bks:               k.Bks,
			Threshold:         k.Threshold,
		}.Clone(), k, nil
	}
	if payload.Bundle == nil {
		return dealer.ShareBundle{}, nil, newError(CodeKeyInvalidShare, "stored share holds no material")
	}
	b, err := DecodeBundle(*payload.Bundle)
	if err != nil {
		return dealer.ShareBundle{}, nil, newError(CodeKeyInvalidShare, "%v", err)
	}
	return b, nil, nil
}

func (s *Server) keyResponse(keyID string, c dealer.Curve, pub *pt.ECPoint, epoch uint64, participants []string, refreshed bool, seq uint64) KeyResponse {
	raw, _ := publicKeyBytes(c, pub)
	resp := KeyResponse{NodeID: s.opts.NodeID, KeyID: keyID, Curve: curveName(c), PublicKey: hex.EncodeToString(raw), Epoch: epoch, Participants: participants, Refreshed: refreshed, AuditSequence: seq}
	if c == dealer.Secp256k1 {
		resp.Address = secpAddress(pub)
	} else {
		var fixed [32]byte
		copy(fixed[:], raw)
		resp.DID = lxwire.DIDFromKey(fixed)
	}
	return resp
}

func decodeRequest(body []byte, v any) *Error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return newError(CodeSessionBadRequest, "malformed request: %v", err)
	}
	if dec.More() {
		return newError(CodeSessionBadRequest, "trailing data after request")
	}
	return nil
}

func requireIDs(sessionID, keyID string) *Error {
	if sessionID == "" || strings.Contains(sessionID, "/") || len(sessionID) > 128 {
		return newError(CodeSessionBadRequest, "session_id must be 1 to 128 characters without a slash")
	}
	if keyID == "" || len(keyID) > 128 {
		return newError(CodeSessionBadRequest, "key_id must be 1 to 128 characters")
	}
	return nil
}

func ridFor(keyID string) []byte {
	sum := sha256.Sum256([]byte("paxeer-x-attestor/rid\x00" + keyID))
	return sum[:]
}

func (s *Server) HandleImport(w http.ResponseWriter, r *http.Request) {
	body, e := readBody(r)
	var resp KeyResponse
	if e == nil {
		resp, e = s.doImport(body)
	}
	respond(w, resp, e)
}

func (s *Server) doImport(body []byte) (KeyResponse, *Error) {
	if !s.opts.Ceremony {
		return KeyResponse{}, newError(CodeKeyImportDisabled, "key import is only accepted while the ceremony flag is set")
	}
	var req ImportRequest
	if e := decodeRequest(body, &req); e != nil {
		return KeyResponse{}, e
	}
	if e := requireIDs(req.SessionID, req.KeyID); e != nil {
		return KeyResponse{}, e
	}
	if req.Owner == "" {
		return KeyResponse{}, newError(CodeSessionBadRequest, "owner is required")
	}
	b, err := DecodeBundle(req.Share)
	if err != nil {
		return KeyResponse{}, newError(CodeKeyInvalidShare, "%v", err)
	}
	defer dealer.Wipe(b.Share)
	if b.ParticipantID != s.opts.NodeID {
		return KeyResponse{}, newError(CodeKeyInvalidShare, "share is issued to %q, not this node", b.ParticipantID)
	}
	account, e := s.accountFor(b.Curve, b.PublicKey, req.Account)
	if e != nil {
		return KeyResponse{}, e
	}
	unlock := s.lockKey(req.KeyID)
	defer unlock()
	if exists, e := s.keyExists(req.KeyID); e != nil || exists {
		if e == nil {
			e = newError(CodeKeyExists, "key %q already exists", req.KeyID)
		}
		return KeyResponse{}, e
	}
	enc := EncodeBundle(b)
	participants := b.ParticipantIDs()
	if e := s.saveShare(req.KeyID, b.Curve, b.PublicKey, 0, participants, storedShare{Owner: req.Owner, Account: account, Bundle: &enc}); e != nil {
		return KeyResponse{}, e
	}
	seq, e := s.audit("keys.import", req.KeyID, req.Owner, "allowed", "imported", req.SessionID)
	if e != nil {
		return KeyResponse{}, e
	}
	return s.keyResponse(req.KeyID, b.Curve, b.PublicKey, 0, participants, false, seq), nil
}

func (s *Server) HandleRefresh(w http.ResponseWriter, r *http.Request) {
	body, e := readBody(r)
	var resp KeyResponse
	if e == nil {
		resp, e = s.doRefresh(r, body)
	}
	respond(w, resp, e)
}

func respond(w http.ResponseWriter, v any, e *Error) {
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) doRefresh(r *http.Request, body []byte) (KeyResponse, *Error) {
	var req RefreshRequest
	if e := decodeRequest(body, &req); e != nil {
		return KeyResponse{}, e
	}
	if e := requireIDs(req.SessionID, req.KeyID); e != nil {
		return KeyResponse{}, e
	}
	unlock := s.lockKey(req.KeyID)
	defer unlock()
	rec, payload, e := s.loadShare(req.KeyID)
	if e != nil {
		return KeyResponse{}, e
	}
	b, _, e := payload.bundle()
	if e != nil {
		return KeyResponse{}, e
	}
	defer dealer.Wipe(b.Share)
	participants := b.ParticipantIDs()
	ssid := deriveSSID("refresh", req.SessionID, req.KeyID)
	next := payload
	switch b.Curve {
	case dealer.Secp256k1:
		var out *refresh.ECDSAShare
		e = s.runSession(r.Context(), req.SessionID, "refresh", protocolRefreshSecp, participants, func(ctx context.Context, ps *peerSession) error {
			var err error
			out, err = refresh.RefreshECDSA(ctx, refreshNet{ps}, b, ssid)
			return err
		})
		if e != nil {
			break
		}
		k := &tssecdsa.KeyShare{
			SelfID:            out.ParticipantID,
			Threshold:         out.Threshold,
			SSID:              ssid,
			Rid:               ridFor(req.KeyID),
			PublicKey:         out.PublicKey,
			Share:             out.Share,
			PartialPublicKeys: out.PartialPublicKeys,
			Bks:               out.Bks,
			PaillierKey:       out.PaillierKey,
			Pedersen:          out.Pedersen,
		}
		raw, err := k.Marshal()
		if err != nil {
			e = newError(CodeKeyInvalidShare, "%v", err)
			break
		}
		next.ECDSA, next.Bundle = raw, nil
	case dealer.Ed25519:
		var out dealer.ShareBundle
		e = s.runSession(r.Context(), req.SessionID, "refresh", protocolRefreshEd, participants, func(ctx context.Context, ps *peerSession) error {
			var err error
			out, err = refresh.RefreshEdDSA(ctx, refreshNet{ps}, b, ssid)
			return err
		})
		if e != nil {
			break
		}
		enc := EncodeBundle(out)
		dealer.Wipe(out.Share)
		next.Bundle, next.ECDSA = &enc, nil
	default:
		e = newError(CodeKeyCurve, "unknown curve")
	}
	if e != nil {
		_, _ = s.audit("keys.refresh", req.KeyID, payload.Owner, "failed", e.Code, req.SessionID)
		return KeyResponse{}, e
	}
	epoch := rec.Epoch + 1
	if e := s.saveShare(req.KeyID, b.Curve, b.PublicKey, epoch, participants, next); e != nil {
		return KeyResponse{}, e
	}
	seq, e := s.audit("keys.refresh", req.KeyID, payload.Owner, "allowed", "refreshed", req.SessionID)
	if e != nil {
		return KeyResponse{}, e
	}
	return s.keyResponse(req.KeyID, b.Curve, b.PublicKey, epoch, participants, true, seq), nil
}

func (s *Server) HandleGenerate(w http.ResponseWriter, r *http.Request) {
	body, e := readBody(r)
	var resp KeyResponse
	if e == nil {
		resp, e = s.doGenerate(r, body)
	}
	respond(w, resp, e)
}

func (s *Server) doGenerate(r *http.Request, body []byte) (KeyResponse, *Error) {
	var req GenerateRequest
	if e := decodeRequest(body, &req); e != nil {
		return KeyResponse{}, e
	}
	if e := requireIDs(req.SessionID, req.KeyID); e != nil {
		return KeyResponse{}, e
	}
	c, ok := parseCurve(req.Curve)
	if !ok {
		return KeyResponse{}, newError(CodeKeyCurve, "unknown curve %q", req.Curve)
	}
	if req.Owner == "" {
		return KeyResponse{}, newError(CodeSessionBadRequest, "owner is required")
	}
	if c == dealer.Ed25519 && !common.IsHexAddress(req.Account) {
		return KeyResponse{}, newError(CodeSessionBadRequest, "ed25519 keys need an account address for policy")
	}
	unlock := s.lockKey(req.KeyID)
	defer unlock()
	if exists, e := s.keyExists(req.KeyID); e != nil || exists {
		if e == nil {
			e = newError(CodeKeyExists, "key %q already exists", req.KeyID)
		}
		return KeyResponse{}, e
	}
	participants := s.opts.Participants
	var pub *pt.ECPoint
	var payload storedShare
	var e *Error
	switch c {
	case dealer.Secp256k1:
		var k *tssecdsa.KeyShare
		e = s.runSession(r.Context(), req.SessionID, "keygen", protocolKeygenSecp, participants, func(ctx context.Context, ps *peerSession) error {
			var err error
			k, err = tssecdsa.Keygen(ctx, ecdsaNet{ps}, deriveSSID("keygen", req.SessionID, req.KeyID))
			return err
		})
		if e != nil {
			break
		}
		raw, err := k.Marshal()
		if err != nil {
			e = newError(CodeKeyInvalidShare, "%v", err)
			break
		}
		pub = k.PublicKey
		payload = storedShare{Owner: req.Owner, ECDSA: raw}
	case dealer.Ed25519:
		var k *eddsa.KeyShare
		e = s.runSession(r.Context(), req.SessionID, "keygen", protocolKeygenEd, participants, func(ctx context.Context, ps *peerSession) error {
			var err error
			k, err = eddsa.Keygen(ctx, eddsaNet{ps})
			return err
		})
		if e != nil {
			break
		}
		kpub, share, bks, ys := k.Material()
		b := dealer.ShareBundle{Curve: dealer.Ed25519, ParticipantID: k.ID(), Share: share, PublicKey: kpub, PartialPublicKeys: ys, Bks: bks, Threshold: k.Threshold()}
		enc := EncodeBundle(b)
		dealer.Wipe(share)
		pub = kpub
		payload = storedShare{Owner: req.Owner, Bundle: &enc}
	}
	if e != nil {
		_, _ = s.audit("keys.generate", req.KeyID, req.Owner, "failed", e.Code, req.SessionID)
		return KeyResponse{}, e
	}
	account, e := s.accountFor(c, pub, req.Account)
	if e != nil {
		return KeyResponse{}, e
	}
	payload.Account = account
	if e := s.saveShare(req.KeyID, c, pub, 0, participants, payload); e != nil {
		return KeyResponse{}, e
	}
	seq, e := s.audit("keys.generate", req.KeyID, req.Owner, "allowed", "generated", req.SessionID)
	if e != nil {
		return KeyResponse{}, e
	}
	return s.keyResponse(req.KeyID, c, pub, 0, participants, c == dealer.Secp256k1, seq), nil
}

func (s *Server) HandleAddShare(w http.ResponseWriter, r *http.Request) {
	body, e := readBody(r)
	var resp KeyResponse
	if e == nil {
		resp, e = s.doAddShare(r, body)
	}
	respond(w, resp, e)
}

func (s *Server) doAddShare(r *http.Request, body []byte) (KeyResponse, *Error) {
	var req AddShareRequest
	if e := decodeRequest(body, &req); e != nil {
		return KeyResponse{}, e
	}
	if e := requireIDs(req.SessionID, req.KeyID); e != nil {
		return KeyResponse{}, e
	}
	c, ok := parseCurve(req.Curve)
	if !ok {
		return KeyResponse{}, newError(CodeKeyCurve, "unknown curve %q", req.Curve)
	}
	pub, err := parsePublicKey(c, req.PublicKey)
	if err != nil {
		return KeyResponse{}, newError(CodeSessionBadRequest, "public_key: %v", err)
	}
	quorum, ok := sortedUnique(req.Quorum)
	if !ok || req.NewParticipantID == "" {
		return KeyResponse{}, newError(CodeSessionBadRequest, "quorum must be distinct ids and new_participant_id is required")
	}
	if len(quorum) < int(dealer.Threshold) {
		return KeyResponse{}, newError(CodeQuorumTooFew, "add-share needs at least %d existing participants", dealer.Threshold)
	}
	all := append(append([]string(nil), quorum...), req.NewParticipantID)
	all, ok = sortedUnique(all)
	if !ok {
		return KeyResponse{}, newError(CodeSessionBadRequest, "new participant is already in the quorum")
	}
	self := s.opts.NodeID
	inQuorum := false
	for _, id := range quorum {
		inQuorum = inQuorum || id == self
	}
	if !inQuorum && self != req.NewParticipantID {
		return KeyResponse{}, newError(CodeQuorumSelfMissing, "this node is neither in the quorum nor the new participant")
	}
	unlock := s.lockKey(req.KeyID)
	defer unlock()
	addReq := refresh.AddShareRequest{Curve: c, PublicKey: pub, Threshold: dealer.Threshold, NewParticipantID: req.NewParticipantID}
	payload := storedShare{Owner: req.Owner}
	var epoch uint64
	if inQuorum {
		rec, stored, e := s.loadShare(req.KeyID)
		if e != nil {
			return KeyResponse{}, e
		}
		b, _, e := stored.bundle()
		if e != nil {
			return KeyResponse{}, e
		}
		defer dealer.Wipe(b.Share)
		if b.Curve != c || !b.PublicKey.Equal(pub) {
			return KeyResponse{}, newError(CodeKeyCurve, "request curve or public key differs from the held share")
		}
		addReq.Existing = &b
		payload.Owner, payload.Account, epoch = stored.Owner, stored.Account, rec.Epoch
	} else {
		if exists, e := s.keyExists(req.KeyID); e != nil || exists {
			if e == nil {
				e = newError(CodeKeyExists, "key %q already exists", req.KeyID)
			}
			return KeyResponse{}, e
		}
		if req.Owner == "" {
			return KeyResponse{}, newError(CodeSessionBadRequest, "owner is required")
		}
		account, e := s.accountFor(c, pub, req.Account)
		if e != nil {
			return KeyResponse{}, e
		}
		payload.Account = account
	}
	var out dealer.ShareBundle
	e := s.runSession(r.Context(), req.SessionID, "addshare", protocolAddShare, all, func(ctx context.Context, ps *peerSession) error {
		var err error
		out, err = refresh.AddShare(ctx, refreshNet{ps}, addReq)
		return err
	})
	if e != nil {
		_, _ = s.audit("keys.addshare", req.KeyID, payload.Owner, "failed", e.Code, req.SessionID)
		return KeyResponse{}, e
	}
	enc := EncodeBundle(out)
	dealer.Wipe(out.Share)
	payload.Bundle = &enc
	participants := out.ParticipantIDs()
	if e := s.saveShare(req.KeyID, c, pub, epoch, participants, payload); e != nil {
		return KeyResponse{}, e
	}
	seq, e := s.audit("keys.addshare", req.KeyID, payload.Owner, "allowed", "share added for "+req.NewParticipantID, req.SessionID)
	if e != nil {
		return KeyResponse{}, e
	}
	return s.keyResponse(req.KeyID, c, pub, epoch, participants, false, seq), nil
}
