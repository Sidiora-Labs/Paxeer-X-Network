package clearing

import (
	"encoding/hex"
	"errors"
	"math"
	"math/big"
	"testing"

	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	agentcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	codectypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	upgradekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/keeper"
	upgradetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

func clearingContext(t *testing.T) (sdk.Context, *upgradekeeper.Keeper) {
	t.Helper()
	db := dbm.NewMemDB()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	key := sdk.NewKVStoreKey(upgradetypes.StoreKey)
	ms := store.NewCommitMultiStore(db)
	ms.MountStoreWithDB(key, sdk.StoreTypeIAVL, db)
	require.NoError(t, ms.LoadLatestVersion())
	ctx := sdk.NewContext(ms, tmproto.Header{Height: 100}, false)
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	upgrade := upgradekeeper.NewKeeper(nil, key, cdc, t.TempDir(), nil)
	upgrade.SetDone(ctx, "merge-plan")
	return ctx, &upgrade
}

func clearingEVM(ctx sdk.Context) *vm.EVM {
	db := new(state.DBImpl)
	db.WithCtx(ctx)
	return &vm.EVM{StateDB: db}
}

func clearingPrecompile(t *testing.T, cap int) (*ClearingPrecompile, *vm.EVM) {
	t.Helper()
	ctx, upgrade := clearingContext(t)
	p, err := NewPrecompile(upgrade, "merge-plan", cap)
	require.NoError(t, err)
	return p, clearingEVM(ctx)
}

func order(offset int16, size int64) clearingOrder {
	return clearingOrder{OffsetBps: offset, Size: big.NewInt(size)}
}

func clearingInput(t *testing.T, p *ClearingPrecompile, oracle *big.Int, buys, sells []clearingOrder) []byte {
	t.Helper()
	input, err := p.Pack(ComputeClearingMethod, oracle, buys, sells)
	require.NoError(t, err)
	return input
}

func clearingResult(t *testing.T, p *ClearingPrecompile, evm *vm.EVM, oracle *big.Int, buys, sells []clearingOrder, offset int16, price, volume string) {
	t.Helper()
	input := clearingInput(t, p, oracle, buys, sells)
	result, err := p.Run(evm, ethcommon.Address{}, ethcommon.Address{}, input, nil, true, false, nil)
	require.NoError(t, err)
	values, err := p.Methods[ComputeClearingMethod].Outputs.Unpack(result)
	require.NoError(t, err)
	require.Len(t, values, 3)
	require.Equal(t, offset, values[0])
	require.Equal(t, price, values[1].(*big.Int).String())
	require.Equal(t, volume, values[2].(*big.Int).String())
}

func clearingRefusal(t *testing.T, p *ClearingPrecompile, evm *vm.EVM, input []byte, code agentcommon.ErrorCode, text string) {
	t.Helper()
	data, err := p.Run(evm, ethcommon.Address{}, ethcommon.Address{}, input, nil, true, false, nil)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	reason, err := abi.UnpackRevert(data)
	require.NoError(t, err)
	require.Equal(t, text, reason)
	require.Equal(t, pcommon.RevertReason(errors.New(text)), data)
	if evm != nil {
		if db := state.GetDBImpl(evm.StateDB); db != nil {
			require.ErrorIs(t, db.GetPrecompileError(), code)
		}
	}
}

func TestComputeClearingLowestMaxLevel(t *testing.T) {
	p, evm := clearingPrecompile(t, 16)
	for _, vector := range []struct {
		name          string
		buys, sells   []clearingOrder
		offset        int16
		price, volume string
	}{
		{"acceptance", []clearingOrder{order(20, 5), order(10, 3)}, []clearingOrder{order(0, 4), order(10, 6)}, 10, "1001", "8"},
		{"reordered", []clearingOrder{order(10, 3), order(20, 5)}, []clearingOrder{order(10, 6), order(0, 4)}, 10, "1001", "8"},
		{"lowest tied level", []clearingOrder{order(20, 5)}, []clearingOrder{order(10, 5)}, 10, "1001", "5"},
		{"negative tied level", []clearingOrder{order(20, 5)}, []clearingOrder{order(-10, 5)}, -10, "999", "5"},
		{"duplicate offsets", []clearingOrder{order(20, 2), order(20, 3), order(10, 3)}, []clearingOrder{order(10, 6), order(0, 2), order(0, 2)}, 10, "1001", "8"},
		{"equal bounds", []clearingOrder{order(10, 5)}, []clearingOrder{order(10, 4)}, 10, "1001", "4"},
		{"supply limits", []clearingOrder{order(20, 9), order(0, 4)}, []clearingOrder{order(0, 5), order(10, 6)}, 10, "1001", "9"},
	} {
		t.Run(vector.name, func(t *testing.T) {
			clearingResult(t, p, evm, big.NewInt(1000), vector.buys, vector.sells, vector.offset, vector.price, vector.volume)
		})
	}
}

func TestComputeClearingNoCross(t *testing.T) {
	p, evm := clearingPrecompile(t, 8)
	for _, vector := range []struct {
		name        string
		buys, sells []clearingOrder
	}{
		{"empty", nil, nil},
		{"empty buys", nil, []clearingOrder{order(0, 4)}},
		{"empty sells", []clearingOrder{order(0, 5)}, nil},
		{"no cross", []clearingOrder{order(-50, 5)}, []clearingOrder{order(0, 4)}},
		{"unsorted no cross", []clearingOrder{order(-60, 5), order(-50, 2)}, []clearingOrder{order(10, 4), order(0, 3)}},
		{"zero buy size", []clearingOrder{order(10, 0)}, []clearingOrder{order(0, 4)}},
		{"zero sell size", []clearingOrder{order(10, 4)}, []clearingOrder{order(0, 0)}},
	} {
		t.Run(vector.name, func(t *testing.T) {
			clearingResult(t, p, evm, big.NewInt(1000), vector.buys, vector.sells, 0, "1000", "0")
		})
	}
	legacy, err := hex.DecodeString("b6a4c9ae")
	require.NoError(t, err)
	require.Equal(t, uint64(3000), p.RequiredGas(legacy))
	data, remaining, err := vm.RunPrecompiledContract(p, evm, ethcommon.Address{}, ethcommon.Address{}, legacy, 3007, nil, nil, true, false)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, uint64(7), remaining)
	reason, err := abi.UnpackRevert(data)
	require.NoError(t, err)
	require.Equal(t, "LXP_PRECOMPILE_UNKNOWN_METHOD: no method with id: 0xb6a4c9ae", reason)
	require.ErrorIs(t, state.GetDBImpl(evm.StateDB).GetPrecompileError(), agentcommon.ErrUnknownMethod)
}

func TestComputeClearingCaps(t *testing.T) {
	p, evm := clearingPrecompile(t, 4)
	buys := []clearingOrder{order(10, 1), order(10, 2)}
	sells := []clearingOrder{order(0, 2), order(0, 3)}
	clearingResult(t, p, evm, big.NewInt(1000), buys, sells, 0, "1000", "3")
	for _, vector := range []struct {
		name        string
		buys, sells []clearingOrder
	}{
		{"combined cap", buys, append(sells, order(0, 1))},
		{"buy cap", []clearingOrder{order(0, 1), order(0, 1), order(0, 1), order(0, 1), order(0, 1)}, nil},
		{"sell cap", nil, []clearingOrder{order(0, 1), order(0, 1), order(0, 1), order(0, 1), order(0, 1)}},
	} {
		t.Run(vector.name, func(t *testing.T) {
			input := clearingInput(t, p, big.NewInt(1000), vector.buys, vector.sells)
			clearingRefusal(t, p, evm, input, agentcommon.ErrBatchTooLarge, "LXP_AGENT_BATCH_TOO_LARGE: batch over the cap (cap set at activation rehearsal)")
		})
	}
	for _, price := range []int64{-1, 0} {
		input := clearingInput(t, p, big.NewInt(price), buys, sells)
		clearingRefusal(t, p, evm, input, agentcommon.ErrOrobOraclePrice, "LXP_AGENT_OROB_ORACLE_PRICE: oraclePrice cannot be zero")
		input = clearingInput(t, p, big.NewInt(price), nil, nil)
		clearingRefusal(t, p, evm, input, agentcommon.ErrOrobOraclePrice, "LXP_AGENT_OROB_ORACLE_PRICE: oraclePrice cannot be zero")
	}
	input := clearingInput(t, p, big.NewInt(0), append(buys, order(-10000, 1)), sells)
	clearingRefusal(t, p, evm, input, agentcommon.ErrBatchTooLarge, "LXP_AGENT_BATCH_TOO_LARGE: batch over the cap (cap set at activation rehearsal)")
}

func TestComputeClearingOffsetBounds(t *testing.T) {
	p, evm := clearingPrecompile(t, 8)
	for _, offset := range []int16{math.MinInt16, -10001, -10000} {
		for _, vector := range []struct{ buys, sells []clearingOrder }{
			{[]clearingOrder{order(offset, 1)}, []clearingOrder{order(0, 1)}},
			{[]clearingOrder{order(0, 1)}, []clearingOrder{order(offset, 1)}},
			{nil, []clearingOrder{order(offset, 1)}},
			{[]clearingOrder{order(offset, 1)}, nil},
		} {
			input := clearingInput(t, p, big.NewInt(10000), vector.buys, vector.sells)
			clearingRefusal(t, p, evm, input, agentcommon.ErrOrobOffsetRange, "LXP_AGENT_OROB_OFFSET_RANGE: offset out of range")
		}
	}
	clearingResult(t, p, evm, big.NewInt(10000), []clearingOrder{order(-9999, 1)}, []clearingOrder{order(-9999, 1)}, -9999, "1", "1")
	clearingResult(t, p, evm, big.NewInt(10000), []clearingOrder{order(math.MaxInt16, 1)}, []clearingOrder{order(math.MaxInt16, 1)}, math.MaxInt16, "42767", "1")
	clearingResult(t, p, evm, big.NewInt(1001), []clearingOrder{order(-25, 1)}, []clearingOrder{order(-25, 1)}, -25, "998", "1")
	clearingResult(t, p, evm, big.NewInt(1001), []clearingOrder{order(25, 1)}, []clearingOrder{order(25, 1)}, 25, "1003", "1")
	clearingResult(t, p, evm, big.NewInt(1), []clearingOrder{order(-9999, 1)}, []clearingOrder{order(-9999, 1)}, -9999, "0", "1")
}

func TestComputeClearingArithmeticBounds(t *testing.T) {
	p, evm := clearingPrecompile(t, 8)
	maxPrice := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(1))
	maxSize := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))
	buys := []clearingOrder{{OffsetBps: 0, Size: maxSize}, {OffsetBps: 0, Size: maxSize}}
	sells := []clearingOrder{{OffsetBps: 0, Size: maxSize}, {OffsetBps: 0, Size: maxSize}}
	clearingResult(t, p, evm, maxPrice, buys, sells, 0, maxPrice.String(), "680564733841876926926749214863536422910")
	input := clearingInput(t, p, maxPrice, []clearingOrder{order(1, 1)}, []clearingOrder{order(1, 1)})
	clearingRefusal(t, p, evm, input, agentcommon.ErrOverflow, "LXP_AGENT_OVERFLOW: result out of range")
	clearingResult(t, p, evm, maxPrice, nil, nil, 0, maxPrice.String(), "0")
	clearingResult(t, p, evm, maxPrice, []clearingOrder{order(-1, 1)}, []clearingOrder{order(0, 1)}, 0, maxPrice.String(), "0")
}

func TestComputeClearingInputValidation(t *testing.T) {
	for _, vector := range []struct {
		name        string
		oracle      *big.Int
		buys, sells []clearingOrder
		code        agentcommon.ErrorCode
		text        string
	}{
		{"nil price", nil, nil, nil, agentcommon.ErrABIDecode, "LXP_AGENT_ABI_DECODE: oraclePrice must be int256"},
		{"large price", new(big.Int).Lsh(big.NewInt(1), 255), nil, nil, agentcommon.ErrABIDecode, "LXP_AGENT_ABI_DECODE: oraclePrice must be int256"},
		{"nil buy size", big.NewInt(1), []clearingOrder{{OffsetBps: 0}}, nil, agentcommon.ErrABIDecode, "LXP_AGENT_ABI_DECODE: order size must be uint128"},
		{"nil sell size", big.NewInt(1), nil, []clearingOrder{{OffsetBps: 0}}, agentcommon.ErrABIDecode, "LXP_AGENT_ABI_DECODE: order size must be uint128"},
		{"negative size", big.NewInt(1), []clearingOrder{order(0, -1)}, nil, agentcommon.ErrABIDecode, "LXP_AGENT_ABI_DECODE: order size must be uint128"},
		{"large size", big.NewInt(1), nil, []clearingOrder{{OffsetBps: 0, Size: new(big.Int).Lsh(big.NewInt(1), 128)}}, agentcommon.ErrABIDecode, "LXP_AGENT_ABI_DECODE: order size must be uint128"},
	} {
		t.Run(vector.name, func(t *testing.T) {
			offset, price, volume, err := ComputeClearing(vector.oracle, vector.buys, vector.sells, 4)
			require.ErrorIs(t, err, vector.code)
			require.EqualError(t, err, vector.text)
			require.Zero(t, offset)
			require.Nil(t, price)
			require.Nil(t, volume)
		})
	}
	p, evm := clearingPrecompile(t, 4)
	input := clearingInput(t, p, big.NewInt(1), []clearingOrder{{OffsetBps: 0, Size: new(big.Int).Lsh(big.NewInt(1), 128)}}, nil)
	clearingRefusal(t, p, evm, input, agentcommon.ErrABIDecode, "LXP_AGENT_ABI_DECODE: order size must be uint128")
}

func TestClearingStructABI(t *testing.T) {
	p, _ := clearingPrecompile(t, 8)
	require.Equal(t, ethcommon.HexToAddress(ClearingAddress), p.Address())
	require.Equal(t, "clearing", p.GetName())
	require.Len(t, p.Methods, 1)
	require.Empty(t, p.Events)
	method := p.Methods[ComputeClearingMethod]
	require.Equal(t, []byte{0x83, 0x97, 0xe9, 0xf8}, method.ID)
	require.Equal(t, "computeClearing(int256,(int16,uint128)[],(int16,uint128)[])", method.Sig)
	require.Equal(t, "pure", method.StateMutability)
	for i, typ := range []string{"int16", "int256", "uint256"} {
		require.Equal(t, typ, method.Outputs[i].Type.String())
	}
	input := clearingInput(t, p, big.NewInt(1000), []clearingOrder{order(10, 5)}, []clearingOrder{order(0, 4)})
	resolved, err := p.MethodById(input[:4])
	require.NoError(t, err)
	require.Equal(t, ComputeClearingMethod, resolved.Name)
	args, err := resolved.Inputs.Unpack(input[4:])
	require.NoError(t, err)
	buys, ok := args[1].([]clearingOrder)
	require.True(t, ok)
	require.Equal(t, int16(10), buys[0].OffsetBps)
	require.Equal(t, "5", buys[0].Size.String())
	_, err = p.MethodById([]byte{0xb6, 0xa4, 0xc9, 0xae})
	require.Error(t, err)
}

func TestClearingHeightGate(t *testing.T) {
	ctx, upgrade := clearingContext(t)
	p, err := NewPrecompile(upgrade, "merge-plan", 4)
	require.NoError(t, err)
	input := clearingInput(t, p, big.NewInt(1000), []clearingOrder{order(10, 5)}, []clearingOrder{order(0, 4)})
	below := ctx.WithBlockHeight(99).WithGasMeter(sdk.NewGasMeter(1, 1, 1))
	clearingRefusal(t, p, clearingEVM(below), input, agentcommon.ErrNotActive, "LXP_AGENT_NOT_ACTIVE: merge-plan not active at height 99")
	require.Zero(t, below.GasMeter().GasConsumed())
	for _, height := range []int64{100, 101} {
		clearingResult(t, p, clearingEVM(ctx.WithBlockHeight(height)), big.NewInt(1000), []clearingOrder{order(10, 5)}, []clearingOrder{order(0, 4)}, 0, "1000", "4")
	}
	unbound, err := NewPrecompile(nil, "merge-plan", 4)
	require.NoError(t, err)
	clearingRefusal(t, unbound, clearingEVM(ctx), input, agentcommon.ErrNotActive, "LXP_AGENT_NOT_ACTIVE: no merge upgrade activation reader bound")
	unplanned, err := NewPrecompile(upgrade, "other-plan", 4)
	require.NoError(t, err)
	clearingRefusal(t, unplanned, clearingEVM(ctx), input, agentcommon.ErrNotActive, "LXP_AGENT_NOT_ACTIVE: other-plan not active at height 100")
	require.NoError(t, upgrade.ScheduleUpgrade(ctx, upgradetypes.Plan{Name: "other-plan", Height: 101}))
	clearingRefusal(t, unplanned, clearingEVM(ctx.WithBlockHeight(101)), input, agentcommon.ErrNotActive, "LXP_AGENT_NOT_ACTIVE: other-plan not active at height 101")
}

func TestClearingFrameworkRefusals(t *testing.T) {
	p, evm := clearingPrecompile(t, 4)
	for _, vector := range []struct {
		name  string
		evm   *vm.EVM
		input []byte
		code  agentcommon.ErrorCode
		text  string
	}{
		{"nil EVM", nil, nil, agentcommon.ErrNoContext, "LXP_PRECOMPILE_NO_CONTEXT: cannot get context from EVM"},
		{"missing state", &vm.EVM{}, nil, agentcommon.ErrNoContext, "LXP_PRECOMPILE_NO_CONTEXT: cannot get context from EVM"},
		{"missing context", clearingEVM(sdk.Context{}), nil, agentcommon.ErrNoContext, "LXP_PRECOMPILE_NO_CONTEXT: cannot get context from EVM"},
		{"short input", evm, []byte{0x83, 0x97, 0xe9}, agentcommon.ErrInputTooShort, "LXP_PRECOMPILE_INPUT_TOO_SHORT: input too short to extract method ID"},
		{"unknown input", evm, []byte{0xde, 0xad, 0xbe, 0xef}, agentcommon.ErrUnknownMethod, "LXP_PRECOMPILE_UNKNOWN_METHOD: no method with id: 0xdeadbeef"},
		{"missing args", evm, p.Methods[ComputeClearingMethod].ID, agentcommon.ErrABIDecode, "LXP_AGENT_ABI_DECODE: abi: attempting to unmarshal an empty string while arguments are expected"},
	} {
		t.Run(vector.name, func(t *testing.T) { clearingRefusal(t, p, vector.evm, vector.input, vector.code, vector.text) })
	}
	input := clearingInput(t, p, big.NewInt(1), nil, nil)
	data, err := p.Run(evm, ethcommon.Address{}, ethcommon.Address{}, input, big.NewInt(1), true, false, nil)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	reason, err := abi.UnpackRevert(data)
	require.NoError(t, err)
	require.Equal(t, "LXP_PRECOMPILE_NON_PAYABLE: sending funds to a non-payable function", reason)
	require.ErrorIs(t, state.GetDBImpl(evm.StateDB).GetPrecompileError(), agentcommon.ErrNonPayable)
}

func TestClearingGas(t *testing.T) {
	p, evm := clearingPrecompile(t, 4)
	input := clearingInput(t, p, big.NewInt(1000), []clearingOrder{order(10, 5)}, []clearingOrder{order(0, 4)})
	require.Len(t, input, 292)
	require.Equal(t, uint64(6876), p.RequiredGas(input))
	method := p.Methods[ComputeClearingMethod]
	executor := PrecompileExecutor{maxOrders: 4}
	require.Equal(t, uint64(6876), executor.RequiredGas(input[4:], &method))
	require.Equal(t, uint64(3000), executor.RequiredGas(nil, nil))
	for _, unknown := range [][]byte{nil, {1}, {1, 2, 3}, {0xb6, 0xa4, 0xc9, 0xae}} {
		require.Equal(t, uint64(3000), p.RequiredGas(unknown))
	}
	_, remaining, err := vm.RunPrecompiledContract(p, evm, ethcommon.Address{}, ethcommon.Address{}, input, 6875, nil, nil, true, false)
	require.ErrorIs(t, err, vm.ErrOutOfGas)
	require.Zero(t, remaining)
	data, remaining, err := vm.RunPrecompiledContract(p, evm, ethcommon.Address{}, ethcommon.Address{}, input, 6883, nil, nil, true, false)
	require.NoError(t, err)
	require.Equal(t, uint64(7), remaining)
	result, err := method.Outputs.Unpack(data)
	require.NoError(t, err)
	require.Equal(t, int16(0), result[0])
	require.Equal(t, "1000", result[1].(*big.Int).String())
	require.Equal(t, "4", result[2].(*big.Int).String())
}

func TestClearingDirectExecutor(t *testing.T) {
	ctx, upgrade := clearingContext(t)
	ctx = agentcommon.WithHeightGate(ctx, upgrade, "merge-plan")
	p, err := NewPrecompile(upgrade, "merge-plan", 4)
	require.NoError(t, err)
	method := p.Methods[ComputeClearingMethod]
	executor := PrecompileExecutor{maxOrders: 4}
	for _, vector := range []struct {
		name string
		args []interface{}
		code agentcommon.ErrorCode
		text string
	}{
		{"arg count", nil, agentcommon.ErrArgCount, "LXP_PRECOMPILE_ARG_COUNT: expected 3 arguments but got 0"},
		{"extra arg", []interface{}{nil, nil, nil, nil}, agentcommon.ErrArgCount, "LXP_PRECOMPILE_ARG_COUNT: expected 3 arguments but got 4"},
		{"oracle type", []interface{}{int64(1), nil, nil}, agentcommon.ErrABIDecode, "LXP_AGENT_ABI_DECODE: oraclePrice must be int256"},
		{"buy type", []interface{}{big.NewInt(1), []int16{}, nil}, agentcommon.ErrABIDecode, "LXP_AGENT_ABI_DECODE: buyOrders must be (int16,uint128)[]"},
		{"sell type", []interface{}{big.NewInt(1), []clearingOrder{}, []int16{}}, agentcommon.ErrABIDecode, "LXP_AGENT_ABI_DECODE: sellOrders must be (int16,uint128)[]"},
		{"nil oracle", []interface{}{(*big.Int)(nil), []clearingOrder{}, []clearingOrder{}}, agentcommon.ErrABIDecode, "LXP_AGENT_ABI_DECODE: oraclePrice must be int256"},
	} {
		t.Run(vector.name, func(t *testing.T) {
			result, err := executor.Execute(ctx, &method, ethcommon.Address{}, ethcommon.Address{}, vector.args, nil, true, nil, nil)
			require.Nil(t, result)
			require.ErrorIs(t, err, vector.code)
			require.EqualError(t, err, vector.text)
		})
	}
	for _, method := range []*abi.Method{nil, {Name: "unsupported"}} {
		_, err := executor.Execute(ctx, method, ethcommon.Address{}, ethcommon.Address{}, nil, nil, true, nil, nil)
		require.ErrorIs(t, err, agentcommon.ErrUnknownMethod)
		require.EqualError(t, err, "LXP_PRECOMPILE_UNKNOWN_METHOD: unknown clearing method")
	}
	_, err = executor.Execute(sdk.Context{}, &method, ethcommon.Address{}, ethcommon.Address{}, nil, nil, true, nil, nil)
	require.ErrorIs(t, err, agentcommon.ErrNoContext)
}

func TestClearingPureAndInputOwnership(t *testing.T) {
	p, evm := clearingPrecompile(t, 8)
	ctx := state.GetDBImpl(evm.StateDB).Ctx()
	buys := []clearingOrder{order(20, 5), order(10, 3)}
	sells := []clearingOrder{order(10, 6), order(0, 4)}
	oracle := big.NewInt(1000)
	input := clearingInput(t, p, oracle, buys, sells)
	events := ctx.EventManager().Events()
	for _, call := range []struct{ readOnly, delegate bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		data, err := p.Run(evm, ethcommon.Address{}, ethcommon.HexToAddress(ClearingAddress), input, nil, call.readOnly, call.delegate, nil)
		require.NoError(t, err)
		result, err := p.Methods[ComputeClearingMethod].Outputs.Unpack(data)
		require.NoError(t, err)
		require.Equal(t, int16(10), result[0])
		require.Equal(t, "1001", result[1].(*big.Int).String())
		require.Equal(t, "8", result[2].(*big.Int).String())
	}
	offset, price, volume, err := ComputeClearing(oracle, buys, sells, 8)
	require.NoError(t, err)
	require.Equal(t, int16(10), offset)
	price.SetInt64(0)
	volume.SetInt64(0)
	require.Equal(t, "1000", oracle.String())
	require.Equal(t, int16(20), buys[0].OffsetBps)
	require.Equal(t, "5", buys[0].Size.String())
	require.Equal(t, int16(10), buys[1].OffsetBps)
	require.Equal(t, "3", buys[1].Size.String())
	require.Equal(t, int16(10), sells[0].OffsetBps)
	require.Equal(t, "6", sells[0].Size.String())
	require.Equal(t, int16(0), sells[1].OffsetBps)
	require.Equal(t, "4", sells[1].Size.String())
	require.Equal(t, events, ctx.EventManager().Events())
	_, price, volume, err = ComputeClearing(oracle, nil, nil, 8)
	require.NoError(t, err)
	price.SetInt64(0)
	volume.SetInt64(1)
	require.Equal(t, "1000", oracle.String())
}

func TestClearingConfiguredCap(t *testing.T) {
	ctx, upgrade := clearingContext(t)
	for _, cap := range []int{-1, 0} {
		p, err := NewPrecompile(upgrade, "merge-plan", cap)
		require.Nil(t, p)
		require.ErrorIs(t, err, agentcommon.ErrBatchTooLarge)
		require.EqualError(t, err, "LXP_AGENT_BATCH_TOO_LARGE: order cap must be positive")
	}
	p, err := NewPrecompile(upgrade, "merge-plan", 1)
	require.NoError(t, err)
	input := clearingInput(t, p, big.NewInt(1), []clearingOrder{order(0, 1)}, []clearingOrder{order(0, 1)})
	clearingRefusal(t, p, clearingEVM(ctx), input, agentcommon.ErrBatchTooLarge, "LXP_AGENT_BATCH_TOO_LARGE: batch over the cap (cap set at activation rehearsal)")
	_, _, _, err = ComputeClearing(big.NewInt(1), nil, nil, 0)
	require.ErrorIs(t, err, agentcommon.ErrBatchTooLarge)
}
