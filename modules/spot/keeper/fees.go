package keeper

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type PriceResult struct {
	Gross     sdk.Int
	Net       sdk.Int
	VolumeUSD sdk.Int
	FeeBps    uint64
	FeeAmount sdk.Int
	ImpactBps uint64
	Deduction sdk.Int
}

type feeSchedule struct {
	base          uint64
	tier1         uint64
	tier2         uint64
	impactEnabled bool
	scalar        uint64
}

type feePricing struct {
	schedule   feeSchedule
	input      types.TokenParams
	output     types.TokenParams
	priceIn    sdk.Int
	priceOut   sdk.Int
	reserveUSD sdk.Int
}

func Tier1Threshold() sdk.Int { return sdk.NewIntWithDecimal(10_000, 18) }
func Tier2Threshold() sdk.Int { return sdk.NewIntWithDecimal(100_000, 18) }

func feeUint(value sdk.Int) bool {
	return !value.IsNil() && !value.IsNegative() && value.BigInt().BitLen() <= 256
}

func feeInt(value *big.Int) (sdk.Int, error) {
	if value.Sign() < 0 || value.BitLen() > 256 {
		return sdk.Int{}, fmt.Errorf("spot: fee arithmetic outside uint256")
	}
	return sdk.NewIntFromBigInt(value), nil
}

func EffectiveFeeBps(base, tier1, tier2 uint64, volume sdk.Int) (uint64, error) {
	if !feeUint(volume) {
		return 0, types.ErrZeroAmount
	}
	if base > types.MaxFeeBps || tier1 > types.MaxFeeBps || tier2 > types.MaxFeeBps || base+tier1+tier2 > types.MaxFeeBps {
		return 0, types.ErrFeeTooHigh
	}
	if tier2 < tier1 {
		return 0, types.ErrTier2BelowTier1
	}
	if volume.GTE(Tier1Threshold()) {
		base += tier1
	}
	if volume.GTE(Tier2Threshold()) {
		base += tier2
	}
	return base, nil
}

func ApplyImpact(afterFee, volume, reserveUSD sdk.Int, enabled bool, scalar uint64) (net sdk.Int, impactBps uint64, deduction sdk.Int, err error) {
	if !feeUint(afterFee) || !feeUint(volume) || !feeUint(reserveUSD) {
		return sdk.Int{}, 0, sdk.Int{}, types.ErrZeroAmount
	}
	if scalar > types.MaxImpactBps {
		return sdk.Int{}, 0, sdk.Int{}, types.ErrScalarTooHigh
	}
	if enabled && reserveUSD.IsPositive() {
		impact := new(big.Int).Mul(volume.BigInt(), new(big.Int).SetUint64(scalar))
		impact.Quo(impact, reserveUSD.BigInt())
		if impact.Cmp(big.NewInt(types.MaxImpactBps)) > 0 {
			impactBps = types.MaxImpactBps
		} else {
			impactBps = impact.Uint64()
		}
	}
	deducted := new(big.Int).Mul(afterFee.BigInt(), new(big.Int).SetUint64(impactBps))
	deducted.Quo(deducted, big.NewInt(types.BpsDenominator))
	deduction, err = feeInt(deducted)
	if err != nil {
		return sdk.Int{}, 0, sdk.Int{}, err
	}
	return afterFee.Sub(deduction), impactBps, deduction, nil
}

func feeRail(rail common.Address) (types.FeeRail, error) {
	switch rail {
	case common.HexToAddress(types.PECORAddress):
		return types.FeeRailSwap, nil
	case common.HexToAddress(types.PECOROrdersAddress), common.HexToAddress(types.PECORStopOrdersAddress):
		return types.FeeRailOrder, nil
	default:
		return 0, types.ErrNotAuthorized
	}
}

func pricingSchedule(params types.Params, rail common.Address) (feeSchedule, error) {
	kind, err := feeRail(rail)
	if err != nil {
		return feeSchedule{}, err
	}
	schedule := feeSchedule{params.SwapFeeBps, params.Tier1FeeBps, params.Tier2FeeBps, params.PriceImpactEnabled, params.PriceImpactScalarBps}
	if kind == types.FeeRailOrder {
		schedule = feeSchedule{params.OrderFeeBps, params.OrderTier1FeeBps, params.OrderTier2FeeBps, params.OrderPriceImpactEnabled, params.OrderPriceImpactScalarBps}
	}
	_, err = EffectiveFeeBps(schedule.base, schedule.tier1, schedule.tier2, sdk.ZeroInt())
	if err == types.ErrFeeTooHigh && kind == types.FeeRailOrder {
		err = types.ErrFeeExceedsMax
	}
	if err != nil {
		return feeSchedule{}, err
	}
	if schedule.scalar > types.MaxImpactBps {
		return feeSchedule{}, types.ErrScalarTooHigh
	}
	return schedule, nil
}

func feeToken(params types.Params, token common.Address) (types.TokenParams, error) {
	for _, entry := range params.Tokens {
		if entry.Address == token {
			if entry.Kind != types.TokenKindERC20 && entry.Kind != types.TokenKindNative {
				return types.TokenParams{}, types.ErrUnsupportedTransfer
			}
			if entry.Decimals > types.MaxTokenDecimals {
				return types.TokenParams{}, types.ErrUnsupportedToken
			}
			if entry.Paused {
				return types.TokenParams{}, types.ErrTokenPaused
			}
			return entry, nil
		}
	}
	return types.TokenParams{}, types.ErrTokenNotRegistered
}

func (k *Keeper) feePricing(ctx sdk.Context, rail, input, output common.Address) (feePricing, error) {
	if input == output {
		return feePricing{}, types.ErrSameToken
	}
	params := k.GetParams(ctx)
	schedule, err := pricingSchedule(params, rail)
	if err != nil {
		return feePricing{}, err
	}
	in, err := feeToken(params, input)
	if err != nil {
		return feePricing{}, err
	}
	out, err := feeToken(params, output)
	if err != nil {
		return feePricing{}, err
	}
	priceIn, err := k.IndexPrice(ctx, input)
	if err != nil {
		return feePricing{}, err
	}
	priceOut, err := k.IndexPrice(ctx, output)
	if err != nil {
		return feePricing{}, err
	}
	spendable, err := k.Spendable(ctx, output)
	if err != nil {
		return feePricing{}, err
	}
	reserveUSD, err := VolumeUSD(spendable, priceOut, out.Decimals)
	if err != nil {
		return feePricing{}, err
	}
	return feePricing{schedule, in, out, priceIn, priceOut, reserveUSD}, nil
}

func (k *Keeper) Price(ctx sdk.Context, rail, input, output common.Address, amount sdk.Int) (PriceResult, error) {
	if !feeUint(amount) || amount.IsZero() {
		return PriceResult{}, types.ErrZeroAmount
	}
	pricing, err := k.feePricing(ctx, rail, input, output)
	if err != nil {
		return PriceResult{}, err
	}
	return pricing.price(amount)
}

func (p feePricing) price(amount sdk.Int) (PriceResult, error) {
	if !feeUint(amount) || amount.IsZero() {
		return PriceResult{}, types.ErrZeroAmount
	}
	gross, err := Convert(amount, p.priceIn, p.priceOut, p.input.Decimals, p.output.Decimals, false)
	if err != nil {
		return PriceResult{}, err
	}
	volume, err := VolumeUSD(amount, p.priceIn, p.input.Decimals)
	if err != nil {
		return PriceResult{}, err
	}
	bps, err := EffectiveFeeBps(p.schedule.base, p.schedule.tier1, p.schedule.tier2, volume)
	if err != nil {
		return PriceResult{}, err
	}
	fee := new(big.Int).Mul(gross.BigInt(), new(big.Int).SetUint64(bps))
	fee.Quo(fee, big.NewInt(types.BpsDenominator))
	feeAmount, err := feeInt(fee)
	if err != nil {
		return PriceResult{}, err
	}
	net, impact, deduction, err := ApplyImpact(gross.Sub(feeAmount), volume, p.reserveUSD, p.schedule.impactEnabled, p.schedule.scalar)
	if err != nil {
		return PriceResult{}, err
	}
	return PriceResult{gross, net, volume, bps, feeAmount, impact, deduction}, nil
}

func (k *Keeper) QuoteExactOut(ctx sdk.Context, rail, input, output common.Address, target sdk.Int) (sdk.Int, error) {
	if !feeUint(target) || target.IsZero() {
		return sdk.Int{}, types.ErrZeroAmount
	}
	pricing, err := k.feePricing(ctx, rail, input, output)
	if err != nil {
		return sdk.Int{}, err
	}
	return pricing.quoteExactOut(target)
}

func feeCeilDiv(numerator, denominator *big.Int) *big.Int {
	quotient, remainder := new(big.Int).QuoRem(numerator, denominator, new(big.Int))
	if remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return quotient
}

func inverseDeduction(target *big.Int, bps uint64) *big.Int {
	value := new(big.Int).Sub(target, big.NewInt(1))
	value.Mul(value, big.NewInt(types.BpsDenominator))
	value.Quo(value, new(big.Int).SetUint64(types.BpsDenominator-bps))
	return value.Add(value, big.NewInt(1))
}

func (p feePricing) quoteExactOut(target sdk.Int) (sdk.Int, error) {
	if !feeUint(target) || target.IsZero() {
		return sdk.Int{}, types.ErrZeroAmount
	}
	if !feeUint(p.priceIn) || p.priceIn.IsZero() || !feeUint(p.priceOut) || p.priceOut.IsZero() {
		return sdk.Int{}, types.ErrInvalidPrice
	}
	if p.input.Decimals > types.MaxTokenDecimals || p.output.Decimals > types.MaxTokenDecimals {
		return sdk.Int{}, types.ErrUnsupportedToken
	}
	if _, err := EffectiveFeeBps(p.schedule.base, p.schedule.tier1, p.schedule.tier2, sdk.ZeroInt()); err != nil {
		return sdk.Int{}, err
	}
	unit := new(big.Int).Exp(big.NewInt(10), new(big.Int).SetUint64(uint64(p.input.Decimals)), nil)
	first := feeCeilDiv(new(big.Int).Mul(Tier1Threshold().BigInt(), unit), p.priceIn.BigInt())
	second := feeCeilDiv(new(big.Int).Mul(Tier2Threshold().BigInt(), unit), p.priceIn.BigInt())
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	lower := big.NewInt(1)
	boundaries := []*big.Int{first, second, new(big.Int).Add(max, big.NewInt(1))}
	fee := p.schedule.base
	for tier, boundary := range boundaries {
		if tier == 1 {
			fee += p.schedule.tier1
		} else if tier == 2 {
			fee += p.schedule.tier2
		}
		upper := new(big.Int).Sub(boundary, big.NewInt(1))
		if upper.Cmp(max) > 0 {
			upper.Set(max)
		}
		if lower.Cmp(upper) <= 0 {
			candidate, found, err := p.inverseTier(target, fee, lower, upper)
			if err != nil {
				return sdk.Int{}, err
			}
			if found {
				return candidate, nil
			}
		}
		lower = new(big.Int).Set(boundary)
		if lower.Cmp(max) > 0 {
			break
		}
	}
	return sdk.Int{}, types.ErrInsufficientLiquidity
}

func (p feePricing) inverseTier(target sdk.Int, fee uint64, lower, upper *big.Int) (sdk.Int, bool, error) {
	var assumedImpact uint64
	for step := 0; step <= types.MaxImpactBps; step++ {
		afterFee := inverseDeduction(target.BigInt(), assumedImpact)
		gross := inverseDeduction(afterFee, fee)
		if gross.BitLen() > 256 {
			return sdk.Int{}, false, nil
		}
		amount, err := Convert(sdk.NewIntFromBigInt(gross), p.priceOut, p.priceIn, p.output.Decimals, p.input.Decimals, true)
		if err != nil {
			return sdk.Int{}, false, err
		}
		candidate := amount.BigInt()
		if candidate.Cmp(lower) < 0 {
			candidate.Set(lower)
		}
		for candidate.Cmp(upper) <= 0 {
			amount = sdk.NewIntFromBigInt(candidate)
			result, err := p.price(amount)
			if err != nil {
				return sdk.Int{}, false, err
			}
			if result.Net.GTE(target) {
				return amount, true, nil
			}
			if result.ImpactBps > assumedImpact {
				assumedImpact = result.ImpactBps
				break
			}
			candidate.Add(candidate, big.NewInt(1))
		}
		if candidate.Cmp(upper) > 0 {
			return sdk.Int{}, false, nil
		}
	}
	return sdk.Int{}, false, fmt.Errorf("spot: exact-output inverse did not converge")
}

func feeAccrualKey(rail types.FeeRail, token common.Address) []byte {
	key := append([]byte(nil), types.FeeAccrualPrefix...)
	key = append(key, byte(rail))
	return append(key, token.Bytes()...)
}

func readFeeAccrual(store sdk.KVStore, rail types.FeeRail, token common.Address) (sdk.Int, error) {
	data := store.Get(feeAccrualKey(rail, token))
	if data == nil {
		return sdk.ZeroInt(), nil
	}
	var accrual types.FeeAccrual
	if err := json.Unmarshal(data, &accrual); err != nil {
		return sdk.Int{}, fmt.Errorf("spot: decode fee accrual: %w", err)
	}
	if accrual.Rail != rail || accrual.Token != token || !feeUint(accrual.Amount) {
		return sdk.Int{}, fmt.Errorf("spot: invalid fee accrual")
	}
	return accrual.Amount, nil
}

func (k *Keeper) AccumulatedFees(ctx sdk.Context, rail, token common.Address) (sdk.Int, error) {
	kind, err := feeRail(rail)
	if err != nil {
		return sdk.Int{}, err
	}
	return readFeeAccrual(ctx.KVStore(k.storeKey), kind, token)
}

func (k *Keeper) AccrueFees(ctx sdk.Context, rail, token common.Address, retained sdk.Int) error {
	kind, err := feeRail(rail)
	if err != nil {
		return err
	}
	if !feeUint(retained) {
		return types.ErrZeroAmount
	}
	spendable, err := k.Spendable(ctx, token)
	if err != nil {
		return err
	}
	return accrueFees(ctx.KVStore(k.storeKey), kind, token, retained, spendable)
}

func accrueFees(store sdk.KVStore, rail types.FeeRail, token common.Address, retained, spendable sdk.Int) error {
	if rail != types.FeeRailSwap && rail != types.FeeRailOrder {
		return types.ErrNotAuthorized
	}
	if !feeUint(retained) || !feeUint(spendable) {
		return types.ErrZeroAmount
	}
	if retained.GT(spendable) {
		return types.ErrInsufficientLiquidity
	}
	if retained.IsZero() {
		return nil
	}
	current, err := readFeeAccrual(store, rail, token)
	if err != nil {
		return err
	}
	total, err := feeInt(new(big.Int).Add(current.BigInt(), retained.BigInt()))
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(types.FeeAccrual{Rail: rail, Token: token, Amount: total})
	if err != nil {
		return err
	}
	store.Set(feeAccrualKey(rail, token), encoded)
	return nil
}

func (p PriceResult) FeeEvents(rail, user, token common.Address, exactInput bool) ([]ethtypes.Log, error) {
	kind, err := feeRail(rail)
	if err != nil {
		return nil, err
	}
	if kind != types.FeeRailSwap || !exactInput {
		return nil, nil
	}
	if !feeUint(p.FeeAmount) || !feeUint(p.Deduction) || !feeUint(p.VolumeUSD) || p.FeeBps > types.MaxFeeBps || p.ImpactBps > types.MaxImpactBps {
		return nil, fmt.Errorf("spot: invalid fee event values")
	}
	events := make([]ethtypes.Log, 0, 2)
	if p.FeeAmount.IsPositive() {
		data := make([]byte, 96)
		p.VolumeUSD.BigInt().FillBytes(data[:32])
		new(big.Int).SetUint64(p.FeeBps).FillBytes(data[32:64])
		p.FeeAmount.BigInt().FillBytes(data[64:])
		events = append(events, ethtypes.Log{Address: rail, Topics: []common.Hash{
			crypto.Keccak256Hash([]byte("TieredFeeApplied(address,address,uint256,uint256,uint256)")),
			common.BytesToHash(user.Bytes()), common.BytesToHash(token.Bytes()),
		}, Data: data})
	}
	if p.Deduction.IsPositive() {
		data := make([]byte, 64)
		new(big.Int).SetUint64(p.ImpactBps).FillBytes(data[:32])
		p.Deduction.BigInt().FillBytes(data[32:])
		events = append(events, ethtypes.Log{Address: rail, Topics: []common.Hash{
			crypto.Keccak256Hash([]byte("PriceImpactApplied(address,address,uint256,uint256)")),
			common.BytesToHash(user.Bytes()), common.BytesToHash(token.Bytes()),
		}, Data: data})
	}
	return events, nil
}
