package refresh

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/getamis/alice/crypto/birkhoffinterpolation"
	pt "github.com/getamis/alice/crypto/ecpointgrouplaw"
	"github.com/getamis/alice/crypto/tss/ecdsa/addshare/newpeer"
	"github.com/getamis/alice/crypto/tss/ecdsa/addshare/oldpeer"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/dealer"
)

var (
	ErrAddShareRequest = errors.New("refresh: add-share request is inconsistent")
	ErrQuorum          = errors.New("refresh: add-share needs a quorum of existing participants")
)

type AddShareRequest struct {
	Curve            dealer.Curve
	PublicKey        *pt.ECPoint
	Threshold        uint32
	NewParticipantID string
	Existing         *dealer.ShareBundle
}

func AddShare(ctx context.Context, net Network, req AddShareRequest) (dealer.ShareBundle, error) {
	curve, err := req.Curve.Elliptic()
	if err != nil {
		return dealer.ShareBundle{}, err
	}
	if req.PublicKey == nil || req.PublicKey.GetCurve() != curve || req.NewParticipantID == "" {
		return dealer.ShareBundle{}, ErrAddShareRequest
	}
	peers := net.PeerIDs()
	if int(net.NumPeers()) != len(peers) {
		return dealer.ShareBundle{}, ErrParticipants
	}
	if req.Existing == nil {
		return receiveShare(ctx, net, req, peers)
	}
	return contributeShare(ctx, net, req, peers)
}

func contributeShare(ctx context.Context, net Network, req AddShareRequest, peers []string) (dealer.ShareBundle, error) {
	b := req.Existing.Clone()
	defer dealer.Wipe(b.Share)
	if b.Curve != req.Curve || b.Threshold != req.Threshold || !b.PublicKey.Equal(req.PublicKey) {
		return dealer.ShareBundle{}, ErrAddShareRequest
	}
	if net.SelfID() != b.ParticipantID {
		return dealer.ShareBundle{}, ErrSelf
	}
	if err := b.Validate(); err != nil {
		return dealer.ShareBundle{}, fmt.Errorf("%w: %v", ErrShareMismatch, err)
	}
	if _, clash := b.Bks[req.NewParticipantID]; clash {
		return dealer.ShareBundle{}, ErrAddShareRequest
	}
	quorum := make(map[string]*birkhoffinterpolation.BkParameter, len(peers)+1)
	quorum[b.ParticipantID] = b.Bks[b.ParticipantID]
	for _, id := range peers {
		bk, ok := b.Bks[id]
		if !ok {
			return dealer.ShareBundle{}, ErrParticipants
		}
		if _, dup := quorum[id]; dup {
			return dealer.ShareBundle{}, ErrParticipants
		}
		quorum[id] = bk
	}
	if len(quorum) < int(b.Threshold) {
		return dealer.ShareBundle{}, ErrQuorum
	}

	listener := newStateListener()
	as, err := oldpeer.NewAddShare(net, b.PublicKey, b.Threshold, new(big.Int).Set(b.Share), quorum, req.NewParticipantID, listener)
	if err != nil {
		return dealer.ShareBundle{}, err
	}
	net.Bind(as)
	if err := runMain(ctx, as, listener, as.Start); err != nil {
		return dealer.ShareBundle{}, err
	}
	res, err := as.GetResult()
	if err != nil {
		return dealer.ShareBundle{}, err
	}
	out := dealer.ShareBundle{
		Curve:             b.Curve,
		ParticipantID:     b.ParticipantID,
		Share:             new(big.Int).Set(b.Share),
		PublicKey:         b.PublicKey.Copy(),
		PartialPublicKeys: res.PartialPublicKeys,
		Bks:               res.Bks,
		Threshold:         b.Threshold,
	}
	if err := out.Validate(); err != nil {
		dealer.Wipe(out.Share)
		return dealer.ShareBundle{}, fmt.Errorf("%w: %v", ErrProtocolFailed, err)
	}
	return out, nil
}

func receiveShare(ctx context.Context, net Network, req AddShareRequest, peers []string) (dealer.ShareBundle, error) {
	if net.SelfID() != req.NewParticipantID {
		return dealer.ShareBundle{}, ErrSelf
	}
	if len(peers) < int(req.Threshold) {
		return dealer.ShareBundle{}, ErrQuorum
	}
	listener := newStateListener()
	as := newpeer.NewAddShare(net, req.PublicKey, req.Threshold, 0, listener)
	net.Bind(as)
	if err := runMain(ctx, as, listener, as.Start); err != nil {
		return dealer.ShareBundle{}, err
	}
	res, err := as.GetResult()
	if err != nil {
		return dealer.ShareBundle{}, err
	}
	out := dealer.ShareBundle{
		Curve:             req.Curve,
		ParticipantID:     req.NewParticipantID,
		Share:             res.Share,
		PublicKey:         req.PublicKey.Copy(),
		PartialPublicKeys: res.PartialPublicKeys,
		Bks:               res.Bks,
		Threshold:         req.Threshold,
	}
	if err := out.Validate(); err != nil {
		dealer.Wipe(out.Share)
		return dealer.ShareBundle{}, fmt.Errorf("%w: %v", ErrProtocolFailed, err)
	}
	return out, nil
}
