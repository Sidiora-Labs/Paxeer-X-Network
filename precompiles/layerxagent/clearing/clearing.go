package clearing

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"

	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	agentcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/common"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	upgradekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
)

const (
	ClearingAddress       = "0x0000000000000000000000000000000000000902"
	ComputeClearingMethod = "computeClearing"
	abiJSON               = `[{"type":"function","name":"computeClearing","stateMutability":"pure","inputs":[{"name":"oraclePrice","type":"int256"},{"name":"buyOrders","type":"tuple[]","components":[{"name":"offsetBps","type":"int16"},{"name":"size","type":"uint128"}]},{"name":"sellOrders","type":"tuple[]","components":[{"name":"offsetBps","type":"int16"},{"name":"size","type":"uint128"}]}],"outputs":[{"name":"clearingOffsetBps","type":"int16"},{"name":"clearingPrice","type":"int256"},{"name":"matchedVolume","type":"uint256"}]}]`
)

type clearingOrder = struct {
	OffsetBps int16    `json:"offsetBps"`
	Size      *big.Int `json:"size"`
}

type ClearingPrecompile struct {
	*agentcommon.Precompile
}

type PrecompileExecutor struct {
	maxOrders int
}

var _ vm.PrecompiledContract = (*ClearingPrecompile)(nil)
var _ pcommon.PrecompileExecutor = PrecompileExecutor{}

func NewPrecompile(upgrade *upgradekeeper.Keeper, planName string, maxOrders int) (*ClearingPrecompile, error) {
	if maxOrders <= 0 {
		return nil, fmt.Errorf("%w: order cap must be positive", agentcommon.ErrBatchTooLarge)
	}
	a, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", agentcommon.ErrABIDecode, err)
	}
	return &ClearingPrecompile{agentcommon.NewPrecompile(a, PrecompileExecutor{maxOrders: maxOrders}, ethcommon.HexToAddress(ClearingAddress), "clearing", upgrade, planName)}, nil
}

func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	if method == nil {
		return pcommon.UnknownMethodCallGas
	}
	if len(input) > math.MaxInt-4 {
		return math.MaxUint64
	}
	return agentcommon.MethodGas(method.ID, len(input)+4)
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, _ ethcommon.Address, _ ethcommon.Address, args []interface{}, value *big.Int, _ bool, _ *vm.EVM, _ *tracing.Hooks) ([]byte, error) {
	if err := agentcommon.HeightGate(ctx); err != nil {
		return nil, err
	}
	if method == nil || method.Name != ComputeClearingMethod {
		return nil, fmt.Errorf("%w: unknown clearing method", agentcommon.ErrUnknownMethod)
	}
	if err := agentcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}
	if err := agentcommon.ValidateArgsLength(args, 3); err != nil {
		return nil, err
	}
	oraclePrice, ok := args[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("%w: oraclePrice must be int256", agentcommon.ErrABIDecode)
	}
	buys, ok := args[1].([]clearingOrder)
	if !ok {
		return nil, fmt.Errorf("%w: buyOrders must be (int16,uint128)[]", agentcommon.ErrABIDecode)
	}
	sells, ok := args[2].([]clearingOrder)
	if !ok {
		return nil, fmt.Errorf("%w: sellOrders must be (int16,uint128)[]", agentcommon.ErrABIDecode)
	}
	offset, price, volume, err := ComputeClearing(oraclePrice, buys, sells, p.maxOrders)
	if err != nil {
		return nil, err
	}
	result, err := method.Outputs.Pack(offset, price, volume)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", agentcommon.ErrABIDecode, err)
	}
	return result, nil
}

func ComputeClearing(oraclePrice *big.Int, buys, sells []clearingOrder, maxOrders int) (int16, *big.Int, *big.Int, error) {
	if maxOrders <= 0 || len(buys) > maxOrders || len(sells) > maxOrders-len(buys) {
		return 0, nil, nil, fmt.Errorf("%w: batch over the cap (cap set at activation rehearsal)", agentcommon.ErrBatchTooLarge)
	}
	if oraclePrice == nil {
		return 0, nil, nil, fmt.Errorf("%w: oraclePrice must be int256", agentcommon.ErrABIDecode)
	}
	if oraclePrice.Sign() <= 0 {
		return 0, nil, nil, fmt.Errorf("%w: oraclePrice cannot be zero", agentcommon.ErrOrobOraclePrice)
	}
	if oraclePrice.BitLen() > 255 {
		return 0, nil, nil, fmt.Errorf("%w: oraclePrice must be int256", agentcommon.ErrABIDecode)
	}
	levels := make(map[int16]struct{})
	for _, orders := range [][]clearingOrder{buys, sells} {
		for _, order := range orders {
			if order.OffsetBps <= -10000 {
				return 0, nil, nil, fmt.Errorf("%w: offset out of range", agentcommon.ErrOrobOffsetRange)
			}
			if order.Size == nil || order.Size.Sign() < 0 || order.Size.BitLen() > 128 {
				return 0, nil, nil, fmt.Errorf("%w: order size must be uint128", agentcommon.ErrABIDecode)
			}
			levels[order.OffsetBps] = struct{}{}
		}
	}
	if len(buys) == 0 || len(sells) == 0 {
		return 0, new(big.Int).Set(oraclePrice), new(big.Int), nil
	}
	candidates := make([]int16, 0, len(levels))
	for offset := range levels {
		candidates = append(candidates, offset)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i] < candidates[j] })
	bestOffset := int16(0)
	bestVolume := new(big.Int)
	for _, level := range candidates {
		demand, supply := new(big.Int), new(big.Int)
		for _, order := range buys {
			if order.OffsetBps >= level {
				demand.Add(demand, order.Size)
			}
		}
		for _, order := range sells {
			if order.OffsetBps <= level {
				supply.Add(supply, order.Size)
			}
		}
		volume := demand
		if supply.Cmp(demand) < 0 {
			volume = supply
		}
		if volume.Cmp(bestVolume) > 0 {
			bestOffset = level
			bestVolume.Set(volume)
		}
	}
	if bestVolume.Sign() == 0 {
		return 0, new(big.Int).Set(oraclePrice), bestVolume, nil
	}
	price := new(big.Int).Mul(oraclePrice, big.NewInt(10000+int64(bestOffset)))
	price.Quo(price, big.NewInt(10000))
	if price.BitLen() > 255 || bestVolume.BitLen() > 256 {
		return 0, nil, nil, fmt.Errorf("%w: result out of range", agentcommon.ErrOverflow)
	}
	return bestOffset, price, bestVolume, nil
}
