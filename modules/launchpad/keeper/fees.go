package keeper

import (
	"bytes"
	"fmt"
	"math/big"
	"strconv"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	// totalLiabilitiesKey holds FeeAccumulator.totalLiabilities: the pool
	// fees, airdrop balances and optical surpluses FeeHeld owes.
	totalLiabilitiesKey = []byte{0x49}
	// opticalSurplusPrefix keys FeeAccumulator._opticalSurplus of one pool.
	opticalSurplusPrefix = []byte{0x4a}
	lpRewardsPrefix      = []byte{0x4b} // pool -> FeeAccumulator._lpRewardsBalance

	opticalGetFlags              = crypto.Keccak256([]byte("getFlags()"))[:4]
	opticalBeforeFeeDistribution = crypto.Keccak256([]byte("beforeFeeDistribution(address,uint256)"))[:4]
	opticalAfterFeeDistribution  = crypto.Keccak256([]byte("afterFeeDistribution(address,uint256)"))[:4]
)

const (
	// BitFlag.BEFORE_FEE_DISTRIBUTION and BitFlag.AFTER_FEE_DISTRIBUTION.
	opticalFlagBeforeFeeDistribution = 1 << 2
	opticalFlagAfterFeeDistribution  = 1 << 3

	// EventTypeDeposited is Treasury.Deposited(token, depositor, amount).
	EventTypeDeposited = "launchpad_treasury_deposited"
	// EventTypeOpticalSurplusRecorded is
	// FeeAccumulator.OpticalSurplusRecorded(pool, amount).
	EventTypeOpticalSurplusRecorded = "launchpad_optical_surplus_recorded"
	// EventTypeOpticalSurplusClaimed is
	// FeeAccumulator.OpticalSurplusClaimed(pool, recipient, amount).
	EventTypeOpticalSurplusClaimed = "launchpad_optical_surplus_claimed"

	AttributeKeyContract  = "contract"
	AttributeKeyToken     = "token"
	AttributeKeyDepositor = "depositor"
)

func opticalSurplusKey(pool common.Address) []byte {
	return append(append([]byte(nil), opticalSurplusPrefix...), pool.Bytes()...)
}

// feeHeld is the FeeHeld ledger of the generation 2 FeeAccumulator, which
// serves the fees of every market.
func (k *Keeper) feeHeld() QuoteLedger { return FeeHeldLedger(k.generation2.FeeAccumulator) }

// TotalLiabilities is FeeAccumulator.totalLiabilities.
func (k *Keeper) TotalLiabilities(ctx sdk.Context) (sdk.Int, error) {
	return k.getLedgerAmount(ctx, totalLiabilitiesKey)
}

func lpRewardsKey(pool common.Address) []byte {
	return append(append([]byte(nil), lpRewardsPrefix...), pool.Bytes()...)
}

// LpRewardsBalance is FeeAccumulator.getLpRewardsBalance(pool): the
// cumulative LP rewards sent to pool.
func (k *Keeper) LpRewardsBalance(ctx sdk.Context, pool common.Address) (sdk.Int, error) {
	return k.getLedgerAmount(ctx, lpRewardsKey(pool))
}

// OpticalSurplus is FeeAccumulator.getOpticalSurplus(pool).
func (k *Keeper) OpticalSurplus(ctx sdk.Context, pool common.Address) (sdk.Int, error) {
	return k.getLedgerAmount(ctx, opticalSurplusKey(pool))
}

// requireBacking is FeeAccumulator._requireBacking: FeeHeld must cover
// totalLiabilities plus amount, or InsufficientSurplus().
func (k *Keeper) requireBacking(ctx sdk.Context, amount sdk.Int) error {
	held, err := k.GetQuoteLedger(ctx, k.feeHeld())
	if err != nil {
		return err
	}
	liabilities, err := k.TotalLiabilities(ctx)
	if err != nil {
		return err
	}
	if held.LT(liabilities.Add(amount)) {
		return fmt.Errorf("%w: fee held %s, liabilities %s, new %s", types.ErrInsufficientSurplus, held, liabilities,
			amount)
	}
	return nil
}

// requireFeeRightsHolder is the FeesRouter gate: caller holds the market's
// fee right in the fee-right store and the right carries strategy.
func (k *Keeper) requireFeeRightsHolder(ctx sdk.Context, caller sdk.AccAddress, denom string,
	strategy types.FeeStrategy) (types.Market, error) {
	market, _, err := k.RequireMarketFeeRight(ctx, k.evmKeeper.GetEVMAddressOrDefault(ctx, caller), denom, strategy)
	return market, err
}

// takeAccumulatedFees zeroes the market's pool-cut fees and returns them.
func (k *Keeper) takeAccumulatedFees(market *types.Market) (sdk.Int, error) {
	amount := market.AccumulatedFees
	if amount.IsNil() || !amount.IsPositive() {
		return sdk.Int{}, types.ErrNoFeesAccumulated
	}
	market.AccumulatedFees = sdk.ZeroInt()
	return amount, nil
}

// optical is the market's optical, the zero address when it has none.
func optical(market types.Market) common.Address {
	if !common.IsHexAddress(market.Optical) {
		return common.Address{}
	}
	return common.HexToAddress(market.Optical)
}

// opticalFlags is IOpticalFee(optical).getFlags().
func (k *Keeper) opticalFlags(ctx sdk.Context, caller NestedCaller, optical common.Address) (uint8, error) {
	ret, err := caller.StaticCall(ctx, k.generation2.FeeAccumulator, optical, erc20Call(opticalGetFlags))
	if err != nil {
		return 0, fmt.Errorf("%w: getFlags %s: %w", types.ErrInvalidHookResponse, optical.Hex(), err)
	}
	if len(ret) != 32 || new(big.Int).SetBytes(ret).Cmp(big.NewInt(255)) > 0 {
		return 0, fmt.Errorf("%w: getFlags %s returned %x", types.ErrInvalidHookResponse, optical.Hex(), ret)
	}
	return ret[31], nil
}

// opticalHook calls one fee hook of optical with (pool, amount) from the
// generation 2 FeeAccumulator and returns its 32-byte return word.
func (k *Keeper) opticalHook(ctx sdk.Context, caller NestedCaller, optical common.Address, selector []byte,
	pool common.Address, amount sdk.Int) (sdk.Context, []byte, error) {
	next, ret, err := caller.Call(ctx, k.generation2.FeeAccumulator, optical,
		erc20Call(selector, addressWord(pool), amountWord(amount.BigInt())))
	if err != nil {
		return ctx, nil, fmt.Errorf("%w: optical %s hook %x: %w", types.ErrInvalidHookResponse, optical.Hex(), selector,
			err)
	}
	if len(ret) != 32 {
		return ctx, nil, fmt.Errorf("%w: optical %s hook %x returned %x", types.ErrInvalidHookResponse, optical.Hex(),
			selector, ret)
	}
	return next, ret, nil
}

// recordSurplus credits amount to the optical surplus of pool.
func (k *Keeper) recordSurplus(ctx sdk.Context, pool common.Address, amount sdk.Int) error {
	if err := k.addLedgerAmount(ctx, opticalSurplusKey(pool), amount); err != nil {
		return err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(EventTypeOpticalSurplusRecorded,
		sdk.NewAttribute(AttributeKeyContract, k.generation2.FeeAccumulator.Hex()),
		sdk.NewAttribute(AttributeKeyPool, pool.Hex()),
		sdk.NewAttribute(types.AttributeKeyAmount, amount.String())))
	return nil
}

// RecordFee is FeeAccumulator.recordFee for denom's pool: fee, already in
// FeeHeld, becomes a liability and splits into the protocol cut
// fee*protocolFeeBps/10000 and the pool cut. A beforeFeeDistribution optical
// may lower the pool cut, the difference becoming its surplus, and refuses
// InsufficientSurplus() when it raises it. The protocol cut moves from FeeHeld
// to TreasuryHeld and TreasuryLedger of the quote token and stops being a
// liability, with Deposited at the Treasury. An afterFeeDistribution optical
// must return its selector or the fee refuses Unauthorized(). It returns the
// protocol and pool cuts of FeeRecorded.
func (k *Keeper) RecordFee(ctx sdk.Context, caller NestedCaller, denom string, fee sdk.Int) (sdk.Context, sdk.Int,
	sdk.Int, error) {
	market, err := k.mustMarket(ctx, denom)
	if err != nil {
		return ctx, sdk.Int{}, sdk.Int{}, err
	}
	if err := positiveAmount(fee); err != nil {
		return ctx, sdk.Int{}, sdk.Int{}, err
	}
	if err := k.requireBacking(ctx, fee); err != nil {
		return ctx, sdk.Int{}, sdk.Int{}, err
	}
	if err := k.addLedgerAmount(ctx, totalLiabilitiesKey, fee); err != nil {
		return ctx, sdk.Int{}, sdk.Int{}, err
	}
	cut, err := types.FeeAmount(fee.BigInt(), new(big.Int).SetUint64(k.GetParams(ctx).ProtocolFeeBps))
	if err != nil {
		return ctx, sdk.Int{}, sdk.Int{}, err
	}
	protocolCut := sdk.NewIntFromBigInt(cut)
	poolCut := fee.Sub(protocolCut)
	pool := common.HexToAddress(market.PoolAddress)

	if hook := optical(market); hook != (common.Address{}) {
		flags, err := k.opticalFlags(ctx, caller, hook)
		if err != nil {
			return ctx, sdk.Int{}, sdk.Int{}, err
		}
		if flags&opticalFlagBeforeFeeDistribution != 0 {
			var ret []byte
			ctx, ret, err = k.opticalHook(ctx, caller, hook, opticalBeforeFeeDistribution, pool, poolCut)
			if err != nil {
				return ctx, sdk.Int{}, sdk.Int{}, err
			}
			adjusted := sdk.NewIntFromBigInt(new(big.Int).SetBytes(ret))
			if adjusted.GT(poolCut) {
				return ctx, sdk.Int{}, sdk.Int{}, fmt.Errorf("%w: optical %s raised the pool cut %s to %s",
					types.ErrInsufficientSurplus, hook.Hex(), poolCut, adjusted)
			}
			if adjusted.LT(poolCut) {
				if err := k.recordSurplus(ctx, pool, poolCut.Sub(adjusted)); err != nil {
					return ctx, sdk.Int{}, sdk.Int{}, err
				}
				poolCut = adjusted
			}
		}
	}

	// Reload the market: the hook may have reentered and changed it.
	market, err = k.mustMarket(ctx, denom)
	if err != nil {
		return ctx, sdk.Int{}, sdk.Int{}, err
	}
	accumulated := market.AccumulatedFees
	if accumulated.IsNil() {
		accumulated = sdk.ZeroInt()
	}
	market.AccumulatedFees = accumulated.Add(poolCut)
	if !uint256Amount(market.AccumulatedFees) {
		return ctx, sdk.Int{}, sdk.Int{}, types.ErrOverflow
	}
	k.setMarket(ctx, market)

	if protocolCut.IsPositive() {
		if err := k.MoveQuote(ctx, k.feeHeld(), TreasuryHeldLedger(), protocolCut); err != nil {
			return ctx, sdk.Int{}, sdk.Int{}, err
		}
		if err := k.TreasuryCreditFee(ctx, protocolCut); err != nil {
			return ctx, sdk.Int{}, sdk.Int{}, err
		}
		if err := k.subLedgerAmount(ctx, totalLiabilitiesKey, protocolCut); err != nil {
			return ctx, sdk.Int{}, sdk.Int{}, err
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent(EventTypeDeposited,
			sdk.NewAttribute(AttributeKeyContract, k.TreasuryContract().Hex()),
			sdk.NewAttribute(AttributeKeyToken, k.quoteToken(ctx).Hex()),
			sdk.NewAttribute(AttributeKeyDepositor, k.generation2.FeeAccumulator.Hex()),
			sdk.NewAttribute(types.AttributeKeyAmount, protocolCut.String())))
	}

	if hook := optical(market); hook != (common.Address{}) {
		flags, err := k.opticalFlags(ctx, caller, hook)
		if err != nil {
			return ctx, sdk.Int{}, sdk.Int{}, err
		}
		if flags&opticalFlagAfterFeeDistribution != 0 {
			var ret []byte
			ctx, ret, err = k.opticalHook(ctx, caller, hook, opticalAfterFeeDistribution, pool, poolCut)
			if err != nil {
				return ctx, sdk.Int{}, sdk.Int{}, err
			}
			if !bytes.Equal(ret, common.RightPadBytes(opticalAfterFeeDistribution, 32)) {
				return ctx, sdk.Int{}, sdk.Int{}, fmt.Errorf("%w: optical %s afterFeeDistribution returned %x",
					types.ErrUnauthorized, hook.Hex(), ret)
			}
		}
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeFeeRecorded,
		sdk.NewAttribute(AttributeKeyContract, k.generation2.FeeAccumulator.Hex()),
		sdk.NewAttribute(types.AttributeKeyDenom, denom),
		sdk.NewAttribute(AttributeKeyPool, pool.Hex()),
		sdk.NewAttribute(types.AttributeKeyFee, fee.String()),
		sdk.NewAttribute(types.AttributeKeyProtocolCut, protocolCut.String()),
		sdk.NewAttribute(types.AttributeKeyPoolCut, poolCut.String())))
	return ctx, protocolCut, poolCut, nil
}

// RecordOpticalSurplus is FeeAccumulator.recordOpticalSurplus for denom's
// pool: amount, already in FeeHeld, becomes a liability and the pool's
// optical surplus. A zero amount does nothing.
func (k *Keeper) RecordOpticalSurplus(ctx sdk.Context, denom string, amount sdk.Int) error {
	market, err := k.mustMarket(ctx, denom)
	if err != nil {
		return err
	}
	if !uint256Amount(amount) {
		return types.ErrOverflow
	}
	if amount.IsZero() {
		return nil
	}
	if err := k.requireBacking(ctx, amount); err != nil {
		return err
	}
	if err := k.addLedgerAmount(ctx, totalLiabilitiesKey, amount); err != nil {
		return err
	}
	return k.recordSurplus(ctx, common.HexToAddress(market.PoolAddress), amount)
}

// ClaimOpticalSurplus is FeeAccumulator.claimOpticalSurplus sent by sender:
// only the optical of denom's pool, a contract, may pay amount of the pool's
// surplus out of FeeHeld to recipient.
func (k *Keeper) ClaimOpticalSurplus(ctx sdk.Context, caller NestedCaller, sender common.Address, denom string,
	amount sdk.Int, recipient common.Address) (sdk.Context, error) {
	market, err := k.mustMarket(ctx, denom)
	if err != nil {
		return ctx, err
	}
	hook := optical(market)
	if hook == (common.Address{}) || hook != sender || len(k.evmKeeper.GetCode(ctx, sender)) == 0 {
		return ctx, types.ErrUnauthorized
	}
	if recipient == (common.Address{}) {
		return ctx, types.ErrZeroAddress
	}
	if err := positiveAmount(amount); err != nil {
		return ctx, err
	}
	pool := common.HexToAddress(market.PoolAddress)
	surplus, err := k.OpticalSurplus(ctx, pool)
	if err != nil {
		return ctx, err
	}
	if surplus.LT(amount) {
		return ctx, fmt.Errorf("%w: pool %s surplus %s, needs %s", types.ErrInsufficientSurplus, pool.Hex(), surplus,
			amount)
	}
	if err := k.setLedgerAmount(ctx, opticalSurplusKey(pool), surplus.Sub(amount)); err != nil {
		return ctx, err
	}
	if err := k.subLedgerAmount(ctx, totalLiabilitiesKey, amount); err != nil {
		return ctx, err
	}
	ctx, err = k.PayQuote(ctx, caller, k.feeHeld(), recipient, amount)
	if err != nil {
		return ctx, err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(EventTypeOpticalSurplusClaimed,
		sdk.NewAttribute(AttributeKeyContract, k.generation2.FeeAccumulator.Hex()),
		sdk.NewAttribute(AttributeKeyPool, pool.Hex()),
		sdk.NewAttribute(types.AttributeKeyRecipient, recipient.Hex()),
		sdk.NewAttribute(types.AttributeKeyAmount, amount.String())))
	return ctx, nil
}

// SetFeeStrategy is FeesRouter.setFeeStrategy: the holder of the market's fee
// right sets the strategy of that right. The market keeps a copy for its
// views.
func (k *Keeper) SetFeeStrategy(ctx sdk.Context, caller sdk.AccAddress, denom string, strategy types.FeeStrategy) error {
	market, err := k.mustMarket(ctx, denom)
	if err != nil {
		return err
	}
	if !common.IsHexAddress(market.FeeRightCollection) || market.FeeRightID == 0 {
		return types.ErrNotFeeRightsHolder
	}
	collection, holder := common.HexToAddress(market.FeeRightCollection), k.evmKeeper.GetEVMAddressOrDefault(ctx, caller)
	if _, err := k.RequireFeeRightHolder(ctx, collection, holder, market.FeeRightID); err != nil {
		return err
	}
	old, err := k.SetFeeRightStrategy(ctx, collection, holder, market.FeeRightID, strategy)
	if err != nil {
		return err
	}
	market.FeeStrategy = strategy
	k.setMarket(ctx, market)
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeFeeStrategyChanged,
		sdk.NewAttribute(types.AttributeKeyDenom, denom),
		sdk.NewAttribute("old_strategy", strconv.FormatUint(uint64(old), 10)),
		sdk.NewAttribute(types.AttributeKeyStrategy, strconv.FormatUint(uint64(strategy), 10))))
	return nil
}

// distribute takes the market's accumulated fees off the liabilities for one
// strategy execution.
func (k *Keeper) distribute(ctx sdk.Context, market *types.Market) (sdk.Int, error) {
	amount, err := k.takeAccumulatedFees(market)
	if err != nil {
		return sdk.Int{}, err
	}
	if err := k.subLedgerAmount(ctx, totalLiabilitiesKey, amount); err != nil {
		return sdk.Int{}, err
	}
	k.setMarket(ctx, *market)
	return amount, nil
}

// Claim is FeesRouter.claimFees over FeeAccumulator.claim: the CLAIM strategy
// pays the market's accumulated fees out of FeeHeld to recipient by
// USDL.transfer from the launchpad module EVM address.
func (k *Keeper) Claim(ctx sdk.Context, caller NestedCaller, sender common.Address, denom string,
	recipient common.Address) (sdk.Context, sdk.Int, error) {
	market, _, err := k.RequireMarketFeeRight(ctx, sender, denom, types.FeeStrategyClaim)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	if recipient == (common.Address{}) {
		return ctx, sdk.Int{}, types.ErrZeroAddress
	}
	return k.claim(ctx, caller, market, recipient)
}

// RouterClaim is FeeAccumulator.claim(pool, recipient) sent by a
// FEES_ROUTER_ROLE holder of the migrated role table.
func (k *Keeper) RouterClaim(ctx sdk.Context, caller NestedCaller, sender, pool, recipient common.Address) (sdk.Context,
	sdk.Int, error) {
	if err := k.CheckRole(ctx, k.generation2.FeeAccumulator, FeesRouterRole, sender); err != nil {
		return ctx, sdk.Int{}, err
	}
	if recipient == (common.Address{}) {
		return ctx, sdk.Int{}, types.ErrZeroAddress
	}
	market, err := k.routerMarket(ctx, pool)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	return k.claim(ctx, caller, market, recipient)
}

// routerMarket is the market of pool for a FeeAccumulator write by
// FEES_ROUTER_ROLE: a pool without a market has no accumulated fees.
func (k *Keeper) routerMarket(ctx sdk.Context, pool common.Address) (types.Market, error) {
	market, found, err := k.poolMarket(ctx, pool)
	if err != nil {
		return types.Market{}, err
	}
	if !found {
		return types.Market{}, types.ErrNoFeesAccumulated
	}
	return market, nil
}

func (k *Keeper) claim(ctx sdk.Context, caller NestedCaller, market types.Market, recipient common.Address) (sdk.Context,
	sdk.Int, error) {
	amount, err := k.distribute(ctx, &market)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	ctx, err = k.PayQuote(ctx, caller, k.feeHeld(), recipient, amount)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeFeesClaimed,
		sdk.NewAttribute(types.AttributeKeyDenom, market.Denom),
		sdk.NewAttribute(types.AttributeKeyRecipient, recipient.Hex()),
		sdk.NewAttribute(types.AttributeKeyAmount, amount.String())))
	return ctx, amount, nil
}

// ClaimFees is Claim from module context.
func (k *Keeper) ClaimFees(ctx sdk.Context, caller sdk.AccAddress, denom string, recipient sdk.AccAddress) (sdk.Int, error) {
	_, amount, err := k.Claim(ctx, k.ModuleCaller(), k.evmKeeper.GetEVMAddressOrDefault(ctx, caller), denom,
		k.evmKeeper.GetEVMAddressOrDefault(ctx, recipient))
	return amount, err
}

// DeadAccount is the bank account of 0x…dEaD, where burned fees go.
func (k *Keeper) DeadAccount(ctx sdk.Context) sdk.AccAddress {
	return k.evmKeeper.GetPaxAddressOrDefault(ctx, common.HexToAddress(types.DeadAddress))
}

// Burn is FeesRouter.executeBurn over FeeAccumulator.burn: the BURN strategy
// pays the accumulated fees out of FeeHeld to 0x…dEaD.
func (k *Keeper) Burn(ctx sdk.Context, caller NestedCaller, sender common.Address, denom string) (sdk.Context,
	sdk.Int, error) {
	market, _, err := k.RequireMarketFeeRight(ctx, sender, denom, types.FeeStrategyBurn)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	return k.burn(ctx, caller, market)
}

// RouterBurn is FeeAccumulator.burn(pool) sent by a FEES_ROUTER_ROLE holder
// of the migrated role table.
func (k *Keeper) RouterBurn(ctx sdk.Context, caller NestedCaller, sender, pool common.Address) (sdk.Context, sdk.Int,
	error) {
	if err := k.CheckRole(ctx, k.generation2.FeeAccumulator, FeesRouterRole, sender); err != nil {
		return ctx, sdk.Int{}, err
	}
	market, err := k.routerMarket(ctx, pool)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	return k.burn(ctx, caller, market)
}

func (k *Keeper) burn(ctx sdk.Context, caller NestedCaller, market types.Market) (sdk.Context, sdk.Int, error) {
	amount, err := k.distribute(ctx, &market)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	ctx, err = k.PayQuote(ctx, caller, k.feeHeld(), common.HexToAddress(types.DeadAddress), amount)
	if err != nil {
		return ctx, sdk.Int{}, err
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeFeesBurned,
		sdk.NewAttribute(types.AttributeKeyDenom, market.Denom),
		sdk.NewAttribute(types.AttributeKeyAmount, amount.String())))
	return ctx, amount, nil
}

// ExecuteBurn is Burn from module context.
func (k *Keeper) ExecuteBurn(ctx sdk.Context, caller sdk.AccAddress, denom string) (sdk.Int, error) {
	_, amount, err := k.Burn(ctx, k.ModuleCaller(), k.evmKeeper.GetEVMAddressOrDefault(ctx, caller), denom)
	return amount, err
}

// ExecuteAirdrop is FeesRouter.executeAirdrop: the AIRDROP strategy opens a
// new epoch holding the accumulated fees for token holders to claim. The
// epoch's holder history, supply and payout token are fixed in the
// same write set, so later mints, burns and transfers cannot change it.
func (k *Keeper) ExecuteAirdrop(ctx sdk.Context, caller sdk.AccAddress, denom string) (sdk.Int, error) {
	market, err := k.requireFeeRightsHolder(ctx, caller, denom, types.FeeStrategyAirdrop)
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
		PayoutToken: k.quoteToken(ctx).Hex(), Height: ctx.BlockHeight(), Paid: sdk.ZeroInt()})
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
	if err := k.subLedgerAmount(cacheCtx, totalLiabilitiesKey, amount); err != nil {
		return sdk.Int{}, err
	}
	if _, err := k.PayQuote(cacheCtx, k.ModuleCaller(), k.feeHeld(), k.evmKeeper.GetEVMAddressOrDefault(ctx, holder),
		amount); err != nil {
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

// ExecuteLpRewards is FeesRouter.executeLpRewards over
// FeeAccumulator.sendLpRewards: the LP_REWARDS strategy moves the accumulated
// fees from FeeHeld to HeldQuote of the pool, and the pool's syncReserves
// adds them to the curve's real quote balance.
func (k *Keeper) ExecuteLpRewards(ctx sdk.Context, caller sdk.AccAddress, denom string) (sdk.Int, error) {
	market, err := k.requireFeeRightsHolder(ctx, caller, denom, types.FeeStrategyLpRewards)
	if err != nil {
		return sdk.Int{}, err
	}
	return k.sendLpRewards(ctx, market)
}

// RouterSendLpRewards is FeeAccumulator.sendLpRewards(pool) sent by a
// FEES_ROUTER_ROLE holder of the migrated role table.
func (k *Keeper) RouterSendLpRewards(ctx sdk.Context, sender, pool common.Address) (sdk.Int, error) {
	if err := k.CheckRole(ctx, k.generation2.FeeAccumulator, FeesRouterRole, sender); err != nil {
		return sdk.Int{}, err
	}
	market, err := k.routerMarket(ctx, pool)
	if err != nil {
		return sdk.Int{}, err
	}
	return k.sendLpRewards(ctx, market)
}

func (k *Keeper) sendLpRewards(ctx sdk.Context, market types.Market) (sdk.Int, error) {
	amount, err := k.distribute(ctx, &market)
	if err != nil {
		return sdk.Int{}, err
	}
	pool := common.HexToAddress(market.PoolAddress)
	if err := k.addLedgerAmount(ctx, lpRewardsKey(pool), amount); err != nil {
		return sdk.Int{}, err
	}
	if err := k.MoveQuote(ctx, k.feeHeld(), HeldQuoteLedger(pool), amount); err != nil {
		return sdk.Int{}, err
	}
	market.RealQuoteBalance = market.RealQuoteBalance.Add(amount)
	k.setMarket(ctx, market)
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeLpRewardsSent,
		sdk.NewAttribute(types.AttributeKeyDenom, market.Denom),
		sdk.NewAttribute(AttributeKeyPool, market.PoolAddress),
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
