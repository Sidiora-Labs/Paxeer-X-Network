package keeper

import (
	"fmt"
	"strconv"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

func (k *Keeper) requireFeeRightsHolder(ctx sdk.Context, caller sdk.AccAddress, denom string) (types.Market, error) {
	market, err := k.mustMarket(ctx, denom)
	if err != nil {
		return types.Market{}, err
	}
	if market.FeeRightsHolder != caller.String() {
		return types.Market{}, types.ErrNotFeeRightsHolder
	}
	return market, nil
}

func (k *Keeper) requireStrategy(ctx sdk.Context, caller sdk.AccAddress, denom string, strategy types.FeeStrategy) (types.Market, error) {
	market, err := k.requireFeeRightsHolder(ctx, caller, denom)
	if err != nil {
		return types.Market{}, err
	}
	if market.FeeStrategy != strategy {
		return types.Market{}, types.ErrWrongStrategy
	}
	return market, nil
}

// takeAccumulatedFees zeroes the market's pool-cut fees and returns them.
func (k *Keeper) takeAccumulatedFees(market *types.Market) (sdk.Int, error) {
	amount := market.AccumulatedFees
	if !amount.IsPositive() {
		return sdk.Int{}, types.ErrNoFeesAccumulated
	}
	market.AccumulatedFees = sdk.ZeroInt()
	return amount, nil
}

func (k *Keeper) payQuote(ctx sdk.Context, to sdk.AccAddress, amount sdk.Int) error {
	denom := k.GetParams(ctx).QuoteDenom
	return k.bankKeeper.SendCoins(ctx, k.ModuleAddress(), to, sdk.NewCoins(sdk.NewCoin(denom, amount)))
}

// SetFeeStrategy is FeesRouter.setFeeStrategy.
func (k *Keeper) SetFeeStrategy(ctx sdk.Context, caller sdk.AccAddress, denom string, strategy types.FeeStrategy) error {
	market, err := k.requireFeeRightsHolder(ctx, caller, denom)
	if err != nil {
		return err
	}
	if err := strategy.Validate(); err != nil {
		return err
	}
	old := market.FeeStrategy
	market.FeeStrategy = strategy
	k.setMarket(ctx, market)
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeFeeStrategyChanged,
		sdk.NewAttribute(types.AttributeKeyDenom, denom),
		sdk.NewAttribute("old_strategy", strconv.FormatUint(uint64(old), 10)),
		sdk.NewAttribute(types.AttributeKeyStrategy, strconv.FormatUint(uint64(strategy), 10))))
	return nil
}

// ClaimFees is FeesRouter.claimFees: the CLAIM strategy pays the market's
// accumulated fees to recipient.
func (k *Keeper) ClaimFees(ctx sdk.Context, caller sdk.AccAddress, denom string, recipient sdk.AccAddress) (sdk.Int, error) {
	market, err := k.requireStrategy(ctx, caller, denom, types.FeeStrategyClaim)
	if err != nil {
		return sdk.Int{}, err
	}
	if recipient.Empty() {
		return sdk.Int{}, types.ErrZeroAddress
	}
	amount, err := k.takeAccumulatedFees(&market)
	if err != nil {
		return sdk.Int{}, err
	}
	k.setMarket(ctx, market)
	if err := k.payQuote(ctx, recipient, amount); err != nil {
		return sdk.Int{}, err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeFeesClaimed,
		sdk.NewAttribute(types.AttributeKeyDenom, denom),
		sdk.NewAttribute(types.AttributeKeyRecipient, recipient.String()),
		sdk.NewAttribute(types.AttributeKeyAmount, amount.String())))
	return amount, nil
}

// DeadAccount is the bank account of 0x…dEaD, where burned fees go.
func (k *Keeper) DeadAccount(ctx sdk.Context) sdk.AccAddress {
	return k.evmKeeper.GetPaxAddressOrDefault(ctx, common.HexToAddress(types.DeadAddress))
}

// ExecuteBurn is FeesRouter.executeBurn: the BURN strategy sends the
// accumulated fees to 0x…dEaD.
func (k *Keeper) ExecuteBurn(ctx sdk.Context, caller sdk.AccAddress, denom string) (sdk.Int, error) {
	market, err := k.requireStrategy(ctx, caller, denom, types.FeeStrategyBurn)
	if err != nil {
		return sdk.Int{}, err
	}
	amount, err := k.takeAccumulatedFees(&market)
	if err != nil {
		return sdk.Int{}, err
	}
	k.setMarket(ctx, market)
	if err := k.payQuote(ctx, k.DeadAccount(ctx), amount); err != nil {
		return sdk.Int{}, err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeFeesBurned,
		sdk.NewAttribute(types.AttributeKeyDenom, denom),
		sdk.NewAttribute(types.AttributeKeyAmount, amount.String())))
	return amount, nil
}

// ExecuteAirdrop is FeesRouter.executeAirdrop: the AIRDROP strategy opens a
// new epoch holding the accumulated fees for token holders to claim. The
// epoch's holder history, supply and payout denomination are fixed in the
// same write set, so later mints, burns and transfers cannot change it.
func (k *Keeper) ExecuteAirdrop(ctx sdk.Context, caller sdk.AccAddress, denom string) (sdk.Int, error) {
	market, err := k.requireStrategy(ctx, caller, denom, types.FeeStrategyAirdrop)
	if err != nil {
		return sdk.Int{}, err
	}
	amount, err := k.takeAccumulatedFees(&market)
	if err != nil {
		return sdk.Int{}, err
	}
	supply := k.bankKeeper.GetSupply(ctx, denom).Amount
	if !supply.IsPositive() {
		return sdk.Int{}, types.ErrZeroAmount
	}
	cacheCtx, write := ctx.CacheContext()
	market.AirdropEpoch++
	market.AirdropBalance = market.AirdropBalance.Add(amount)
	k.setMarket(cacheCtx, market)
	k.setAirdropEpochAmount(cacheCtx, denom, market.AirdropEpoch, amount)
	if err := k.OpenHoldingHistory(cacheCtx, denom, market.AirdropEpoch); err != nil {
		return sdk.Int{}, err
	}
	k.setAirdropBasis(cacheCtx, types.AirdropEpochBasis{Denom: denom, Epoch: market.AirdropEpoch, Supply: supply,
		PayoutDenom: k.GetParams(ctx).QuoteDenom, Height: ctx.BlockHeight(), Paid: sdk.ZeroInt()})
	write()
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeAirdropTriggered,
		sdk.NewAttribute(types.AttributeKeyDenom, denom),
		sdk.NewAttribute(types.AttributeKeyAmount, amount.String()),
		sdk.NewAttribute(types.AttributeKeyEpoch, strconv.FormatUint(market.AirdropEpoch, 10))))
	return amount, nil
}

// ClaimAirdrop is FeeAccumulator.claimAirdrop: any holder takes its
// entitlement of the current epoch once.
func (k *Keeper) ClaimAirdrop(ctx sdk.Context, holder sdk.AccAddress, denom string) (sdk.Int, error) {
	market, err := k.mustMarket(ctx, denom)
	if err != nil {
		return sdk.Int{}, err
	}
	if market.AirdropEpoch == 0 {
		return sdk.Int{}, types.ErrAirdropNotTriggered
	}
	return k.ClaimAirdropForEpoch(ctx, holder, denom, market.AirdropEpoch)
}

// AirdropEntitlement is floor(epochAmount*balance/supply) over holder's
// balance and the supply at epoch's boundary.
func (k *Keeper) AirdropEntitlement(ctx sdk.Context, holder sdk.AccAddress, denom string, epoch uint64) (sdk.Int, types.AirdropEpochBasis, error) {
	market, err := k.mustMarket(ctx, denom)
	if err != nil {
		return sdk.Int{}, types.AirdropEpochBasis{}, err
	}
	if epoch == 0 || epoch > market.AirdropEpoch {
		return sdk.Int{}, types.AirdropEpochBasis{}, fmt.Errorf("%w: %s/%d", types.ErrInvalidAirdropEpoch, denom, epoch)
	}
	basis, found, err := k.GetAirdropBasis(ctx, denom, epoch)
	if err != nil {
		return sdk.Int{}, types.AirdropEpochBasis{}, err
	}
	if !found {
		return sdk.Int{}, types.AirdropEpochBasis{}, fmt.Errorf("%w: %s/%d", types.ErrLegacyAirdropEpoch, denom, epoch)
	}
	epochAmount := k.GetAirdropEpochAmount(ctx, denom, epoch)
	if !epochAmount.IsPositive() {
		return sdk.Int{}, types.AirdropEpochBasis{}, types.ErrNoFeesAccumulated
	}
	balance, err := k.EpochBalance(ctx, denom, holder, epoch)
	if err != nil {
		return sdk.Int{}, types.AirdropEpochBasis{}, err
	}
	if !balance.IsPositive() {
		return sdk.Int{}, types.AirdropEpochBasis{}, types.ErrZeroAmount
	}
	product, err := types.MulDiv(epochAmount.BigInt(), balance.BigInt(), basis.Supply.BigInt())
	if err != nil {
		return sdk.Int{}, types.AirdropEpochBasis{}, err
	}
	amount := sdk.NewIntFromBigInt(product)
	if !amount.IsPositive() {
		return sdk.Int{}, types.AirdropEpochBasis{}, types.ErrZeroAmount
	}
	if basis.Paid.Add(amount).GT(epochAmount) {
		return sdk.Int{}, types.AirdropEpochBasis{}, types.ErrOverflow
	}
	return amount, basis, nil
}

// ClaimAirdropForEpoch pays holder its entitlement of one opened epoch. The
// claim marker, paid total, market balance and payment commit together or
// not at all.
func (k *Keeper) ClaimAirdropForEpoch(ctx sdk.Context, holder sdk.AccAddress, denom string, epoch uint64) (sdk.Int, error) {
	market, err := k.mustMarket(ctx, denom)
	if err != nil {
		return sdk.Int{}, err
	}
	if epoch == 0 || epoch > market.AirdropEpoch {
		return sdk.Int{}, fmt.Errorf("%w: %s/%d", types.ErrInvalidAirdropEpoch, denom, epoch)
	}
	if k.HasClaimedAirdrop(ctx, denom, holder, epoch) {
		return sdk.Int{}, types.ErrAlreadyClaimed
	}
	amount, basis, err := k.AirdropEntitlement(ctx, holder, denom, epoch)
	if err != nil {
		return sdk.Int{}, err
	}
	if amount.GT(market.AirdropBalance) {
		return sdk.Int{}, types.ErrOverflow
	}
	cacheCtx, write := ctx.CacheContext()
	k.setAirdropClaimed(cacheCtx, denom, holder, epoch)
	basis.Paid = basis.Paid.Add(amount)
	k.setAirdropBasis(cacheCtx, basis)
	market.AirdropBalance = market.AirdropBalance.Sub(amount)
	k.setMarket(cacheCtx, market)
	if err := k.bankKeeper.SendCoins(cacheCtx, k.ModuleAddress(), holder,
		sdk.NewCoins(sdk.NewCoin(basis.PayoutDenom, amount))); err != nil {
		return sdk.Int{}, err
	}
	write()
	ctx.EventManager().EmitEvents(cacheCtx.EventManager().Events())
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeAirdropClaimed,
		sdk.NewAttribute(types.AttributeKeyDenom, denom),
		sdk.NewAttribute(types.AttributeKeyHolder, holder.String()),
		sdk.NewAttribute(types.AttributeKeyAmount, amount.String()),
		sdk.NewAttribute(types.AttributeKeyEpoch, strconv.FormatUint(epoch, 10))))
	return amount, nil
}

// ExecuteLpRewards is FeesRouter.executeLpRewards: the LP_REWARDS strategy
// returns the accumulated fees to the curve's real quote balance, which is
// what the pool's syncReserves picks up.
func (k *Keeper) ExecuteLpRewards(ctx sdk.Context, caller sdk.AccAddress, denom string) (sdk.Int, error) {
	market, err := k.requireStrategy(ctx, caller, denom, types.FeeStrategyLpRewards)
	if err != nil {
		return sdk.Int{}, err
	}
	amount, err := k.takeAccumulatedFees(&market)
	if err != nil {
		return sdk.Int{}, err
	}
	market.RealQuoteBalance = market.RealQuoteBalance.Add(amount)
	k.setMarket(ctx, market)
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeLpRewardsSent,
		sdk.NewAttribute(types.AttributeKeyDenom, denom),
		sdk.NewAttribute(types.AttributeKeyAmount, amount.String())))
	return amount, nil
}

// Pause is SidioraPool.pause: the guardian halts swaps.
func (k *Keeper) Pause(ctx sdk.Context, caller sdk.AccAddress, denom string) error {
	return k.setPaused(ctx, caller, denom, true)
}

// Unpause is SidioraPool.unpause.
func (k *Keeper) Unpause(ctx sdk.Context, caller sdk.AccAddress, denom string) error {
	return k.setPaused(ctx, caller, denom, false)
}

func (k *Keeper) setPaused(ctx sdk.Context, caller sdk.AccAddress, denom string, paused bool) error {
	market, err := k.mustMarket(ctx, denom)
	if err != nil {
		return err
	}
	if market.Guardian != caller.String() {
		return types.ErrNotGuardian
	}
	if market.Paused == paused {
		if paused {
			return types.ErrPaused
		}
		return types.ErrNotPaused
	}
	market.Paused = paused
	k.setMarket(ctx, market)
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypePauseToggled,
		sdk.NewAttribute(types.AttributeKeyDenom, denom),
		sdk.NewAttribute(types.AttributeKeyPaused, strconv.FormatBool(paused))))
	return nil
}
