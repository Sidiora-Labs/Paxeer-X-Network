package pofq

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
	PofqAddress              = "0x0000000000000000000000000000000000000904"
	PrecompileName           = "pofq"
	ScoreFillMethod          = "scoreFill"
	ScoreBatchMethod         = "scoreBatch"
	UpdateRollingScoreMethod = "updateRollingScore"
)

const abiJSON = `[
	{"type":"function","name":"scoreFill","stateMutability":"pure","inputs":[{"name":"fillPrice","type":"int256"},{"name":"oraclePrice","type":"int256"}],"outputs":[{"name":"score","type":"uint256"}]},
	{"type":"function","name":"scoreBatch","stateMutability":"pure","inputs":[{"name":"fills","type":"tuple[]","components":[{"name":"fillPrice","type":"int256"},{"name":"oraclePrice","type":"int256"},{"name":"size","type":"uint128"}]}],"outputs":[{"name":"avgScore","type":"uint256"},{"name":"totalVolume","type":"uint256"}]},
	{"type":"function","name":"updateRollingScore","stateMutability":"pure","inputs":[{"name":"currentScore","type":"uint256"},{"name":"currentWeight","type":"uint256"},{"name":"newScore","type":"uint256"},{"name":"newWeight","type":"uint256"},{"name":"decayBps","type":"uint16"}],"outputs":[{"name":"updatedScore","type":"uint256"},{"name":"updatedWeight","type":"uint256"}]}
]`

type Fill = struct {
	FillPrice   *big.Int `json:"fillPrice"`
	OraclePrice *big.Int `json:"oraclePrice"`
	Size        *big.Int `json:"size"`
}

type PofqPrecompile struct {
	*agentcommon.Precompile
}

type PrecompileExecutor struct {
	abi      abi.ABI
	maxBatch uint64
}

var _ vm.PrecompiledContract = (*PofqPrecompile)(nil)
var _ pcommon.PrecompileExecutor = (*PrecompileExecutor)(nil)

func NewPrecompile(upgrade *upgradekeeper.Keeper, planName string, maxBatch uint64) (*PofqPrecompile, error) {
	if maxBatch == 0 {
		return nil, fmt.Errorf("%w: batch cap must be positive", agentcommon.ErrBatchTooLarge)
	}
	a, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", agentcommon.ErrABIDecode, err)
	}
	executor := &PrecompileExecutor{abi: a, maxBatch: maxBatch}
	return &PofqPrecompile{agentcommon.NewPrecompile(a, executor, common.HexToAddress(PofqAddress), PrecompileName, upgrade, planName)}, nil
}

func (p *PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	if method == nil || p == nil {
		return pcommon.UnknownMethodCallGas
	}
	if _, err := p.abi.MethodById(method.ID); err != nil {
		return pcommon.UnknownMethodCallGas
	}
	if len(input) > math.MaxInt-4 {
		return math.MaxUint64
	}
	return agentcommon.MethodGas(method.ID, len(input)+4)
}

func (p *PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, _ common.Address, _ common.Address, args []interface{}, value *big.Int, readOnly bool, _ *vm.EVM, _ *tracing.Hooks) ([]byte, error) {
	if err := agentcommon.HeightGate(ctx); err != nil {
		return nil, err
	}
	if p == nil || method == nil {
		return nil, agentcommon.ErrUnknownMethod
	}
	canonical, err := p.abi.MethodById(method.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", agentcommon.ErrUnknownMethod, err)
	}
	if !canonical.IsConstant() {
		if err := agentcommon.GuardWrite(ctx, readOnly, false); err != nil {
			return nil, err
		}
	}
	if err := agentcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}
	if err := agentcommon.ValidateArgsLength(args, len(canonical.Inputs)); err != nil {
		return nil, err
	}
	switch canonical.Name {
	case ScoreFillMethod:
		fill, fillOK := args[0].(*big.Int)
		oracle, oracleOK := args[1].(*big.Int)
		if !fillOK || !oracleOK {
			return nil, decodeError()
		}
		score, err := ScoreFill(fill, oracle)
		if err != nil {
			return nil, err
		}
		return pack(canonical, score)
	case ScoreBatchMethod:
		fills, ok := args[0].([]Fill)
		if !ok {
			return nil, decodeError()
		}
		score, volume, err := ScoreBatch(fills, p.maxBatch)
		if err != nil {
			return nil, err
		}
		return pack(canonical, score, volume)
	case UpdateRollingScoreMethod:
		values := make([]*big.Int, 4)
		for i := range values {
			var ok bool
			values[i], ok = args[i].(*big.Int)
			if !ok {
				return nil, decodeError()
			}
		}
		decay, ok := args[4].(uint16)
		if !ok {
			return nil, decodeError()
		}
		score, weight, err := UpdateRollingScore(values[0], values[1], values[2], values[3], decay)
		if err != nil {
			return nil, err
		}
		return pack(canonical, score, weight)
	default:
		return nil, agentcommon.ErrUnknownMethod
	}
}

func ScoreFill(fillPrice, oraclePrice *big.Int) (*big.Int, error) {
	if !validInt256(fillPrice) || !validInt256(oraclePrice) {
		return nil, decodeError()
	}
	if oraclePrice.Sign() <= 0 || fillPrice.Sign() <= 0 {
		return nil, fmt.Errorf("%w: oraclePrice cannot be zero", agentcommon.ErrOrobOraclePrice)
	}
	scale := big.NewInt(1000000000000000000)
	difference := new(big.Int).Sub(fillPrice, oraclePrice)
	difference.Abs(difference)
	penalty := new(big.Int).Mul(difference, scale)
	penalty.Quo(penalty, oraclePrice)
	score := new(big.Int).Sub(scale, penalty)
	if score.Sign() < 0 {
		score.SetInt64(0)
	}
	if !validUint(score, 256) {
		return nil, overflowError()
	}
	return score, nil
}

func ScoreBatch(fills []Fill, maxBatch uint64) (*big.Int, *big.Int, error) {
	if uint64(len(fills)) > maxBatch {
		return nil, nil, fmt.Errorf("%w: batch over the cap (cap set at activation rehearsal)", agentcommon.ErrBatchTooLarge)
	}
	weightedScore := new(big.Int)
	volume := new(big.Int)
	for _, fill := range fills {
		if !validUint(fill.Size, 128) {
			return nil, nil, decodeError()
		}
		score, err := ScoreFill(fill.FillPrice, fill.OraclePrice)
		if err != nil {
			return nil, nil, err
		}
		weightedScore.Add(weightedScore, new(big.Int).Mul(score, fill.Size))
		volume.Add(volume, fill.Size)
	}
	if !validUint(volume, 256) {
		return nil, nil, overflowError()
	}
	if volume.Sign() == 0 {
		return new(big.Int), volume, nil
	}
	score := new(big.Int).Quo(weightedScore, volume)
	if !validUint(score, 256) {
		return nil, nil, overflowError()
	}
	return score, volume, nil
}

func UpdateRollingScore(currentScore, currentWeight, newScore, newWeight *big.Int, decayBps uint16) (*big.Int, *big.Int, error) {
	if decayBps >= 10000 {
		return nil, nil, fmt.Errorf("%w: decayBps must be < 10000", agentcommon.ErrPofqDecayRange)
	}
	for _, value := range []*big.Int{currentScore, currentWeight, newScore, newWeight} {
		if !validUint(value, 256) {
			return nil, nil, decodeError()
		}
	}
	oldWeight := new(big.Int).Mul(currentWeight, big.NewInt(int64(10000-decayBps)))
	addedWeight := new(big.Int).Mul(newWeight, big.NewInt(10000))
	w := new(big.Int).Add(oldWeight, addedWeight)
	if w.Sign() == 0 {
		return new(big.Int), new(big.Int), nil
	}
	score := new(big.Int).Mul(currentScore, oldWeight)
	score.Add(score, new(big.Int).Mul(newScore, addedWeight))
	score.Quo(score, w)
	weight := new(big.Int).Quo(w, big.NewInt(10000))
	if !validUint(score, 256) || !validUint(weight, 256) {
		return nil, nil, overflowError()
	}
	return score, weight, nil
}

func validUint(value *big.Int, bits int) bool {
	return value != nil && value.Sign() >= 0 && value.BitLen() <= bits
}

func validInt256(value *big.Int) bool {
	if value == nil {
		return false
	}
	limit := new(big.Int).Lsh(big.NewInt(1), 255)
	if value.Sign() < 0 {
		return value.Cmp(new(big.Int).Neg(limit)) >= 0
	}
	return value.Cmp(limit) < 0
}

func decodeError() error {
	return fmt.Errorf("%w: argument decode failed", agentcommon.ErrABIDecode)
}

func overflowError() error {
	return fmt.Errorf("%w: result out of range", agentcommon.ErrOverflow)
}

func pack(method *abi.Method, values ...interface{}) ([]byte, error) {
	output, err := method.Outputs.Pack(values...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", agentcommon.ErrABIDecode, err)
	}
	return output, nil
}
