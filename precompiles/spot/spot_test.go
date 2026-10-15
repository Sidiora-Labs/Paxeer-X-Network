package spot_test

import (
	"bytes"
	"math/big"
	"os"
	"testing"
	"time"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	spotkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/spot"
	putils "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// spotKeepers are the application keepers with the spot keeper beside them.
type spotKeepers struct {
	putils.Keepers
	spot *spotkeeper.Keeper
}

func (k spotKeepers) SpotK() *spotkeeper.Keeper { return k.spot }

var (
	pecorAddr      = common.HexToAddress(types.PECORAddress)
	ordersAddr     = common.HexToAddress(types.PECOROrdersAddress)
	stopOrdersAddr = common.HexToAddress(types.PECORStopOrdersAddress)
	quoterAddr     = common.HexToAddress(types.PECORQuoterAddress)
	vaultAddr      = common.HexToAddress(types.PECORVaultAddress)
	unmapped       = []common.Address{
		common.HexToAddress("0x0000000000000000000000000000000000001026"),
		common.HexToAddress("0x0000000000000000000000000000000000001027"),
	}
	served = []common.Address{pecorAddr, ordersAddr, stopOrdersAddr, quoterAddr, vaultAddr}
)

func fixtureSet(t *testing.T, f *pecorFixture) map[common.Address]*pcommon.Precompile {
	t.Helper()
	set, err := spot.NewPrecompileSet(spotKeepers{testkeeper.EVMTestApp.GetPrecompileKeepers(), f.keeper})
	require.NoError(t, err)
	return set
}

func fixtureContracts(t *testing.T, f *pecorFixture) map[common.Address]vm.DynamicGasPrecompiledContract {
	t.Helper()
	versioned, err := spot.GetVersioned(app.LatestUpgrade, spotKeepers{testkeeper.EVMTestApp.GetPrecompileKeepers(), f.keeper})
	require.NoError(t, err)
	out := make(map[common.Address]vm.DynamicGasPrecompiledContract, len(versioned))
	for address, versions := range versioned {
		contract, ok := versions[app.LatestUpgrade].(vm.DynamicGasPrecompiledContract)
		require.True(t, ok, address.Hex())
		out[address] = contract
	}
	return out
}

// run calls contract at address through RunAndCalculateGas inside an EVM over
// the fixture state, committing the state the call leaves.
func run(t *testing.T, f *pecorFixture, contract vm.DynamicGasPrecompiledContract, address common.Address, input []byte,
	gas uint64, value *big.Int, readOnly bool) (ret []byte, left uint64, callErr error) {
	t.Helper()
	require.NoError(t, testkeeper.EVMTestApp.EvmKeeper.RunWithOneOffEVMInstance(f.ctx, func(evm *vm.EVM) error {
		ret, left, callErr = contract.RunAndCalculateGas(evm, f.user, address, input, gas, value, nil, readOnly, false)
		return nil
	}, func(string, string) {}))
	return
}

func packed(t *testing.T, contract abi.ABI, name string, args ...interface{}) []byte {
	t.Helper()
	data, err := contract.Pack(name, args...)
	require.NoError(t, err)
	return data
}

func TestNewPrecompileSetAddresses(t *testing.T) {
	f := newPECORFixture(t)
	set := fixtureSet(t, f)
	require.Len(t, set, len(served))
	names := map[common.Address]string{pecorAddr: "spot_pecor", ordersAddr: "spot_orders", stopOrdersAddr: "spot_stoporders",
		quoterAddr: "spot_quoter", vaultAddr: "spot_vault"}
	for _, address := range served {
		require.Contains(t, set, address)
		require.Equal(t, address, set[address].Address())
		require.Equal(t, names[address], set[address].GetName())
	}
	for _, address := range unmapped {
		require.NotContains(t, set, address)
	}

	keepers := testkeeper.EVMTestApp.GetPrecompileKeepers()
	_, err := spot.NewPrecompileSet(&putils.EmptyKeepers{})
	require.EqualError(t, err, "spot: the application keepers carry no spot keeper")
	_, err = spot.NewPrecompileSet(spotKeepers{keepers, nil})
	require.EqualError(t, err, "spot: the application keepers carry no spot keeper")
	_, err = spot.NewPrecompileSet(spotKeepers{&putils.EmptyKeepers{}, f.keeper})
	require.EqualError(t, err, "spot: the application keepers carry no EVM keeper")
	_, err = spot.GetVersioned(app.LatestUpgrade, &putils.EmptyKeepers{})
	require.EqualError(t, err, "spot: the application keepers carry no spot keeper")

	versioned, err := spot.GetVersioned(app.LatestUpgrade, spotKeepers{keepers, f.keeper})
	require.NoError(t, err)
	require.Len(t, versioned, len(served))
	for _, address := range served {
		versions := versioned[address]
		require.Contains(t, versions, app.LatestUpgrade)
		require.Contains(t, versions, app.V6120Upgrade)
		require.Same(t, versions[app.LatestUpgrade], versions[app.V6120Upgrade])
		_, dynamic := versions[app.V6120Upgrade].(vm.DynamicGasPrecompiledContract)
		require.True(t, dynamic, address.Hex())
	}

	contract := fixtureContracts(t, f)[pecorAddr]
	ret, left, err := run(t, f, contract, pecorAddr, f.pack("vault"), 1_000_000, new(big.Int), true)
	require.NoError(t, err)
	require.Equal(t, common.LeftPadBytes(vaultAddr.Bytes(), 32), ret)
	require.LessOrEqual(t, left, uint64(1_000_000-3000))
}

func TestMutabilityMatchesABI(t *testing.T) {
	f := newPECORFixture(t)
	set := fixtureSet(t, f)
	contracts := fixtureContracts(t, f)
	payable := map[common.Address][]string{}
	for address, base := range set {
		for name, method := range base.GetABI().Methods {
			if method.StateMutability == "payable" {
				payable[address] = append(payable[address], name)
			}
		}
	}
	require.Equal(t, map[common.Address][]string{pecorAddr: {"swapExactInNative"}, vaultAddr: {"depositNative"}}, payable)
	require.Equal(t, "nonpayable", set[pecorAddr].GetABI().Methods["multicall"].StateMutability)
	require.True(t, set[pecorAddr].HasReceive())
	require.True(t, set[vaultAddr].HasReceive())

	nonPayable := pcommon.RevertReason(pcommon.ValidateNonPayable(big.NewInt(1)))
	writeProtected := pcommon.RevertReason(vm.ErrWriteProtection)
	zeros := make([]byte, 32*64)
	for address, base := range set {
		for name, method := range base.GetABI().Methods {
			input := append(append([]byte(nil), method.ID...), zeros...)
			if method.StateMutability != "payable" {
				ret, _, err := run(t, f, contracts[address], address, input, 10_000_000, big.NewInt(1), false)
				require.ErrorIs(t, err, vm.ErrExecutionReverted, name)
				require.Equal(t, nonPayable, ret, "%s %s", address.Hex(), name)
			}
			if !method.IsConstant() {
				ret, _, err := run(t, f, contracts[address], address, input, 10_000_000, new(big.Int), true)
				require.ErrorIs(t, err, vm.ErrExecutionReverted, name)
				require.Equal(t, writeProtected, ret, "%s %s", address.Hex(), name)
			}
		}
	}

	before := f.balance(f.stable, f.user)
	book := pecorSnapshot(t, f.ctx, f.key)
	ret, _, err := run(t, f, contracts[pecorAddr], pecorAddr,
		f.pack("marketBuy", f.stable, f.pax, big.NewInt(1000_000000), big.NewInt(1990_000000), big.NewInt(100)), 10_000_000, big.NewInt(1), false)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, nonPayable, ret)
	require.Equal(t, before, f.balance(f.stable, f.user))
	require.Equal(t, book, pecorSnapshot(t, f.ctx, f.key))
}

// freshSpotApp is a new application whose EVM serves the spot set at the
// returned height-8 context.
func freshSpotApp(t *testing.T) (*app.App, sdk.Context) {
	t.Helper()
	a := app.Setup(t, false, true, false)
	ctx := a.GetContextForDeliverTx([]byte{}).WithBlockHeight(8).WithBlockTime(time.Unix(1000, 0).UTC())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	feedKey, spotKey := sdk.NewKVStoreKey(pricefeedtypes.StoreKey), sdk.NewKVStoreKey(types.StoreKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, feedKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, spotKey)
	feed := pricefeedkeeper.NewKeeper(feedKey, a.StakingKeeper, a.AccountKeeper, &a.EvmKeeper)
	k := spotkeeper.NewKeeper(spotKey, a.BankKeeper, &a.EvmKeeper, feed)
	versioned, err := spot.GetVersioned(app.LatestUpgrade, spotKeepers{a.GetPrecompileKeepers(), k})
	require.NoError(t, err)
	a.EvmKeeper.SetCustomPrecompiles(versioned, app.LatestUpgrade)
	return a, ctx
}

// call sends input to address through the EVM of a at ctx.
func call(t *testing.T, a *app.App, ctx sdk.Context, address common.Address, input []byte, gas uint64) (ret []byte, left uint64, callErr error) {
	t.Helper()
	require.NoError(t, a.EvmKeeper.RunWithOneOffEVMInstance(ctx, func(evm *vm.EVM) error {
		ret, left, callErr = evm.Call(common.HexToAddress("0x3100"), address, input, gas, uint256.NewInt(0))
		return nil
	}, func(string, string) {}))
	return
}

func pecorABI(t *testing.T) abi.ABI {
	t.Helper()
	contractJSON, err := os.ReadFile("abi/pecor.json")
	require.NoError(t, err)
	contract, err := abi.JSON(bytes.NewReader(contractJSON))
	require.NoError(t, err)
	return contract
}

func TestUnmappedAddressesHaveNoCode(t *testing.T) {
	a, ctx := freshSpotApp(t)
	contract := pecorABI(t)
	present := a.EvmKeeper.CustomPrecompiles(ctx)
	for _, address := range served {
		require.Contains(t, present, address)
	}
	for _, address := range unmapped {
		require.NotContains(t, present, address)
		require.Empty(t, a.EvmKeeper.GetCode(ctx, address))
		for _, input := range [][]byte{packed(t, contract, "vault"), packed(t, contract, "owner"), nil} {
			ret, left, err := call(t, a, ctx, address, input, 100_000)
			require.NoError(t, err, address.Hex())
			require.Empty(t, ret, address.Hex())
			require.Equal(t, uint64(100_000), left, address.Hex())
		}
	}
	ret, _, err := call(t, a, ctx, pecorAddr, packed(t, contract, "vault"), 100_000)
	require.NoError(t, err)
	require.Equal(t, common.LeftPadBytes(vaultAddr.Bytes(), 32), ret)
}

func TestServedOnlyFromActivation(t *testing.T) {
	a, ctx := freshSpotApp(t)
	contract := pecorABI(t)
	modules := make(map[common.Address][]string, len(served))
	for _, address := range served {
		modules[address] = []string{types.ModuleName}
	}
	a.EvmKeeper.SetCustomPrecompileActivation([]evmkeeper.CustomPrecompileActivation{{Upgrade: app.V6120Upgrade, Modules: modules}})
	const activation = int64(20)
	a.EvmKeeper.UpgradeKeeper().SetDone(ctx.WithBlockHeight(activation), app.V6120Upgrade)

	buy := packed(t, contract, "marketBuy", common.HexToAddress("0x4300"), common.HexToAddress("0x4100"), big.NewInt(1), new(big.Int), new(big.Int))
	for _, height := range []int64{8, activation - 1} {
		at := ctx.WithBlockHeight(height)
		present := a.EvmKeeper.CustomPrecompiles(at)
		for _, address := range served {
			require.NotContains(t, present, address, height)
		}
		for _, input := range [][]byte{buy, packed(t, contract, "vault")} {
			ret, left, err := call(t, a, at, pecorAddr, input, 200_000)
			require.NoError(t, err, height)
			require.Empty(t, ret, height)
			require.Equal(t, uint64(200_000), left, height)
		}
	}
	for _, height := range []int64{activation, activation + 1} {
		at := ctx.WithBlockHeight(height)
		present := a.EvmKeeper.CustomPrecompiles(at)
		for _, address := range served {
			require.Contains(t, present, address, height)
		}
		ret, left, err := call(t, a, at, pecorAddr, packed(t, contract, "vault"), 200_000)
		require.NoError(t, err, height)
		require.Equal(t, common.LeftPadBytes(vaultAddr.Bytes(), 32), ret, height)
		require.LessOrEqual(t, left, uint64(200_000-3000), height)
	}
}

func TestGasTableRows(t *testing.T) {
	f := newPECORFixture(t)
	set := fixtureSet(t, f)
	require.Equal(t, spot.GasRow{Base: 60000}, spot.GasTable[pecorAddr]["marketBuy"])
	require.Equal(t, spot.GasRow{Base: 3000}, spot.GasTable[pecorAddr]["owner"])
	require.Equal(t, spot.GasRow{Base: 6000}, spot.GasTable[pecorAddr]["getQuoteExactIn"])
	require.Equal(t, spot.GasRow{Base: 30000}, spot.GasTable[ordersAddr]["cancelLimitOrder"])
	require.Equal(t, spot.GasRow{Base: 50000}, spot.GasTable[ordersAddr]["placeLimitBuy"])
	require.Equal(t, spot.GasRow{Base: 20000, PerID: 60000}, spot.GasTable[ordersAddr]["batchExecuteLimitOrders"])
	require.Equal(t, spot.GasRow{Base: 20000, PerID: 60000}, spot.GasTable[stopOrdersAddr]["batchExecuteStopLossOrders"])
	require.Equal(t, spot.GasRow{PerToken: 20000}, spot.GasTable[vaultAddr]["syncAllReserves"])
	require.Equal(t, spot.GasRow{Base: 20000}, spot.GasTable[vaultAddr]["syncReserves"])

	buy := f.pack("marketBuy", f.stable, f.pax, big.NewInt(1000_000000), big.NewInt(1990_000000), big.NewInt(100))
	require.Equal(t, 60000+spot.GasPerInputByte*uint64(len(buy)-4), set[pecorAddr].RequiredGas(buy))
	owner := f.pack("owner")
	require.Equal(t, uint64(3000), set[pecorAddr].RequiredGas(owner))
	ordersABI := set[ordersAddr].GetABI()
	for _, ids := range [][]*big.Int{{}, {big.NewInt(1)}, {big.NewInt(1), big.NewInt(2), big.NewInt(3)}} {
		batch := packed(t, ordersABI, "batchExecuteLimitOrders", ids)
		require.Equal(t, 20000+60000*uint64(len(ids))+spot.GasPerInputByte*uint64(len(batch)-4), set[ordersAddr].RequiredGas(batch), len(ids))
	}
	vaultABI := set[vaultAddr].GetABI()
	require.Equal(t, uint64(20000), set[vaultAddr].RequiredGas(packed(t, vaultABI, "syncAllReserves")))
	sync := packed(t, vaultABI, "syncReserves", f.pax)
	require.Equal(t, 20000+spot.GasPerInputByte*uint64(len(sync)-4), set[vaultAddr].RequiredGas(sync))
	require.Equal(t, pcommon.UnknownMethodCallGas, set[pecorAddr].RequiredGas([]byte{1, 2, 3, 4}))

	// The dynamic path charges syncAllReserves for each of the three tokens.
	contracts := fixtureContracts(t, f)
	all := packed(t, vaultABI, "syncAllReserves")
	ret, left, err := run(t, f, contracts[vaultAddr], vaultAddr, all, 3*20000-1, new(big.Int), false)
	require.ErrorIs(t, err, vm.ErrOutOfGas)
	require.Nil(t, ret)
	require.Zero(t, left)
	// The row and the per-byte charge come off the supplied gas before the call.
	ret, left, err = run(t, f, contracts[pecorAddr], pecorAddr, owner, 3000, new(big.Int), true)
	require.ErrorIs(t, err, vm.ErrOutOfGas)
	require.Nil(t, ret)
	require.Zero(t, left)
	ret, left, err = run(t, f, contracts[pecorAddr], pecorAddr, owner, 1_000_000, new(big.Int), true)
	require.NoError(t, err)
	require.Len(t, ret, 32)
	require.LessOrEqual(t, left, uint64(1_000_000-3000))
}

func TestEveryMethodHasGasRow(t *testing.T) {
	f := newPECORFixture(t)
	set := fixtureSet(t, f)
	require.Len(t, spot.GasTable, len(served))
	for _, address := range unmapped {
		require.NotContains(t, spot.GasTable, address)
	}
	for _, address := range served {
		rows, ok := spot.GasTable[address]
		require.True(t, ok, address.Hex())
		methods := set[address].GetABI().Methods
		require.Len(t, rows, len(methods), address.Hex())
		for name := range methods {
			row, ok := rows[name]
			require.True(t, ok, "%s %s", address.Hex(), name)
			require.NotEqual(t, spot.GasRow{}, row, "%s %s", address.Hex(), name)
		}
		for name := range rows {
			require.Contains(t, methods, name, address.Hex())
		}
	}
}

func TestBankMovesChargeGas(t *testing.T) {
	f := newPECORFixture(t)
	contract := fixtureContracts(t, f)[pecorAddr]
	buy := f.pack("marketBuy", f.stable, f.pax, big.NewInt(1000_000000), big.NewInt(1990_000000), big.NewInt(100))
	row := 60000 + spot.GasPerInputByte*uint64(len(buy)-4)

	stable, pax := f.balance(f.stable, f.user), f.balance(f.pax, f.user)
	book := pecorSnapshot(t, f.ctx, f.key)
	for _, gas := range []uint64{row - 1, row} {
		ret, left, err := run(t, f, contract, pecorAddr, buy, gas, new(big.Int), false)
		require.ErrorIs(t, err, vm.ErrOutOfGas, gas)
		require.Nil(t, ret, gas)
		require.Zero(t, left, gas)
		require.Equal(t, stable, f.balance(f.stable, f.user), gas)
		require.Equal(t, pax, f.balance(f.pax, f.user), gas)
		require.Equal(t, book, pecorSnapshot(t, f.ctx, f.key), gas)
	}

	const supplied = uint64(10_000_000)
	ret, left, err := run(t, f, contract, pecorAddr, buy, supplied, new(big.Int), false)
	require.NoError(t, err)
	values, err := f.abi.Methods["marketBuy"].Outputs.Unpack(ret)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(1994_000000), values[0])
	require.Equal(t, new(big.Int).Add(pax, big.NewInt(1994_000000)), f.balance(f.pax, f.user))
	require.Equal(t, new(big.Int).Sub(stable, big.NewInt(1000_000000)), f.balance(f.stable, f.user))
	require.Greater(t, supplied-left, row)
}
