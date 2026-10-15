package launchpad

import (
	"bytes"
	"math/big"
	"strings"

	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
)

const opticalABIJSON = `[
{"type":"function","name":"getFlags","stateMutability":"view","inputs":[],"outputs":[{"type":"uint8"}]},
{"type":"function","name":"beforeSwap","stateMutability":"nonpayable","inputs":[{"type":"address"},{"type":"address"},{"type":"bool"},{"type":"uint256"}],"outputs":[{"type":"bool"},{"type":"int256"}]},
{"type":"function","name":"afterSwap","stateMutability":"nonpayable","inputs":[{"type":"address"},{"type":"address"},{"type":"bool"},{"type":"uint256"},{"type":"uint256"}],"outputs":[{"type":"bytes4"}]},
{"type":"function","name":"beforeFeeDistribution","stateMutability":"nonpayable","inputs":[{"type":"address"},{"type":"uint256"}],"outputs":[{"type":"uint256"}]},
{"type":"function","name":"afterFeeDistribution","stateMutability":"nonpayable","inputs":[{"type":"address"},{"type":"uint256"}],"outputs":[{"type":"bytes4"}]}
]`

var opticalHookABI, opticalHookABIError = abi.JSON(strings.NewReader(opticalABIJSON))

func hasFlag(flags, flag uint8) bool { return flags&flag != 0 }

func opticalAddress(market types.Market) common.Address {
	return common.HexToAddress(market.Optical)
}

func callOptical(ctx sdk.Context,
	callNested func(sdk.Context, common.Address, common.Address, []byte) (sdk.Context, []byte, error),
	market types.Market, flags uint8, hook string, args ...interface{}) (sdk.Context, []byte, error) {
	var flag uint8
	switch hook {
	case "getFlags":
	case "beforeSwap":
		flag = 1
	case "afterSwap":
		flag = 2
	default:
		return ctx, nil, types.ErrInvalidHookResponse
	}
	if hook != "getFlags" {
		args = append([]interface{}{common.HexToAddress(market.PoolAddress)}, args...)
	}
	return invokeOptical(ctx, callNested, market, common.HexToAddress(market.PoolAddress), flags, flag, hook, args...)
}

func callFeeOptical(ctx sdk.Context,
	callNested func(sdk.Context, common.Address, common.Address, []byte) (sdk.Context, []byte, error),
	market types.Market, hook string, amount *big.Int) (sdk.Context, []byte, error) {
	var flag uint8
	switch hook {
	case "beforeFeeDistribution":
		flag = 4
	case "afterFeeDistribution":
		flag = 8
	default:
		return ctx, nil, types.ErrInvalidHookResponse
	}
	from := launchpadkeeper.Generation2Surfaces[types.RoleFeeAccumulator]
	ctx, ret, err := invokeOptical(ctx, callNested, market, from, 0, 0, "getFlags")
	if err != nil {
		return ctx, ret, err
	}
	return invokeOptical(ctx, callNested, market, from, ret[31], flag, hook,
		common.HexToAddress(market.PoolAddress), amount)
}

func invokeOptical(ctx sdk.Context,
	callNested func(sdk.Context, common.Address, common.Address, []byte) (sdk.Context, []byte, error),
	market types.Market, from common.Address, flags, flag uint8, hook string, args ...interface{}) (sdk.Context, []byte, error) {
	if (market.Optical != "" && !common.IsHexAddress(market.Optical)) || !common.IsHexAddress(market.PoolAddress) {
		return ctx, nil, types.ErrInvalidMarket
	}
	if opticalHookABIError != nil {
		return ctx, nil, opticalHookABIError
	}
	method, ok := opticalHookABI.Methods[hook]
	if !ok {
		return ctx, nil, types.ErrInvalidHookResponse
	}
	for _, arg := range args {
		if amount, ok := arg.(*big.Int); ok && (amount == nil || amount.Sign() < 0 || amount.BitLen() > 256) {
			return ctx, nil, types.ErrOverflow
		}
	}
	input, err := opticalHookABI.Pack(hook, args...)
	if err != nil {
		return ctx, nil, err
	}
	if opticalAddress(market) == (common.Address{}) || (flag != 0 && !hasFlag(flags, flag)) {
		var ret []byte
		switch hook {
		case "getFlags":
			ret, err = method.Outputs.Pack(uint8(0))
		case "beforeSwap":
			ret, err = method.Outputs.Pack(true, new(big.Int))
		case "beforeFeeDistribution":
			ret, err = method.Outputs.Pack(args[1])
		default:
			ret, err = method.Outputs.Pack([4]byte(method.ID))
		}
		return ctx, ret, err
	}
	ctx, ret, err := callNested(ctx, from, opticalAddress(market), input)
	if err != nil {
		return ctx, ret, err
	}
	values, err := method.Outputs.Unpack(ret)
	if err != nil {
		return ctx, nil, vm.ErrExecutionReverted
	}
	switch hook {
	case "beforeSwap":
		delta := values[1].(*big.Int)
		if !values[0].(bool) || delta.Sign() > 0 || new(big.Int).Neg(delta).Cmp(args[3].(*big.Int)) >= 0 {
			return ctx, nil, types.ErrInsufficientInput
		}
	case "afterSwap", "afterFeeDistribution":
		ack := values[0].([4]byte)
		if !bytes.Equal(ack[:], method.ID) {
			if hook == "afterFeeDistribution" {
				return ctx, nil, types.ErrUnauthorized
			}
			return ctx, nil, types.ErrInvalidHookResponse
		}
	case "beforeFeeDistribution":
		if values[0].(*big.Int).Cmp(args[1].(*big.Int)) > 0 {
			return ctx, nil, types.ErrInsufficientSurplus
		}
	}
	return ctx, ret, nil
}
