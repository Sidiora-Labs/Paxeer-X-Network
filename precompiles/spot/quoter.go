package spot

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"math"
	"math/big"

	spotkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
)

//go:embed abi/quoter.json
var quoterABI []byte

type quoterExecutor struct {
	keeper    *spotkeeper.Keeper
	evmKeeper utils.EVMKeeper
}

type quoterRequest struct {
	TokenIn   common.Address
	TokenOut  common.Address
	Amount    *big.Int
	IsExactIn bool
}

func NewQuoterPrecompile(k *spotkeeper.Keeper, evmKeeper utils.EVMKeeper) (*pcommon.Precompile, error) {
	if k == nil || evmKeeper == nil {
		return nil, errors.New("spot: quoter requires spot and EVM keepers")
	}
	parsed, err := abi.JSON(bytes.NewReader(quoterABI))
	if err != nil {
		return nil, err
	}
	return pcommon.NewPrecompile(parsed, &quoterExecutor{keeper: k, evmKeeper: evmKeeper}, common.HexToAddress(types.PECORQuoterAddress), "spot_quoter"), nil
}

func (p quoterExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	base := uint64(3000)
	switch method.Name {
	case "batchQuote", "getAllLiquidityInfo", "getLiquidityInfo", "getTWAP", "getTokenPrices":
		base = 6000
	case "WETH", "pecor", "priceOracle", "vault", "getTokenPrice", "quoteExactIn", "quoteExactInNative", "quoteExactInToNative", "quoteExactOut", "quoteMarketBuy", "quoteMarketSell", "setPecor":
	default:
		return pcommon.UnknownMethodCallGas
	}
	if uint64(len(input)) > (math.MaxUint64-base)/16 {
		return math.MaxUint64
	}
	return base + 16*uint64(len(input))
}

func (p quoterExecutor) Execute(ctx sdk.Context, method *abi.Method, caller, _ common.Address, args []interface{}, value *big.Int, _ bool, evm *vm.EVM, _ *tracing.Hooks) (ret []byte, err error) {
	defer func() {
		var refusal *types.AbiError
		if errors.As(err, &refusal) {
			ret = refusal.Data()
		} else if err != nil {
			ret = pcommon.RevertReason(err)
		}
	}()
	if err = pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}
	if method == nil {
		return nil, errors.New("spot: missing quoter method")
	}
	if len(args) != len(method.Inputs) {
		return nil, errors.New("spot: invalid quoter argument count")
	}
	if evm != nil {
		gas := pcommon.GetRemainingGas(ctx, p.evmKeeper)
		run := &spotkeeper.Run{EVM: evm, Gas: gas}
		ctx = spotkeeper.WithRun(ctx, run)
		defer func() {
			ctx.GasMeter().ConsumeGas(p.evmKeeper.GetCosmosGasLimitFromEVMGas(ctx, gas-run.Gas), "spot quoter token reads")
		}()
	}
	switch method.Name {
	case "setPecor":
		return nil, types.OwnableUnauthorizedAccount(caller)
	case "pecor":
		return method.Outputs.Pack(common.HexToAddress(types.PECORAddress))
	case "priceOracle":
		return method.Outputs.Pack(common.HexToAddress("0x0000000000000000000000000000000000001020"))
	case "vault":
		return method.Outputs.Pack(common.HexToAddress(types.PECORVaultAddress))
	case "WETH":
		token, err := p.nativeToken(ctx)
		if err != nil {
			return nil, err
		}
		return method.Outputs.Pack(token)
	case "quoteExactIn", "quoteExactOut", "quoteMarketBuy", "quoteMarketSell":
		return method.Outputs.Pack(p.keeper.Quote(ctx, args[0].(common.Address), args[1].(common.Address), sdk.NewIntFromBigInt(args[2].(*big.Int)), method.Name != "quoteExactOut"))
	case "quoteExactInNative", "quoteExactInToNative":
		native, err := p.nativeToken(ctx)
		if err != nil {
			return nil, err
		}
		in, out := native, args[0].(common.Address)
		if method.Name == "quoteExactInToNative" {
			in, out = out, in
		}
		return method.Outputs.Pack(p.keeper.Quote(ctx, in, out, sdk.NewIntFromBigInt(args[1].(*big.Int)), true))
	case "batchQuote":
		requests := *abi.ConvertType(args[0], new([]quoterRequest)).(*[]quoterRequest)
		results := make([]spotkeeper.QuoteResult, len(requests))
		for i, request := range requests {
			if request.TokenIn == (common.Address{}) || request.TokenOut == (common.Address{}) {
				native, err := p.nativeToken(ctx)
				if err != nil {
					return nil, err
				}
				if request.TokenIn == (common.Address{}) {
					request.TokenIn = native
				}
				if request.TokenOut == (common.Address{}) {
					request.TokenOut = native
				}
			}
			results[i] = p.keeper.Quote(ctx, request.TokenIn, request.TokenOut, sdk.NewIntFromBigInt(request.Amount), request.IsExactIn)
		}
		return method.Outputs.Pack(results)
	case "getLiquidityInfo":
		available, price, stale, err := p.keeper.LiquidityInfo(ctx, args[0].(common.Address))
		if err != nil {
			return nil, err
		}
		return method.Outputs.Pack(available, price, stale)
	case "getAllLiquidityInfo":
		params, err := p.keeper.GetParams(ctx)
		if err != nil {
			return nil, err
		}
		tokens := make([]common.Address, len(params.Tokens))
		reserves := make([]*big.Int, len(tokens))
		prices := make([]*big.Int, len(tokens))
		stale := make([]bool, len(tokens))
		for i, token := range params.Tokens {
			tokens[i] = token.Address
			reserves[i], prices[i], stale[i], err = p.keeper.LiquidityInfo(ctx, token.Address)
			if err != nil {
				return nil, err
			}
		}
		return method.Outputs.Pack(tokens, reserves, prices, stale)
	case "getTokenPrice":
		price, timestamp, stale := p.keeper.TokenPrice(ctx, args[0].(common.Address))
		return method.Outputs.Pack(price, timestamp, stale)
	case "getTokenPrices":
		tokens := args[0].([]common.Address)
		prices := make([]*big.Int, len(tokens))
		timestamps := make([]*big.Int, len(tokens))
		stale := make([]bool, len(tokens))
		for i, token := range tokens {
			prices[i], timestamps[i], stale[i] = p.keeper.TokenPrice(ctx, token)
		}
		return method.Outputs.Pack(prices, timestamps, stale)
	case "getTWAP":
		period := args[1].(*big.Int)
		if !period.IsUint64() || period.Sign() == 0 || ctx.BlockTime().Unix() < 0 || period.Uint64() > uint64(ctx.BlockTime().Unix()) {
			return nil, types.ErrTwapWindowInvalid
		}
		token := args[0].(common.Address)
		_, _, stale := p.keeper.TokenPrice(ctx, token)
		if stale {
			return nil, types.ErrStalePrice
		}
		price, err := p.keeper.TWAP(ctx, token, period.Uint64())
		if err != nil {
			return nil, err
		}
		return method.Outputs.Pack(price.BigInt())
	default:
		return nil, fmt.Errorf("spot: unknown quoter method %s", method.Name)
	}
}

func (p quoterExecutor) nativeToken(ctx sdk.Context) (common.Address, error) {
	token, _, exists := p.evmKeeper.GetERC20NativePointer(ctx, p.evmKeeper.GetBaseDenom(ctx))
	if !exists || token == (common.Address{}) {
		return common.Address{}, types.ErrUnsupportedToken
	}
	return token, nil
}
