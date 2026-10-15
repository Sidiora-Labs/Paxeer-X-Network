package keeper

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// TransferGuardian is SidioraPool.transferGuardian: the guardian proposes
// next, which takes the role only by accepting it. A caller other than the
// guardian is refused Unauthorized() before a zero next is refused
// ZeroAddress().
func (k *Keeper) TransferGuardian(ctx sdk.Context, caller sdk.AccAddress, denom string, next sdk.AccAddress) error {
	market, err := k.mustMarket(ctx, denom)
	if err != nil {
		return err
	}
	if caller.Empty() || market.Guardian != caller.String() {
		return types.ErrNotGuardian
	}
	if next.Empty() {
		return types.ErrZeroAddress
	}
	market.PendingGuardian = next.String()
	k.setMarket(ctx, market)
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeGuardianTransferStarted,
		sdk.NewAttribute(types.AttributeKeyDenom, denom),
		sdk.NewAttribute(types.AttributeKeyGuardian, market.Guardian),
		sdk.NewAttribute(types.AttributeKeyPendingGuardian, market.PendingGuardian)))
	return nil
}

// AcceptGuardian is SidioraPool.acceptGuardian: the pending guardian becomes
// the guardian and the pending guardian is cleared. Any other caller, and
// every caller while no transfer is pending, is refused Unauthorized().
func (k *Keeper) AcceptGuardian(ctx sdk.Context, caller sdk.AccAddress, denom string) error {
	market, err := k.mustMarket(ctx, denom)
	if err != nil {
		return err
	}
	if caller.Empty() || market.PendingGuardian != caller.String() {
		return types.ErrNotGuardian
	}
	previous := market.Guardian
	market.Guardian = market.PendingGuardian
	market.PendingGuardian = ""
	k.setMarket(ctx, market)
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeGuardianTransferred,
		sdk.NewAttribute(types.AttributeKeyDenom, denom),
		sdk.NewAttribute(types.AttributeKeyPreviousGuardian, previous),
		sdk.NewAttribute(types.AttributeKeyGuardian, market.Guardian)))
	return nil
}
