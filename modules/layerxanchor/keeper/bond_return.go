package keeper

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxanchor/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

func (k Keeper) ReturnAllBonds(ctx sdk.Context) (sdk.Int, error) {
	before := k.ModuleBalance(ctx)
	cached, write := ctx.CacheContext()
	total := sdk.ZeroInt()
	refund := func(recipient string, amount sdk.Int) error {
		if amount.IsNil() || amount.IsNegative() {
			return types.ErrBond.Wrap("refund amount must not be nil or negative")
		}
		address, err := sdk.AccAddressFromBech32(recipient)
		if err != nil {
			return types.ErrBond.Wrapf("refund recipient: %s", err)
		}
		remaining := before.Sub(total)
		if amount.GT(remaining) {
			return types.ErrBond.Wrapf("refund amount %s exceeds remaining module balance %s", amount, remaining)
		}
		if amount.IsZero() {
			return nil
		}
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(cached, types.ModuleName, address, k.coins(cached, amount)); err != nil {
			return err
		}
		total = total.Add(amount)
		return nil
	}

	for _, guarantor := range k.GetGuarantors(cached) {
		if err := refund(guarantor.Operator, guarantor.Bond); err != nil {
			return sdk.ZeroInt(), err
		}
		guarantor.Bond = sdk.ZeroInt()
		k.setGuarantor(cached, guarantor)
	}
	for _, entry := range k.GetUnbondings(cached) {
		if err := refund(entry.Operator, entry.Amount); err != nil {
			return sdk.ZeroInt(), err
		}
		cached.KVStore(k.storeKey).Delete(types.UnbondingKey(entry.ID))
	}
	for _, challenge := range k.GetChallenges(cached) {
		if challenge.Status != types.ChallengeOpen {
			continue
		}
		if err := refund(challenge.Challenger, challenge.Bond); err != nil {
			return sdk.ZeroInt(), err
		}
		challenge.Status = types.ChallengeUpheld
		challenge.ResolvedHeight = ctx.BlockHeight()
		k.set(cached, types.ChallengeKey(challenge.ID), challenge)
		checkpoint, present := k.GetCheckpoint(cached, challenge.BatchNumber)
		if present && checkpoint.CheckpointID == challenge.CheckpointID && checkpoint.OpenChallenges > 0 {
			checkpoint.OpenChallenges--
			k.setCheckpoint(cached, checkpoint)
		}
	}
	if !total.Equal(before) {
		return sdk.ZeroInt(), types.ErrBond.Wrapf("returned %s but module balance before refunds was %s", total, before)
	}
	if after := k.ModuleBalance(cached); !after.IsZero() {
		return sdk.ZeroInt(), types.ErrBond.Wrapf("module balance after refunds is %s", after)
	}
	write()
	ctx.EventManager().EmitEvents(cached.EventManager().Events())
	return total, nil
}
