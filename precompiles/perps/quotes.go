package perps

import (
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
)

func init() {
	handlers["quoteMarket"] = quoteMarket
	handlers["quoteOpenPosition"] = quoteOpenPosition
	handlers["quoteClosePosition"] = quoteClosePosition
	handlers["quotePartialClose"] = quotePartialClose
}

// marketQuoteTuple is the MarketQuote tuple of Hyperpax:src/hooks/perps/useQuoter.ts:30-42.
type marketQuoteTuple struct {
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

// openPositionQuoteTuple is the OpenPositionQuote tuple of Hyperpax:src/hooks/perps/useQuoter.ts:7-18.
type openPositionQuoteTuple struct {
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

// closePositionQuoteTuple is the ClosePositionQuote tuple of Hyperpax:src/hooks/perps/useQuoter.ts:20-28.
type closePositionQuoteTuple struct {
	ExitPrice       *big.Int
	UnrealizedPnl   *big.Int
	TradingFee      *big.Int
	TradingFeeUsd   *big.Int
	FundingOwed     *big.Int
	NetPnl          *big.Int
	EstimatedPayout *big.Int
}

// quoteMarket serves quoteMarket(uint256 marketId), the call of
// Hyperpax:src/hooks/perps/useQuoter.ts:119; priceStale reports a pricefeed
// price that is not fresh instead of reverting.
func quoteMarket(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address,
	args []interface{}, _ *vm.EVM) ([]byte, error) {
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	marketID, err := quoteMarketID(args[0])
	if err != nil {
		return nil, err
	}
	quote, err := p.keeper.QuoteMarket(ctx, marketID)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(marketQuoteTuple{
		IndexPrice:           quote.IndexPrice.ToBig(),
		MarkPrice:            quote.MarkPrice.ToBig(),
		OracleTWAP:           quote.OracleTWAP.ToBig(),
		FundingRatePerSecond: new(big.Int).Set(quote.FundingRatePerSecond),
		FundingRate24h:       new(big.Int).Set(quote.FundingRate24h),
		LongOI:               quote.LongOI.ToBig(),
		ShortOI:              quote.ShortOI.ToBig(),
		MaxLeverage:          new(big.Int).SetUint64(quote.MaxLeverage),
		MaintenanceMarginBps: new(big.Int).SetUint64(quote.MaintenanceMarginBps),
		Enabled:              quote.Enabled,
		PriceStale:           quote.PriceStale,
	})
}

// quoteOpenPosition serves quoteOpenPosition(uint256 marketId, address
// collateralToken, uint256 collateralAmount, uint256 leverage, bool isLong),
// the call of Hyperpax:src/hooks/perps/useQuoter.ts:136: what openPosition
// with the same arguments fills for the caller in the same state, at the
// caller's fee tier, without writing state.
func quoteOpenPosition(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, caller common.Address,
	args []interface{}, _ *vm.EVM) ([]byte, error) {
	if err := pcommon.ValidateArgsLength(args, 5); err != nil {
		return nil, err
	}
	marketID, err := quoteMarketID(args[0])
	if err != nil {
		return nil, err
	}
	token, ok := args[1].(common.Address)
	if !ok {
		return nil, fmt.Errorf("perps: collateralToken is %T, want address", args[1])
	}
	collateralAmount, err := quoteWord("collateralAmount", args[2])
	if err != nil {
		return nil, err
	}
	leverage, err := quoteWord("leverage", args[3])
	if err != nil {
		return nil, err
	}
	isLong, ok := args[4].(bool)
	if !ok {
		return nil, fmt.Errorf("perps: isLong is %T, want bool", args[4])
	}
	params, err := p.keeper.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	if token != params.CollateralToken {
		return nil, fmt.Errorf("%w: %s", types.ErrUnsupportedCollateral, token)
	}
	quote, err := p.keeper.QuoteOpen(ctx, caller, marketID, collateralAmount, leverage, isLong)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(openPositionQuoteTuple{
		EntryPrice:           quote.EntryPrice.ToBig(),
		SizeUsd:              quote.SizeUsd.ToBig(),
		CollateralUsd:        quote.CollateralUsd.ToBig(),
		Leverage:             quote.Leverage.ToBig(),
		TradingFee:           quote.TradingFee.ToBig(),
		TradingFeeUsd:        quote.TradingFeeUsd.ToBig(),
		PriceImpact:          quote.PriceImpact.ToBig(),
		LiquidationPrice:     quote.LiquidationPrice.ToBig(),
		EstimatedFunding24h:  new(big.Int).Set(quote.EstimatedFunding24h),
		MaintenanceMarginBps: new(big.Int).SetUint64(quote.MaintenanceMarginBps),
	})
}

// quoteClosePosition serves quoteClosePosition(uint256 positionId), the call
// of Hyperpax:src/hooks/perps/useQuoter.ts:146: the full close of the
// position as closePosition executes it in the same state, without writing
// state.
func quoteClosePosition(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address,
	args []interface{}, _ *vm.EVM) ([]byte, error) {
	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	positionID, err := quoteWord("positionId", args[0])
	if err != nil {
		return nil, err
	}
	quote, err := p.keeper.QuoteClose(ctx, positionID)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(closePositionQuoteTuple{
		ExitPrice:       quote.ExitPrice.ToBig(),
		UnrealizedPnl:   new(big.Int).Set(quote.UnrealizedPnl),
		TradingFee:      quote.TradingFee.ToBig(),
		TradingFeeUsd:   quote.TradingFeeUsd.ToBig(),
		FundingOwed:     new(big.Int).Set(quote.FundingOwed),
		NetPnl:          new(big.Int).Set(quote.NetPnl),
		EstimatedPayout: quote.EstimatedPayout.ToBig(),
	})
}

// quotePartialClose serves quotePartialClose(uint256 positionId, uint256
// closeSizeUsd): the exit price, PnL, fee and payout partialClose with the
// same arguments books in the same state, without writing state.
func quotePartialClose(p PrecompileExecutor, ctx sdk.Context, method *abi.Method, _ common.Address,
	args []interface{}, _ *vm.EVM) ([]byte, error) {
	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	positionID, err := quoteWord("positionId", args[0])
	if err != nil {
		return nil, err
	}
	closeSizeUsd, err := quoteWord("closeSizeUsd", args[1])
	if err != nil {
		return nil, err
	}
	quote, err := p.keeper.QuotePartialClose(ctx, positionID, closeSizeUsd)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(quote.ExitPrice.ToBig(), new(big.Int).Set(quote.ClosedPnl), quote.Fee.ToBig(),
		quote.EstimatedPayout.ToBig())
}

// quoteMarketID reads a uint256 market id; an id above the uint64 range names
// no market.
func quoteMarketID(arg interface{}) (uint64, error) {
	id, ok := arg.(*big.Int)
	if !ok || id == nil {
		return 0, fmt.Errorf("perps: marketId is %T, want uint256", arg)
	}
	if !id.IsUint64() {
		return 0, fmt.Errorf("%w: %s", types.ErrUnknownMarket, id)
	}
	return id.Uint64(), nil
}

// quoteWord reads a uint256 argument as a 256-bit word.
func quoteWord(name string, arg interface{}) (uint256.Int, error) {
	value, ok := arg.(*big.Int)
	if !ok || value == nil {
		return uint256.Int{}, fmt.Errorf("perps: %s is %T, want uint256", name, arg)
	}
	var word uint256.Int
	if value.Sign() < 0 || word.SetFromBig(value) {
		return uint256.Int{}, fmt.Errorf("%w: %s %s is outside uint256", types.ErrOverflow, name, value)
	}
	return word, nil
}
