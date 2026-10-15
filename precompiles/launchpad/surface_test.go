package launchpad_test

import (
	"encoding/json"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/launchpad"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const (
	surfaceGas      uint64 = 30_000_000
	surfaceUsdlMint int64  = 30_000_000_000_000
	bobFunds        int64  = 5_000_000
)

// generation1PoolRegistry is the retired generation 1 PoolRegistry
// (contracts/launchpad.md section 1).
var generation1PoolRegistry = common.HexToAddress("0x7684382c89f79104574D8EF9b31eFf2eD2C2BA0b")

// callFrame is one call frame the EVM entered, as its tracer saw it.
type callFrame struct {
	depth    int
	op       vm.OpCode
	from, to common.Address
	input    []byte
	gas      uint64
	value    *big.Int
}

// surfaceHarness is the launchpad keeper and precompile over the test
// application with a USDL ERC20 deployed in its EVM, the generation 2 role
// surfaces installed, a migrated generation 1 and generation 2 pool of
// creator, a market created after the upgrade by creator, and the generation
// 1 PoolRegistry retired. pools lists them in that order.
type surfaceHarness struct {
	t        *testing.T
	stateDB  *state.DBImpl
	evm      *vm.EVM
	contract vm.PrecompiledContract
	keeper   *launchpadkeeper.Keeper

	usdl, holder, creator, alice, bob common.Address
	pools                             [3]common.Address
	tokens                            [3]common.Address
	markets                           [3]types.Market
	frames                            []callFrame
}

func named(name string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte(name))[12:])
}

func addressWord(address common.Address) []byte { return common.LeftPadBytes(address.Bytes(), 32) }

func amountWord(amount int64) []byte { return common.LeftPadBytes(big.NewInt(amount).Bytes(), 32) }

func selector(signature string) []byte { return crypto.Keccak256([]byte(signature))[:4] }

// usdlCreation is the SidioraERC20 creation code the launchpad keeper deploys
// pool tokens with; USDL has the same ERC20 shape.
func usdlCreation(t *testing.T) []byte {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("..", "..", "modules", "launchpad", "keeper", "market.go"))
	require.NoError(t, err)
	match := regexp.MustCompile(`var sidioraERC20Creation = common\.FromHex\("(0x[0-9a-f]+)"\)`).FindSubmatch(source)
	require.Len(t, match, 2)
	return common.FromHex(string(match[1]))
}

// migratedMarket is the market record of a migrated pool of generation
// carrying the generation 2 role addresses (ruling 5).
func migratedMarket(index uint64, generation types.Generation, pool, token common.Address, creator sdk.AccAddress,
	roles launchpadkeeper.RoleAddresses,
) types.Market {
	snapshots := make([]sdk.Int, types.SnapshotSlots)
	for i := range snapshots {
		snapshots[i] = sdk.ZeroInt()
	}
	supply := sdk.NewInt(1_000_000_000_000_000)
	return types.Market{
		Denom: types.ModuleName + "/" + pool.Hex(), Index: index, Name: "Migrated Token", Symbol: "MGRT",
		Creator: creator.String(), Guardian: creator.String(), FeeRightsHolder: creator.String(),
		FeeStrategy: types.FeeStrategyClaim, TotalSupply: supply, VirtualQuoteReserve: sdk.NewInt(1_000_000_000),
		RealQuoteBalance: sdk.ZeroInt(), TokenReserve: supply, CreationTime: genesisTime.Unix() - 3600,
		CumulativeVolume: sdk.ZeroInt(), PriceSnapshots: snapshots, SnapshotHeights: make([]uint64, types.SnapshotSlots),
		AccumulatedQuoteFees: sdk.ZeroInt(), AccumulatedTokenFees: sdk.ZeroInt(), AccumulatedFees: sdk.ZeroInt(),
		AirdropBalance: sdk.ZeroInt(), Generation: generation, PoolAddress: pool.Hex(), TokenAddress: token.Hex(),
		Optical: common.Address{}.Hex(), Factory: roles.SidioraFactory.Hex(), FeeAccumulator: roles.FeeAccumulator.Hex(),
		ProtocolConfig: roles.ProtocolConfig.Hex(), EventEmitter: roles.EventEmitter.Hex(),
		CumulativeBuyQuoteVolume: sdk.ZeroInt(), CumulativeSellQuoteVolume: sdk.ZeroInt(),
		FeeRightCollection: roles.SidioraNFT.Hex(),
	}
}

func newSurfaceHarness(t *testing.T) *surfaceHarness {
	t.Helper()
	app := testkeeper.EVMTestApp
	evmKeeper := &app.EvmKeeper
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockTime(genesisTime)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	k, ctx := launchpadtest.NewKeeper(app, ctx)
	s := &surfaceHarness{t: t, keeper: k, holder: named("usdl holder"), creator: named("creator"),
		alice: named("alice"), bob: named("bob")}

	deployer := named("usdl deployer")
	args, err := abi.Arguments{{Type: abiType(t, "string")}, {Type: abiType(t, "string")}, {Type: abiType(t, "uint256")},
		{Type: abiType(t, "address")}}.Pack("USD Ledger", "USDL", big.NewInt(surfaceUsdlMint), s.holder)
	require.NoError(t, err)
	nonce := evmKeeper.GetNonce(ctx, deployer)
	_, err = evmKeeper.CallEVM(ctx, deployer, nil, nil, append(usdlCreation(t), args...))
	require.NoError(t, err)
	s.usdl = crypto.CreateAddress(deployer, nonce)
	require.NotEmpty(t, evmKeeper.GetCode(ctx, s.usdl))

	gs := types.DefaultGenesis()
	gs.Params.QuoteToken = s.usdl.Hex()
	creator := evmKeeper.GetPaxAddressOrDefault(ctx, s.creator)
	s.pools[0], s.tokens[0] = named("generation 1 pool"), named("generation 1 pool token")
	s.pools[1], s.tokens[1] = named("generation 2 pool"), named("generation 2 pool token")
	gs.Markets = []types.Market{
		migratedMarket(1, types.Generation1, s.pools[0], s.tokens[0], creator, k.Generation2()),
		migratedMarket(2, types.Generation2, s.pools[1], s.tokens[1], creator, k.Generation2()),
	}
	k.InitGenesis(ctx, *gs)
	require.NoError(t, launchpad.InstallRoleSurfaces(ctx, evmKeeper, k))
	for i, market := range gs.Markets {
		require.NoError(t, launchpad.InstallSurface(ctx, evmKeeper, k, s.pools[i],
			launchpadkeeper.Surface{Role: types.RolePool, Generation: market.Generation, Market: market.Denom}))
		s.markets[i] = market
	}
	require.NoError(t, launchpad.InstallRetired(ctx, evmKeeper, generation1PoolRegistry,
		launchpadkeeper.Generation2Surfaces[types.RolePoolRegistry]))

	usdlSend := func(from common.Address, signature string, words ...[]byte) {
		input := selector(signature)
		for _, w := range words {
			input = append(input, w...)
		}
		_, err := evmKeeper.CallEVM(ctx, from, &s.usdl, nil, input)
		require.NoError(t, err, signature)
	}
	fee := gs.Params.CreationFee.Int64()
	usdlSend(s.holder, "transfer(address,uint256)", addressWord(s.creator), amountWord(fee))
	usdlSend(s.creator, "approve(address,uint256)", addressWord(precompile), amountWord(fee))
	usdlSend(s.holder, "transfer(address,uint256)", addressWord(s.bob), amountWord(bobFunds))
	usdlSend(s.bob, "approve(address,uint256)", addressWord(precompile), amountWord(bobFunds))
	ctx, s.markets[2], err = k.CreateMarket(ctx, k.ModuleCaller(), nil, s.creator, "Kindle Token", "KNDL",
		types.FeeStrategyClaim, common.Address{})
	require.NoError(t, err)
	s.pools[2], s.tokens[2] = common.HexToAddress(s.markets[2].PoolAddress), common.HexToAddress(s.markets[2].TokenAddress)

	contract, err := launchpad.NewPrecompile(keepers{Keepers: app.GetPrecompileKeepers(), launchpad: k})
	require.NoError(t, err)
	s.contract = contract
	s.stateDB = state.NewDBImpl(ctx, evmKeeper, false)
	blockCtx, err := evmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
	require.NoError(t, err)
	precompiles := map[common.Address]vm.PrecompiledContract{}
	for address, p := range evmKeeper.CustomPrecompiles(ctx) {
		precompiles[address] = p
	}
	precompiles[precompile] = s.contract
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(evmKeeper.ChainID(ctx))
	hooks := &tracing.Hooks{OnEnter: func(depth int, typ byte, from, to common.Address, input []byte, gas uint64,
		value *big.Int,
	) {
		s.frames = append(s.frames, callFrame{depth: depth, op: vm.OpCode(typ), from: from, to: to,
			input: append([]byte(nil), input...), gas: gas, value: value})
	}}
	s.evm = vm.NewEVM(*blockCtx, s.stateDB, cfg, vm.Config{Tracer: hooks}, precompiles)
	return s
}

func abiType(t *testing.T, kind string) abi.Type {
	t.Helper()
	typ, err := abi.NewType(kind, "", nil)
	require.NoError(t, err)
	return typ
}

func (s *surfaceHarness) ctx() sdk.Context { return s.stateDB.Ctx() }

func (s *surfaceHarness) method(role, name string) abi.Method {
	s.t.Helper()
	m, ok := parseABIFile(s.t, filepath.Join("abi", role+".json")).Methods[name]
	require.True(s.t, ok, "%s.%s", role, name)
	return m
}

func (s *surfaceHarness) encode(role, name string, args ...interface{}) []byte {
	s.t.Helper()
	m := s.method(role, name)
	packed, err := m.Inputs.Pack(args...)
	require.NoError(s.t, err)
	return append(append([]byte(nil), m.ID...), packed...)
}

func (s *surfaceHarness) decode(role, name string, ret []byte) []interface{} {
	s.t.Helper()
	out, err := s.method(role, name).Outputs.Unpack(ret)
	require.NoError(s.t, err)
	return out
}

func (s *surfaceHarness) call(from, to common.Address, input []byte) ([]byte, error) {
	s.frames = nil
	ret, _, err := s.evm.Call(from, to, input, surfaceGas, uint256.NewInt(0))
	return ret, err
}

func (s *surfaceHarness) staticCall(from, to common.Address, input []byte) ([]byte, error) {
	s.frames = nil
	ret, _, err := s.evm.StaticCall(from, to, input, surfaceGas)
	return ret, err
}

func (s *surfaceHarness) usdlRead(signature string, words ...[]byte) *big.Int {
	s.t.Helper()
	input := selector(signature)
	for _, w := range words {
		input = append(input, w...)
	}
	ret, _, err := s.evm.StaticCall(s.alice, s.usdl, input, surfaceGas)
	require.NoError(s.t, err, signature)
	require.Len(s.t, ret, 32, signature)
	return new(big.Int).SetBytes(ret)
}

func (s *surfaceHarness) usdlBalance(holder common.Address) *big.Int {
	return s.usdlRead("balanceOf(address)", addressWord(holder))
}

func (s *surfaceHarness) usdlAllowance(owner common.Address) *big.Int {
	return s.usdlRead("allowance(address,address)", addressWord(owner), addressWord(precompile))
}

// exported is the launchpad genesis export of the current state as JSON.
func (s *surfaceHarness) exported() string {
	s.t.Helper()
	bz, err := json.Marshal(s.keeper.ExportGenesis(s.ctx()))
	require.NoError(s.t, err)
	return string(bz)
}

func surfaceAt(role string) common.Address { return launchpadkeeper.Generation2Surfaces[role] }

func TestSurfaceRuntimeForwards(t *testing.T) {
	s := newSurfaceHarness(t)
	require.Len(t, launchpad.SurfaceRuntime, 41)
	require.Equal(t, "0x363d3d373360601b36523d3d601436013d3d6110175af13d600060003e6024573d6000fd5b3d6000f3",
		hexutil.Encode(launchpad.SurfaceRuntime))

	require.Len(t, launchpadkeeper.Generation2Surfaces, 9)
	for role, address := range launchpadkeeper.Generation2Surfaces {
		require.Equal(t, launchpad.SurfaceRuntime, s.stateDB.GetCode(address), role)
		got, found, err := s.keeper.GetSurface(s.ctx(), address)
		require.NoError(t, err, role)
		require.True(t, found, role)
		require.Equal(t, launchpadkeeper.Surface{Role: role, Generation: types.Generation2}, got, role)
	}
	for i, pool := range s.pools {
		require.Equal(t, launchpad.SurfaceRuntime, s.stateDB.GetCode(pool), pool.Hex())
		got, found, err := s.keeper.GetSurface(s.ctx(), pool)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, launchpadkeeper.Surface{Role: types.RolePool, Generation: s.markets[i].Generation,
			Market: s.markets[i].Denom}, got)
	}
	require.Equal(t, s.stateDB.GetCode(surfaceAt(types.RoleRouter)), s.stateDB.GetCode(s.pools[0]))
	require.NotEmpty(t, s.stateDB.GetCode(s.usdl))
	require.NotEqual(t, launchpad.SurfaceRuntime, s.stateDB.GetCode(s.usdl))
	require.NotEmpty(t, s.stateDB.GetCode(s.tokens[2]))
	require.NotEqual(t, launchpad.SurfaceRuntime, s.stateDB.GetCode(s.tokens[2]))

	registry := surfaceAt(types.RolePoolRegistry)
	input := s.encode("PoolRegistry", "getPoolCount")
	forwarded := append(append([]byte(nil), input...), s.alice.Bytes()...)
	ret, err := s.call(s.alice, registry, input)
	require.NoError(t, err)
	require.Equal(t, []interface{}{big.NewInt(3)}, s.decode("PoolRegistry", "getPoolCount", ret))
	require.Len(t, s.frames, 2)
	outer, inner := s.frames[0], s.frames[1]
	require.Equal(t, 0, outer.depth)
	require.Equal(t, vm.CALL, outer.op)
	require.Equal(t, s.alice, outer.from)
	require.Equal(t, registry, outer.to)
	require.Equal(t, input, outer.input)
	require.Equal(t, surfaceGas, outer.gas)
	require.Equal(t, 1, inner.depth)
	require.Equal(t, vm.CALL, inner.op)
	require.Equal(t, registry, inner.from)
	require.Equal(t, precompile, inner.to)
	require.Equal(t, forwarded, inner.input)
	require.Zero(t, inner.value.Sign())
	require.LessOrEqual(t, inner.gas, surfaceGas-surfaceGas/64)
	require.GreaterOrEqual(t, inner.gas, (surfaceGas-5_000)-(surfaceGas-5_000)/64)

	direct, err := s.contract.Run(s.evm, registry, registry, forwarded, new(big.Int), false, false, nil)
	require.NoError(t, err)
	require.Equal(t, direct, ret)

	unknown := []byte{0xde, 0xad, 0xbe, 0xef}
	ret, err = s.call(s.alice, s.pools[0], unknown)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	want, wantErr := s.contract.Run(s.evm, s.pools[0], s.pools[0], append(append([]byte(nil), unknown...),
		s.alice.Bytes()...), new(big.Int), false, false, nil)
	require.ErrorIs(t, wantErr, vm.ErrExecutionReverted)
	require.Equal(t, want, ret)
}

func TestSuffixTrustedOnlyFromSurfaces(t *testing.T) {
	s := newSurfaceHarness(t)
	router := surfaceAt(types.RoleRouter)
	pool := s.pools[2]
	buy := s.encode("Router", "buy", pool, big.NewInt(1_000_000), big.NewInt(0), deadline)
	suffixed := append(append([]byte(nil), buy...), s.bob.Bytes()...)
	balance, allowance := s.usdlBalance(s.bob), s.usdlAllowance(s.bob)
	require.Equal(t, big.NewInt(bobFunds), balance)
	require.Equal(t, big.NewInt(bobFunds), allowance)
	before := s.exported()

	// Account A is not a surface: its trailing bytes are calldata, which no
	// longer decodes as buy.
	_, err := s.call(s.alice, precompile, suffixed)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, balance, s.usdlBalance(s.bob))
	require.Equal(t, allowance, s.usdlAllowance(s.bob))
	require.Equal(t, before, s.exported())

	// Through a surface the surface appends A after B, so B is calldata again.
	_, err = s.call(s.alice, router, suffixed)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, balance, s.usdlBalance(s.bob))
	require.Equal(t, allowance, s.usdlAllowance(s.bob))
	require.Equal(t, before, s.exported())

	// The caller a surface appends is trusted: B buying through the Router
	// surface spends B's USDL and B's allowance to 0x1017.
	ret, err := s.call(s.bob, router, buy)
	require.NoError(t, err)
	out := s.decode("Router", "buy", ret)
	require.Len(t, out, 1)
	require.Positive(t, out[0].(*big.Int).Sign())
	require.Equal(t, big.NewInt(bobFunds-1_000_000), s.usdlBalance(s.bob))
	require.Equal(t, big.NewInt(bobFunds-1_000_000), s.usdlAllowance(s.bob))
}

func TestRoleAndGenerationSelection(t *testing.T) {
	s := newSurfaceHarness(t)
	byCreator := []common.Address{s.pools[0], s.pools[1], s.pools[2]}

	input := s.encode("PoolRegistry", "getPoolsByCreator", s.creator)
	ret, err := s.call(s.alice, surfaceAt(types.RolePoolRegistry), input)
	require.NoError(t, err)
	require.Equal(t, []interface{}{byCreator}, s.decode("PoolRegistry", "getPoolsByCreator", ret))

	ret, err = s.call(s.alice, precompile, input)
	require.NoError(t, err)
	require.Equal(t, []interface{}{byCreator}, s.decode("Quoter", "getPoolsByCreator", ret))

	ret, err = s.call(s.alice, generation1PoolRegistry, input)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, retiredRevertData(surfaceAt(types.RolePoolRegistry)), ret)

	// One pool selector resolves to the market of the pool surface called,
	// migrated generation 1 and 2 pools and the new pool alike.
	tokenAddress := s.encode("SidioraPool", "tokenAddress")
	for i, pool := range s.pools {
		ret, err := s.call(s.alice, pool, tokenAddress)
		require.NoError(t, err, pool.Hex())
		require.Equal(t, []interface{}{s.tokens[i]}, s.decode("SidioraPool", "tokenAddress", ret), pool.Hex())
	}

	// A pool selector is not part of the Router role nor of the direct ABI.
	for _, to := range []common.Address{surfaceAt(types.RoleRouter), precompile} {
		ret, err := s.call(s.alice, to, tokenAddress)
		require.ErrorIs(t, err, vm.ErrExecutionReverted, to.Hex())
		require.Empty(t, ret, to.Hex())
	}

	// The generation 2 role surfaces serve every generation: the registry
	// answers isRegisteredPool for the migrated generation 1 pool.
	isRegistered := s.encode("PoolRegistry", "isRegisteredPool", s.pools[0])
	ret, err = s.call(s.alice, surfaceAt(types.RolePoolRegistry), isRegistered)
	require.NoError(t, err)
	require.Equal(t, []interface{}{true}, s.decode("PoolRegistry", "isRegisteredPool", ret))
}

func TestSurfaceCallKindRefusals(t *testing.T) {
	s := newSurfaceHarness(t)
	getPoolCount := s.encode("PoolRegistry", "getPoolCount")

	// A contract that delegatecalls 0x1017 with its calldata and returns or
	// reverts with the callee data.
	delegator := named("delegator")
	testkeeper.EVMTestApp.EvmKeeper.SetCode(s.ctx(), delegator, common.FromHex(
		"0x363d3d373d3d363d6110175af43d600060003e601a573d6000fd5b3d6000f3"))
	_, err := s.call(s.alice, delegator, getPoolCount)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Len(t, s.frames, 2)
	require.Equal(t, vm.DELEGATECALL, s.frames[1].op)
	require.Equal(t, precompile, s.frames[1].to)

	ret, err := s.call(s.alice, precompile, getPoolCount)
	require.NoError(t, err)
	require.Equal(t, []interface{}{big.NewInt(3)}, s.decode("PoolRegistry", "getPoolCount", ret))

	// A write under staticcall through the Router surface reverts and changes
	// nothing; a view under staticcall is served.
	before := s.exported()
	balance, allowance := s.usdlBalance(s.bob), s.usdlAllowance(s.bob)
	buy := s.encode("Router", "buy", s.pools[2], big.NewInt(1_000_000), big.NewInt(0), deadline)
	_, err = s.staticCall(s.bob, surfaceAt(types.RoleRouter), buy)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, before, s.exported())
	require.Equal(t, balance, s.usdlBalance(s.bob))
	require.Equal(t, allowance, s.usdlAllowance(s.bob))

	ret, err = s.staticCall(s.alice, surfaceAt(types.RolePoolRegistry), getPoolCount)
	require.NoError(t, err)
	require.Equal(t, []interface{}{big.NewInt(3)}, s.decode("PoolRegistry", "getPoolCount", ret))

	// Value attached to any call reverts, upgradeToAndCall included.
	upgrade := s.encode("Router", "upgradeToAndCall", named("implementation"), []byte{})
	for _, input := range [][]byte{getPoolCount, buy, upgrade} {
		_, err := s.contract.Run(s.evm, s.bob, s.bob, input, big.NewInt(1), false, false, nil)
		require.ErrorIs(t, err, vm.ErrExecutionReverted)
	}
	require.Equal(t, before, s.exported())
}

func TestUnknownSelectorAndSurfaceGenesis(t *testing.T) {
	s := newSurfaceHarness(t)
	unknown := []byte{0xde, 0xad, 0xbe, 0xef}
	for _, to := range []common.Address{s.pools[0], s.pools[2], surfaceAt(types.RoleRouter),
		surfaceAt(types.RoleTreasury), precompile} {
		ret, err := s.call(s.alice, to, unknown)
		require.ErrorIs(t, err, vm.ErrExecutionReverted, to.Hex())
		require.Empty(t, ret, to.Hex())
	}

	exported := s.keeper.ExportGenesis(s.ctx())
	require.Len(t, exported.Surfaces, 12)
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockTime(genesisTime)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	fresh, ctx := launchpadtest.NewKeeper(app, ctx)
	fresh.InitGenesis(ctx, *exported)
	for _, entry := range exported.Surfaces {
		address := common.HexToAddress(entry.Address)
		want, found, err := s.keeper.GetSurface(s.ctx(), address)
		require.NoError(t, err)
		require.True(t, found, entry.Address)
		got, found, err := fresh.GetSurface(ctx, address)
		require.NoError(t, err)
		require.True(t, found, entry.Address)
		require.Equal(t, want, got, entry.Address)
	}
	require.Equal(t, exported.Surfaces, fresh.ExportGenesis(ctx).Surfaces)
}
