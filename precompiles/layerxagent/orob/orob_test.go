package orob_test

import (
	"encoding/hex"
	"math"
	"math/big"
	"testing"

	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	transferkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/interchain/modules/apps/transfer/keeper"
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	agentcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/orob"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	codectypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/rootmulti"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/keeper"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	paramtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/params/types"
	upgradekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/keeper"
	upgradetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

func fixture(t *testing.T, cap int) (*agentcommon.Precompile, sdk.Context, *upgradekeeper.Keeper, *evmkeeper.Keeper) {
	t.Helper()
	db := dbm.NewMemDB()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	key := sdk.NewKVStoreKey(upgradetypes.StoreKey)
	evmKey := sdk.NewKVStoreKey(evmtypes.StoreKey)
	authKey := sdk.NewKVStoreKey(authtypes.StoreKey)
	paramsKey := sdk.NewKVStoreKey("params")
	paramsTransientKey := sdk.NewTransientStoreKey("transient_params")
	evmTransientKey := sdk.NewTransientStoreKey(evmtypes.TransientStoreKey)
	ms := store.NewCommitMultiStore(db)
	for _, key := range []*sdk.KVStoreKey{key, evmKey, authKey, paramsKey} {
		ms.MountStoreWithDB(key, sdk.StoreTypeIAVL, db)
	}
	ms.MountStoreWithDB(paramsTransientKey, sdk.StoreTypeTransient, nil)
	ms.MountStoreWithDB(evmTransientKey, sdk.StoreTypeTransient, nil)
	require.NoError(t, ms.LoadLatestVersion())
	ctx := sdk.NewContext(ms, tmproto.Header{Height: 100}, false)
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	upgrade := upgradekeeper.NewKeeper(nil, key, cdc, t.TempDir(), nil)
	amino := codec.NewLegacyAmino()
	authSubspace := paramtypes.NewSubspace(cdc, amino, paramsKey, paramsTransientKey, authtypes.ModuleName)
	evmSubspace := paramtypes.NewSubspace(cdc, amino, paramsKey, paramsTransientKey, evmtypes.ModuleName)
	accounts := authkeeper.NewAccountKeeper(cdc, authKey, authSubspace, authtypes.ProtoBaseAccount, map[string][]string{authtypes.FeeCollectorName: nil})
	evmKeeper := evmkeeper.NewKeeper(evmKey, evmTransientKey, evmSubspace, nil, nil, &accounts, nil, transferkeeper.Keeper{}, nil, nil, &upgrade)
	evmKeeper.SetAddressMapping(ctx, accounts.GetModuleAddress(authtypes.FeeCollectorName), ethcommon.Address{1})
	upgrade.SetDone(ctx, "merge-plan")
	p, err := orob.NewPrecompile(&upgrade, "merge-plan", cap)
	require.NoError(t, err)
	return p, ctx, &upgrade, evmKeeper
}

func evmContext(ctx sdk.Context) *vm.EVM {
	db := new(state.DBImpl)
	db.WithCtx(ctx)
	return &vm.EVM{StateDB: db}
}

func call(t *testing.T, p *agentcommon.Precompile, ctx sdk.Context, method string, args ...interface{}) ([]byte, error) {
	t.Helper()
	input, err := p.GetABI().Pack(method, args...)
	require.NoError(t, err)
	return p.Run(evmContext(ctx), ethcommon.Address{}, p.Address(), input, nil, true, false, nil)
}

func revertText(t *testing.T, data []byte) string {
	t.Helper()
	require.GreaterOrEqual(t, len(data), 4)
	require.Equal(t, []byte{0x08, 0xc3, 0x79, 0xa0}, data[:4])
	text, err := abi.UnpackRevert(data)
	require.NoError(t, err)
	return text
}

func maxPrice() *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(1))
}

func TestResolveOffset(t *testing.T) {
	p, ctx, _, _ := fixture(t, 3)
	for _, test := range []struct {
		name   string
		price  *big.Int
		offset int16
		want   *big.Int
		reason string
	}{
		{"parity", big.NewInt(2000000), 250, big.NewInt(2050000), ""},
		{"discount", big.NewInt(1000), -100, big.NewInt(990), ""},
		{"truncation", big.NewInt(3), 5000, big.NewInt(4), ""},
		{"small price", big.NewInt(1), -9999, big.NewInt(0), ""},
		{"lowest valid offset", big.NewInt(10000), -9999, big.NewInt(1), ""},
		{"highest offset", big.NewInt(10000), math.MaxInt16, big.NewInt(42767), ""},
		{"largest price", maxPrice(), 0, maxPrice(), ""},
		{"largest price discount", maxPrice(), -5000, new(big.Int).Quo(maxPrice(), big.NewInt(2)), ""},
		{"overflow", maxPrice(), 1, nil, "LXP_AGENT_OVERFLOW: result out of range"},
		{"zero oracle", big.NewInt(0), 0, nil, "LXP_AGENT_OROB_ORACLE_PRICE: oraclePrice cannot be zero"},
		{"negative oracle", big.NewInt(-1), 0, nil, "LXP_AGENT_OROB_ORACLE_PRICE: oraclePrice cannot be zero"},
		{"minimum oracle", new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 255)), 0, nil, "LXP_AGENT_OROB_ORACLE_PRICE: oraclePrice cannot be zero"},
		{"offset boundary", big.NewInt(1000), -10000, nil, "LXP_AGENT_OROB_OFFSET_RANGE: offset out of range"},
		{"offset below boundary", big.NewInt(1000), -10001, nil, "LXP_AGENT_OROB_OFFSET_RANGE: offset out of range"},
		{"minimum offset", big.NewInt(1000), math.MinInt16, nil, "LXP_AGENT_OROB_OFFSET_RANGE: offset out of range"},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := new(big.Int).Set(test.price)
			data, err := call(t, p, ctx, orob.ResolveOffsetMethod, test.price, test.offset)
			require.Zero(t, before.Cmp(test.price))
			if test.reason != "" {
				require.ErrorIs(t, err, vm.ErrExecutionReverted)
				require.Equal(t, test.reason, revertText(t, data))
				return
			}
			require.NoError(t, err)
			values, err := p.GetABI().Unpack(orob.ResolveOffsetMethod, data)
			require.NoError(t, err)
			require.Len(t, values, 1)
			require.Zero(t, test.want.Cmp(values[0].(*big.Int)))
			require.Equal(t, ethcommon.LeftPadBytes(test.want.Bytes(), 32), data)
		})
	}
}

func TestResolveOffsetBatch(t *testing.T) {
	p, ctx, _, _ := fixture(t, 3)
	for _, test := range []struct {
		name    string
		price   *big.Int
		offsets []int16
		want    []string
		reason  string
	}{
		{"parity at cap", big.NewInt(1000), []int16{-100, 0, 100}, []string{"990", "1000", "1010"}, ""},
		{"one offset", big.NewInt(1000), []int16{250}, []string{"1025"}, ""},
		{"order and repetition", big.NewInt(3), []int16{5000, -9999, 5000}, []string{"4", "0", "4"}, ""},
		{"over cap", big.NewInt(1000), []int16{0, 0, 0, 0}, nil, "LXP_AGENT_BATCH_TOO_LARGE: batch over the cap (cap set at activation rehearsal)"},
		{"cap before arithmetic", big.NewInt(0), []int16{-10000, 0, 0, 0}, nil, "LXP_AGENT_BATCH_TOO_LARGE: batch over the cap (cap set at activation rehearsal)"},
		{"empty", big.NewInt(1000), []int16{}, nil, "LXP_AGENT_ABI_DECODE: argument decode failed"},
		{"zero oracle", big.NewInt(0), []int16{0}, nil, "LXP_AGENT_OROB_ORACLE_PRICE: oraclePrice cannot be zero"},
		{"negative oracle", big.NewInt(-1), []int16{0}, nil, "LXP_AGENT_OROB_ORACLE_PRICE: oraclePrice cannot be zero"},
		{"invalid later offset", big.NewInt(1000), []int16{0, -10000}, nil, "LXP_AGENT_OROB_OFFSET_RANGE: offset out of range"},
		{"overflow later offset", maxPrice(), []int16{0, 1}, nil, "LXP_AGENT_OVERFLOW: result out of range"},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := new(big.Int).Set(test.price)
			offsetsBefore := append([]int16{}, test.offsets...)
			data, err := call(t, p, ctx, orob.ResolveOffsetBatchMethod, test.price, test.offsets)
			require.Zero(t, before.Cmp(test.price))
			require.Equal(t, offsetsBefore, test.offsets)
			if test.reason != "" {
				require.ErrorIs(t, err, vm.ErrExecutionReverted)
				require.Equal(t, test.reason, revertText(t, data))
				return
			}
			require.NoError(t, err)
			values, err := p.GetABI().Unpack(orob.ResolveOffsetBatchMethod, data)
			require.NoError(t, err)
			require.Len(t, values, 1)
			prices := values[0].([]*big.Int)
			require.Len(t, prices, len(test.want))
			for i, want := range test.want {
				require.Equal(t, want, prices[i].String())
			}
		})
	}
}

func TestToOffsetRange(t *testing.T) {
	p, ctx, _, _ := fixture(t, 3)
	for _, test := range []struct {
		name     string
		oracle   *big.Int
		absolute *big.Int
		want     int16
		reason   string
	}{
		{"parity", big.NewInt(1000), big.NewInt(1025), 250, ""},
		{"negative offset", big.NewInt(1000), big.NewInt(999), -10, ""},
		{"negative truncation", big.NewInt(3), big.NewInt(1), -6666, ""},
		{"positive truncation", big.NewInt(3), big.NewInt(4), 3333, ""},
		{"negative fraction to zero", big.NewInt(30000), big.NewInt(29998), 0, ""},
		{"lowest reachable offset", big.NewInt(10000), big.NewInt(1), -9999, ""},
		{"highest offset", big.NewInt(10000), big.NewInt(42767), math.MaxInt16, ""},
		{"fraction above highest offset", big.NewInt(20000), big.NewInt(85535), math.MaxInt16, ""},
		{"first overflow", big.NewInt(10000), big.NewInt(42768), 0, "LXP_AGENT_OVERFLOW: result out of range"},
		{"parity overflow", big.NewInt(1), big.NewInt(5000000), 0, "LXP_AGENT_OVERFLOW: result out of range"},
		{"large overflow", big.NewInt(1), maxPrice(), 0, "LXP_AGENT_OVERFLOW: result out of range"},
		{"largest equal prices", maxPrice(), maxPrice(), 0, ""},
		{"zero oracle", big.NewInt(0), big.NewInt(1), 0, "LXP_AGENT_OROB_ORACLE_PRICE: oraclePrice cannot be zero"},
		{"negative oracle", big.NewInt(-1), big.NewInt(1), 0, "LXP_AGENT_OROB_ORACLE_PRICE: oraclePrice cannot be zero"},
		{"zero absolute", big.NewInt(1), big.NewInt(0), 0, "LXP_AGENT_OROB_ORACLE_PRICE: oraclePrice cannot be zero"},
		{"negative absolute", big.NewInt(1), big.NewInt(-1), 0, "LXP_AGENT_OROB_ORACLE_PRICE: oraclePrice cannot be zero"},
	} {
		t.Run(test.name, func(t *testing.T) {
			oracleBefore := new(big.Int).Set(test.oracle)
			absoluteBefore := new(big.Int).Set(test.absolute)
			data, err := call(t, p, ctx, orob.ToOffsetMethod, test.oracle, test.absolute)
			require.Zero(t, oracleBefore.Cmp(test.oracle))
			require.Zero(t, absoluteBefore.Cmp(test.absolute))
			if test.reason != "" {
				require.ErrorIs(t, err, vm.ErrExecutionReverted)
				require.Equal(t, test.reason, revertText(t, data))
				return
			}
			require.NoError(t, err)
			values, err := p.GetABI().Unpack(orob.ToOffsetMethod, data)
			require.NoError(t, err)
			require.Equal(t, []interface{}{test.want}, values)
		})
	}
}

func TestFrozenABIAndGas(t *testing.T) {
	p, _, _, _ := fixture(t, 3)
	require.Equal(t, ethcommon.HexToAddress(orob.OrobAddress), p.Address())
	require.Equal(t, "orob", p.GetName())
	a := p.GetABI()
	require.Len(t, a.Methods, 3)
	require.Empty(t, a.Events)
	for _, test := range []struct {
		method   string
		selector string
		args     []interface{}
		gas      uint64
	}{
		{orob.ResolveOffsetMethod, "454ad3a1", []interface{}{big.NewInt(1000), int16(100)}, 1204},
		{orob.ResolveOffsetBatchMethod, "3df597e3", []interface{}{big.NewInt(1000), []int16{-100, 0, 100}}, 1588},
		{orob.ToOffsetMethod, "3f779aef", []interface{}{big.NewInt(1000), big.NewInt(1025)}, 1204},
	} {
		t.Run(test.method, func(t *testing.T) {
			method := a.Methods[test.method]
			require.Equal(t, test.selector, hex.EncodeToString(method.ID))
			require.Equal(t, "pure", method.StateMutability)
			input, err := a.Pack(test.method, test.args...)
			require.NoError(t, err)
			require.Equal(t, test.gas, p.RequiredGas(input))
			require.Equal(t, test.gas, p.GetExecutor().RequiredGas(input[4:], &method))
			require.Equal(t, test.gas+3, p.RequiredGas(append(input, 0)))
		})
	}
	for _, input := range [][]byte{nil, {0x45}, {0x45, 0x4a, 0xd3}, {0xde, 0xad, 0xbe, 0xef}, {0xde, 0xad, 0xbe, 0xef, 0}} {
		require.Equal(t, uint64(3000), p.RequiredGas(input))
	}
	executor := new(orob.OrobPrecompile)
	require.Equal(t, uint64(3000), executor.RequiredGas(nil, nil))
	require.Equal(t, uint64(3000), executor.RequiredGas(nil, &abi.Method{ID: []byte{0xde, 0xad, 0xbe, 0xef}}))
	var nilExecutor *orob.OrobPrecompile
	require.Equal(t, uint64(3000), nilExecutor.RequiredGas(nil, nil))
}

func TestCallRefusals(t *testing.T) {
	p, ctx, _, _ := fixture(t, 3)
	valid, err := p.GetABI().Pack(orob.ResolveOffsetMethod, big.NewInt(1000), int16(100))
	require.NoError(t, err)
	batch, err := p.GetABI().Pack(orob.ResolveOffsetBatchMethod, big.NewInt(1000), []int16{100})
	require.NoError(t, err)
	for _, test := range []struct {
		name   string
		input  []byte
		value  *big.Int
		code   agentcommon.ErrorCode
		reason string
	}{
		{"empty", nil, nil, agentcommon.ErrInputTooShort, "LXP_PRECOMPILE_INPUT_TOO_SHORT: input too short to extract method ID"},
		{"short", []byte{0x45, 0x4a, 0xd3}, nil, agentcommon.ErrInputTooShort, "LXP_PRECOMPILE_INPUT_TOO_SHORT: input too short to extract method ID"},
		{"unknown", []byte{0xde, 0xad, 0xbe, 0xef}, nil, agentcommon.ErrUnknownMethod, "LXP_PRECOMPILE_UNKNOWN_METHOD: no method with id: 0xdeadbeef"},
		{"selector only", valid[:4], nil, agentcommon.ErrABIDecode, ""},
		{"truncated argument", valid[:len(valid)-1], nil, agentcommon.ErrABIDecode, ""},
		{"truncated batch", batch[:len(batch)-1], nil, agentcommon.ErrABIDecode, ""},
		{"nonpayable", valid, big.NewInt(1), agentcommon.ErrNonPayable, "LXP_PRECOMPILE_NON_PAYABLE: sending funds to a non-payable function"},
	} {
		t.Run(test.name, func(t *testing.T) {
			evm := evmContext(ctx)
			data, err := p.Run(evm, ethcommon.Address{}, p.Address(), test.input, test.value, true, false, nil)
			require.ErrorIs(t, err, vm.ErrExecutionReverted)
			require.ErrorIs(t, state.GetDBImpl(evm.StateDB).GetPrecompileError(), test.code)
			if test.reason != "" {
				require.Equal(t, test.reason, revertText(t, data))
			} else {
				method, err := p.MethodById(test.input[:4])
				require.NoError(t, err)
				_, decodeErr := method.Inputs.Unpack(test.input[4:])
				require.Error(t, decodeErr)
				require.Equal(t, "LXP_AGENT_ABI_DECODE: "+decodeErr.Error(), revertText(t, data))
			}
		})
	}
	input, err := p.GetABI().Pack(orob.ResolveOffsetBatchMethod, big.NewInt(1000), []int16{100})
	require.NoError(t, err)
	for i := 68; i < 100; i++ {
		input[i] = 0xff
	}
	data, err := p.Run(evmContext(ctx), ethcommon.Address{}, p.Address(), input, nil, true, false, nil)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	method := p.GetABI().Methods[orob.ResolveOffsetBatchMethod]
	_, decodeErr := method.Inputs.Unpack(input[4:])
	require.Error(t, decodeErr)
	require.Equal(t, "LXP_AGENT_ABI_DECODE: "+decodeErr.Error(), revertText(t, data))
}

func TestHeightGateAndContext(t *testing.T) {
	p, ctx, upgrade, _ := fixture(t, 3)
	input, err := p.GetABI().Pack(orob.ResolveOffsetMethod, big.NewInt(1000), int16(100))
	require.NoError(t, err)
	for _, method := range []struct {
		name string
		args []interface{}
	}{
		{orob.ResolveOffsetMethod, []interface{}{big.NewInt(1000), int16(100)}},
		{orob.ResolveOffsetBatchMethod, []interface{}{big.NewInt(1000), []int16{100}}},
		{orob.ToOffsetMethod, []interface{}{big.NewInt(1000), big.NewInt(1025)}},
	} {
		for _, height := range []int64{99, 100, 101} {
			data, err := call(t, p, ctx.WithBlockHeight(height), method.name, method.args...)
			if height < 100 {
				require.ErrorIs(t, err, vm.ErrExecutionReverted)
				require.Equal(t, "LXP_AGENT_NOT_ACTIVE: merge-plan not active at height 99", revertText(t, data))
			} else {
				require.NoError(t, err)
				require.NotEmpty(t, data)
			}
		}
	}
	unbound, err := orob.NewPrecompile(upgrade, "other-plan", 3)
	require.NoError(t, err)
	data, err := unbound.Run(evmContext(ctx), ethcommon.Address{}, unbound.Address(), input, nil, true, false, nil)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, "LXP_AGENT_NOT_ACTIVE: other-plan not active at height 100", revertText(t, data))
	for _, evm := range []*vm.EVM{nil, {}, evmContext(sdk.Context{})} {
		data, err := p.Run(evm, ethcommon.Address{}, p.Address(), input, nil, true, false, nil)
		require.ErrorIs(t, err, vm.ErrExecutionReverted)
		require.Equal(t, "LXP_PRECOMPILE_NO_CONTEXT: cannot get context from EVM", revertText(t, data))
	}
}

func TestPureCallsKeepStateAndEvents(t *testing.T) {
	p, ctx, _, evmKeeper := fixture(t, 3)
	stores := ctx.MultiStore().(*rootmulti.Store).GetStores()
	var store sdk.KVStore
	for key := range stores {
		if key.Name() == upgradetypes.StoreKey {
			store = ctx.KVStore(key)
		}
	}
	require.NotNil(t, store)
	store.Set([]byte("orob-sentinel"), []byte{1, 2, 3})
	ctx.EventManager().EmitEvent(sdk.NewEvent("existing"))
	snapshot := func() map[string]map[string]string {
		entries := make(map[string]map[string]string)
		for key := range stores {
			values := make(map[string]string)
			iter := ctx.KVStore(key).Iterator(nil, nil)
			for ; iter.Valid(); iter.Next() {
				values[string(iter.Key())] = string(iter.Value())
			}
			require.NoError(t, iter.Close())
			entries[key.Name()] = values
		}
		return entries
	}
	before := snapshot()
	events := append(sdk.Events{}, ctx.EventManager().Events()...)
	for _, test := range []struct {
		method string
		args   []interface{}
	}{
		{orob.ResolveOffsetMethod, []interface{}{big.NewInt(1000), int16(100)}},
		{orob.ResolveOffsetBatchMethod, []interface{}{big.NewInt(1000), []int16{-100, 0, 100}}},
		{orob.ToOffsetMethod, []interface{}{big.NewInt(1000), big.NewInt(1025)}},
	} {
		input, err := p.GetABI().Pack(test.method, test.args...)
		require.NoError(t, err)
		for _, flags := range []struct{ readOnly, delegate, contextDelegate bool }{
			{false, false, false}, {true, false, false}, {false, true, false}, {true, true, false}, {true, false, true},
		} {
			callCtx := ctx.WithEVMPrecompileCalledFromDelegateCall(flags.contextDelegate)
			db := state.NewDBImpl(callCtx, evmKeeper, false)
			db.WithCtx(callCtx)
			evm := &vm.EVM{StateDB: db}
			data, err := p.Run(evm, ethcommon.Address{1}, p.Address(), input, big.NewInt(0), flags.readOnly, flags.delegate, nil)
			require.NoError(t, err)
			require.NotEmpty(t, data)
			require.Empty(t, state.GetDBImpl(evm.StateDB).GetAllLogs())
			require.Equal(t, before, snapshot())
			require.Equal(t, events, ctx.EventManager().Events())
		}
	}
}

func TestExecutorArgumentRefusals(t *testing.T) {
	p, ctx, upgrade, _ := fixture(t, 3)
	ctx = agentcommon.WithHeightGate(ctx, upgrade, "merge-plan")
	a := p.GetABI()
	for _, test := range []struct {
		method string
		args   []interface{}
		reason string
	}{
		{orob.ResolveOffsetMethod, nil, "LXP_PRECOMPILE_ARG_COUNT: expected 2 arguments but got 0"},
		{orob.ResolveOffsetMethod, []interface{}{big.NewInt(1)}, "LXP_PRECOMPILE_ARG_COUNT: expected 2 arguments but got 1"},
		{orob.ResolveOffsetMethod, []interface{}{big.NewInt(1), int16(1), int16(1)}, "LXP_PRECOMPILE_ARG_COUNT: expected 2 arguments but got 3"},
		{orob.ResolveOffsetMethod, []interface{}{int64(1), int16(1)}, "LXP_AGENT_ABI_DECODE: argument decode failed"},
		{orob.ResolveOffsetMethod, []interface{}{(*big.Int)(nil), int16(1)}, "LXP_AGENT_ABI_DECODE: argument decode failed"},
		{orob.ResolveOffsetMethod, []interface{}{big.NewInt(1), int64(1)}, "LXP_AGENT_ABI_DECODE: argument decode failed"},
		{orob.ResolveOffsetBatchMethod, []interface{}{big.NewInt(1), []int64{1}}, "LXP_AGENT_ABI_DECODE: argument decode failed"},
		{orob.ToOffsetMethod, []interface{}{big.NewInt(1), int64(1)}, "LXP_AGENT_ABI_DECODE: argument decode failed"},
		{orob.ToOffsetMethod, []interface{}{big.NewInt(1), (*big.Int)(nil)}, "LXP_AGENT_ABI_DECODE: argument decode failed"},
	} {
		method := a.Methods[test.method]
		data, err := p.GetExecutor().Execute(ctx, &method, ethcommon.Address{}, p.Address(), test.args, nil, true, nil, nil)
		require.Nil(t, data)
		require.EqualError(t, err, test.reason)
	}
	for _, method := range []*abi.Method{nil, {ID: []byte{0xde, 0xad, 0xbe, 0xef}}} {
		data, err := p.GetExecutor().Execute(ctx, method, ethcommon.Address{}, p.Address(), nil, nil, true, nil, nil)
		require.Nil(t, data)
		require.ErrorIs(t, err, agentcommon.ErrUnknownMethod)
	}
	method := a.Methods[orob.ResolveOffsetMethod]
	data, err := p.GetExecutor().Execute(sdk.Context{}, &method, ethcommon.Address{}, p.Address(), []interface{}{big.NewInt(1), int16(1)}, nil, true, nil, nil)
	require.Nil(t, data)
	require.EqualError(t, err, "LXP_PRECOMPILE_NO_CONTEXT: cannot get context from EVM")
	data, err = p.GetExecutor().Execute(ctx, &method, ethcommon.Address{}, p.Address(), []interface{}{big.NewInt(1), int16(1)}, big.NewInt(1), true, nil, nil)
	require.Nil(t, data)
	require.EqualError(t, err, "LXP_PRECOMPILE_NON_PAYABLE: sending funds to a non-payable function")
}

func TestConfigurationAndDirectBounds(t *testing.T) {
	_, _, upgrade, _ := fixture(t, 3)
	for _, test := range []struct {
		upgrade *upgradekeeper.Keeper
		plan    string
		cap     int
		code    agentcommon.ErrorCode
	}{
		{nil, "merge-plan", 3, agentcommon.ErrNotActive},
		{upgrade, "", 3, agentcommon.ErrNotActive},
		{upgrade, "merge-plan", 0, agentcommon.ErrBatchTooLarge},
		{upgrade, "merge-plan", -1, agentcommon.ErrBatchTooLarge},
	} {
		p, err := orob.NewPrecompile(test.upgrade, test.plan, test.cap)
		require.Nil(t, p)
		require.ErrorIs(t, err, test.code)
	}
	price := big.NewInt(1000)
	resolved, err := orob.ResolveOffset(price, 100)
	require.NoError(t, err)
	resolved.SetInt64(1)
	require.Equal(t, "1000", price.String())
	prices, err := orob.ResolveOffsetBatch(price, []int16{0, 0}, 2)
	require.NoError(t, err)
	prices[0].SetInt64(1)
	require.Equal(t, "1000", prices[1].String())
	require.Equal(t, "1000", price.String())
	_, err = orob.ResolveOffset(nil, 0)
	require.EqualError(t, err, "LXP_AGENT_ABI_DECODE: argument decode failed")
	_, err = orob.ResolveOffset(new(big.Int).Lsh(big.NewInt(1), 255), 0)
	require.EqualError(t, err, "LXP_AGENT_OVERFLOW: result out of range")
	_, err = orob.ResolveOffsetBatch(price, []int16{0}, 0)
	require.EqualError(t, err, "LXP_AGENT_BATCH_TOO_LARGE: batch over the cap (cap set at activation rehearsal)")
	_, err = orob.ResolveOffsetBatch(price, []int16{0}, -1)
	require.EqualError(t, err, "LXP_AGENT_BATCH_TOO_LARGE: batch over the cap (cap set at activation rehearsal)")
	_, err = orob.ToOffset(nil, price)
	require.EqualError(t, err, "LXP_AGENT_ABI_DECODE: argument decode failed")
	_, err = orob.ToOffset(price, nil)
	require.EqualError(t, err, "LXP_AGENT_ABI_DECODE: argument decode failed")
	_, err = orob.ToOffset(price, new(big.Int).Lsh(big.NewInt(1), 255))
	require.EqualError(t, err, "LXP_AGENT_OVERFLOW: result out of range")
}
