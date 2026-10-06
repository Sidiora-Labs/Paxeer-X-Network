package keeper

import (
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

func (k *Keeper) GetBalance(ctx sdk.Context, addr sdk.AccAddress) *big.Int {
	denom := k.GetBaseDenom(ctx)
	allUhpx := k.BankKeeper().GetBalance(ctx, addr, denom).Amount
	lockedUhpx := k.BankKeeper().LockedCoins(ctx, addr).AmountOf(denom) // LockedCoins doesn't use iterators
	uhpx := allUhpx.Sub(lockedUhpx)
	wei := k.BankKeeper().GetWeiBalance(ctx, addr)
	return uhpx.Mul(state.SdkUhpxToSweiMultiplier).Add(wei).BigInt()
}
