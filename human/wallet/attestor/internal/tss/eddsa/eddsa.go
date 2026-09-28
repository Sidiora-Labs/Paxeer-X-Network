package eddsa

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/getamis/alice/crypto/birkhoffinterpolation"
	pt "github.com/getamis/alice/crypto/ecpointgrouplaw"
	"github.com/getamis/alice/crypto/elliptic"
	"github.com/getamis/alice/crypto/tss/dkg"
	frostdkg "github.com/getamis/alice/crypto/tss/eddsa/frost/dkg"
	"github.com/getamis/alice/crypto/tss/eddsa/frost/signer"
	"github.com/getamis/alice/types"
)

const (
	Parties   = 5
	Threshold = 3
	rank      = 0
)

var (
	ErrParticipants     = errors.New("eddsa: participant set does not match the session")
	ErrBelowThreshold   = errors.New("eddsa: fewer participants than the signing threshold")
	ErrSessionFailed    = errors.New("eddsa: protocol session failed")
	ErrSignatureInvalid = errors.New("eddsa: produced signature does not verify")
)

type PeerManager interface {
	types.PeerManager
	Register(main types.MessageMain)
}

type stateListener struct {
	final chan types.MainState
}

func newStateListener() *stateListener {
	return &stateListener{final: make(chan types.MainState, 1)}
}

func (l *stateListener) OnStateChanged(_ types.MainState, newState types.MainState) {
	if newState != types.StateDone && newState != types.StateFailed {
		return
	}
	select {
	case l.final <- newState:
	default:
	}
}

func run(ctx context.Context, pm PeerManager, main types.MessageMain, listener *stateListener) error {
	pm.Register(main)
	main.Start()
	defer main.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("%w: %w", ErrSessionFailed, ctx.Err())
	case state := <-listener.final:
		if state != types.StateDone {
			return ErrSessionFailed
		}
		return nil
	}
}

func Keygen(ctx context.Context, pm PeerManager) (*KeyShare, error) {
	if pm.NumPeers()+1 != Parties {
		return nil, ErrParticipants
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSessionFailed, err)
	}
	listener := newStateListener()
	session, err := frostdkg.NewDKG(pm, Threshold, rank, listener)
	if err != nil {
		return nil, err
	}
	if err := run(ctx, pm, session, listener); err != nil {
		return nil, err
	}
	result, err := session.GetResult()
	if err != nil {
		return nil, err
	}
	return newKeyShare(pm.SelfID(), Threshold, result.PublicKey, result.Share, result.Bks, result.Ys)
}

func Sign(ctx context.Context, share *KeyShare, pm PeerManager, participants []string, message []byte) ([64]byte, error) {
	var signature [64]byte
	if uint32(len(participants)) < share.threshold {
		return signature, ErrBelowThreshold
	}
	if !sort.StringsAreSorted(participants) {
		return signature, ErrParticipants
	}
	if pm.SelfID() != share.id {
		return signature, ErrParticipants
	}
	peers := make(map[string]struct{}, pm.NumPeers())
	for _, pid := range pm.PeerIDs() {
		peers[pid] = struct{}{}
	}
	if uint32(len(peers)) != pm.NumPeers() || len(peers)+1 != len(participants) {
		return signature, ErrParticipants
	}
	bks := make(map[string]*birkhoffinterpolation.BkParameter, len(participants))
	ys := make(map[string]*pt.ECPoint, len(participants))
	for i, pid := range participants {
		if i > 0 && participants[i-1] == pid {
			return signature, ErrParticipants
		}
		bk, ok := share.bks[pid]
		if !ok {
			return signature, ErrParticipants
		}
		if _, ok := peers[pid]; !ok && pid != share.id {
			return signature, ErrParticipants
		}
		bks[pid] = bk
		ys[pid] = share.ys[pid].Copy()
	}
	if _, ok := bks[share.id]; !ok {
		return signature, ErrParticipants
	}
	if err := ctx.Err(); err != nil {
		return signature, fmt.Errorf("%w: %w", ErrSessionFailed, err)
	}
	secret := new(big.Int).Set(share.share)
	listener := newStateListener()
	session, err := signer.NewSigner(share.publicKey.Copy(), pm, share.threshold, secret, &dkg.Result{
		PublicKey: share.publicKey.Copy(),
		Share:     secret,
		Bks:       bks,
		Ys:        ys,
	}, append([]byte(nil), message...), listener)
	if err != nil {
		return signature, err
	}
	if err := run(ctx, pm, session, listener); err != nil {
		return signature, err
	}
	result, err := session.GetResult()
	if err != nil {
		return signature, err
	}
	signature, err = SignatureBytes(result)
	if err != nil {
		return [64]byte{}, err
	}
	publicKey := share.PublicKeyBytes()
	if !ed25519.Verify(ed25519.PublicKey(publicKey[:]), message, signature[:]) {
		return [64]byte{}, ErrSignatureInvalid
	}
	return signature, nil
}

func SignatureBytes(result *signer.Result) ([64]byte, error) {
	var signature [64]byte
	if result == nil || result.R == nil || result.S == nil {
		return signature, ErrEncoding
	}
	r, err := encodePoint(result.R)
	if err != nil {
		return signature, err
	}
	n := elliptic.Ed25519().Params().N
	if result.S.Sign() < 0 || result.S.Cmp(n) >= 0 {
		return signature, ErrEncoding
	}
	copy(signature[:32], r[:])
	copy(signature[32:], encodeScalar(result.S))
	return signature, nil
}
