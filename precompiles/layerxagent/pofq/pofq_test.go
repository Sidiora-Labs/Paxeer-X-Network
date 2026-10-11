package pofq_test

import (
	"bytes"
	"encoding/hex"
	"math"
	"math/big"
	"testing"

	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	agentcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/pofq"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	codectypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	upgradekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/keeper"
	upgradetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

func decimal(t *testing.T, text string) *big.Int {
	t.Helper()
	value, ok := new(big.Int).SetString(text, 10)
	require.True(t, ok)
	return value
}

func fixture(t *testing.T, maxBatch uint64) (*pofq.PofqPrecompile, sdk.Context, *upgradekeeper.Keeper) {
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
	p, err := pofq.NewPrecompile(&upgrade, "merge-plan", maxBatch)
	require.NoError(t, err)
	return p, ctx, &upgrade
}

func evmContext(ctx sdk.Context) (*vm.EVM, *state.DBImpl) {
	db := new(state.DBImpl)
	db.WithCtx(ctx)
	return &vm.EVM{StateDB: db}, db
}

func input(t *testing.T, p *pofq.PofqPrecompile, name string, args ...interface{}) []byte {
	t.Helper()
	data, err := p.GetABI().Pack(name, args...)
	require.NoError(t, err)
	return data
}

func run(t *testing.T, p *pofq.PofqPrecompile, ctx sdk.Context, name string, args ...interface{}) []*big.Int {
	t.Helper()
	data := input(t, p, name, args...)
	require.Equal(t, uint64(1000+3*len(data)), p.RequiredGas(data))
	evm, db := evmContext(ctx)
	events := len(ctx.EventManager().Events())
	result, err := p.Run(evm, ethcommon.Address{}, p.Address(), data, nil, true, false, nil)
	require.NoError(t, err)
	require.NoError(t, db.GetPrecompileError())
	require.Len(t, ctx.EventManager().Events(), events)
	method := p.GetABI().Methods[name]
	values, err := method.Outputs.Unpack(result)
	require.NoError(t, err)
	require.Len(t, result, 32*len(values))
	decoded := make([]*big.Int, len(values))
	for i, value := range values {
		var ok bool
		decoded[i], ok = value.(*big.Int)
		require.True(t, ok)
	}
	return decoded
}

func assertRevert(t *testing.T, data []byte, err error, db *state.DBImpl, code agentcommon.ErrorCode, message string) {
	t.Helper()
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.GreaterOrEqual(t, len(data), 4)
	require.Equal(t, []byte{0x08, 0xc3, 0x79, 0xa0}, data[:4])
	text, decodeErr := abi.UnpackRevert(data)
	require.NoError(t, decodeErr)
	require.Equal(t, message, text)
	if db != nil {
		require.ErrorIs(t, db.GetPrecompileError(), code)
	}
}

func refusal(t *testing.T, p *pofq.PofqPrecompile, ctx sdk.Context, name string, code agentcommon.ErrorCode, message string, args ...interface{}) {
	t.Helper()
	evm, db := evmContext(ctx)
	data, err := p.Run(evm, ethcommon.Address{}, p.Address(), input(t, p, name, args...), nil, true, false, nil)
	assertRevert(t, data, err, db, code, message)
}

func TestScoreFill(t *testing.T) {
	p, ctx, _ := fixture(t, 4)
	for _, test := range []struct {
		name   string
		fill   string
		oracle string
		want   string
	}{
		{"exact", "1000", "1000", "1000000000000000000"},
		{"above", "1010", "1000", "990000000000000000"},
		{"below", "990", "1000", "990000000000000000"},
		{"clamped", "3000", "1000", "0"},
		{"boundary", "2000", "1000", "0"},
		{"unit", "1", "1000", "1000000000000000"},
		{"fraction", "4", "3", "666666666666666667"},
		{"fraction below", "2", "3", "666666666666666667"},
		{"largest equal", "57896044618658097711785492504343953926634992332820282019728792003956564819967", "57896044618658097711785492504343953926634992332820282019728792003956564819967", "1000000000000000000"},
		{"large intermediate", "57896044618658097711785492504343953926634992332820282019728792003956564819967", "1", "0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fill, oracle := decimal(t, test.fill), decimal(t, test.oracle)
			got := run(t, p, ctx, pofq.ScoreFillMethod, fill, oracle)
			require.Equal(t, test.want, got[0].String())
			require.Equal(t, test.fill, fill.String())
			require.Equal(t, test.oracle, oracle.String())
		})
	}
	for _, test := range []struct {
		name   string
		fill   int64
		oracle int64
	}{
		{"zero oracle", 1000, 0},
		{"negative oracle", 1000, -1},
		{"zero fill", 0, 1000},
		{"negative fill", -1, 1000},
	} {
		t.Run(test.name, func(t *testing.T) {
			refusal(t, p, ctx, pofq.ScoreFillMethod, agentcommon.ErrOrobOraclePrice, "LXP_AGENT_OROB_ORACLE_PRICE: oraclePrice cannot be zero", big.NewInt(test.fill), big.NewInt(test.oracle))
		})
	}
}

func TestScoreBatch(t *testing.T) {
	p, ctx, _ := fixture(t, 3)
	for _, test := range []struct {
		name   string
		fills  []pofq.Fill
		score  string
		volume string
	}{
		{"vector", []pofq.Fill{{big.NewInt(1000), big.NewInt(1000), big.NewInt(2)}, {big.NewInt(1010), big.NewInt(1000), big.NewInt(2)}}, "995000000000000000", "4"},
		{"empty", []pofq.Fill{}, "0", "0"},
		{"zero volume", []pofq.Fill{{big.NewInt(1010), big.NewInt(1000), big.NewInt(0)}}, "0", "0"},
		{"weighted", []pofq.Fill{{big.NewInt(1000), big.NewInt(1000), big.NewInt(1)}, {big.NewInt(1500), big.NewInt(1000), big.NewInt(3)}}, "625000000000000000", "4"},
		{"zero sized", []pofq.Fill{{big.NewInt(3000), big.NewInt(1000), big.NewInt(0)}, {big.NewInt(1000), big.NewInt(1000), big.NewInt(2)}}, "1000000000000000000", "2"},
		{"at cap", []pofq.Fill{{big.NewInt(1000), big.NewInt(1000), big.NewInt(1)}, {big.NewInt(2000), big.NewInt(1000), big.NewInt(1)}, {big.NewInt(2000), big.NewInt(1000), big.NewInt(1)}}, "333333333333333333", "3"},
		{"full size", []pofq.Fill{{big.NewInt(1000), big.NewInt(1000), decimal(t, "340282366920938463463374607431768211455")}, {big.NewInt(1010), big.NewInt(1000), decimal(t, "340282366920938463463374607431768211455")}}, "995000000000000000", "680564733841876926926749214863536422910"},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := make([][3]string, len(test.fills))
			for i, fill := range test.fills {
				before[i] = [3]string{fill.FillPrice.String(), fill.OraclePrice.String(), fill.Size.String()}
			}
			got := run(t, p, ctx, pofq.ScoreBatchMethod, test.fills)
			require.Equal(t, test.score, got[0].String())
			require.Equal(t, test.volume, got[1].String())
			for i, fill := range test.fills {
				require.Equal(t, before[i], [3]string{fill.FillPrice.String(), fill.OraclePrice.String(), fill.Size.String()})
			}
		})
	}
	for _, oracle := range []int64{0, -1} {
		refusal(t, p, ctx, pofq.ScoreBatchMethod, agentcommon.ErrOrobOraclePrice, "LXP_AGENT_OROB_ORACLE_PRICE: oraclePrice cannot be zero", []pofq.Fill{{big.NewInt(1000), big.NewInt(oracle), big.NewInt(1)}})
	}
	fills := make([]pofq.Fill, 4)
	for i := range fills {
		fills[i] = pofq.Fill{big.NewInt(1000), big.NewInt(0), big.NewInt(1)}
	}
	refusal(t, p, ctx, pofq.ScoreBatchMethod, agentcommon.ErrBatchTooLarge, "LXP_AGENT_BATCH_TOO_LARGE: batch over the cap (cap set at activation rehearsal)", fills)
	for _, size := range []*big.Int{big.NewInt(-1), new(big.Int).Lsh(big.NewInt(1), 128)} {
		calldata := input(t, p, pofq.ScoreBatchMethod, []pofq.Fill{{big.NewInt(1000), big.NewInt(1000), big.NewInt(1)}})
		copy(calldata[132:164], size.FillBytes(make([]byte, 32)))
		if size.Sign() < 0 {
			for i := 132; i < 164; i++ {
				calldata[i] = 0xff
			}
		}
		evm, db := evmContext(ctx)
		data, err := p.Run(evm, ethcommon.Address{}, p.Address(), calldata, nil, true, false, nil)
		assertRevert(t, data, err, db, agentcommon.ErrABIDecode, "LXP_AGENT_ABI_DECODE: argument decode failed")
	}
}

func TestUpdateRollingScoreBounded(t *testing.T) {
	p, ctx, _ := fixture(t, 4)
	max := "115792089237316195423570985008687907853269984665640564039457584007913129639935"
	for _, test := range []struct {
		name   string
		cs     string
		cw     string
		ns     string
		nw     string
		decay  uint16
		score  string
		weight string
	}{
		{"vector", "1000000000000000000", "10", "500000000000000000", "10", 5000, "666666666666666666", "15"},
		{"no decay", "1000000000000000000", "10", "500000000000000000", "10", 0, "750000000000000000", "20"},
		{"empty weights", "1000000000000000000", "0", "500000000000000000", "0", 0, "0", "0"},
		{"new only", "1000000000000000000", "0", "500000000000000000", "10", 9999, "500000000000000000", "10"},
		{"old only", "1000000000000000000", "10", "500000000000000000", "0", 5000, "1000000000000000000", "5"},
		{"weight truncates", "1000000000000000000", "1", "0", "0", 9999, "1000000000000000000", "0"},
		{"fractional old weight", "10", "1", "0", "1", 5000, "3", "1"},
		{"largest intermediate", max, max, max, "0", 0, max, max},
		{"largest new", "0", "0", max, max, 9999, max, max},
		{"zero scores", "0", "10", "0", "10", 5000, "0", "15"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cs, cw, ns, nw := decimal(t, test.cs), decimal(t, test.cw), decimal(t, test.ns), decimal(t, test.nw)
			got := run(t, p, ctx, pofq.UpdateRollingScoreMethod, cs, cw, ns, nw, test.decay)
			require.Equal(t, test.score, got[0].String())
			require.Equal(t, test.weight, got[1].String())
			require.Equal(t, []string{test.cs, test.cw, test.ns, test.nw}, []string{cs.String(), cw.String(), ns.String(), nw.String()})
		})
	}
	for _, decay := range []uint16{10000, math.MaxUint16} {
		refusal(t, p, ctx, pofq.UpdateRollingScoreMethod, agentcommon.ErrPofqDecayRange, "LXP_AGENT_POFQ_DECAY_RANGE: decayBps must be < 10000", big.NewInt(1), big.NewInt(1), big.NewInt(1), big.NewInt(1), decay)
	}
	refusal(t, p, ctx, pofq.UpdateRollingScoreMethod, agentcommon.ErrOverflow, "LXP_AGENT_OVERFLOW: result out of range", decimal(t, max), decimal(t, max), decimal(t, max), big.NewInt(1), uint16(0))
	refusal(t, p, ctx, pofq.UpdateRollingScoreMethod, agentcommon.ErrOverflow, "LXP_AGENT_OVERFLOW: result out of range", decimal(t, max), decimal(t, max), decimal(t, max), decimal(t, max), uint16(5000))
	rollingScore, rollingWeight := big.NewInt(1000000000000000000), big.NewInt(10)
	for _, weight := range []string{"15", "17", "18", "19", "19", "19"} {
		got := run(t, p, ctx, pofq.UpdateRollingScoreMethod, rollingScore, rollingWeight, big.NewInt(1000000000000000000), big.NewInt(10), uint16(5000))
		require.Equal(t, "1000000000000000000", got[0].String())
		require.Equal(t, weight, got[1].String())
		rollingScore, rollingWeight = got[0], got[1]
	}
}

func TestSelectorsAndGas(t *testing.T) {
	p, _, _ := fixture(t, 4)
	require.Equal(t, ethcommon.HexToAddress(pofq.PofqAddress), p.Address())
	require.Equal(t, "pofq", p.GetName())
	require.Len(t, p.GetABI().Methods, 3)
	require.Empty(t, p.GetABI().Events)
	for _, row := range []struct {
		name      string
		signature string
		selector  string
		args      []interface{}
	}{
		{pofq.ScoreFillMethod, "scoreFill(int256,int256)", "e087a6ba", []interface{}{big.NewInt(1010), big.NewInt(1000)}},
		{pofq.ScoreBatchMethod, "scoreBatch((int256,int256,uint128)[])", "1b1fba48", []interface{}{[]pofq.Fill{{big.NewInt(1000), big.NewInt(1000), big.NewInt(2)}}}},
		{pofq.UpdateRollingScoreMethod, "updateRollingScore(uint256,uint256,uint256,uint256,uint16)", "a8f32b0e", []interface{}{big.NewInt(1), big.NewInt(1), big.NewInt(1), big.NewInt(1), uint16(5000)}},
	} {
		t.Run(row.name, func(t *testing.T) {
			method := p.GetABI().Methods[row.name]
			require.Equal(t, row.signature, method.Sig)
			require.Equal(t, row.selector, hex.EncodeToString(method.ID))
			require.Equal(t, method.ID, crypto.Keccak256([]byte(row.signature))[:4])
			require.True(t, method.IsConstant())
			calldata := input(t, p, row.name, row.args...)
			require.Equal(t, uint64(1000+3*len(calldata)), p.RequiredGas(calldata))
			require.Equal(t, p.RequiredGas(calldata), p.GetExecutor().RequiredGas(calldata[4:], &method))
			require.Equal(t, uint64(1012), p.RequiredGas(method.ID))
			require.Equal(t, uint64(1012), p.GetExecutor().RequiredGas(nil, &method))
		})
	}
	for _, calldata := range [][]byte{nil, {1}, {1, 2, 3}, {0xde, 0xad, 0xbe, 0xef}, {0xc6, 0xf7, 0x59, 0x1b}} {
		require.Equal(t, uint64(3000), p.RequiredGas(calldata))
	}
	executor := new(pofq.PrecompileExecutor)
	require.Equal(t, uint64(3000), executor.RequiredGas(nil, nil))
	require.Equal(t, uint64(3000), executor.RequiredGas(nil, &abi.Method{ID: []byte{0xde, 0xad, 0xbe, 0xef}}))
}

func TestActivationAndPureCalls(t *testing.T) {
	p, ctx, upgrade := fixture(t, 4)
	for _, method := range []struct {
		name string
		args []interface{}
		want []string
	}{
		{pofq.ScoreFillMethod, []interface{}{big.NewInt(1010), big.NewInt(1000)}, []string{"990000000000000000"}},
		{pofq.ScoreBatchMethod, []interface{}{[]pofq.Fill{{big.NewInt(1000), big.NewInt(1000), big.NewInt(2)}}}, []string{"1000000000000000000", "2"}},
		{pofq.UpdateRollingScoreMethod, []interface{}{big.NewInt(1000000000000000000), big.NewInt(10), big.NewInt(500000000000000000), big.NewInt(10), uint16(5000)}, []string{"666666666666666666", "15"}},
	} {
		t.Run(method.name, func(t *testing.T) {
			calldata := input(t, p, method.name, method.args...)
			for _, height := range []int64{99, 100, 101} {
				for _, readOnly := range []bool{false, true} {
					for _, delegate := range []bool{false, true} {
						meter := sdk.NewGasMeter(1, 1, 1)
						evm, db := evmContext(ctx.WithBlockHeight(height).WithGasMeter(meter))
						result, err := p.Run(evm, ethcommon.Address{}, p.Address(), calldata, nil, readOnly, delegate, nil)
						if height < 100 {
							assertRevert(t, result, err, db, agentcommon.ErrNotActive, "LXP_AGENT_NOT_ACTIVE: merge-plan not active at height 99")
						} else {
							require.NoError(t, err)
							var expected []byte
							for _, value := range method.want {
								expected = append(expected, decimal(t, value).FillBytes(make([]byte, 32))...)
							}
							require.Equal(t, expected, result)
						}
						var expectedGas sdk.Gas
						require.Equal(t, expectedGas, meter.GasConsumed())
					}
				}
			}
		})
	}
	for _, test := range []struct {
		upgrade *upgradekeeper.Keeper
		plan    string
	}{
		{nil, "merge-plan"}, {upgrade, ""}, {upgrade, "other-plan"},
	} {
		unbound, err := pofq.NewPrecompile(test.upgrade, test.plan, 4)
		require.NoError(t, err)
		evm, db := evmContext(ctx)
		data, err := unbound.Run(evm, ethcommon.Address{}, unbound.Address(), input(t, unbound, pofq.ScoreFillMethod, big.NewInt(1000), big.NewInt(1000)), nil, true, false, nil)
		require.ErrorIs(t, err, vm.ErrExecutionReverted)
		require.ErrorIs(t, db.GetPrecompileError(), agentcommon.ErrNotActive)
		text, decodeErr := abi.UnpackRevert(data)
		require.NoError(t, decodeErr)
		if test.plan == "other-plan" {
			require.Equal(t, "LXP_AGENT_NOT_ACTIVE: other-plan not active at height 100", text)
		} else {
			require.Equal(t, "LXP_AGENT_NOT_ACTIVE: no merge upgrade activation reader bound", text)
		}
	}
	scheduledCtx, _ := ctx.CacheContext()
	require.NoError(t, upgrade.ScheduleUpgrade(scheduledCtx, upgradetypes.Plan{Name: "scheduled-plan", Height: 100}))
	scheduled, err := pofq.NewPrecompile(upgrade, "scheduled-plan", 4)
	require.NoError(t, err)
	refusal(t, scheduled, scheduledCtx, pofq.ScoreFillMethod, agentcommon.ErrNotActive, "LXP_AGENT_NOT_ACTIVE: scheduled-plan not active at height 100", big.NewInt(1000), big.NewInt(1000))
}

func TestFrameworkRefusals(t *testing.T) {
	p, ctx, _ := fixture(t, 4)
	for _, selector := range [][]byte{{0xde, 0xad, 0xbe, 0xef}, {0xc6, 0xf7, 0x59, 0x1b}} {
		evm, db := evmContext(ctx)
		data, err := p.Run(evm, ethcommon.Address{}, p.Address(), selector, nil, true, false, nil)
		assertRevert(t, data, err, db, agentcommon.ErrUnknownMethod, "LXP_PRECOMPILE_UNKNOWN_METHOD: no method with id: 0x"+hex.EncodeToString(selector))
	}
	for _, calldata := range [][]byte{nil, {1}, {1, 2, 3}} {
		evm, db := evmContext(ctx)
		data, err := p.Run(evm, ethcommon.Address{}, p.Address(), calldata, nil, true, false, nil)
		assertRevert(t, data, err, db, agentcommon.ErrInputTooShort, "LXP_PRECOMPILE_INPUT_TOO_SHORT: input too short to extract method ID")
	}
	for _, name := range []string{pofq.ScoreFillMethod, pofq.ScoreBatchMethod, pofq.UpdateRollingScoreMethod} {
		evm, db := evmContext(ctx)
		selector := p.GetABI().Methods[name].ID
		data, err := p.Run(evm, ethcommon.Address{}, p.Address(), selector, big.NewInt(1), true, false, nil)
		assertRevert(t, data, err, db, agentcommon.ErrNonPayable, "LXP_PRECOMPILE_NON_PAYABLE: sending funds to a non-payable function")
		data, err = p.Run(evm, ethcommon.Address{}, p.Address(), selector, nil, true, false, nil)
		assertRevert(t, data, err, db, agentcommon.ErrABIDecode, "LXP_AGENT_ABI_DECODE: abi: attempting to unmarshal an empty string while arguments are expected")
	}
	data, err := p.Run(nil, ethcommon.Address{}, p.Address(), nil, nil, true, false, nil)
	assertRevert(t, data, err, nil, agentcommon.ErrNoContext, "LXP_PRECOMPILE_NO_CONTEXT: cannot get context from EVM")
	evm, db := evmContext(sdk.Context{})
	data, err = p.Run(evm, ethcommon.Address{}, p.Address(), nil, nil, true, false, nil)
	assertRevert(t, data, err, db, agentcommon.ErrNoContext, "LXP_PRECOMPILE_NO_CONTEXT: cannot get context from EVM")
	data, err = p.Run(&vm.EVM{}, ethcommon.Address{}, p.Address(), nil, nil, true, false, nil)
	assertRevert(t, data, err, nil, agentcommon.ErrNoContext, "LXP_PRECOMPILE_NO_CONTEXT: cannot get context from EVM")
}

func TestMalformedBatchABI(t *testing.T) {
	p, ctx, _ := fixture(t, 4)
	valid := input(t, p, pofq.ScoreBatchMethod, []pofq.Fill{{big.NewInt(1000), big.NewInt(1000), big.NewInt(1)}})
	for _, test := range []struct {
		calldata []byte
		message  string
	}{
		{valid[:len(valid)-1], "LXP_AGENT_ABI_DECODE: abi: cannot marshal in to go type: length insufficient 95 require 96"},
		{append(bytes.Clone(valid[:4]), bytes.Repeat([]byte{0xff}, 32)...), "LXP_AGENT_ABI_DECODE: abi: cannot marshal in to go slice: offset 115792089237316195423570985008687907853269984665640564039457584007913129639967 would go over slice boundary (len=32)"},
		{append(bytes.Clone(valid[:36]), bytes.Repeat([]byte{0xff}, 32)...), "LXP_AGENT_ABI_DECODE: abi: length larger than int64: 115792089237316195423570985008687907853269984665640564039457584007913129639999"},
	} {
		evm, db := evmContext(ctx)
		data, err := p.Run(evm, ethcommon.Address{}, p.Address(), test.calldata, nil, true, false, nil)
		assertRevert(t, data, err, db, agentcommon.ErrABIDecode, test.message)
	}
}

func TestExecutorArgumentValidation(t *testing.T) {
	p, ctx, _ := fixture(t, 4)
	executor := p.GetExecutor()
	for _, test := range []struct {
		name    string
		args    []interface{}
		code    agentcommon.ErrorCode
		message string
	}{
		{pofq.ScoreFillMethod, nil, agentcommon.ErrArgCount, "LXP_PRECOMPILE_ARG_COUNT: expected 2 arguments but got 0"},
		{pofq.ScoreFillMethod, []interface{}{1, big.NewInt(1000)}, agentcommon.ErrABIDecode, "LXP_AGENT_ABI_DECODE: argument decode failed"},
		{pofq.ScoreFillMethod, []interface{}{(*big.Int)(nil), big.NewInt(1000)}, agentcommon.ErrABIDecode, "LXP_AGENT_ABI_DECODE: argument decode failed"},
		{pofq.ScoreBatchMethod, []interface{}{[]int{1}}, agentcommon.ErrABIDecode, "LXP_AGENT_ABI_DECODE: argument decode failed"},
		{pofq.UpdateRollingScoreMethod, []interface{}{big.NewInt(1), "1", big.NewInt(1), big.NewInt(1), uint16(0)}, agentcommon.ErrABIDecode, "LXP_AGENT_ABI_DECODE: argument decode failed"},
		{pofq.UpdateRollingScoreMethod, []interface{}{big.NewInt(1), big.NewInt(1), big.NewInt(1), big.NewInt(1), uint32(0)}, agentcommon.ErrABIDecode, "LXP_AGENT_ABI_DECODE: argument decode failed"},
	} {
		method := p.GetABI().Methods[test.name]
		result, err := executor.Execute(ctx, &method, ethcommon.Address{}, p.Address(), test.args, nil, true, nil, nil)
		require.Nil(t, result)
		require.ErrorIs(t, err, test.code)
		require.EqualError(t, err, test.message)
	}
	for _, method := range []*abi.Method{nil, {ID: []byte{0xde, 0xad, 0xbe, 0xef}}} {
		result, err := executor.Execute(ctx, method, ethcommon.Address{}, p.Address(), nil, nil, true, nil, nil)
		require.Nil(t, result)
		require.ErrorIs(t, err, agentcommon.ErrUnknownMethod)
	}
	method := p.GetABI().Methods[pofq.ScoreFillMethod]
	args := []interface{}{big.NewInt(1000), big.NewInt(1000)}
	result, err := executor.Execute(ctx, &method, ethcommon.Address{}, p.Address(), args, big.NewInt(1), true, nil, nil)
	require.Nil(t, result)
	require.ErrorIs(t, err, agentcommon.ErrNonPayable)
	require.EqualError(t, err, "LXP_PRECOMPILE_NON_PAYABLE: sending funds to a non-payable function")
	result, err = executor.Execute(ctx.WithBlockHeight(99), &method, ethcommon.Address{}, p.Address(), args, nil, true, nil, nil)
	require.Nil(t, result)
	require.ErrorIs(t, err, agentcommon.ErrNotActive)
	require.EqualError(t, err, "LXP_AGENT_NOT_ACTIVE: merge-plan not active at height 99")
}

func TestArithmeticInputBounds(t *testing.T) {
	for _, value := range []*big.Int{nil, new(big.Int).Lsh(big.NewInt(1), 255), new(big.Int).Neg(new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(1)))} {
		for _, prices := range [][2]*big.Int{{value, big.NewInt(1000)}, {big.NewInt(1000), value}} {
			score, err := pofq.ScoreFill(prices[0], prices[1])
			require.Nil(t, score)
			require.ErrorIs(t, err, agentcommon.ErrABIDecode)
			require.EqualError(t, err, "LXP_AGENT_ABI_DECODE: argument decode failed")
		}
	}
	for _, size := range []*big.Int{nil, big.NewInt(-1), new(big.Int).Lsh(big.NewInt(1), 128)} {
		score, volume, err := pofq.ScoreBatch([]pofq.Fill{{big.NewInt(1000), big.NewInt(1000), size}}, 1)
		require.Nil(t, score)
		require.Nil(t, volume)
		require.ErrorIs(t, err, agentcommon.ErrABIDecode)
		require.EqualError(t, err, "LXP_AGENT_ABI_DECODE: argument decode failed")
	}
	for _, value := range []*big.Int{nil, big.NewInt(-1), new(big.Int).Lsh(big.NewInt(1), 256)} {
		for i := 0; i < 4; i++ {
			values := []*big.Int{big.NewInt(1), big.NewInt(1), big.NewInt(1), big.NewInt(1)}
			values[i] = value
			score, weight, err := pofq.UpdateRollingScore(values[0], values[1], values[2], values[3], 0)
			require.Nil(t, score)
			require.Nil(t, weight)
			require.ErrorIs(t, err, agentcommon.ErrABIDecode)
			require.EqualError(t, err, "LXP_AGENT_ABI_DECODE: argument decode failed")
		}
	}
	score, weight, err := pofq.UpdateRollingScore(nil, nil, nil, nil, 10000)
	require.Nil(t, score)
	require.Nil(t, weight)
	require.ErrorIs(t, err, agentcommon.ErrPofqDecayRange)
	require.EqualError(t, err, "LXP_AGENT_POFQ_DECAY_RANGE: decayBps must be < 10000")
}

func TestBatchConfiguration(t *testing.T) {
	p, err := pofq.NewPrecompile(nil, "merge-plan", 0)
	require.Nil(t, p)
	require.ErrorIs(t, err, agentcommon.ErrBatchTooLarge)
	require.EqualError(t, err, "LXP_AGENT_BATCH_TOO_LARGE: batch cap must be positive")
	p, _, _ = fixture(t, math.MaxUint64)
	require.NotNil(t, p)
	var absent *agentcommon.Precompile
	require.Equal(t, pcommon.UnknownMethodCallGas, absent.RequiredGas(nil))
}
