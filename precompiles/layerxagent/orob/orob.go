package orob

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	agentcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/common"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	upgradekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
)

const (
	OrobAddress              = "0x0000000000000000000000000000000000000901"
	PrecompileName           = "orob"
	ResolveOffsetMethod      = "resolveOffset"
	ResolveOffsetBatchMethod = "resolveOffsetBatch"
	ToOffsetMethod           = "toOffset"
	abiJSON                  = `[
		{"type":"function","name":"resolveOffset","stateMutability":"pure","inputs":[{"name":"oraclePrice","type":"int256"},{"name":"offsetBps","type":"int16"}],"outputs":[{"name":"absolutePrice","type":"int256"}]},
		{"type":"function","name":"resolveOffsetBatch","stateMutability":"pure","inputs":[{"name":"oraclePrice","type":"int256"},{"name":"offsetsBps","type":"int16[]"}],"outputs":[{"name":"absolutePrices","type":"int256[]"}]},
		{"type":"function","name":"toOffset","stateMutability":"pure","inputs":[{"name":"oraclePrice","type":"int256"},{"name":"absolutePrice","type":"int256"}],"outputs":[{"name":"offsetBps","type":"int16"}]}
	]`
)

type OrobPrecompile struct {
	abi          abi.ABI
	maxBatchSize int
}

var _ pcommon.PrecompileExecutor = (*OrobPrecompile)(nil)

func NewPrecompile(upgrade *upgradekeeper.Keeper, planName string, maxBatchSize int) (*agentcommon.Precompile, error) {
	if upgrade == nil || planName == "" {
		return nil, fmt.Errorf("%w: no merge upgrade activation reader bound", agentcommon.ErrNotActive)
	}
	if maxBatchSize < 1 {
		return nil, fmt.Errorf("%w: batch cap must be positive", agentcommon.ErrBatchTooLarge)
	}
	a, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", agentcommon.ErrABIDecode, err)
	}
	p := &OrobPrecompile{abi: a, maxBatchSize: maxBatchSize}
	return agentcommon.NewPrecompile(a, p, common.HexToAddress(OrobAddress), PrecompileName, upgrade, planName), nil
}

func (p *OrobPrecompile) RequiredGas(input []byte, method *abi.Method) uint64 {
	if p == nil || method == nil {
		return pcommon.UnknownMethodCallGas
	}
	resolved, err := p.abi.MethodById(method.ID)
	if err != nil {
		return pcommon.UnknownMethodCallGas
	}
	if len(input) > math.MaxInt-4 {
		return math.MaxUint64
	}
	return agentcommon.MethodGas(resolved.ID, len(input)+4)
}

func (p *OrobPrecompile) Execute(ctx sdk.Context, method *abi.Method, _ common.Address, _ common.Address, args []interface{}, value *big.Int, _ bool, _ *vm.EVM, _ *tracing.Hooks) ([]byte, error) {
	if err := agentcommon.HeightGate(ctx); err != nil {
		return nil, err
	}
	if p == nil || method == nil {
		return nil, fmt.Errorf("%w: method is unavailable", agentcommon.ErrUnknownMethod)
	}
	resolved, err := p.abi.MethodById(method.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", agentcommon.ErrUnknownMethod, err)
	}
	if err := agentcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}
	if err := agentcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}
	oraclePrice, ok := args[0].(*big.Int)
	if !ok || oraclePrice == nil {
		return nil, fmt.Errorf("%w: argument decode failed", agentcommon.ErrABIDecode)
	}
	var result interface{}
	switch resolved.Name {
	case ResolveOffsetMethod:
		offset, ok := args[1].(int16)
		if !ok {
			return nil, fmt.Errorf("%w: argument decode failed", agentcommon.ErrABIDecode)
		}
		result, err = ResolveOffset(oraclePrice, offset)
	case ResolveOffsetBatchMethod:
		offsets, ok := args[1].([]int16)
		if !ok {
			return nil, fmt.Errorf("%w: argument decode failed", agentcommon.ErrABIDecode)
		}
		result, err = ResolveOffsetBatch(oraclePrice, offsets, p.maxBatchSize)
	case ToOffsetMethod:
		absolutePrice, ok := args[1].(*big.Int)
		if !ok || absolutePrice == nil {
			return nil, fmt.Errorf("%w: argument decode failed", agentcommon.ErrABIDecode)
		}
		result, err = ToOffset(oraclePrice, absolutePrice)
	default:
		return nil, fmt.Errorf("%w: no method with id: %#x", agentcommon.ErrUnknownMethod, method.ID)
	}
	if err != nil {
		return nil, err
	}
	data, err := resolved.Outputs.Pack(result)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", agentcommon.ErrABIDecode, err)
	}
	return data, nil
}

func positivePrice(price *big.Int) error {
	if price == nil {
		return fmt.Errorf("%w: argument decode failed", agentcommon.ErrABIDecode)
	}
	if price.Sign() <= 0 {
		return fmt.Errorf("%w: oraclePrice cannot be zero", agentcommon.ErrOrobOraclePrice)
	}
	if price.BitLen() > 255 {
		return fmt.Errorf("%w: result out of range", agentcommon.ErrOverflow)
	}
	return nil
}

func ResolveOffset(oraclePrice *big.Int, offsetBps int16) (*big.Int, error) {
	if err := positivePrice(oraclePrice); err != nil {
		return nil, err
	}
	if offsetBps <= -10000 {
		return nil, fmt.Errorf("%w: offset out of range", agentcommon.ErrOrobOffsetRange)
	}
	price := new(big.Int).Mul(oraclePrice, big.NewInt(10000+int64(offsetBps)))
	price.Quo(price, big.NewInt(10000))
	if price.BitLen() > 255 {
		return nil, fmt.Errorf("%w: result out of range", agentcommon.ErrOverflow)
	}
	return price, nil
}

func ResolveOffsetBatch(oraclePrice *big.Int, offsetsBps []int16, maxBatchSize int) ([]*big.Int, error) {
	if maxBatchSize < 1 || len(offsetsBps) > maxBatchSize {
		return nil, fmt.Errorf("%w: batch over the cap (cap set at activation rehearsal)", agentcommon.ErrBatchTooLarge)
	}
	if len(offsetsBps) == 0 {
		return nil, fmt.Errorf("%w: argument decode failed", agentcommon.ErrABIDecode)
	}
	if err := positivePrice(oraclePrice); err != nil {
		return nil, err
	}
	prices := make([]*big.Int, len(offsetsBps))
	for i, offset := range offsetsBps {
		price, err := ResolveOffset(oraclePrice, offset)
		if err != nil {
			return nil, err
		}
		prices[i] = price
	}
	return prices, nil
}

func ToOffset(oraclePrice, absolutePrice *big.Int) (int16, error) {
	if err := positivePrice(oraclePrice); err != nil {
		return 0, err
	}
	if err := positivePrice(absolutePrice); err != nil {
		return 0, err
	}
	offset := new(big.Int).Sub(absolutePrice, oraclePrice)
	offset.Mul(offset, big.NewInt(10000))
	offset.Quo(offset, oraclePrice)
	if !offset.IsInt64() || offset.Int64() < math.MinInt16 || offset.Int64() > math.MaxInt16 {
		return 0, fmt.Errorf("%w: result out of range", agentcommon.ErrOverflow)
	}
	return int16(offset.Int64()), nil
}
