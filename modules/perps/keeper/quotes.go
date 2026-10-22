package keeper

import (
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

type OpenPositionQuote struct {
	EntryPrice           *big.Int
	SizeUsd              *big.Int
	CollateralUsd        *big.Int
	Leverage             *big.Int
	TradingFee           *big.Int
	TradingFeeUsd        *big.Int
	PriceImpact          *big.Int
	LiquidationPrice     *big.Int
	EstimatedFunding24h  *big.Int
	MaintenanceMarginBps *big.Int
}

type ClosePositionQuote struct {
	ExitPrice       *big.Int
	UnrealizedPnl   *big.Int
	TradingFee      *big.Int
	TradingFeeUsd   *big.Int
	FundingOwed     *big.Int
	NetPnl          *big.Int
	EstimatedPayout *big.Int
}

type PartialCloseQuote struct {
	ExitPrice       *big.Int
	ClosedPnl       *big.Int
	Fee             *big.Int
	EstimatedPayout *big.Int
}

type MarketQuote struct {
	IndexPrice           *big.Int
	MarkPrice            *big.Int
	OracleTWAP           *big.Int
	FundingRatePerSecond *big.Int
	FundingRate24h       *big.Int
	LongOI               *big.Int
	ShortOI              *big.Int
	MaxLeverage          *big.Int
	MaintenanceMarginBps *big.Int
	Enabled              bool
	PriceStale           bool
}

func (k Keeper) QuoteOpen(ctx sdk.Context, account common.Address, marketID uint64, token common.Address, collateral, leverage uint256.Int, isLong bool) (OpenPositionQuote, error) {
	cache, _ := ctx.CacheContext()
	params, err := k.GetParams(cache)
	if err != nil {
		return OpenPositionQuote{}, err
	}
	if params.Paused {
		return OpenPositionQuote{}, types.ErrPaused
	}
	if token != params.CollateralToken {
		return OpenPositionQuote{}, types.ErrUnsupportedCollateral
	}
	if collateral.IsZero() {
		return OpenPositionQuote{}, types.ErrZeroAmount
	}
	if _, err := k.Deposit(cache, account, collateral); err != nil {
		return OpenPositionQuote{}, err
	}
	order, err := k.CompatOpenOrder(cache, account, marketID, collateral, leverage, isLong)
	if err != nil {
		return OpenPositionQuote{}, err
	}
	events, err := k.quotePlace(cache, order, uint256.Int{})
	if err != nil {
		return OpenPositionQuote{}, err
	}
	totals, err := quoteExecution(events, account, nil)
	if err != nil {
		return OpenPositionQuote{}, err
	}
	market, err := k.GetMarket(cache, marketID)
	if err != nil {
		return OpenPositionQuote{}, err
	}
	state, err := k.quoteState(cache, marketID)
	if err != nil {
		return OpenPositionQuote{}, err
	}
	positionID, err := quoteWord(totals.positionID)
	if err != nil {
		return OpenPositionQuote{}, err
	}
	position, err := k.quotePosition(cache, positionID)
	if err != nil {
		return OpenPositionQuote{}, err
	}
	notional := new(big.Int).Quo(new(big.Int).Mul(position.SizeBase.ToBig(), state.MarkPrice.ToBig()), quoteScale())
	bps, err := quoteMaintenanceBps(market, notional)
	if err != nil {
		return OpenPositionQuote{}, err
	}
	liquidationPrice, err := k.LiquidationPrice(cache, positionID)
	if err != nil {
		return OpenPositionQuote{}, err
	}
	entry := totals.price()
	if state.MarkPrice.IsZero() {
		return OpenPositionQuote{}, types.ErrPriceStale
	}
	impact := new(big.Int).Abs(new(big.Int).Sub(entry, state.MarkPrice.ToBig()))
	impact.Quo(impact.Mul(impact, big.NewInt(types.BpsDenominator)), state.MarkPrice.ToBig())
	funding := new(big.Int).Mul(totals.base, state.IndexPrice.ToBig())
	funding.Mul(funding, quoteSigned(state.HourlyRate))
	funding.Mul(funding, big.NewInt(24))
	if isLong {
		funding.Neg(funding)
	}
	funding.Div(funding, new(big.Int).Mul(quoteScale(), quoteScale()))
	collateralUsd := new(big.Int).Mul(collateral.ToBig(), big.NewInt(1_000_000_000_000))
	quote := OpenPositionQuote{entry, totals.notional, collateralUsd, leverage.ToBig(), totals.feeTokens,
		totals.feeUsd, impact, liquidationPrice.ToBig(), funding, bps}
	if err := quoteBounds(false, quote.EntryPrice, quote.SizeUsd, quote.CollateralUsd, quote.Leverage, quote.TradingFee,
		quote.TradingFeeUsd, quote.PriceImpact, quote.LiquidationPrice, quote.MaintenanceMarginBps); err != nil {
		return OpenPositionQuote{}, err
	}
	if err := quoteBounds(true, funding); err != nil {
		return OpenPositionQuote{}, err
	}
	return quote, nil
}

func (k Keeper) CompatOpenOrder(ctx sdk.Context, account common.Address, marketID uint64, collateral, leverage uint256.Int, isLong bool) (types.Order, error) {
	market, err := k.GetMarket(ctx, marketID)
	if err != nil {
		return types.Order{}, err
	}
	if !market.Enabled {
		return types.Order{}, types.ErrMarketDisabled
	}
	if err := k.RequireFresh(ctx, marketID); err != nil {
		return types.Order{}, err
	}
	if leverage.ToBig().Cmp(quoteScale()) < 0 {
		return types.Order{}, types.ErrLeverageTooHigh
	}
	state, err := k.quoteState(ctx, marketID)
	if err != nil {
		return types.Order{}, err
	}
	tier, err := k.FeeTier(ctx, account)
	if err != nil {
		return types.Order{}, err
	}
	params, err := k.GetParams(ctx)
	if err != nil {
		return types.Order{}, err
	}
	if market.PxDecimals > types.USDDecimals || market.SzDecimals > types.USDDecimals || params.DefaultSlippagePpm > types.PpmDenominator {
		return types.Order{}, types.ErrInvalidParams
	}
	price := new(big.Int)
	if isLong {
		price.Mul(state.MarkPrice.ToBig(), new(big.Int).SetUint64(types.PpmDenominator+params.DefaultSlippagePpm))
		price.Quo(price, big.NewInt(types.PpmDenominator))
		tick := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(types.USDDecimals-market.PxDecimals)), nil)
		price.Mul(price.Quo(price, tick), tick)
	} else {
		it := sdk.KVStorePrefixIterator(ctx.KVStore(k.storeKey), types.BookSidePrefix(marketID, types.SideBuy))
		defer it.Close()
		for ; it.Valid(); it.Next() {
			if len(it.Value()) != 32 {
				return types.Order{}, fmt.Errorf("perps: invalid book order id")
			}
			var id uint256.Int
			id.SetBytes(it.Value())
			var maker types.Order
			if err := maker.Unmarshal(ctx.KVStore(k.storeKey).Get(types.OrderKey(id))); err != nil {
				return types.Order{}, err
			}
			if maker.Account != account && maker.FilledBase.Lt(&maker.SizeBase) {
				price.Set(maker.LimitPrice.ToBig())
				break
			}
		}
		if price.Sign() == 0 {
			return types.Order{}, types.ErrNoLiquidity
		}
	}
	if price.Sign() == 0 {
		return types.Order{}, types.ErrPriceStale
	}
	numerator := new(big.Int).Mul(collateral.ToBig(), big.NewInt(1_000_000_000_000))
	numerator.Mul(numerator, leverage.ToBig()).Mul(numerator, big.NewInt(types.PpmDenominator))
	denominator := new(big.Int).Mul(leverage.ToBig(), new(big.Int).SetUint64(tier.TakerFeePpm))
	denominator.Add(denominator, new(big.Int).Mul(quoteScale(), big.NewInt(types.PpmDenominator)))
	notional := numerator.Quo(numerator, denominator)
	if err := quoteBounds(false, notional); err != nil {
		return types.Order{}, err
	}
	base := new(big.Int).Quo(new(big.Int).Mul(notional, quoteScale()), price)
	lot := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(types.USDDecimals-market.SzDecimals)), nil)
	base.Mul(base.Quo(base, lot), lot)
	if base.Sign() == 0 {
		return types.Order{}, types.ErrLotSize
	}
	sizeBase, err := quoteWord(base)
	if err != nil {
		return types.Order{}, err
	}
	sizeUsd, err := quoteWord(new(big.Int).Quo(new(big.Int).Mul(base, state.MarkPrice.ToBig()), quoteScale()))
	if err != nil {
		return types.Order{}, err
	}
	return types.Order{Account: account, MarketID: marketID, IsLong: isLong, Type: types.OrderTypeMarket,
		TimeInForce: types.TimeInForceIOC, SizeUsd: sizeUsd, SizeBase: sizeBase, Leverage: leverage}, nil
}

func (k Keeper) QuoteClose(ctx sdk.Context, positionID uint256.Int) (ClosePositionQuote, error) {
	cache, _ := ctx.CacheContext()
	position, err := k.quotePosition(cache, positionID)
	if err != nil {
		return ClosePositionQuote{}, err
	}
	order, err := k.ClosePosition(cache, position.Account, positionID)
	if err != nil {
		return ClosePositionQuote{}, err
	}
	events, err := k.quotePlace(cache, order, order.SizeBase)
	if err != nil {
		return ClosePositionQuote{}, err
	}
	return quoteCloseEvents(events, position)
}

func (k Keeper) QuotePartialClose(ctx sdk.Context, positionID, closeSizeUsd uint256.Int) (PartialCloseQuote, error) {
	cache, _ := ctx.CacheContext()
	position, err := k.quotePosition(cache, positionID)
	if err != nil {
		return PartialCloseQuote{}, err
	}
	order, err := k.PartialClose(cache, position.Account, positionID, closeSizeUsd)
	if err != nil {
		return PartialCloseQuote{}, err
	}
	events, err := k.quotePlace(cache, order, uint256.Int{})
	if err != nil {
		return PartialCloseQuote{}, err
	}
	quote, err := quoteCloseEvents(events, position)
	if err != nil {
		return PartialCloseQuote{}, err
	}
	return PartialCloseQuote{quote.ExitPrice, quote.UnrealizedPnl, quote.TradingFeeUsd, quote.EstimatedPayout}, nil
}

func (k Keeper) QuoteMarket(ctx sdk.Context, marketID uint64) (MarketQuote, error) {
	cache, _ := ctx.CacheContext()
	market, err := k.GetMarket(cache, marketID)
	if err != nil {
		return MarketQuote{}, err
	}
	price, err := k.pricefeed.GetPrice(cache, marketID)
	if err != nil {
		return MarketQuote{}, err
	}
	state, err := k.quoteState(cache, marketID)
	if err != nil {
		return MarketQuote{}, err
	}
	twap, err := k.OracleTWAP(cache, marketID)
	if err != nil {
		return MarketQuote{}, err
	}
	bps, err := quoteMaintenanceBps(market, new(big.Int))
	if err != nil {
		return MarketQuote{}, err
	}
	rate := quoteSigned(state.HourlyRate)
	quote := MarketQuote{IndexPrice: price.Index.BigInt(), MarkPrice: state.MarkPrice.ToBig(), OracleTWAP: twap,
		FundingRatePerSecond: new(big.Int).Quo(rate, big.NewInt(3600)), FundingRate24h: new(big.Int).Mul(rate, big.NewInt(24)),
		LongOI:      new(big.Int).Quo(new(big.Int).Mul(state.LongOpenInterest.ToBig(), state.MarkPrice.ToBig()), quoteScale()),
		ShortOI:     new(big.Int).Quo(new(big.Int).Mul(state.ShortOpenInterest.ToBig(), state.MarkPrice.ToBig()), quoteScale()),
		MaxLeverage: new(big.Int).SetUint64(market.MaxLeverage), MaintenanceMarginBps: bps, Enabled: market.Enabled, PriceStale: !price.Fresh}
	if err := quoteBounds(false, quote.IndexPrice, quote.MarkPrice, quote.OracleTWAP, quote.LongOI, quote.ShortOI); err != nil {
		return MarketQuote{}, err
	}
	if err := quoteBounds(true, quote.FundingRatePerSecond, quote.FundingRate24h); err != nil {
		return MarketQuote{}, err
	}
	return quote, nil
}

// OracleTWAP is the hourly premium-sample average price: the mean of the
// index prices observed at the premium samples of the current funding hour,
// or the current index price before the first sample of the hour.
func (k Keeper) OracleTWAP(ctx sdk.Context, marketID uint64) (*big.Int, error) {
	state, err := k.quoteState(ctx, marketID)
	if err != nil {
		return nil, err
	}
	if state.PremiumCount == 0 {
		return state.IndexPrice.ToBig(), nil
	}
	return new(big.Int).Quo(state.OracleSum.ToBig(), new(big.Int).SetUint64(state.PremiumCount)), nil
}

// quotePlace places order as the quotes simulate it; a non-zero sizeBase
// sizes it in base, as the whole-position close is placed.
func (k Keeper) quotePlace(ctx sdk.Context, order types.Order, sizeBase uint256.Int) ([]types.EventRecord, error) {
	placed, err := k.PlaceOrder(ctx, order.Account, OrderParams{MarketID: order.MarketID, IsLong: order.IsLong,
		OrderType: uint8(order.Type), TimeInForce: uint8(order.TimeInForce), SizeUsd: order.SizeUsd, SizeBase: sizeBase,
		Leverage: order.Leverage, ReduceOnly: order.ReduceOnly})
	if err != nil {
		return nil, err
	}
	return placed.Events, nil
}

func (k Keeper) quotePosition(ctx sdk.Context, id uint256.Int) (types.Position, error) {
	data := ctx.KVStore(k.storeKey).Get(types.PositionKey(id))
	if data == nil {
		return types.Position{}, types.ErrUnknownPosition
	}
	var position types.Position
	if err := position.Unmarshal(data); err != nil {
		return types.Position{}, err
	}
	return position, nil
}

func (k Keeper) quoteState(ctx sdk.Context, marketID uint64) (types.MarketState, error) {
	var state types.MarketState
	data := ctx.KVStore(k.storeKey).Get(types.MarketStateKey(marketID))
	if data == nil {
		return state, types.ErrPriceStale
	}
	if err := state.Unmarshal(data); err != nil {
		return state, err
	}
	return state, nil
}

func quoteSigned(word uint256.Int) *big.Int {
	value := word.ToBig()
	if value.Bit(255) != 0 {
		value.Sub(value, new(big.Int).Lsh(big.NewInt(1), 256))
	}
	return value
}

func quoteScale() *big.Int { return big.NewInt(1_000_000_000_000_000_000) }

func quoteWord(value *big.Int) (uint256.Int, error) {
	if err := quoteBounds(false, value); err != nil {
		return uint256.Int{}, err
	}
	var word uint256.Int
	word.SetFromBig(value)
	return word, nil
}

func quoteBounds(signed bool, values ...*big.Int) error {
	for _, value := range values {
		if value == nil {
			return types.ErrOverflow
		}
		if !signed {
			if value.Sign() < 0 || value.BitLen() > 256 {
				return types.ErrOverflow
			}
		} else {
			lower := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 255))
			upper := new(big.Int).Neg(lower)
			if value.Cmp(lower) < 0 || value.Cmp(upper) >= 0 {
				return types.ErrOverflow
			}
		}
	}
	return nil
}

func quoteMaintenanceBps(market types.Market, notional *big.Int) (*big.Int, error) {
	tiers, err := types.TiersFor(market)
	if err != nil {
		return nil, err
	}
	var bps uint64
	for _, tier := range tiers {
		if notional.Cmp(tier.LowerBoundUsd.BigInt()) < 0 {
			break
		}
		bps = tier.MaintenanceBps
	}
	return new(big.Int).SetUint64(bps), nil
}

type quoteExecutionTotals struct {
	base, notional, feeUsd, feeTokens, pnl, funding, payout, positionID *big.Int
}

func (totals quoteExecutionTotals) price() *big.Int {
	return new(big.Int).Quo(new(big.Int).Mul(totals.notional, quoteScale()), totals.base)
}

func quoteExecution(events []types.EventRecord, account common.Address, positionID *big.Int) (quoteExecutionTotals, error) {
	totals := quoteExecutionTotals{new(big.Int), new(big.Int), new(big.Int), new(big.Int), new(big.Int), new(big.Int), new(big.Int), new(big.Int)}
	for _, event := range events {
		if event.Name != types.EventTradeSettled && event.Name != types.EventFeeCollected {
			continue
		}
		for _, arg := range event.Args {
			if value, ok := arg.(*big.Int); ok && value == nil {
				return totals, types.ErrOverflow
			}
		}
		if _, _, err := perpsabi.PackEvent(event.Name, event.Args...); err != nil {
			return totals, err
		}
		holder, ok := event.Args[1].(common.Address)
		if !ok {
			return totals, fmt.Errorf("perps: invalid quote event account")
		}
		if holder != account {
			continue
		}
		id, ok := event.Args[0].(*big.Int)
		if !ok || id == nil {
			return totals, fmt.Errorf("perps: invalid quote event position")
		}
		if positionID != nil && id.Cmp(positionID) != 0 {
			continue
		}
		if event.Name == types.EventFeeCollected {
			feeType, ok := event.Args[3].(uint8)
			if !ok || feeType != 0 {
				return totals, fmt.Errorf("perps: non-trading fee in taker quote")
			}
			fee, ok := event.Args[5].(*big.Int)
			if !ok || quoteBounds(false, fee) != nil {
				return totals, types.ErrOverflow
			}
			totals.feeTokens.Add(totals.feeTokens, fee)
			continue
		}
		values := make([]*big.Int, 6)
		for i := range values {
			value, ok := event.Args[i+4].(*big.Int)
			if !ok {
				return totals, fmt.Errorf("perps: invalid quote settlement field %d", i+4)
			}
			if err := quoteBounds(i == 2 || i == 4, value); err != nil {
				return totals, err
			}
			values[i] = value
		}
		size, price := values[0], values[1]
		if price.Sign() == 0 || size.Sign() == 0 {
			return totals, fmt.Errorf("perps: empty quote settlement")
		}
		base, remainder := new(big.Int).QuoRem(new(big.Int).Mul(size, quoteScale()), price, new(big.Int))
		if remainder.Sign() != 0 {
			return totals, fmt.Errorf("perps: settlement notional does not encode an exact base size")
		}
		totals.positionID.Set(id)
		totals.base.Add(totals.base, base)
		totals.notional.Add(totals.notional, size)
		totals.pnl.Add(totals.pnl, values[2])
		totals.feeUsd.Add(totals.feeUsd, values[3])
		totals.funding.Add(totals.funding, values[4])
		totals.payout.Add(totals.payout, values[5])
	}
	if totals.base.Sign() == 0 {
		return totals, types.ErrNoLiquidity
	}
	if err := quoteBounds(false, totals.base, totals.notional, totals.feeUsd, totals.feeTokens, totals.payout, totals.positionID); err != nil {
		return totals, err
	}
	if err := quoteBounds(true, totals.pnl, totals.funding); err != nil {
		return totals, err
	}
	return totals, nil
}

func quoteCloseEvents(events []types.EventRecord, position types.Position) (ClosePositionQuote, error) {
	totals, err := quoteExecution(events, position.Account, position.ID.ToBig())
	if err != nil {
		return ClosePositionQuote{}, err
	}
	net := new(big.Int).Sub(new(big.Int).Add(totals.pnl, totals.funding), totals.feeUsd)
	if err := quoteBounds(true, net); err != nil {
		return ClosePositionQuote{}, err
	}
	return ClosePositionQuote{totals.price(), totals.pnl, totals.feeTokens, totals.feeUsd, totals.funding, net, totals.payout}, nil
}
