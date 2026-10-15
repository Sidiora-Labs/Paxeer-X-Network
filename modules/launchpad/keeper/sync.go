package keeper

import (
	"fmt"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

// SyncReserves is SidioraPool.syncReserves under the pool lock held in locks:
// the pool address is swept into HeldQuote(pool), then realUsdlBalance is set
// to HeldQuote(pool) and tokenReserve to token.balanceOf(pool)
// (Launch-x:contracts/contracts/core/SidioraPool.sol:279-291). It runs while
// the pool is paused, as the contract does, and returns the context the run
// continues on and the market as written.
func (k *Keeper) SyncReserves(ctx sdk.Context, caller NestedCaller, locks TransientStore,
	denom string) (sdk.Context, types.Market, error) {
	market, err := k.mustMarket(ctx, denom)
	if err != nil {
		return ctx, types.Market{}, err
	}
	if market.Generation != types.Generation1 && market.Generation != types.Generation2 {
		return ctx, types.Market{}, fmt.Errorf("%w: market %s has no pool", types.ErrInvalidMarket, denom)
	}
	pool := common.HexToAddress(market.PoolAddress)
	release, err := k.EnterLock(locks, PoolLock(pool))
	if err != nil {
		return ctx, types.Market{}, err
	}
	defer release()
	ctx, _, err = k.SweepPool(ctx, caller, pool)
	if err != nil {
		return ctx, types.Market{}, err
	}
	held, err := k.GetQuoteLedger(ctx, HeldQuoteLedger(pool))
	if err != nil {
		return ctx, types.Market{}, err
	}
	tokens, err := k.quoteBalance(ctx, caller, common.HexToAddress(market.TokenAddress), pool)
	if err != nil {
		return ctx, types.Market{}, err
	}
	market.RealQuoteBalance = held
	market.TokenReserve = tokens
	price, err := market.Price()
	if err != nil {
		return ctx, types.Market{}, err
	}
	k.setMarket(ctx, market)
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypePoolStateUpdated,
		sdk.NewAttribute(types.AttributeKeyDenom, denom),
		sdk.NewAttribute(AttributeKeyPool, market.PoolAddress),
		sdk.NewAttribute(types.AttributeKeyVirtualQuoteReserve, market.VirtualQuoteReserve.String()),
		sdk.NewAttribute(types.AttributeKeyRealQuoteBalance, held.String()),
		sdk.NewAttribute(types.AttributeKeyTokenReserve, tokens.String()),
		sdk.NewAttribute(types.AttributeKeyPrice, price.String())))
	return ctx, market, nil
}
