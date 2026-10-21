package precompiles_test

import (
	"maps"
	"os"
	"regexp"
	"slices"
	"testing"
	"time"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/feetoken"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/clearing"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/eip712"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/orob"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/pofq"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/xweb"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

func TestFeeTokenRegistration(t *testing.T) {
	initialized := precompiles.Initialized
	infoBefore := precompiles.PrecompileNamesToInfo
	precompiles.Initialized = false
	precompiles.PrecompileNamesToInfo = maps.Clone(infoBefore)
	t.Cleanup(func() {
		precompiles.Initialized = initialized
		precompiles.PrecompileNamesToInfo = infoBefore
	})
	for _, contracts := range []*vm.PrecompiledContracts{
		&vm.PrecompiledContractsHomestead, &vm.PrecompiledContractsByzantium,
		&vm.PrecompiledContractsIstanbul, &vm.PrecompiledContractsBerlin,
		&vm.PrecompiledContractsCancun, &vm.PrecompiledContractsBLS,
	} {
		before := *contracts
		*contracts = maps.Clone(before)
		t.Cleanup(func() { *contracts = before })
	}
	for _, addresses := range []*[]common.Address{
		&vm.PrecompiledAddressesHomestead, &vm.PrecompiledAddressesByzantium,
		&vm.PrecompiledAddressesIstanbul, &vm.PrecompiledAddressesBerlin,
		&vm.PrecompiledAddressesCancun,
	} {
		before := *addresses
		*addresses = slices.Clone(before)
		t.Cleanup(func() { *addresses = before })
	}
	address := common.HexToAddress("0x0000000000000000000000000000000000001018")
	require.Equal(t, address, common.HexToAddress(feetoken.FeeTokenAddress))
	require.NoError(t, precompiles.InitializePrecompiles(true, testkeeper.EVMTestApp.GetPrecompileKeepers()))
	info := precompiles.GetPrecompileInfo(feetoken.PrecompileName)
	require.Equal(t, address, info.Address)
	require.Len(t, info.ABI.Methods, 3)
	seen := make(map[common.Address]string)
	for name, entry := range precompiles.PrecompileNamesToInfo {
		previous, exists := seen[entry.Address]
		require.False(t, exists, "%s collides with %s at %s", name, previous, entry.Address)
		seen[entry.Address] = name
	}
	require.Equal(t, "v6.7", precompiles.FeeTokenUpgrade)
	below := precompiles.GetCustomPrecompiles("v6.6", testkeeper.EVMTestApp.GetPrecompileKeepers())
	_, belowFound := below[address]
	require.False(t, belowFound)
	versioned := precompiles.GetCustomPrecompiles(precompiles.FeeTokenUpgrade, testkeeper.EVMTestApp.GetPrecompileKeepers())
	versions, found := versioned[address]
	require.True(t, found)
	require.Len(t, versions, 1)
	p, ok := versions[precompiles.FeeTokenUpgrade].(precompiles.IPrecompile)
	require.True(t, ok)
	require.Equal(t, feetoken.PrecompileName, p.GetName())
	require.Equal(t, address, p.Address())
	for registered, entries := range versioned {
		for _, entry := range entries {
			named, ok := entry.(precompiles.IPrecompile)
			require.True(t, ok)
			require.Equal(t, registered, named.Address())
			if registered != address {
				require.NotEqual(t, feetoken.PrecompileName, named.GetName())
			}
		}
	}
	require.NoError(t, precompiles.InitializePrecompiles(false, testkeeper.EVMTestApp.GetPrecompileKeepers()))
	for _, contracts := range []map[common.Address]vm.PrecompiledContract{
		vm.PrecompiledContractsHomestead, vm.PrecompiledContractsByzantium,
		vm.PrecompiledContractsIstanbul, vm.PrecompiledContractsBerlin,
		vm.PrecompiledContractsCancun, vm.PrecompiledContractsBLS,
	} {
		entry, exists := contracts[address]
		require.True(t, exists)
		named, ok := entry.(precompiles.IPrecompile)
		require.True(t, ok)
		require.Equal(t, feetoken.PrecompileName, named.GetName())
	}
}

func TestXWebRegistration(t *testing.T) {
	initialized := precompiles.Initialized
	infoBefore := precompiles.PrecompileNamesToInfo
	precompiles.Initialized = false
	precompiles.PrecompileNamesToInfo = maps.Clone(infoBefore)
	t.Cleanup(func() {
		precompiles.Initialized = initialized
		precompiles.PrecompileNamesToInfo = infoBefore
	})
	for _, contracts := range []*vm.PrecompiledContracts{
		&vm.PrecompiledContractsHomestead, &vm.PrecompiledContractsByzantium,
		&vm.PrecompiledContractsIstanbul, &vm.PrecompiledContractsBerlin,
		&vm.PrecompiledContractsCancun, &vm.PrecompiledContractsBLS,
	} {
		before := *contracts
		*contracts = maps.Clone(before)
		t.Cleanup(func() { *contracts = before })
	}
	for _, addresses := range []*[]common.Address{
		&vm.PrecompiledAddressesHomestead, &vm.PrecompiledAddressesByzantium,
		&vm.PrecompiledAddressesIstanbul, &vm.PrecompiledAddressesBerlin,
		&vm.PrecompiledAddressesCancun,
	} {
		before := *addresses
		*addresses = slices.Clone(before)
		t.Cleanup(func() { *addresses = before })
	}
	address := common.HexToAddress("0x0000000000000000000000000000000000001019")
	require.Equal(t, address, common.HexToAddress(xweb.XWebAddress))
	require.NoError(t, precompiles.InitializePrecompiles(true, testkeeper.EVMTestApp.GetPrecompileKeepers()))
	info := precompiles.GetPrecompileInfo(xweb.PrecompileName)
	require.Equal(t, address, info.Address)
	require.Len(t, info.ABI.Methods, 9)
	require.Len(t, info.ABI.Events, 3)
	seen := make(map[common.Address]string)
	for name, entry := range precompiles.PrecompileNamesToInfo {
		previous, exists := seen[entry.Address]
		require.False(t, exists, "%s collides with %s at %s", name, previous, entry.Address)
		seen[entry.Address] = name
	}
	versioned := precompiles.GetCustomPrecompiles("v6.8", testkeeper.EVMTestApp.GetPrecompileKeepers())
	versions, found := versioned[address]
	require.True(t, found)
	require.Len(t, versions, 1)
	p, ok := versions["v6.8"].(precompiles.IPrecompile)
	require.True(t, ok)
	require.Equal(t, xweb.PrecompileName, p.GetName())
	require.Equal(t, address, p.Address())
	_, metered := versions["v6.8"].(vm.DynamicGasPrecompiledContract)
	require.True(t, metered)
	for registered, entries := range versioned {
		for _, entry := range entries {
			named, ok := entry.(precompiles.IPrecompile)
			require.True(t, ok)
			require.Equal(t, registered, named.Address())
			if registered != address {
				require.NotEqual(t, xweb.PrecompileName, named.GetName())
			}
		}
	}
	require.NoError(t, precompiles.InitializePrecompiles(false, testkeeper.EVMTestApp.GetPrecompileKeepers()))
	for _, contracts := range []map[common.Address]vm.PrecompiledContract{
		vm.PrecompiledContractsHomestead, vm.PrecompiledContractsByzantium,
		vm.PrecompiledContractsIstanbul, vm.PrecompiledContractsBerlin,
		vm.PrecompiledContractsCancun, vm.PrecompiledContractsBLS,
	} {
		entry, exists := contracts[address]
		require.True(t, exists)
		named, ok := entry.(precompiles.IPrecompile)
		require.True(t, ok)
		require.Equal(t, xweb.PrecompileName, named.GetName())
	}
	require.Contains(t, vm.PrecompiledAddressesCancun, address)
}

func TestMergeAddressTable(t *testing.T) {
	// The address and name pairs of contracts/address-table.md section (a).
	expected := []precompiles.MergeAddress{
		{Address: common.HexToAddress("0x000000000000000000000000000000000000101A"), Name: "layerxledger"},
		{Address: common.HexToAddress("0x000000000000000000000000000000000000101B"), Name: "layerxpay"},
		{Address: common.HexToAddress("0x000000000000000000000000000000000000101C"), Name: "layerxauth"},
		{Address: common.HexToAddress("0x000000000000000000000000000000000000101D"), Name: "layerxprograms"},
		{Address: common.HexToAddress("0x0000000000000000000000000000000000000901"), Name: "orob"},
		{Address: common.HexToAddress("0x0000000000000000000000000000000000000902"), Name: "clearing"},
		{Address: common.HexToAddress("0x0000000000000000000000000000000000000903"), Name: "oracle aggregator"},
		{Address: common.HexToAddress("0x0000000000000000000000000000000000000904"), Name: "pofq"},
		{Address: common.HexToAddress("0x0000000000000000000000000000000000000905"), Name: "scheduler"},
		{Address: common.HexToAddress("0x0000000000000000000000000000000000000906"), Name: "streams"},
		{Address: common.HexToAddress("0x0000000000000000000000000000000000000907"), Name: "tee attestor"},
		{Address: common.HexToAddress("0x0000000000000000000000000000000000000908"), Name: "eip712"},
	}
	require.Equal(t, expected, precompiles.MergeAddressTable)
	require.Equal(t, app.MergeUpgrade, precompiles.MergeUpgrade)

	// The wallet gateway routes reach the agent-function addresses.
	source, err := os.ReadFile("../services/wallet/gateway/src/precompiles.ts")
	require.NoError(t, err)
	routes := map[string]string{}
	for _, match := range regexp.MustCompile(`(?m)^\s+(\w+): '(0x[0-9a-fA-F]{40})',$`).FindAllStringSubmatch(string(source), -1) {
		routes[match[1]] = match[2]
	}
	for i, key := range []string{"orob", "clearing", "oracle", "pofq", "scheduler", "streams", "teeAttestor", "eip712"} {
		route, found := routes[key]
		require.True(t, found, key)
		require.Equal(t, expected[4+i].Address, common.HexToAddress(route), key)
	}

	// The registry refuses an address outside the table, a nil constructor and
	// a second constructor for one address.
	testApp := app.Setup(t, false, true, false)
	keepers := testApp.GetPrecompileKeepers()
	upgradeKeeper := &testApp.UpgradeKeeper
	outside := common.HexToAddress(xweb.XWebAddress)
	require.EqualError(t, precompiles.RegisterMergeConstructor(outside, func(utils.Keepers, string) (precompiles.IPrecompile, error) {
		return orob.NewPrecompile(upgradeKeeper, precompiles.MergeUpgrade, 64)
	}), "merge constructor for "+outside.Hex()+" outside the merge address table")
	require.EqualError(t, precompiles.RegisterMergeConstructor(expected[4].Address, nil),
		"nil merge constructor for "+expected[4].Address.Hex())

	// Register the agent functions whose packages are in the tree, unless
	// their packages registered them already.
	constructors := map[common.Address]precompiles.MergeConstructor{
		expected[4].Address: func(_ utils.Keepers, plan string) (precompiles.IPrecompile, error) {
			return orob.NewPrecompile(upgradeKeeper, plan, 64)
		},
		expected[5].Address: func(_ utils.Keepers, plan string) (precompiles.IPrecompile, error) {
			return clearing.NewPrecompile(upgradeKeeper, plan, 64)
		},
		expected[7].Address: func(_ utils.Keepers, plan string) (precompiles.IPrecompile, error) {
			return pofq.NewPrecompile(upgradeKeeper, plan, 64)
		},
		expected[11].Address: func(_ utils.Keepers, plan string) (precompiles.IPrecompile, error) {
			return eip712.NewPrecompile(upgradeKeeper, plan, 64, 64)
		},
	}
	registeredBefore := precompiles.GetCustomPrecompiles(app.LatestUpgrade, keepers)
	for addr, constructor := range constructors {
		if _, registered := registeredBefore[addr]; registered {
			continue
		}
		require.NoError(t, precompiles.RegisterMergeConstructor(addr, constructor))
	}
	require.EqualError(t, precompiles.RegisterMergeConstructor(expected[4].Address, constructors[expected[4].Address]),
		"merge constructor for "+expected[4].Address.Hex()+" already registered")

	// Every address with a registered constructor carries its precompile at
	// the latest upgrade and at the merge plan; no other merge address is in
	// the set.
	set := precompiles.GetCustomPrecompiles(app.LatestUpgrade, keepers)
	served := map[common.Address]string{}
	for _, entry := range expected {
		versions, found := set[entry.Address]
		_, constructed := constructors[entry.Address]
		if !found {
			require.False(t, constructed, entry.Name)
			continue
		}
		require.Len(t, versions, 2, entry.Name)
		p, ok := versions[precompiles.MergeUpgrade].(precompiles.IPrecompile)
		require.True(t, ok, entry.Name)
		require.Equal(t, entry.Name, p.GetName())
		require.Equal(t, entry.Address, p.Address())
		require.Same(t, p, versions[app.LatestUpgrade])
		served[entry.Address] = entry.Name
	}
	for addr := range constructors {
		require.Contains(t, served, addr)
	}

	// Served from the merge plan height through the keeper activation entry:
	// below it the set equals the set without the merge entries, at and above
	// it every registered address is served with its precompile.
	unmerged := maps.Clone(set)
	for _, entry := range expected {
		delete(unmerged, entry.Address)
	}
	modules := map[common.Address][]string{
		expected[0].Address: {"layerxledger"},
		expected[1].Address: {"layerxpay"},
		expected[2].Address: {"layerxauth"},
		expected[3].Address: {"layerxprograms"},
	}
	for _, entry := range expected[4:] {
		modules[entry.Address] = []string{"layerxagent"}
	}
	k := &testApp.EvmKeeper
	k.SetCustomPrecompileActivation([]evmkeeper.CustomPrecompileActivation{{Upgrade: precompiles.MergeUpgrade, Modules: modules}})
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(8).WithBlockTime(time.Now())
	const planHeight = int64(20)
	k.UpgradeKeeper().SetDone(ctx.WithBlockHeight(planHeight), precompiles.MergeUpgrade)

	heights := []int64{1, planHeight - 1, planHeight, planHeight + 1}
	baselines := map[bool]map[int64]map[common.Address]vm.PrecompiledContract{}
	k.SetCustomPrecompiles(unmerged, app.LatestUpgrade)
	for _, tracing := range []bool{false, true} {
		baselines[tracing] = map[int64]map[common.Address]vm.PrecompiledContract{}
		for _, height := range heights {
			baselines[tracing][height] = k.CustomPrecompiles(ctx.WithBlockHeight(height).WithIsTracing(tracing))
		}
	}
	k.SetCustomPrecompiles(set, app.LatestUpgrade)
	for _, tracing := range []bool{false, true} {
		baseline := baselines[tracing]
		for _, height := range []int64{1, planHeight - 1} {
			below := k.CustomPrecompiles(ctx.WithBlockHeight(height).WithIsTracing(tracing))
			require.Len(t, below, len(baseline[height]), height)
			for addr, p := range baseline[height] {
				require.Same(t, p, below[addr], height)
			}
			for _, entry := range expected {
				require.NotContains(t, below, entry.Address, height)
			}
		}
		for _, height := range []int64{planHeight, planHeight + 1} {
			at := k.CustomPrecompiles(ctx.WithBlockHeight(height).WithIsTracing(tracing))
			require.Len(t, at, len(baseline[height])+len(served), height)
			for addr, p := range baseline[height] {
				require.Same(t, p, at[addr], height)
			}
			for _, entry := range expected {
				name, registered := served[entry.Address]
				if !registered {
					require.NotContains(t, at, entry.Address, height)
					continue
				}
				named, ok := at[entry.Address].(precompiles.IPrecompile)
				require.True(t, ok, height)
				require.Equal(t, name, named.GetName(), height)
				require.Same(t, set[entry.Address][precompiles.MergeUpgrade], at[entry.Address], height)
			}
		}
	}
}
