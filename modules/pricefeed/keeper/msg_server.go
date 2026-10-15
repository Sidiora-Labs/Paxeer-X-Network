package keeper

import (
	"context"
	"encoding/binary"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

type msgServer struct {
	keeper Keeper
}

// NewMsgServerImpl returns the Msg service over the keeper.
func NewMsgServerImpl(k Keeper) types.MsgServer { return msgServer{keeper: k} }

var _ types.MsgServer = msgServer{}

func (s msgServer) SubmitPrices(goCtx context.Context, msg *types.MsgSubmitPrices) (*types.MsgSubmitPricesResponse, error) {
	accepted, refused, err := s.keeper.SubmitPrices(sdk.UnwrapSDKContext(goCtx), *msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgSubmitPricesResponse{Accepted: accepted, Refused: refused}, nil
}

func (s msgServer) SetParams(goCtx context.Context, msg *types.MsgSetParams) (*types.MsgSetParamsResponse, error) {
	params := types.Params{
		Authority:                    msg.Authority,
		Markets:                      msg.Markets,
		StalenessWindowMs:            msg.StalenessWindowMs,
		MaxClockSkewMs:               msg.MaxClockSkewMs,
		MinSources:                   msg.MinSources,
		MaxDivergenceBps:             msg.MaxDivergenceBps,
		MaxDeviationBps:              msg.MaxDeviationBps,
		MaxObservationsPerSubmission: msg.MaxObservationsPerSubmission,
		MaxSubmissionBytes:           msg.MaxSubmissionBytes,
		MarkEmaTauMs:                 msg.MarkEmaTauMs,
		MaxMarkBasisBps:              msg.MaxMarkBasisBps,
		MaxPriceAgeMs:                msg.MaxPriceAgeMs,
	}
	if err := s.keeper.UpdateParams(sdk.UnwrapSDKContext(goCtx), msg.Authority, params); err != nil {
		return nil, err
	}
	return &types.MsgSetParamsResponse{}, nil
}

// SubmitPrices applies the structural refusals to the whole message in the
// order of the ante CheckSubmission (tx size, registered feeder, bonded
// validator, observation count, one submission per height), records the
// block height as the validator's last submission height and then checks
// each observation on its own. It returns the accepted and refused counts.
func (k Keeper) SubmitPrices(ctx sdk.Context, msg types.MsgSubmitPrices) (uint32, uint32, error) {
	if err := msg.ValidateBasic(); err != nil {
		return 0, 0, err
	}
	params := k.GetParams(ctx)
	if size := len(ctx.TxBytes()); size > int(params.MaxSubmissionBytes) {
		return 0, 0, types.ErrSubmissionTooLarge.Wrapf("%d tx bytes, at most %d", size, params.MaxSubmissionBytes)
	}
	feeder := msg.GetSigners()[0]
	operator, ok := k.GetValidatorByFeedAccount(ctx, feeder)
	if !ok {
		return 0, 0, types.ErrUnregisteredFeeder.Wrapf("feeder %s", msg.Feeder)
	}
	if err := k.RequireBonded(ctx, operator); err != nil {
		return 0, 0, err
	}
	if count := len(msg.Observations); count > int(params.MaxObservationsPerSubmission) {
		return 0, 0, types.ErrTooManyObservations.Wrapf("%d observations, at most %d",
			count, params.MaxObservationsPerSubmission)
	}
	height := ctx.BlockHeight()
	if last, ok := k.GetLastSubmissionHeight(ctx, operator); ok && last == height {
		return 0, 0, types.ErrRateLimited.Wrapf("validator %s at height %d", operator, height)
	}
	k.SetLastSubmissionHeight(ctx, operator, height)

	var accepted, refused uint32
	for _, observation := range msg.Observations {
		reason, record, err := k.checkObservation(ctx, params, operator, observation)
		if err != nil {
			return 0, 0, err
		}
		if reason != "" {
			refused++
			emitRefused(ctx, operator, observation, reason)
			continue
		}
		accepted++
		k.set(ctx, types.ObservationKey(observation.MarketId, operator), record)
		emitAccepted(ctx, operator, observation.MarketId, record)
	}
	return accepted, refused, nil
}

// GetLastSubmissionHeight returns the height stored under key 0x06 for a
// validator and whether one is stored.
func (k Keeper) GetLastSubmissionHeight(ctx sdk.Context, operator sdk.ValAddress) (int64, bool) {
	encoded := ctx.KVStore(k.storeKey).Get(types.LastSubmitKey(operator))
	if encoded == nil {
		return 0, false
	}
	return int64(binary.BigEndian.Uint64(encoded)), true
}

// SetLastSubmissionHeight stores height under key 0x06 for a validator, 8
// bytes big-endian.
func (k Keeper) SetLastSubmissionHeight(ctx sdk.Context, operator sdk.ValAddress, height int64) {
	encoded := make([]byte, 8)
	binary.BigEndian.PutUint64(encoded, uint64(height))
	ctx.KVStore(k.storeKey).Set(types.LastSubmitKey(operator), encoded)
}
