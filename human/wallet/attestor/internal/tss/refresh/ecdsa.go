package refresh

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/getamis/alice/crypto/birkhoffinterpolation"
	pt "github.com/getamis/alice/crypto/ecpointgrouplaw"
	"github.com/getamis/alice/crypto/homo/paillier"
	cggmprefresh "github.com/getamis/alice/crypto/tss/ecdsa/cggmp/refresh"
	"github.com/getamis/alice/crypto/zkproof"
	paillierzkproof "github.com/getamis/alice/crypto/zkproof/paillier"
	"github.com/getamis/alice/types"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/tss/dealer"
)

const (
	PaillierKeySize = 2048

	shareProofMessageType types.MessageType = 1
	maxPendingPerPeer                       = 32
)

var (
	ErrCurve             = errors.New("refresh: bundle curve does not match the protocol")
	ErrSelf              = errors.New("refresh: bundle participant is not the network's own id")
	ErrParticipants      = errors.New("refresh: network peers do not match the bundle participant set")
	ErrShareMismatch     = errors.New("refresh: own share does not match the public key")
	ErrPeerShareRefused  = errors.New("refresh: a peer's share does not match the public key")
	ErrUnexpectedMessage = errors.New("refresh: unexpected protocol message")
	ErrProtocolFailed    = errors.New("refresh: protocol failed")
	ErrBacklog           = errors.New("refresh: message backlog exceeded")
)

type Receiver interface {
	AddMessage(senderID string, msg types.Message) error
}

type Network interface {
	types.PeerManager
	Bind(r Receiver)
}

type ShareProofMessage struct {
	From  string
	Proof *zkproof.SchnorrProofMessage
}

func (m *ShareProofMessage) GetId() string { return m.From }

func (m *ShareProofMessage) GetMessageType() types.MessageType { return shareProofMessageType }

func (m *ShareProofMessage) IsValid() bool { return m.From != "" && m.Proof != nil && m.Proof.V != nil }

type ECDSAShare struct {
	ParticipantID     string
	Share             *big.Int
	PublicKey         *pt.ECPoint
	PartialPublicKeys map[string]*pt.ECPoint
	Bks               map[string]*birkhoffinterpolation.BkParameter
	Threshold         uint32
	PaillierKey       *paillier.Paillier
	Pedersen          map[string]*paillierzkproof.PederssenOpenParameter
}

func (s *ECDSAShare) Bundle() dealer.ShareBundle {
	return dealer.ShareBundle{
		Curve:             dealer.Secp256k1,
		ParticipantID:     s.ParticipantID,
		Share:             s.Share,
		PublicKey:         s.PublicKey,
		PartialPublicKeys: s.PartialPublicKeys,
		Bks:               s.Bks,
		Threshold:         s.Threshold,
	}.Clone()
}

func RefreshECDSA(ctx context.Context, net Network, bundle dealer.ShareBundle, ssid []byte) (*ECDSAShare, error) {
	if bundle.Curve != dealer.Secp256k1 {
		return nil, ErrCurve
	}
	if err := checkMembership(net, bundle); err != nil {
		return nil, err
	}
	b := bundle.Clone()
	d := newDispatcher(net.NumPeers())
	net.Bind(d)
	if err := verifyShares(ctx, net, d.proofs, b, ssid); err != nil {
		return nil, err
	}

	listener := newStateListener()
	r, err := cggmprefresh.NewRefresh(new(big.Int).Set(b.Share), b.PublicKey, net, b.Threshold, b.PartialPublicKeys, b.Bks, PaillierKeySize, ssid, listener)
	if err != nil {
		return nil, err
	}
	d.attach(r)
	if err := runMain(ctx, r, listener, r.Start); err != nil {
		return nil, err
	}
	res, err := r.GetResult()
	if err != nil {
		return nil, err
	}
	out := &ECDSAShare{
		ParticipantID:     b.ParticipantID,
		Share:             new(big.Int).Set(res.Share),
		PublicKey:         b.PublicKey.Copy(),
		PartialPublicKeys: res.PartialPubKey,
		Bks:               b.Bks,
		Threshold:         b.Threshold,
		PaillierKey:       res.PaillierKey,
		Pedersen:          res.PedParameter,
	}
	dealer.Wipe(b.Share)
	if err := out.Bundle().Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProtocolFailed, err)
	}
	return out, nil
}

func checkMembership(net Network, b dealer.ShareBundle) error {
	if net.SelfID() != b.ParticipantID {
		return ErrSelf
	}
	peers := net.PeerIDs()
	if int(net.NumPeers()) != len(peers) || len(peers)+1 != len(b.Bks) {
		return ErrParticipants
	}
	if _, ok := b.Bks[b.ParticipantID]; !ok {
		return ErrParticipants
	}
	seen := map[string]struct{}{b.ParticipantID: {}}
	for _, id := range peers {
		if _, ok := b.Bks[id]; !ok {
			return ErrParticipants
		}
		if _, dup := seen[id]; dup {
			return ErrParticipants
		}
		seen[id] = struct{}{}
	}
	return nil
}

func proofSeed(ssid []byte, id string) []byte {
	seed := make([]byte, 0, len(ssid)+len(id)+1)
	seed = append(seed, ssid...)
	seed = append(seed, 0)
	return append(seed, id...)
}

func verifyShares(ctx context.Context, net Network, proofs <-chan *ShareProofMessage, b dealer.ShareBundle, ssid []byte) error {
	curve, err := b.Curve.Elliptic()
	if err != nil {
		return err
	}
	proof, err := zkproof.NewBaseSchorrMessage(curve, b.Share, proofSeed(ssid, b.ParticipantID))
	if err != nil {
		return err
	}
	msg := &ShareProofMessage{From: b.ParticipantID, Proof: proof}
	for _, id := range net.PeerIDs() {
		net.MustSend(id, msg)
	}
	if err := b.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrShareMismatch, err)
	}
	received := make(map[string]*ShareProofMessage, net.NumPeers())
	for len(received) < int(net.NumPeers()) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case m := <-proofs:
			if _, member := b.Bks[m.From]; !member || m.From == b.ParticipantID {
				return ErrUnexpectedMessage
			}
			if _, dup := received[m.From]; dup {
				return ErrUnexpectedMessage
			}
			received[m.From] = m
		}
	}
	G := pt.NewBase(curve)
	for id, m := range received {
		if err := m.Proof.Verify(G, proofSeed(ssid, id)); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrPeerShareRefused, id, err)
		}
		point, err := m.Proof.V.ToPoint()
		if err != nil {
			return fmt.Errorf("%w: %s: %v", ErrPeerShareRefused, id, err)
		}
		if !point.Equal(b.PartialPublicKeys[id]) {
			return fmt.Errorf("%w: %s", ErrPeerShareRefused, id)
		}
	}
	return nil
}

type dispatcher struct {
	proofs chan *ShareProofMessage

	mu      sync.Mutex
	main    types.MessageMain
	pending []pendingMessage
	limit   int
}

type pendingMessage struct {
	sender string
	msg    types.Message
}

func newDispatcher(peers uint32) *dispatcher {
	return &dispatcher{
		proofs: make(chan *ShareProofMessage, peers),
		limit:  int(peers) * maxPendingPerPeer,
	}
}

func (d *dispatcher) AddMessage(senderID string, msg types.Message) error {
	if proof, ok := msg.(*ShareProofMessage); ok {
		if senderID != proof.From || !proof.IsValid() {
			return ErrUnexpectedMessage
		}
		select {
		case d.proofs <- proof:
			return nil
		default:
			return ErrBacklog
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.main == nil {
		if len(d.pending) >= d.limit {
			return ErrBacklog
		}
		d.pending = append(d.pending, pendingMessage{sender: senderID, msg: msg})
		return nil
	}
	return d.main.AddMessage(senderID, msg)
}

func (d *dispatcher) attach(m types.MessageMain) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.main = m
	for _, p := range d.pending {
		_ = m.AddMessage(p.sender, p.msg)
	}
	d.pending = nil
}

type stateListener struct {
	ch chan types.MainState
}

func newStateListener() *stateListener {
	return &stateListener{ch: make(chan types.MainState, 1)}
}

func (l *stateListener) OnStateChanged(_ types.MainState, newState types.MainState) {
	if newState == types.StateDone || newState == types.StateFailed {
		select {
		case l.ch <- newState:
		default:
		}
	}
}

func runMain(ctx context.Context, m types.MessageMain, l *stateListener, start func()) error {
	start()
	select {
	case state := <-l.ch:
		if state != types.StateDone {
			return ErrProtocolFailed
		}
		return nil
	case <-ctx.Done():
		m.Stop()
		return ctx.Err()
	}
}
