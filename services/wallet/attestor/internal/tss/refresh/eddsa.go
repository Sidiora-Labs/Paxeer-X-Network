package refresh

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/getamis/alice/crypto/birkhoffinterpolation"
	"github.com/getamis/alice/crypto/commitment"
	pt "github.com/getamis/alice/crypto/ecpointgrouplaw"
	"github.com/getamis/alice/crypto/polynomial"
	"github.com/getamis/alice/crypto/utils"
	"github.com/getamis/alice/crypto/zkproof"
	"github.com/getamis/alice/types"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/tss/dealer"
)

const edDSARefreshMessageType types.MessageType = 2

var (
	ErrCommitmentRefused = errors.New("refresh: a peer's refresh commitment is not a zero sharing of the agreed degree")
	ErrEvaluationRefused = errors.New("refresh: a peer's refresh evaluation does not match its commitment")
)

type EdDSARefreshMessage struct {
	From       string
	Proof      *zkproof.SchnorrProofMessage
	Commitment *commitment.PointCommitmentMessage
	Evaluation []byte
}

func (m *EdDSARefreshMessage) GetId() string { return m.From }

func (m *EdDSARefreshMessage) GetMessageType() types.MessageType { return edDSARefreshMessageType }

func (m *EdDSARefreshMessage) IsValid() bool {
	return m.From != "" && m.Proof != nil && m.Proof.V != nil && m.Commitment != nil && len(m.Evaluation) > 0
}

type edDSAReceiver struct {
	ch chan *EdDSARefreshMessage
}

func (r *edDSAReceiver) AddMessage(senderID string, msg types.Message) error {
	m, ok := msg.(*EdDSARefreshMessage)
	if !ok || senderID != m.From || !m.IsValid() {
		return ErrUnexpectedMessage
	}
	select {
	case r.ch <- m:
		return nil
	default:
		return ErrBacklog
	}
}

func RefreshEdDSA(ctx context.Context, net Network, bundle dealer.ShareBundle, ssid []byte) (dealer.ShareBundle, error) {
	if bundle.Curve != dealer.Ed25519 {
		return dealer.ShareBundle{}, ErrCurve
	}
	if err := checkMembership(net, bundle); err != nil {
		return dealer.ShareBundle{}, err
	}
	b := bundle.Clone()
	defer dealer.Wipe(b.Share)
	curve, err := b.Curve.Elliptic()
	if err != nil {
		return dealer.ShareBundle{}, err
	}
	order := curve.Params().N
	degree := b.Threshold - 1

	recv := &edDSAReceiver{ch: make(chan *EdDSARefreshMessage, net.NumPeers())}
	net.Bind(recv)

	poly, err := polynomial.RandomPolynomial(order, degree)
	if err != nil {
		return dealer.ShareBundle{}, err
	}
	poly.SetConstant(big.NewInt(0))
	feldman, err := commitment.NewFeldmanCommitmenter(curve, poly)
	if err != nil {
		return dealer.ShareBundle{}, err
	}
	ownCommitment := feldman.GetCommitmentMessage()
	proof, err := zkproof.NewBaseSchorrMessage(curve, b.Share, proofSeed(ssid, b.ParticipantID))
	if err != nil {
		return dealer.ShareBundle{}, err
	}
	for _, id := range net.PeerIDs() {
		net.MustSend(id, &EdDSARefreshMessage{
			From:       b.ParticipantID,
			Proof:      proof,
			Commitment: ownCommitment,
			Evaluation: evaluateAt(poly, b.Bks[id], order).Bytes(),
		})
	}
	if err := b.Validate(); err != nil {
		return dealer.ShareBundle{}, fmt.Errorf("%w: %v", ErrShareMismatch, err)
	}

	received := make(map[string]*EdDSARefreshMessage, net.NumPeers())
	for len(received) < int(net.NumPeers()) {
		select {
		case <-ctx.Done():
			return dealer.ShareBundle{}, ctx.Err()
		case m := <-recv.ch:
			if _, member := b.Bks[m.From]; !member || m.From == b.ParticipantID {
				return dealer.ShareBundle{}, ErrUnexpectedMessage
			}
			if _, dup := received[m.From]; dup {
				return dealer.ShareBundle{}, ErrUnexpectedMessage
			}
			received[m.From] = m
		}
	}

	G := pt.NewBase(curve)
	ownPoints, err := ownCommitment.EcPoints()
	if err != nil {
		return dealer.ShareBundle{}, err
	}
	commitments := map[string][]*pt.ECPoint{b.ParticipantID: ownPoints}
	newShare := new(big.Int).Add(b.Share, evaluateAt(poly, b.Bks[b.ParticipantID], order))
	for id, m := range received {
		if err := m.Proof.Verify(G, proofSeed(ssid, id)); err != nil {
			return dealer.ShareBundle{}, fmt.Errorf("%w: %s: %v", ErrPeerShareRefused, id, err)
		}
		point, err := m.Proof.V.ToPoint()
		if err != nil || !point.Equal(b.PartialPublicKeys[id]) {
			return dealer.ShareBundle{}, fmt.Errorf("%w: %s", ErrPeerShareRefused, id)
		}
		points, err := m.Commitment.EcPoints()
		if err != nil || len(points) != int(degree)+1 || points[0].GetCurve() != curve || !points[0].IsIdentity() {
			return dealer.ShareBundle{}, fmt.Errorf("%w: %s", ErrCommitmentRefused, id)
		}
		evaluation := new(big.Int).SetBytes(m.Evaluation)
		if err := utils.InRange(evaluation, big.NewInt(0), order); err != nil {
			return dealer.ShareBundle{}, fmt.Errorf("%w: %s", ErrEvaluationRefused, id)
		}
		if err := commitment.FeldmanVerify(curve, b.Bks[b.ParticipantID], points, degree, evaluation); err != nil {
			return dealer.ShareBundle{}, fmt.Errorf("%w: %s", ErrEvaluationRefused, id)
		}
		commitments[id] = points
		newShare.Add(newShare, evaluation)
	}
	newShare.Mod(newShare, order)

	partials := make(map[string]*pt.ECPoint, len(b.Bks))
	for id, bk := range b.Bks {
		sum := b.PartialPublicKeys[id].Copy()
		for _, points := range commitments {
			delta, err := commitment.ComputePolyEvaluatePoint(order, bk, points, degree)
			if err != nil {
				return dealer.ShareBundle{}, err
			}
			sum, err = sum.Add(delta)
			if err != nil {
				return dealer.ShareBundle{}, err
			}
		}
		partials[id] = sum
	}

	out := b.Clone()
	out.Share = newShare
	out.PartialPublicKeys = partials
	if err := out.Validate(); err != nil {
		dealer.Wipe(newShare)
		return dealer.ShareBundle{}, fmt.Errorf("%w: %v", ErrProtocolFailed, err)
	}
	return out, nil
}

func evaluateAt(poly *polynomial.Polynomial, bk *birkhoffinterpolation.BkParameter, order *big.Int) *big.Int {
	v := poly.Differentiate(bk.GetRank()).Evaluate(bk.GetX())
	return v.Mod(v, order)
}
