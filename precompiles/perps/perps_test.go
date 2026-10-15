package perps_test

import (
	"math/big"
	"testing"

	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	perpskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	perpstypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/cachemulti"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/dbadapter"
	storetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	tmdb "github.com/tendermint/tm-db"
)

var (
	staticCallSelector   = hexutil.MustDecode("0x32a35088")
	delegatecallSelector = hexutil.MustDecode("0x856fe7e1")
)

// wiredKeepers are the application's precompile keepers plus the perps
// keeper, as the application exposes them once it wires the module.
type wiredKeepers struct {
	utils.Keepers
	perps *perpskeeper.Keeper
}

func (k wiredKeepers) PerpsK() *perpskeeper.Keeper { return k.perps }

// bareKeepers expose only the Keepers interface, so PerpsK is not promoted.
type bareKeepers struct {
	utils.Keepers
}

type harness struct {
	t          *testing.T
	key        storetypes.StoreKey
	stateDB    *state.DBImpl
	evm        *vm.EVM
	precompile *pcommon.Precompile
	caller     common.Address
}

func mounted(parent storetypes.MultiStore, key storetypes.StoreKey) (store storetypes.KVStore, ok bool) {
	defer func() {
		if recover() != nil {
			store, ok = nil, false
		}
	}()
	return parent.GetKVStore(key), true
}

// withStore adds the perps store to ctx's multistore next to the app's.
func withStore(ctx sdk.Context, key storetypes.StoreKey) sdk.Context {
	parent := ctx.MultiStore()
	stores := map[storetypes.StoreKey]storetypes.CacheWrapper{}
	keys := map[string]storetypes.StoreKey{}
	for _, existing := range parent.StoreKeys() {
		if store, ok := mounted(parent, existing); ok {
			stores[existing] = store
			keys[existing.Name()] = existing
		}
	}
	stores[key] = dbadapter.Store{DB: tmdb.NewMemDB()}
	keys[key.Name()] = key
	return ctx.WithMultiStore(cachemulti.NewStore(tmdb.NewMemDB(), stores, keys, nil, nil, nil, 0))
}

// newHarness builds the precompile on the test app's keepers, with the perps
// keeper on its own store when wired is true, and an EVM over that state.
func newHarness(t *testing.T, wired bool) *harness {
	t.Helper()
	app := testkeeper.EVMTestApp
	base, _ := app.NewContext(false, tmtypes.Header{}).WithBlockHeight(11).CacheContext()
	key := sdk.NewKVStoreKey(perpstypes.StoreKey)
	ctx := withStore(base, key)
	var keepers utils.Keepers = bareKeepers{Keepers: app.GetPrecompileKeepers()}
	if wired {
		keepers = wiredKeepers{Keepers: app.GetPrecompileKeepers(), perps: perpskeeper.NewKeeper(key, &app.EvmKeeper, nil)}
	}
	precompile, err := perps.NewPrecompile(keepers)
	require.NoError(t, err)
	_, caller := testkeeper.MockAddressPair()
	stateDB := state.NewDBImpl(ctx.WithEventManager(sdk.NewEventManager()), &app.EvmKeeper, true)
	return &harness{t: t, key: key, stateDB: stateDB, precompile: precompile, caller: caller,
		evm: &vm.EVM{StateDB: stateDB, TxContext: vm.TxContext{Origin: caller}}}
}

// words is the number of 32-byte head words of a static ABI type.
func words(t *testing.T, typ abi.Type) int {
	t.Helper()
	require.NotEqual(t, abi.SliceTy, typ.T, typ.String())
	require.NotEqual(t, abi.StringTy, typ.T, typ.String())
	require.NotEqual(t, abi.BytesTy, typ.T, typ.String())
	if typ.T != abi.TupleTy {
		return 1
	}
	n := 0
	for _, elem := range typ.TupleElems {
		n += words(t, *elem)
	}
	return n
}

// zeroInput is the calldata of method with every argument zero.
func zeroInput(t *testing.T, method abi.Method) []byte {
	t.Helper()
	n := 0
	for _, input := range method.Inputs {
		n += words(t, input.Type)
	}
	input := append(append([]byte(nil), method.ID...), make([]byte, 32*n)...)
	_, err := method.Inputs.Unpack(input[4:])
	require.NoError(t, err, method.Name)
	return input
}

func (h *harness) run(input []byte, value *big.Int, readOnly, delegate bool) ([]byte, error) {
	h.t.Helper()
	var (
		res []byte
		err error
	)
	require.NotPanics(h.t, func() {
		res, err = h.precompile.Run(h.evm, h.caller, h.caller, input, value, readOnly, delegate, nil)
	})
	return res, err
}

func (h *harness) storeEmpty() bool {
	it := h.stateDB.Ctx().KVStore(h.key).Iterator(nil, nil)
	defer it.Close()
	return !it.Valid()
}

func TestPerpsGasTable(t *testing.T) {
	h := newHarness(t, true)
	methods := h.precompile.GetABI().Methods
	require.Len(t, methods, 67)
	require.Len(t, perps.GasTable, 67)
	writes := 0
	for name, method := range methods {
		row, ok := perps.GasTable[name]
		require.True(t, ok, name)
		require.Equal(t, !method.IsConstant(), row.Write, name)
		if !row.Write {
			require.Zero(t, row.Surcharge, name)
			continue
		}
		writes++
		input := zeroInput(t, method)
		require.Equal(t, 2000+30*uint64(len(input)-4)+row.Surcharge, h.precompile.RequiredGas(input), name)
	}
	require.Equal(t, 25, writes)

	surcharges := map[string]uint64{
		"createVault": 20_000, "deposit": 130_000, "withdraw": 130_000, "emergencyWithdraw": 130_000,
		"transferCollateral": 30_000, "setMarginMode": 5_000, "topUpPosition": 130_000, "transferMargin": 5_000,
		"placeOrder": 250_000, "placeLimitOrder": 350_000, "placeStopLimitOrder": 350_000, "cancelOrder": 20_000,
		"cancelByCloid": 20_000, "placeTakeProfitOrder": 60_000, "placeStopLossOrder": 60_000,
		"setPositionTpSl": 60_000, "executeOrder": 250_000, "openPosition": 350_000, "addSize": 350_000,
		"addCollateral": 130_000, "removeCollateral": 130_000, "closePosition": 250_000, "partialClose": 250_000,
		"closePositionWithParams": 250_000, "liquidate": 300_000,
	}
	for name, surcharge := range surcharges {
		require.Equal(t, surcharge, perps.GasTable[name].Surcharge, name)
	}
	require.Equal(t, uint64(100_000), perps.TokenCallGas)
	for _, name := range []string{"deposit", "withdraw", "emergencyWithdraw", "topUpPosition", "addCollateral",
		"removeCollateral", "placeLimitOrder", "placeStopLimitOrder", "openPosition", "addSize"} {
		require.GreaterOrEqual(t, perps.GasTable[name].Surcharge, perps.TokenCallGas, name)
	}

	placeOrder := zeroInput(t, methods["placeOrder"])
	require.Len(t, placeOrder, 484)
	require.Equal(t, uint64(266_400), h.precompile.RequiredGas(placeOrder))
	deposit := zeroInput(t, methods["deposit"])
	require.Len(t, deposit, 68)
	require.Equal(t, uint64(133_920), h.precompile.RequiredGas(deposit))
	require.Equal(t, uint64(1000+3*32), h.precompile.RequiredGas(zeroInput(t, methods["getMarkPrice"])))
	require.Equal(t, uint64(1000), h.precompile.RequiredGas(zeroInput(t, methods["getMarginMode"])))
	require.Equal(t, uint64(1000+3*32), h.precompile.RequiredGas(zeroInput(t, methods["getMarginMode0"])))
	for _, name := range []string{"getBalance", "getBalance0", "getLockedBalance", "getLockedBalance0",
		"getAccountSummary", "getAccountSummary0", "getMarginMode", "getMarginMode0"} {
		_, ok := perps.GasTable[name]
		require.True(t, ok, name)
	}

	require.Equal(t, pcommon.UnknownMethodCallGas, h.precompile.RequiredGas(crypto.Keccak256([]byte("pushOracle(bytes,bytes)"))[:4]))
	require.Equal(t, pcommon.UnknownMethodCallGas, h.precompile.RequiredGas([]byte{0x47, 0xe7}))
}

func TestPerpsCallKinds(t *testing.T) {
	h := newHarness(t, true)
	methods := h.precompile.GetABI().Methods

	res, err := h.run(zeroInput(t, methods["deposit"]), nil, true, false)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, staticCallSelector, res)

	res, err = h.run(zeroInput(t, methods["getMarkPrice"]), nil, false, true)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, delegatecallSelector, res)

	for name, method := range methods {
		input := zeroInput(t, method)
		res, err = h.run(input, nil, true, false)
		if perps.GasTable[name].Write {
			require.ErrorIs(t, err, vm.ErrExecutionReverted, name)
			require.Equal(t, staticCallSelector, res, name)
		} else {
			require.NotEqual(t, staticCallSelector, res, name)
		}
		for _, readOnly := range []bool{false, true} {
			res, err = h.run(input, nil, readOnly, true)
			require.ErrorIs(t, err, vm.ErrExecutionReverted, name)
			require.Equal(t, delegatecallSelector, res, name)
		}
	}

	pushOracle := append(crypto.Keccak256([]byte("pushOracle(bytes,bytes)"))[:4], make([]byte, 128)...)
	_, err = h.precompile.MethodById(pushOracle[:4])
	require.Error(t, err)
	res, err = h.run(pushOracle, nil, false, false)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Empty(t, res)
	require.True(t, h.storeEmpty())
}

func TestPerpsNonPayable(t *testing.T) {
	h := newHarness(t, true)
	methods := h.precompile.GetABI().Methods
	want := pcommon.RevertReason(pcommon.ValidateNonPayable(big.NewInt(1)))

	usdl := common.HexToAddress(perpstypes.DefaultCollateralToken)
	deposit := methods["deposit"]
	packed, err := deposit.Inputs.Pack(usdl, big.NewInt(1_000_000))
	require.NoError(t, err)
	res, err := h.run(append(append([]byte(nil), deposit.ID...), packed...), big.NewInt(1), false, false)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, want, res)
	require.True(t, h.storeEmpty())

	for name, method := range methods {
		res, err = h.run(zeroInput(t, method), big.NewInt(1), false, false)
		require.ErrorIs(t, err, vm.ErrExecutionReverted, name)
		require.Equal(t, want, res, name)
	}
	require.True(t, h.storeEmpty())
}

func TestPerpsKeepersAssertion(t *testing.T) {
	_, implements := utils.Keepers(bareKeepers{Keepers: testkeeper.EVMTestApp.GetPrecompileKeepers()}).(perps.PerpsKeepers)
	require.False(t, implements)
	var _ perps.PerpsKeepers = wiredKeepers{}
	missing := pcommon.RevertReason(perps.ErrKeeperMissing)

	bare := newHarness(t, false)
	methods := bare.precompile.GetABI().Methods
	for name, method := range methods {
		input := zeroInput(t, method)
		res, err := bare.run(input, nil, !perps.GasTable[name].Write, false)
		require.ErrorIs(t, err, vm.ErrExecutionReverted, name)
		require.Equal(t, missing, res, name)
	}
	res, err := bare.run(zeroInput(t, methods["getMarkPrice"]), nil, true, true)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, delegatecallSelector, res)
	require.True(t, bare.storeEmpty())

	wired := newHarness(t, true)
	res, _ = wired.run(zeroInput(t, methods["getMarkPrice"]), nil, true, false)
	require.NotEqual(t, missing, res)
}
