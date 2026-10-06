package keeper

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

type msgServer struct {
	keeper Keeper
}

// NewMsgServerImpl returns the Msg service over the keeper. Every message is
// executed by the keeper method that carries it, which refuses any authority
// but the module's.
func NewMsgServerImpl(k Keeper) types.MsgServer { return msgServer{keeper: k} }

var _ types.MsgServer = msgServer{}

func (s msgServer) RegisterAttestor(goCtx context.Context, msg *types.MsgRegisterAttestor) (*types.MsgRegisterAttestorResponse, error) {
	if err := s.keeper.RegisterAttestor(sdk.UnwrapSDKContext(goCtx), *msg); err != nil {
		return nil, err
	}
	return &types.MsgRegisterAttestorResponse{}, nil
}

func (s msgServer) RemoveAttestor(goCtx context.Context, msg *types.MsgRemoveAttestor) (*types.MsgRemoveAttestorResponse, error) {
	if err := s.keeper.RemoveAttestor(sdk.UnwrapSDKContext(goCtx), *msg); err != nil {
		return nil, err
	}
	return &types.MsgRemoveAttestorResponse{}, nil
}

func (s msgServer) SetThreshold(goCtx context.Context, msg *types.MsgSetThreshold) (*types.MsgSetThresholdResponse, error) {
	if err := s.keeper.SetThreshold(sdk.UnwrapSDKContext(goCtx), *msg); err != nil {
		return nil, err
	}
	return &types.MsgSetThresholdResponse{}, nil
}

func (s msgServer) SetParams(goCtx context.Context, msg *types.MsgSetParams) (*types.MsgSetParamsResponse, error) {
	if err := s.keeper.UpdateParams(sdk.UnwrapSDKContext(goCtx), *msg); err != nil {
		return nil, err
	}
	return &types.MsgSetParamsResponse{}, nil
}

func (s msgServer) Pause(goCtx context.Context, msg *types.MsgPause) (*types.MsgPauseResponse, error) {
	if err := s.keeper.Pause(sdk.UnwrapSDKContext(goCtx), *msg); err != nil {
		return nil, err
	}
	return &types.MsgPauseResponse{}, nil
}

func (s msgServer) Unpause(goCtx context.Context, msg *types.MsgUnpause) (*types.MsgUnpauseResponse, error) {
	if err := s.keeper.Unpause(sdk.UnwrapSDKContext(goCtx), *msg); err != nil {
		return nil, err
	}
	return &types.MsgUnpauseResponse{}, nil
}

// RegisterAttestor adds an attestor with its payout account and, when given,
// the public key credential envelopes are sealed to. The set is kept
// in ascending signer order, and the threshold is raised to the majority of
// the new set when it falls below it.
func (k Keeper) RegisterAttestor(ctx sdk.Context, msg types.MsgRegisterAttestor) error {
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	if err := k.requireAuthority(ctx, msg.Authority); err != nil {
		return err
	}
	set := k.GetAttestorSet(ctx)
	if set.Has(msg.Attestor.Signer) {
		return types.ErrInvalidAttestors.Wrapf("%s is already registered", msg.Attestor.Signer.Hex())
	}
	if len(set.Attestors) >= types.MaxAttestors {
		return types.ErrInvalidAttestors.Wrapf("set is full at %d attestors", types.MaxAttestors)
	}
	set.Attestors = append(set.Attestors, msg.Attestor)
	sort.Slice(set.Attestors, func(i, j int) bool {
		return bytes.Compare(set.Attestors[i].Signer[:], set.Attestors[j].Signer[:]) < 0
	})
	if majority := types.Majority(len(set.Attestors)); set.Threshold < majority {
		set.Threshold = majority
	}
	if err := set.Validate(); err != nil {
		return err
	}
	k.setAttestorSet(ctx, set)
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventAttestorAdded,
		sdk.NewAttribute(types.AttributeSigner, msg.Attestor.Signer.Hex()),
		sdk.NewAttribute(types.AttributePayout, msg.Attestor.Payout),
		sdk.NewAttribute(types.AttributePublicKey, hex.EncodeToString(msg.Attestor.PublicKey)),
		sdk.NewAttribute(types.AttributeThreshold, fmt.Sprint(set.Threshold))))
	return nil
}

// RemoveAttestor removes a registered attestor. A threshold above the new set
// size drops to the set size, which is still a majority; the empty set has
// threshold zero.
func (k Keeper) RemoveAttestor(ctx sdk.Context, msg types.MsgRemoveAttestor) error {
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	if err := k.requireAuthority(ctx, msg.Authority); err != nil {
		return err
	}
	set := k.GetAttestorSet(ctx)
	if !set.Has(msg.Signer) {
		return types.ErrUnknownAttestor.Wrap(msg.Signer.Hex())
	}
	remaining := make([]types.Attestor, 0, len(set.Attestors)-1)
	for _, attestor := range set.Attestors {
		if attestor.Signer != msg.Signer {
			remaining = append(remaining, attestor)
		}
	}
	set.Attestors = remaining
	if int(set.Threshold) > len(remaining) {
		set.Threshold = uint32(len(remaining))
	}
	if err := set.Validate(); err != nil {
		return err
	}
	k.setAttestorSet(ctx, set)
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventAttestorRemoved,
		sdk.NewAttribute(types.AttributeSigner, msg.Signer.Hex()),
		sdk.NewAttribute(types.AttributeThreshold, fmt.Sprint(set.Threshold))))
	return nil
}

// SetThreshold sets the threshold, refusing one at or below half the set or
// above it.
func (k Keeper) SetThreshold(ctx sdk.Context, msg types.MsgSetThreshold) error {
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	if err := k.requireAuthority(ctx, msg.Authority); err != nil {
		return err
	}
	set := k.GetAttestorSet(ctx)
	if !types.ValidThreshold(msg.Threshold, len(set.Attestors)) {
		return types.ErrInvalidThreshold.Wrapf("threshold %d of %d attestors", msg.Threshold, len(set.Attestors))
	}
	set.Threshold = msg.Threshold
	k.setAttestorSet(ctx, set)
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventThresholdSet,
		sdk.NewAttribute(types.AttributeThreshold, fmt.Sprint(msg.Threshold))))
	return nil
}

// UpdateParams sets the fee, the payload and callback caps and the timeout.
// The authority is unchanged. Pending requests keep the fee and timeout
// height they were stored with.
func (k Keeper) UpdateParams(ctx sdk.Context, msg types.MsgSetParams) error {
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	if err := k.requireAuthority(ctx, msg.Authority); err != nil {
		return err
	}
	params := k.GetParams(ctx)
	params.Fee = msg.Fee
	params.MaxPayloadBytes = msg.MaxPayloadBytes
	params.MaxCallbackGas = msg.MaxCallbackGas
	params.TimeoutBlocks = msg.TimeoutBlocks
	if err := k.SetParams(ctx, params); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventParamsSet,
		sdk.NewAttribute(types.AttributeFee, msg.Fee.String()),
		sdk.NewAttribute(types.AttributePayloadCap, fmt.Sprint(msg.MaxPayloadBytes)),
		sdk.NewAttribute(types.AttributeCallbackCap, fmt.Sprint(msg.MaxCallbackGas)),
		sdk.NewAttribute(types.AttributeTimeoutBlocks, fmt.Sprint(msg.TimeoutBlocks))))
	return nil
}

// Pause stops every request and fulfilment; refunds stay open.
func (k Keeper) Pause(ctx sdk.Context, msg types.MsgPause) error {
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	if err := k.requireAuthority(ctx, msg.Authority); err != nil {
		return err
	}
	k.setPaused(ctx, true)
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventPaused))
	return nil
}

func (k Keeper) Unpause(ctx sdk.Context, msg types.MsgUnpause) error {
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	if err := k.requireAuthority(ctx, msg.Authority); err != nil {
		return err
	}
	k.setPaused(ctx, false)
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventUnpaused))
	return nil
}
