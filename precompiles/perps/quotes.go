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
	return method.Outputs.Pack(quote)
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
	quote, err := p.keeper.QuoteOpen(ctx, caller, marketID, token, collateralAmount, leverage, isLong)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(quote)
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
	return method.Outputs.Pack(quote)
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
	return method.Outputs.Pack(quote.ExitPrice, quote.ClosedPnl, quote.Fee, quote.EstimatedPayout)
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
