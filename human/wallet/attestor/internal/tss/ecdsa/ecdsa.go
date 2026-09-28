package ecdsa

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/getamis/alice/crypto/birkhoffinterpolation"
	pt "github.com/getamis/alice/crypto/ecpointgrouplaw"
	"github.com/getamis/alice/crypto/elliptic"
	"github.com/getamis/alice/crypto/tss/ecdsa/cggmp"
	"github.com/getamis/alice/crypto/tss/ecdsa/cggmp/dkg"
	"github.com/getamis/alice/crypto/tss/ecdsa/cggmp/refresh"
	"github.com/getamis/alice/crypto/tss/ecdsa/cggmp/sign"
	paillierzkproof "github.com/getamis/alice/crypto/zkproof/paillier"
	"github.com/getamis/alice/types"
)

const (
	Threshold       = 3
	Participants    = 5
	DigestLength    = 32
	PaillierKeySize = 2048
)

var (
	ErrInvalidNetwork           = errors.New("ecdsa: invalid peer network")
	ErrInvalidSessionID         = errors.New("ecdsa: empty session id")
	ErrInvalidDigest            = errors.New("ecdsa: digest must be 32 bytes")
	ErrInvalidParticipants      = errors.New("ecdsa: invalid participants")
	ErrBelowThreshold           = errors.New("ecdsa: participants below threshold")
	ErrSessionFailed            = errors.New("ecdsa: session failed")
	ErrUnexpectedMessage        = errors.New("ecdsa: unexpected protocol message")
	ErrPartialPublicKeys        = errors.New("ecdsa: partial public keys inconsistent")
	ErrSignatureOutOfRange      = errors.New("ecdsa: signature scalar out of range")
	ErrSignatureDoesNotRecover  = errors.New("ecdsa: signature does not recover the key address")
	secp256k1N                  = elliptic.Secp256k1().Params().N
	secp256k1HalfN              = new(big.Int).Rsh(secp256k1N, 1)
	errPhaseAlreadyAttached     = errors.New("ecdsa: protocol phase already attached")
	errProtocolResultIncomplete = errors.New("ecdsa: protocol result incomplete")
)

type Receiver interface {
	AddMessage(senderID string, msg types.Message) error
}

type Network interface {
	types.PeerManager
	Bind(r Receiver)
}

type EthereumSignature struct {
	R [32]byte
	S [32]byte
	V byte
}

func (s EthereumSignature) Bytes() []byte {
	out := make([]byte, 65)
	copy(out[:32], s.R[:])
	copy(out[32:64], s.S[:])
	out[64] = s.V
	return out
}

func Keygen(ctx context.Context, net Network, sessionID []byte) (*KeyShare, error) {
	if len(sessionID) == 0 {
		return nil, ErrInvalidSessionID
	}
	ids, err := networkParticipants(net)
	if err != nil {
		return nil, err
	}
	if len(ids) != Participants {
		return nil, fmt.Errorf("%w: keygen needs %d participants, got %d", ErrInvalidNetwork, Participants, len(ids))
	}
	curve := elliptic.Secp256k1()
	selfID := net.SelfID()
	r := newRouter()
	net.Bind(r)

	dkgListener := newListener()
	dkgCore, err := dkg.NewDKG(curve, net, sessionID, Threshold, 0, dkgListener)
	if err != nil {
		return nil, fmt.Errorf("ecdsa: dkg: %w", err)
	}
	if err := r.attach(phaseDKG, dkgCore); err != nil {
		return nil, err
	}
	if err := run(ctx, dkgCore, dkgListener, dkgCore.Start); err != nil {
		return nil, fmt.Errorf("ecdsa: dkg: %w", err)
	}
	dkgResult, err := dkgCore.GetResult()
	if err != nil {
		return nil, fmt.Errorf("ecdsa: dkg result: %w", err)
	}
	partials, err := r.dkgPartialPublicKeys(selfID, dkgResult.Share, dkgResult.Bks, dkgResult.PublicKey)
	if err != nil {
		return nil, err
	}

	ssid := cggmp.ComputeSSID(sessionID, []byte(strings.Join(ids, ",")), dkgResult.Rid)
	refreshListener := newListener()
	refreshCore, err := refresh.NewRefresh(dkgResult.Share, dkgResult.PublicKey, net, Threshold, partials, dkgResult.Bks, PaillierKeySize, ssid, refreshListener)
	if err != nil {
		return nil, fmt.Errorf("ecdsa: refresh: %w", err)
	}
	if err := r.attach(phaseRefresh, refreshCore); err != nil {
		return nil, err
	}
	if err := run(ctx, refreshCore, refreshListener, refreshCore.Start); err != nil {
		return nil, fmt.Errorf("ecdsa: refresh: %w", err)
	}
	refreshResult, err := refreshCore.GetResult()
	if err != nil {
		return nil, fmt.Errorf("ecdsa: refresh result: %w", err)
	}

	share := &KeyShare{
		SelfID:            selfID,
		Threshold:         Threshold,
		SSID:              ssid,
		Rid:               dkgResult.Rid,
		PublicKey:         dkgResult.PublicKey,
		Share:             refreshResult.Share,
		PartialPublicKeys: refreshResult.PartialPubKey,
		Bks:               dkgResult.Bks,
		PaillierKey:       refreshResult.PaillierKey,
		Pedersen:          refreshResult.PedParameter,
	}
	if err := share.Validate(); err != nil {
		return nil, err
	}
	return share, nil
}

func Sign(ctx context.Context, share *KeyShare, net Network, participants []string, digest []byte) (EthereumSignature, error) {
	if len(digest) != DigestLength {
		return EthereumSignature{}, ErrInvalidDigest
	}
	if err := share.Validate(); err != nil {
		return EthereumSignature{}, err
	}
	if err := checkSigners(share, net, participants); err != nil {
		return EthereumSignature{}, err
	}
	address, err := share.Address()
	if err != nil {
		return EthereumSignature{}, err
	}

	bks := make(map[string]*birkhoffinterpolation.BkParameter, len(participants))
	partials := make(map[string]*pt.ECPoint, len(participants))
	peds := make(map[string]*paillierzkproof.PederssenOpenParameter, len(participants))
	for _, id := range participants {
		bks[id] = share.Bks[id]
		partials[id] = share.PartialPublicKeys[id]
		peds[id] = share.Pedersen[id]
	}

	msg := make([]byte, DigestLength)
	copy(msg, digest)
	r := newRouter()
	net.Bind(r)
	listener := newListener()
	signer, err := sign.NewSign(share.Threshold, share.SSID, share.Share, share.PublicKey, partials, share.PaillierKey, peds, bks, msg, net, listener)
	if err != nil {
		return EthereumSignature{}, fmt.Errorf("ecdsa: sign: %w", err)
	}
	if err := r.attach(phaseSign, signer); err != nil {
		return EthereumSignature{}, err
	}
	if err := run(ctx, signer, listener, signer.Start); err != nil {
		return EthereumSignature{}, fmt.Errorf("ecdsa: sign: %w", err)
	}
	result, err := signer.GetResult()
	if err != nil {
		return EthereumSignature{}, fmt.Errorf("ecdsa: sign result: %w", err)
	}
	if result == nil || result.R == nil || result.S == nil {
		return EthereumSignature{}, errProtocolResultIncomplete
	}
	return ethereumSignature(address, msg, result.R, result.S)
}

func ethereumSignature(address common.Address, digest []byte, r, s *big.Int) (EthereumSignature, error) {
	if r.Sign() <= 0 || r.Cmp(secp256k1N) >= 0 || s.Sign() <= 0 || s.Cmp(secp256k1N) >= 0 {
		return EthereumSignature{}, ErrSignatureOutOfRange
	}
	lowS := new(big.Int).Set(s)
	if lowS.Cmp(secp256k1HalfN) > 0 {
		lowS.Sub(secp256k1N, lowS)
	}
	var sig EthereumSignature
	r.FillBytes(sig.R[:])
	lowS.FillBytes(sig.S[:])
	for v := byte(0); v < 2; v++ {
		sig.V = v
		pub, err := gethcrypto.SigToPub(digest, sig.Bytes())
		if err != nil {
			continue
		}
		if gethcrypto.PubkeyToAddress(*pub) == address {
			return sig, nil
		}
	}
	return EthereumSignature{}, ErrSignatureDoesNotRecover
}

func networkParticipants(net Network) ([]string, error) {
	if net == nil {
		return nil, fmt.Errorf("%w: nil", ErrInvalidNetwork)
	}
	selfID := net.SelfID()
	if selfID == "" {
		return nil, fmt.Errorf("%w: empty self id", ErrInvalidNetwork)
	}
	peers := net.PeerIDs()
	if uint32(len(peers)) != net.NumPeers() {
		return nil, fmt.Errorf("%w: peer count mismatch", ErrInvalidNetwork)
	}
	seen := map[string]struct{}{selfID: {}}
	ids := []string{selfID}
	for _, id := range peers {
		if id == "" {
			return nil, fmt.Errorf("%w: empty peer id", ErrInvalidNetwork)
		}
		if _, ok := seen[id]; ok {
			return nil, fmt.Errorf("%w: duplicate id %q", ErrInvalidNetwork, id)
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func checkSigners(share *KeyShare, net Network, participants []string) error {
	if uint32(len(participants)) < share.Threshold {
		return fmt.Errorf("%w: %d of %d", ErrBelowThreshold, len(participants), share.Threshold)
	}
	if !sort.StringsAreSorted(participants) {
		return fmt.Errorf("%w: not sorted", ErrInvalidParticipants)
	}
	hasSelf := false
	for i, id := range participants {
		if i > 0 && participants[i-1] == id {
			return fmt.Errorf("%w: duplicate id %q", ErrInvalidParticipants, id)
		}
		if _, ok := share.Bks[id]; !ok {
			return fmt.Errorf("%w: %q holds no share of this key", ErrInvalidParticipants, id)
		}
		if id == share.SelfID {
			hasSelf = true
		}
	}
	if !hasSelf {
		return fmt.Errorf("%w: self not included", ErrInvalidParticipants)
	}
	ids, err := networkParticipants(net)
	if err != nil {
		return err
	}
	if net.SelfID() != share.SelfID {
		return fmt.Errorf("%w: network self id %q differs from share owner %q", ErrInvalidNetwork, net.SelfID(), share.SelfID)
	}
	if len(ids) != len(participants) {
		return fmt.Errorf("%w: network peers differ from participants", ErrInvalidNetwork)
	}
	for i := range ids {
		if ids[i] != participants[i] {
			return fmt.Errorf("%w: network peers differ from participants", ErrInvalidNetwork)
		}
	}
	return nil
}

type listener struct {
	final chan types.MainState
}

func newListener() *listener {
	return &listener{final: make(chan types.MainState, 1)}
}

func (l *listener) OnStateChanged(_ types.MainState, newState types.MainState) {
	if newState != types.StateDone && newState != types.StateFailed {
		return
	}
	select {
	case l.final <- newState:
	default:
	}
}

func run(ctx context.Context, main types.MessageMain, l *listener, start func()) error {
	start()
	select {
	case state := <-l.final:
		if state != types.StateDone {
			return fmt.Errorf("%w: state %s", ErrSessionFailed, state)
		}
		return nil
	case <-ctx.Done():
		main.Stop()
		return fmt.Errorf("%w: %w", ErrSessionFailed, ctx.Err())
	}
}

type phase int

const (
	phaseDKG phase = iota
	phaseRefresh
	phaseSign
)

type pendingMessage struct {
	sender string
	msg    types.Message
}

type router struct {
	mu       sync.Mutex
	mains    map[phase]types.MessageMain
	pending  map[phase][]pendingMessage
	siG      map[string]*pt.ECPoint
	conflict map[string]bool
}

func newRouter() *router {
	return &router{
		mains:    make(map[phase]types.MessageMain),
		pending:  make(map[phase][]pendingMessage),
		siG:      make(map[string]*pt.ECPoint),
		conflict: make(map[string]bool),
	}
}

func classify(msg types.Message) (phase, bool) {
	switch msg.(type) {
	case *dkg.Message:
		return phaseDKG, true
	case *refresh.Message:
		return phaseRefresh, true
	case *sign.Message:
		return phaseSign, true
	}
	return 0, false
}

func (r *router) AddMessage(senderID string, msg types.Message) error {
	if msg == nil {
		return ErrUnexpectedMessage
	}
	p, ok := classify(msg)
	if !ok {
		return fmt.Errorf("%w: %T", ErrUnexpectedMessage, msg)
	}
	r.mu.Lock()
	if p == phaseDKG {
		r.captureSiG(senderID, msg.(*dkg.Message))
	}
	main, attached := r.mains[p]
	if !attached {
		r.pending[p] = append(r.pending[p], pendingMessage{sender: senderID, msg: msg})
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()
	return main.AddMessage(senderID, msg)
}

func (r *router) captureSiG(senderID string, msg *dkg.Message) {
	if msg.GetType() != dkg.Type_Result || senderID != msg.GetId() || msg.GetEchoHashRelay() != nil {
		return
	}
	point, err := msg.GetResult().GetSiGProofMsg().GetV().ToPoint()
	if err != nil {
		r.conflict[senderID] = true
		return
	}
	if prev, ok := r.siG[senderID]; ok && !prev.Equal(point) {
		r.conflict[senderID] = true
		return
	}
	r.siG[senderID] = point
}

func (r *router) attach(p phase, main types.MessageMain) error {
	r.mu.Lock()
	if _, ok := r.mains[p]; ok {
		r.mu.Unlock()
		return errPhaseAlreadyAttached
	}
	r.mains[p] = main
	queued := r.pending[p]
	delete(r.pending, p)
	r.mu.Unlock()
	for _, m := range queued {
		_ = main.AddMessage(m.sender, m.msg)
	}
	return nil
}

func (r *router) dkgPartialPublicKeys(selfID string, share *big.Int, bks map[string]*birkhoffinterpolation.BkParameter, pubKey *pt.ECPoint) (map[string]*pt.ECPoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	curve := pubKey.GetCurve()
	partials := make(map[string]*pt.ECPoint, len(bks))
	params := make(birkhoffinterpolation.BkParameters, 0, len(bks))
	points := make([]*pt.ECPoint, 0, len(bks))
	for id, bk := range bks {
		var point *pt.ECPoint
		if id == selfID {
			point = pt.ScalarBaseMult(curve, share)
		} else {
			if r.conflict[id] {
				return nil, fmt.Errorf("%w: conflicting value from %q", ErrPartialPublicKeys, id)
			}
			captured, ok := r.siG[id]
			if !ok {
				return nil, fmt.Errorf("%w: none from %q", ErrPartialPublicKeys, id)
			}
			point = captured
		}
		partials[id] = point
		params = append(params, bk)
		points = append(points, point)
	}
	if err := params.ValidatePublicKey(points, Threshold, pubKey); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPartialPublicKeys, err)
	}
	return partials, nil
}
