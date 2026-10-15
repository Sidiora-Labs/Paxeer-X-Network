package keeper

import (
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/holiman/uint256"
)

const (
	secondsPerHour = 3600
	hoursPerDay    = 24
	fundingPeriods = 8
)

var (
	fixedOne      = big.NewInt(1e18)
	ppmToFixed    = big.NewInt(1e12)
	usdlUnitInUsd = big.NewInt(1e12)
)

// FundingPayment is the funding of one position since its last settlement.
// Usd carries the interface sign, negative when the position pays; Usdl is
// that amount in USDL units rounded toward the venue (a payment up, a receipt
// down); ResidueUsd is what the USDL rounding keeps in the venue, booked in
// residueUsd; Basis is the funding basis of the position after settlement.
type FundingPayment struct {
	Usd        sdk.Int
	Usdl       sdk.Int
	ResidueUsd sdk.Int
	Basis      uint256.Int
}

// SamplePremium records one premium sample of the market when the last one is
// at least Params.PremiumIntervalSeconds of block time old, from the impact
// bid and ask of the market's impact notional against the book and the
// pricefeed index price (contracts/perps-parameters.md section 3). It reports
// whether a sample was taken.
func (k *Keeper) SamplePremium(ctx sdk.Context, market types.Market) (bool, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return false, err
	}
	now, err := blockTimeSeconds(ctx)
	if err != nil {
		return false, err
	}
	state, err := k.fundingMarketState(ctx, market.ID)
	if err != nil {
		return false, err
	}
	if state.LastSampleTime != 0 && now-state.LastSampleTime < params.PremiumIntervalSeconds {
		return false, nil
	}
	if state.IndexPrice.IsZero() {
		return false, nil
	}
	oracle := state.IndexPrice.ToBig()
	if market.ImpactNotionalUsd.IsNil() || !market.ImpactNotionalUsd.IsPositive() {
		return false, fmt.Errorf("%w: market %d impact notional must be positive", types.ErrInvalidParams, market.ID)
	}
	notional := market.ImpactNotionalUsd.BigInt()
	impactBid, err := k.impactPrice(ctx, market.ID, types.SideBuy, notional, oracle)
	if err != nil {
		return false, err
	}
	impactAsk, err := k.impactPrice(ctx, market.ID, types.SideSell, notional, oracle)
	if err != nil {
		return false, err
	}
	premium := premiumOf(oracle, impactBid, impactAsk)
	sum := new(big.Int).Add(signedOf(state.PremiumSum), premium)
	if state.PremiumSum, err = signedWord(sum, market.ID); err != nil {
		return false, err
	}
	if state.PremiumCount == ^uint64(0) {
		return false, fmt.Errorf("%w: market %d premium sample count", types.ErrOverflow, market.ID)
	}
	state.PremiumCount++
	state.LastSampleTime = now
	k.setFundingMarketState(ctx, state)
	return true, nil
}

// premiumOf is (max(impactBid - oracle, 0) - max(oracle - impactAsk, 0)) /
// oracle as an 18-decimal signed fraction, truncated toward zero.
func premiumOf(oracle, impactBid, impactAsk *big.Int) *big.Int {
	premium := new(big.Int)
	if impactBid.Cmp(oracle) > 0 {
		premium.Sub(impactBid, oracle)
	}
	if impactAsk.Cmp(oracle) < 0 {
		premium.Sub(premium, new(big.Int).Sub(oracle, impactAsk))
	}
	premium.Mul(premium, fixedOne)
	return premium.Quo(premium, oracle)
}

// impactPrice is the average execution price of selling (side SideBuy, the
// bids) or buying (side SideSell, the asks) notional USD against the book,
// rounded against the trader: the bid down and the ask up. A side whose
// resting size does not cover the notional prices at the oracle.
func (k *Keeper) impactPrice(ctx sdk.Context, marketID uint64, side byte, notional, oracle *big.Int) (*big.Int, error) {
	remaining := new(big.Int).Set(notional)
	base := new(big.Int)
	var visitErr error
	err := k.IterateBook(ctx, marketID, side, func(order types.Order) bool {
		var open uint256.Int
		if _, underflow := open.SubOverflow(&order.SizeBase, &order.FilledBase); underflow {
			visitErr = fmt.Errorf("%w: order %s filled above its size", types.ErrOverflow, order.ID.Dec())
			return true
		}
		price := order.LimitPrice.ToBig()
		if open.IsZero() || price.Sign() == 0 {
			return false
		}
		openBase := open.ToBig()
		levelUsd := new(big.Int).Mul(openBase, price)
		levelUsd.Quo(levelUsd, fixedOne)
		if levelUsd.Cmp(remaining) < 0 {
			base.Add(base, openBase)
			remaining.Sub(remaining, levelUsd)
			return false
		}
		taken := new(big.Int).Mul(remaining, fixedOne)
		if side == types.SideBuy {
			taken = ceilDiv(taken, price)
		} else {
			taken.Quo(taken, price)
		}
		base.Add(base, taken)
		remaining.SetInt64(0)
		return true
	})
	if err != nil {
		return nil, err
	}
	if visitErr != nil {
		return nil, visitErr
	}
	if remaining.Sign() > 0 || base.Sign() == 0 {
		return new(big.Int).Set(oracle), nil
	}
	average := new(big.Int).Mul(notional, fixedOne)
	if side == types.SideBuy {
		return average.Quo(average, base), nil
	}
	return ceilDiv(average, base), nil
}

// SettleFunding runs at each new whole hour of block time: it fixes the
// hourly rate F / 8 capped at the funding cap, where F = P + clamp(I - P,
// -clamp, +clamp) from the mean premium P of the hour's samples, and advances
// the cumulative funding index per unit of base by oracle x hourly rate for
// every elapsed hour, as lx_perps_funding_index_update does for its elapsed
// intervals. No position is visited; positions realise funding at their next
// settlement. It returns FundingRateUpdated and FundingSettled, or nothing
// when the hour has not changed.
func (k *Keeper) SettleFunding(ctx sdk.Context, market types.Market) ([]types.EventRecord, error) {
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	now, err := blockTimeSeconds(ctx)
	if err != nil {
		return nil, err
	}
	state, err := k.fundingMarketState(ctx, market.ID)
	if err != nil {
		return nil, err
	}
	hour := now / secondsPerHour
	if hour <= state.LastIndexHour {
		return nil, nil
	}
	first := state.LastIndexHour == 0
	elapsed := hour - state.LastIndexHour
	state.LastIndexHour = hour
	if first {
		state.PremiumSum = uint256.Int{}
		state.PremiumCount = 0
		k.setFundingMarketState(ctx, state)
		return nil, nil
	}
	meanPremium := new(big.Int)
	if state.PremiumCount > 0 {
		meanPremium.Quo(signedOf(state.PremiumSum), new(big.Int).SetUint64(state.PremiumCount))
	}
	rate := hourlyRate(meanPremium, params, market)
	if state.HourlyRate, err = signedWord(rate, market.ID); err != nil {
		return nil, err
	}
	step := new(big.Int).Mul(state.IndexPrice.ToBig(), rate)
	step.Mul(step, new(big.Int).SetUint64(elapsed))
	step.Quo(step, fixedOne)
	index := new(big.Int).Add(signedOf(state.FundingIndex), step)
	if state.FundingIndex, err = signedWord(index, market.ID); err != nil {
		return nil, err
	}
	state.PremiumSum = uint256.Int{}
	state.PremiumCount = 0
	k.setFundingMarketState(ctx, state)

	longPayment := new(big.Int).Mul(state.LongOpenInterest.ToBig(), step)
	longPayment.Quo(longPayment, fixedOne).Neg(longPayment)
	shortPayment := new(big.Int).Mul(state.ShortOpenInterest.ToBig(), step)
	shortPayment.Quo(shortPayment, fixedOne)
	marketID := new(big.Int).SetUint64(market.ID)
	return []types.EventRecord{
		{Name: types.EventFundingRateUpdated, Args: []any{
			marketID, new(big.Int).Quo(rate, big.NewInt(secondsPerHour)), new(big.Int).Mul(rate, big.NewInt(hoursPerDay)),
		}},
		{Name: types.EventFundingSettled, Args: []any{new(big.Int).Set(marketID), new(big.Int).Set(rate), longPayment, shortPayment}},
	}, nil
}

// hourlyRate is F / 8 for F = P + clamp(I - P, -clamp, +clamp), clamped to
// the smaller of the venue and market funding caps, as an 18-decimal signed
// fraction truncated toward zero. It replaces the bps clamp of
// lx_perps_funding_rate.
func hourlyRate(meanPremium *big.Int, params types.Params, market types.Market) *big.Int {
	interest := new(big.Int).Mul(new(big.Int).SetUint64(params.FundingInterestPpm8h), ppmToFixed)
	bound := new(big.Int).Mul(new(big.Int).SetUint64(params.FundingPremiumClampPpm), ppmToFixed)
	rate := clampAbs(new(big.Int).Sub(interest, meanPremium), bound)
	rate.Add(rate, meanPremium)
	rate.Quo(rate, big.NewInt(fundingPeriods))
	capPpm := params.FundingCapPpmPerHour
	if market.FundingCapPpmPerHour < capPpm {
		capPpm = market.FundingCapPpmPerHour
	}
	return clampAbs(rate, new(big.Int).Mul(new(big.Int).SetUint64(capPpm), ppmToFixed))
}

func clampAbs(value, bound *big.Int) *big.Int {
	if value.Cmp(bound) > 0 {
		return new(big.Int).Set(bound)
	}
	if lower := new(big.Int).Neg(bound); value.Cmp(lower) < 0 {
		return lower
	}
	return new(big.Int).Set(value)
}

// FundingOwed is the funding of the position since its last settlement at the
// cumulative index, side x (basis at index - funding basis), as
// lx_perps_funding_owed computes it. The basis at index, size x index / 1e18,
// rounds up for a long and down for a short, so the position pays at least
// and receives at most the exact amount; the USDL amount rounds a payment up
// and a receipt down, the remainder being the residue.
func FundingOwed(position types.Position, fundingIndex uint256.Int) (FundingPayment, error) {
	basis, err := fundingBasis(position.IsLong, position.MarketID, position.SizeBase, fundingIndex)
	if err != nil {
		return FundingPayment{}, err
	}
	owed := new(big.Int).Sub(signedOf(basis), signedOf(position.FundingBasis))
	if !position.IsLong {
		owed.Neg(owed)
	}
	payment := FundingPayment{Usd: sdk.NewIntFromBigInt(new(big.Int).Neg(owed)), Basis: basis}
	switch owed.Sign() {
	case 0:
		payment.Usdl, payment.ResidueUsd = sdk.ZeroInt(), sdk.ZeroInt()
	case 1:
		usdl, residue, err := types.UsdToUsdlUp(sdk.NewIntFromBigInt(owed))
		if err != nil {
			return FundingPayment{}, err
		}
		payment.Usdl, payment.ResidueUsd = usdl.Neg(), residue
	default:
		usdl, residue, err := types.UsdToUsdlDown(sdk.NewIntFromBigInt(new(big.Int).Neg(owed)))
		if err != nil {
			return FundingPayment{}, err
		}
		payment.Usdl, payment.ResidueUsd = usdl, residue
	}
	return payment, nil
}

// fundingBasis is size x index / 1e18 in USD, rounded up for a long and down
// for a short, the funding basis a settlement stores.
func fundingBasis(isLong bool, marketID uint64, size, fundingIndex uint256.Int) (uint256.Int, error) {
	product := new(big.Int).Mul(size.ToBig(), signedOf(fundingIndex))
	quotient, remainder := new(big.Int).QuoRem(product, fixedOne, new(big.Int))
	if isLong && remainder.Sign() > 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !isLong && remainder.Sign() < 0 {
		quotient.Sub(quotient, big.NewInt(1))
	}
	return signedWord(quotient, marketID)
}

// SettlePositionFunding books the funding of the position since its last
// settlement into the account's collateral at the market's cumulative index:
// a receipt credits the USDL amount rounded down, a payment debits it rounded
// up, the rounding remainder goes to residueUsd and a payment above the
// collateral leaves it at 0 with the remainder recorded as the account's
// deficit. The position's funding basis is reset. It returns
// PositionFundingApplied when the funding is non-zero; the caller stores the
// account and the position.
func (k *Keeper) SettlePositionFunding(ctx sdk.Context, account *types.Account, position *types.Position, fundingIndex uint256.Int) (*types.EventRecord, error) {
	if account.Address != position.Account {
		return nil, fmt.Errorf("%w: position %s", types.ErrNotPositionHolder, position.ID.Dec())
	}
	books, err := k.fundingVenueBooks(ctx)
	if err != nil {
		return nil, err
	}
	_, event, err := positionSettleFunding(account, position, fundingIndex, &books)
	if err != nil {
		return nil, err
	}
	if event != nil {
		k.setFundingVenueBooks(ctx, books)
	}
	return event, nil
}

func (k *Keeper) fundingMarketState(ctx sdk.Context, marketID uint64) (types.MarketState, error) {
	bz := k.store(ctx).Get(types.MarketStateKey(marketID))
	if bz == nil {
		return types.MarketState{MarketID: marketID}, nil
	}
	var state types.MarketState
	if err := state.Unmarshal(bz); err != nil {
		return types.MarketState{}, err
	}
	return state, nil
}

func (k *Keeper) setFundingMarketState(ctx sdk.Context, state types.MarketState) {
	k.store(ctx).Set(types.MarketStateKey(state.MarketID), state.Marshal())
}

func (k *Keeper) fundingVenueBooks(ctx sdk.Context) (types.VenueBooks, error) {
	var books types.VenueBooks
	bz := k.store(ctx).Get(types.VenueBooksKey)
	if bz == nil {
		return books, nil
	}
	if err := books.Unmarshal(bz); err != nil {
		return types.VenueBooks{}, err
	}
	return books, nil
}

func (k *Keeper) setFundingVenueBooks(ctx sdk.Context, books types.VenueBooks) {
	k.store(ctx).Set(types.VenueBooksKey, books.Marshal())
}

// signedOf reads a uint256 word as an int256 in two's complement.
func signedOf(word uint256.Int) *big.Int {
	if word.Sign() >= 0 {
		return word.ToBig()
	}
	var magnitude uint256.Int
	magnitude.Neg(&word)
	return new(big.Int).Neg(magnitude.ToBig())
}
