package launchpad_test

import (
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/launchpad"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const (
	feeTestSetupHeight int64  = 10
	feeTestRunHeight   int64  = 12
	feeTestTokenSupply int64  = 1_000_000
	feeTestPoolTokens  int64  = 400_000
	feeTestFeeHeld     int64  = 100_000
	feeTestFee         int64  = 10_000
	feeTestProtocolCut int64  = 1_000
	feeTestPoolCut     int64  = 9_000
	feeTestSurplus     int64  = 500
	feeTestCallGas     uint64 = 30_000_000
)

var (
	feeTestPoolRole         = [32]byte(crypto.Keccak256Hash([]byte("POOL_ROLE")))
	feeTestFactoryRole      = [32]byte(crypto.Keccak256Hash([]byte("FACTORY_ROLE")))
	feeTestOpticalClaimRole = [32]byte(crypto.Keccak256Hash([]byte("OPTICAL_CLAIM_ROLE")))
	feeTestGranterRole      = [32]byte(crypto.Keccak256Hash([]byte("OPTICAL_GRANTER_ROLE")))
)

// feeTestEnv is a generation 2 market at a pool surface over the real
// launchpad keeper and EVM: USDL and the checkpointed pool token deployed as
// SidioraERC20 contracts, fee right 1 of the generation 2 collection, the
// migrated FeeAccumulator role table and the surface runtime at the pool,
// FeesRouter and FeeAccumulator addresses.
type feeTestEnv struct {
	t        *testing.T
	k        *launchpadkeeper.Keeper
	stateDB  *state.DBImpl
	evm      *vm.EVM
	abis     map[string]abi.ABI
	roles    launchpadkeeper.RoleAddresses
	denom    string
	usdl     common.Address
	token    common.Address
	pool     common.Address
	owner    common.Address
	holder   common.Address
	factory  common.Address
	granter  common.Address
	outsider common.Address
	served   map[string]bool
}

func feeTestAddress(name string) common.Address { return poolAddress(name) }

func newFeeTestEnv(t *testing.T) *feeTestEnv {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockHeight(feeTestSetupHeight).WithBlockTime(genesisTime)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	k, ctx := launchpadtest.NewKeeper(app, ctx)
	k.InitGenesis(ctx, *types.DefaultGenesis())
	abis := map[string]abi.ABI{}
	for _, role := range []string{types.RoleFeesRouter, types.RoleFeeAccumulator, types.RoleTreasury,
		types.RoleFeeRights, "SidioraERC20"} {
		abis[role] = readABI(t, "abi/"+role+".json")
	}
	e := &feeTestEnv{t: t, k: k, abis: abis, roles: k.Generation2(), denom: "factory/launchpad/fees",
		pool: feeTestAddress("fee pool P"), owner: feeTestAddress("fee right owner O"),
		holder: feeTestAddress("token holder T"), factory: feeTestAddress("factory member F"),
		granter: feeTestAddress("optical granter G"), outsider: feeTestAddress("outsider X"), served: map[string]bool{}}

	usdlHolder := feeTestAddress("usdl holder U")
	e.usdl = e.deploy(ctx, "USD Ledger", "USDL", 1_000_000_000, usdlHolder)
	params := k.GetParams(ctx)
	params.QuoteToken = e.usdl.Hex()
	require.NoError(t, k.UpdateParams(ctx, k.Authority(), params))
	require.Equal(t, uint64(1_000), k.GetParams(ctx).ProtocolFeeBps)
	e.token = e.deploy(ctx, "Fee Token", "FEE", feeTestTokenSupply, e.holder)
	e.callEVM(ctx, e.holder, e.token, "transfer", e.pool, big.NewInt(feeTestPoolTokens))
	for _, surface := range []common.Address{e.pool, e.roles.FeesRouter, e.roles.FeeAccumulator} {
		app.EvmKeeper.SetCode(ctx, surface, surfaceRuntime)
	}

	guardianAcc, _ := testkeeper.MockAddressPair()
	snapshots := make([]sdk.Int, types.SnapshotSlots)
	for i := range snapshots {
		snapshots[i] = sdk.ZeroInt()
	}
	market := types.Market{
		Denom: e.denom, Index: 1, Name: "Fee Token", Symbol: "FEE", Pointer: e.token.Hex(),
		Creator: guardianAcc.String(), Guardian: guardianAcc.String(), FeeRightsHolder: guardianAcc.String(),
		FeeStrategy: types.FeeStrategyClaim, TotalSupply: sdk.NewInt(feeTestTokenSupply),
		VirtualQuoteReserve: sdk.NewInt(10_000_000_000), RealQuoteBalance: sdk.ZeroInt(),
		TokenReserve: sdk.NewInt(feeTestPoolTokens), CreationTime: genesisTime.Add(-time.Hour).Unix(),
		CumulativeVolume: sdk.ZeroInt(), PriceSnapshots: snapshots, SnapshotHeights: make([]uint64, types.SnapshotSlots),
		AccumulatedQuoteFees: sdk.ZeroInt(), AccumulatedTokenFees: sdk.ZeroInt(), AccumulatedFees: sdk.ZeroInt(),
		AirdropBalance: sdk.ZeroInt(), Generation: types.Generation2, PoolAddress: e.pool.Hex(),
		TokenAddress: e.token.Hex(), Optical: common.Address{}.Hex(), Factory: e.roles.SidioraFactory.Hex(),
		FeeAccumulator: e.roles.FeeAccumulator.Hex(), ProtocolConfig: e.roles.ProtocolConfig.Hex(),
		EventEmitter: e.roles.EventEmitter.Hex(), CumulativeBuyQuoteVolume: sdk.ZeroInt(),
		CumulativeSellQuoteVolume: sdk.ZeroInt(), FeeRightCollection: e.roles.SidioraNFT.Hex(), FeeRightID: 1,
	}
	accumulator := e.roles.FeeAccumulator.Hex()
	gs := types.DefaultGenesis()
	gs.Params = k.GetParams(ctx)
	gs.Markets = []types.Market{market}
	gs.Surfaces = []types.Surface{{Address: e.pool.Hex(), Role: types.RolePool},
		{Address: accumulator, Role: types.RoleFeeAccumulator},
		{Address: e.roles.FeesRouter.Hex(), Role: types.RoleFeesRouter}}
	gs.RoleMembers = []types.RoleMember{
		{Contract: accumulator, Role: common.Hash(feeTestPoolRole).Hex(), Account: e.pool.Hex()},
		{Contract: accumulator, Role: common.Hash(launchpadkeeper.FeesRouterRole).Hex(), Account: e.roles.FeesRouter.Hex()},
		{Contract: accumulator, Role: common.Hash(feeTestFactoryRole).Hex(), Account: e.factory.Hex()},
		{Contract: accumulator, Role: common.Hash(feeTestGranterRole).Hex(), Account: e.granter.Hex()},
	}
	require.NoError(t, gs.Validate())
	k.InitGenesis(ctx, *gs)
	id, err := k.MintFeeRight(ctx, e.roles.SidioraNFT, e.owner, e.pool, types.FeeStrategyClaim)
	require.NoError(t, err)
	require.Equal(t, uint64(1), id)

	e.callEVM(ctx, usdlHolder, e.usdl, "approve", common.HexToAddress(types.LaunchpadAddress), big.NewInt(feeTestFeeHeld))
	ctx, received, err := k.PullQuote(ctx, k.ModuleCaller(), usdlHolder,
		launchpadkeeper.FeeHeldLedger(e.roles.FeeAccumulator), sdk.NewInt(feeTestFeeHeld))
	require.NoError(t, err)
	require.Equal(t, sdk.NewInt(feeTestFeeHeld).String(), received.String())

	pre, err := launchpad.NewPrecompileWithKeeper(k, &app.EvmKeeper)
	require.NoError(t, err)
	ctx = ctx.WithBlockHeight(feeTestRunHeight)
	e.stateDB = state.NewDBImpl(ctx, &app.EvmKeeper, false)
	blockCtx, err := app.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
	require.NoError(t, err)
	precompiles := map[common.Address]vm.PrecompiledContract{}
	for address, contract := range app.EvmKeeper.CustomPrecompiles(ctx) {
		precompiles[address] = contract
	}
	precompiles[common.HexToAddress(types.LaunchpadAddress)] = pre
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(app.EvmKeeper.ChainID(ctx))
	e.evm = vm.NewEVM(*blockCtx, e.stateDB, cfg, vm.Config{}, precompiles)
	return e
}

func (e *feeTestEnv) deploy(ctx sdk.Context, name, symbol string, supply int64, holder common.Address) common.Address {
	e.t.Helper()
	evm := &testkeeper.EVMTestApp.EvmKeeper
	args, err := e.abis["SidioraERC20"].Constructor.Inputs.Pack(name, symbol, big.NewInt(supply), holder)
	require.NoError(e.t, err)
	deployer := feeTestAddress("deployer of " + symbol)
	nonce := evm.GetNonce(ctx, deployer)
	_, err = evm.CallEVM(ctx, deployer, nil, nil, append(common.FromHex(sidioraERC20Creation), args...))
	require.NoError(e.t, err)
	contract := crypto.CreateAddress(deployer, nonce)
	require.NotEmpty(e.t, evm.GetCode(ctx, contract))
	return contract
}

func (e *feeTestEnv) callEVM(ctx sdk.Context, from, to common.Address, name string, args ...interface{}) {
	e.t.Helper()
	input, err := e.abis["SidioraERC20"].Pack(name, args...)
	require.NoError(e.t, err)
	_, err = testkeeper.EVMTestApp.EvmKeeper.CallEVM(ctx, from, &to, nil, input)
	require.NoError(e.t, err)
}

func (e *feeTestEnv) balance(token, account common.Address) int64 {
	e.t.Helper()
	input, err := e.abis["SidioraERC20"].Pack("balanceOf", account)
	require.NoError(e.t, err)
	ret, _, err := e.evm.StaticCall(account, token, input, feeTestCallGas)
	require.NoError(e.t, err)
	return new(big.Int).SetBytes(ret).Int64()
}

func (e *feeTestEnv) surface(role string) common.Address {
	if role == types.RoleFeesRouter {
		return e.roles.FeesRouter
	}
	return e.roles.FeeAccumulator
}

func (e *feeTestEnv) input(role, name string, args ...interface{}) []byte {
	e.t.Helper()
	method, ok := e.abis[role].Methods[name]
	require.True(e.t, ok, "%s.%s", role, name)
	e.served[role+"."+name] = true
	packed, err := method.Inputs.Pack(args...)
	require.NoError(e.t, err, name)
	return append(common.CopyBytes(method.ID), packed...)
}

// send calls name at the role's surface from caller and returns the decoded
// outputs, or the revert data when the call reverts.
func (e *feeTestEnv) send(role string, caller common.Address, name string, args ...interface{}) ([]interface{}, []byte) {
	e.t.Helper()
	ret, _, err := e.evm.Call(caller, e.surface(role), e.input(role, name, args...), feeTestCallGas, uint256.NewInt(0))
	if err != nil {
		require.ErrorIs(e.t, err, vm.ErrExecutionReverted, "%s.%s", role, name)
		return nil, ret
	}
	out, err := e.abis[role].Methods[name].Outputs.Unpack(ret)
	require.NoError(e.t, err, name)
	return out, nil
}

func (e *feeTestEnv) write(role string, caller common.Address, name string, args ...interface{}) []interface{} {
	e.t.Helper()
	out, revert := e.send(role, caller, name, args...)
	require.Nil(e.t, revert, "%s.%s reverted with %x", role, name, revert)
	return out
}

// view staticcalls name at the role's surface and returns its one output.
func (e *feeTestEnv) view(role, name string, args ...interface{}) interface{} {
	e.t.Helper()
	ret, _, err := e.evm.StaticCall(e.outsider, e.surface(role), e.input(role, name, args...), feeTestCallGas)
	require.NoError(e.t, err, "%s.%s reverted with %x", role, name, ret)
	out, err := e.abis[role].Methods[name].Outputs.Unpack(ret)
	require.NoError(e.t, err, name)
	require.Len(e.t, out, 1, name)
	return out[0]
}

// refused requires the call to revert with the Launch-x custom error of want
// built from values.
func (e *feeTestEnv) refused(want *sdkerrors.Error, role string, caller common.Address, name string,
	args ...interface{}) {
	e.t.Helper()
	e.refusedWith(want, nil, role, caller, name, args...)
}

func (e *feeTestEnv) refusedWith(want *sdkerrors.Error, values []interface{}, role string, caller common.Address,
	name string, args ...interface{}) {
	e.t.Helper()
	contractError, ok := types.ContractErrorOf(want)
	require.True(e.t, ok, want.Error())
	data, err := contractError.Revert(values...)
	require.NoError(e.t, err)
	out, revert := e.send(role, caller, name, args...)
	require.Nil(e.t, out, "%s.%s", role, name)
	require.Equal(e.t, data, revert, "%s.%s refused with %x", role, name, revert)
}

// roleRefused requires the role refusal want(account, role).
func (e *feeTestEnv) roleRefused(want *sdkerrors.Error, account common.Address, role [32]byte, contract string,
	caller common.Address, name string, args ...interface{}) {
	e.t.Helper()
	e.refusedWith(want, []interface{}{account, role}, contract, caller, name, args...)
}

func (e *feeTestEnv) logCount() int { return len(e.stateDB.GetAllLogs()) }

func (e *feeTestEnv) logsSince(from int) []*ethtypes.Log { return e.stateDB.GetAllLogs()[from:] }

// requireLog requires log to be event name of role at address with exactly
// the indexed and data arguments args.
func (e *feeTestEnv) requireLog(log *ethtypes.Log, address common.Address, role, name string, args ...interface{}) {
	e.t.Helper()
	event, ok := e.abis[role].Events[name]
	require.True(e.t, ok, "%s.%s", role, name)
	require.Equal(e.t, address, log.Address, name)
	require.Equal(e.t, event.ID, log.Topics[0], name)
	require.Len(e.t, args, len(event.Inputs), name)
	topic, data := 1, []interface{}{}
	for i, input := range event.Inputs {
		if !input.Indexed {
			data = append(data, args[i])
			continue
		}
		topics, err := abi.MakeTopics([]interface{}{args[i]})
		require.NoError(e.t, err, name)
		require.Equal(e.t, topics[0][0], log.Topics[topic], "%s topic %d", name, topic)
		topic++
	}
	require.Len(e.t, log.Topics, topic, name)
	packed, err := event.Inputs.NonIndexed().Pack(data...)
	require.NoError(e.t, err, name)
	require.Equal(e.t, packed, log.Data, name)
}

// initializeArgs is an initialize argument list of role naming admin in
// every address slot.
func (e *feeTestEnv) initializeArgs(role string, admin common.Address) []interface{} {
	args := make([]interface{}, len(e.abis[role].Methods["initialize"].Inputs))
	for i := range args {
		args[i] = admin
	}
	return args
}

func TestFeeRolesServeEverySelector(t *testing.T) {
	e := newFeeTestEnv(t)
	fr, fa := types.RoleFeesRouter, types.RoleFeeAccumulator
	accumulator, router := e.roles.FeeAccumulator, e.roles.FeesRouter
	admin := launchpadkeeper.DefaultAdminRole
	one := n(1)
	beyond := new(big.Int).Lsh(one, 64)

	// Constants, configuration and the AccessControl views of both roles.
	require.Equal(t, common.HexToAddress(types.DeadAddress), e.view(fa, "DEAD_ADDRESS"))
	require.Equal(t, [32]byte(crypto.Keccak256Hash([]byte("POOL_ROLE"))), e.view(fa, "POOL_ROLE"))
	require.Equal(t, [32]byte(crypto.Keccak256Hash([]byte("FEES_ROUTER_ROLE"))), e.view(fa, "FEES_ROUTER_ROLE"))
	require.Equal(t, [32]byte(crypto.Keccak256Hash([]byte("FACTORY_ROLE"))), e.view(fa, "FACTORY_ROLE"))
	require.Equal(t, [32]byte(crypto.Keccak256Hash([]byte("OPTICAL_CLAIM_ROLE"))), e.view(fa, "OPTICAL_CLAIM_ROLE"))
	require.Equal(t, [32]byte(crypto.Keccak256Hash([]byte("OPTICAL_GRANTER_ROLE"))), e.view(fa, "OPTICAL_GRANTER_ROLE"))
	require.Equal(t, e.roles.ProtocolConfig, e.view(fa, "protocolConfig"))
	require.Equal(t, e.roles.Treasury, e.view(fa, "treasury"))
	require.Equal(t, e.roles.PoolRegistry, e.view(fa, "poolRegistry"))
	require.Equal(t, e.roles.EventEmitter, e.view(fa, "eventEmitter"))
	require.Equal(t, e.usdl, e.view(fa, "usdlAddress"))
	require.Equal(t, e.roles.SidioraNFT, e.view(fr, "nftContract"))
	require.Equal(t, accumulator, e.view(fr, "feeAccumulator"))
	require.Equal(t, e.roles.PoolRegistry, e.view(fr, "poolRegistry"))
	for _, role := range []string{fr, fa} {
		require.Equal(t, admin, e.view(role, "DEFAULT_ADMIN_ROLE"))
		require.Equal(t, admin, e.view(role, "getRoleAdmin", feeTestPoolRole))
		same(t, n(0), e.view(role, "registeredDefaultAdminCount"))
		require.Equal(t, false, e.view(role, "hasRole", admin, e.owner))
		e.refused(types.ErrAlreadyInitialized, role, e.owner, "initialize", e.initializeArgs(role, e.owner)...)
		e.refused(types.ErrUUPSNotThroughActiveProxy, role, e.owner, "proxiableUUID")
		e.roleRefused(types.ErrAccessControlUnauthorizedAccount, e.owner, admin, role, e.owner,
			"grantRole", feeTestPoolRole, e.outsider)
		e.roleRefused(types.ErrAccessControlUnauthorizedAccount, e.owner, admin, role, e.owner,
			"revokeRole", feeTestPoolRole, e.pool)
		e.roleRefused(types.ErrAccessControlUnauthorizedAccount, e.owner, admin, role, e.owner,
			"upgradeToAndCall", e.outsider, []byte{})
		e.roleRefused(types.ErrMissingRole, e.owner, feeTestPoolRole, role, e.owner,
			"renounceRole", feeTestPoolRole, e.outsider)
	}
	require.Equal(t, true, e.view(fa, "hasRole", feeTestPoolRole, e.pool))
	require.Equal(t, true, e.view(fa, "hasRole", launchpadkeeper.FeesRouterRole, router))
	require.Equal(t, false, e.view(fr, "hasRole", feeTestPoolRole, e.pool))
	require.Equal(t, true, e.view(fa, "supportsAirdrop", e.pool))
	require.Equal(t, false, e.view(fa, "supportsAirdrop", e.outsider))

	// The DEFAULT_ADMIN_ROLE writes of FeeAccumulator refuse every caller.
	for _, caller := range []common.Address{e.owner, e.factory, router} {
		e.roleRefused(types.ErrAccessControlUnauthorizedAccount, caller, admin, fa, caller,
			"allocateLegacyAirdrop", e.pool, one, [32]byte{1}, one)
		e.roleRefused(types.ErrAccessControlUnauthorizedAccount, caller, admin, fa, caller,
			"migrateAccounting", one)
		e.roleRefused(types.ErrAccessControlUnauthorizedAccount, caller, admin, fa, caller,
			"migrateAdditionalAccounting", []common.Address{e.pool})
	}

	// Callers outside the migrated role table revert MissingRole(account, role).
	for _, gated := range []struct {
		name string
		role [32]byte
		args []interface{}
	}{
		{"recordFee", feeTestPoolRole, []interface{}{e.pool, n(feeTestFee)}},
		{"recordOpticalSurplus", feeTestPoolRole, []interface{}{e.pool, n(feeTestSurplus)}},
		{"claim", launchpadkeeper.FeesRouterRole, []interface{}{e.pool, e.owner}},
		{"burn", launchpadkeeper.FeesRouterRole, []interface{}{e.pool}},
		{"triggerAirdrop", launchpadkeeper.FeesRouterRole, []interface{}{e.pool}},
		{"sendLpRewards", launchpadkeeper.FeesRouterRole, []interface{}{e.pool}},
		{"authorizePool", feeTestFactoryRole, []interface{}{e.outsider}},
		{"authorizeOptical", feeTestGranterRole, []interface{}{e.outsider}},
	} {
		e.roleRefused(types.ErrMissingRole, e.outsider, gated.role, fa, e.outsider, gated.name,
			gated.args...)
	}

	// The role check runs before the FeeAccumulator lock.
	release, err := e.k.EnterLock(e.stateDB, launchpadkeeper.FeeAccumulatorLock)
	require.NoError(t, err)
	e.roleRefused(types.ErrMissingRole, e.outsider, feeTestPoolRole, fa, e.outsider, "recordFee",
		e.pool, n(feeTestFee))
	e.refused(types.ErrReentrancyGuardReentrantCall, fa, e.pool, "recordFee", e.pool, n(feeTestFee))
	same(t, n(0), e.view(fa, "getAccumulatedFees", e.pool))
	release()

	// recordFee: only the registered pool records its own fees.
	e.refused(types.ErrUnauthorized, fa, e.pool, "recordFee", e.outsider, n(feeTestFee))
	e.refused(types.ErrZeroAmount, fa, e.pool, "recordFee", e.pool, n(0))
	start := e.logCount()
	require.Empty(t, e.write(fa, e.pool, "recordFee", e.pool, n(feeTestFee)))
	logs := e.logsSince(start)
	require.Len(t, logs, 2)
	e.requireLog(logs[0], e.roles.Treasury, types.RoleTreasury, "Deposited", e.usdl, accumulator, n(feeTestProtocolCut))
	e.requireLog(logs[1], accumulator, fa, "FeeRecorded", e.pool, n(feeTestFee), n(feeTestProtocolCut),
		n(feeTestPoolCut))
	require.Equal(t, n(feeTestPoolCut), e.view(fa, "getAccumulatedFees", e.pool))
	require.Equal(t, n(feeTestPoolCut), e.view(fa, "totalLiabilities"))

	// recordOpticalSurplus: zero records nothing; a positive amount becomes
	// the pool's surplus, which only its optical may claim.
	start = e.logCount()
	require.Empty(t, e.write(fa, e.pool, "recordOpticalSurplus", e.pool, n(0)))
	require.Equal(t, start, e.logCount())
	require.Empty(t, e.write(fa, e.pool, "recordOpticalSurplus", e.pool, n(feeTestSurplus)))
	logs = e.logsSince(start)
	require.Len(t, logs, 1)
	e.requireLog(logs[0], accumulator, fa, "OpticalSurplusRecorded", e.pool, n(feeTestSurplus))
	require.Equal(t, n(feeTestSurplus), e.view(fa, "getOpticalSurplus", e.pool))
	require.Equal(t, n(feeTestPoolCut+feeTestSurplus), e.view(fa, "totalLiabilities"))
	e.refused(types.ErrUnauthorized, fa, e.outsider, "claimOpticalSurplus", e.pool, one, e.outsider)
	e.refused(types.ErrUnauthorized, fa, e.outsider, "claimOpticalSurplus", e.outsider, one, e.outsider)

	// The FEES_ROUTER_ROLE transfers have no pool-keyed keeper entry.
	e.refused(types.ErrInvalidConfiguration, fa, router, "claim", e.pool, e.owner)
	e.refused(types.ErrInvalidConfiguration, fa, router, "burn", e.pool)
	e.refused(types.ErrInvalidConfiguration, fa, router, "sendLpRewards", e.pool)
	for _, name := range []string{"accountingInitialized", "accountingMigrationCursor", "getProtocolFeesPending"} {
		e.refused(types.ErrInvalidConfiguration, fa, e.outsider, name)
	}
	e.refused(types.ErrInvalidConfiguration, fa, e.outsider, "getLpRewardsBalance", e.pool)
	e.refused(types.ErrInvalidConfiguration, fa, e.outsider, "legacyAirdropUnallocated", e.pool)

	// claimFees: the owner of fee right 1 under CLAIM takes the pool cut.
	e.refused(types.ErrNotFeeRightsHolder, fr, e.outsider, "claimFees", one)
	e.refused(types.ErrNotFeeRightsHolder, fr, e.owner, "claimFees", n(2))
	e.refused(types.ErrNotFeeRightsHolder, fr, e.owner, "claimFees", beyond)
	e.refused(types.ErrWrongStrategy, fr, e.owner, "executeBurn", one)
	e.refused(types.ErrWrongStrategy, fr, e.owner, "executeAirdrop", one)
	e.refused(types.ErrWrongStrategy, fr, e.owner, "executeLpRewards", one)
	release, err = e.k.EnterLock(e.stateDB, launchpadkeeper.FeesRouterLock)
	require.NoError(t, err)
	e.refused(types.ErrReentrancyGuardReentrantCall, fr, e.owner, "claimFees", one)
	release()
	start = e.logCount()
	require.Equal(t, []interface{}{n(feeTestPoolCut)}, e.write(fr, e.owner, "claimFees", one))
	logs = e.logsSince(start)
	require.GreaterOrEqual(t, len(logs), 2)
	e.requireLog(logs[len(logs)-2], accumulator, fa, "FeesClaimed", e.pool, e.owner, n(feeTestPoolCut))
	e.requireLog(logs[len(logs)-1], router, fr, "FeesClaimed", one, e.owner, n(feeTestPoolCut))
	require.Equal(t, feeTestPoolCut, e.balance(e.usdl, e.owner))
	same(t, n(0), e.view(fa, "getAccumulatedFees", e.pool))
	require.Equal(t, n(feeTestSurplus), e.view(fa, "totalLiabilities"))
	e.refused(types.ErrNoFeesAccumulated, fr, e.owner, "claimFees", one)

	// setFeeStrategy: the owner moves fee right 1 between strategies.
	e.refused(types.ErrNotFeeRightsHolder, fr, e.outsider, "setFeeStrategy", one, uint8(types.FeeStrategyBurn))
	e.refused(types.ErrInvalidStrategy, fr, e.owner, "setFeeStrategy", one, uint8(4))
	e.setStrategy(types.FeeStrategyClaim, types.FeeStrategyBurn)

	// executeBurn: the pool cut goes to 0x…dEaD.
	e.write(fa, e.pool, "recordFee", e.pool, n(feeTestFee))
	start = e.logCount()
	require.Equal(t, []interface{}{n(feeTestPoolCut)}, e.write(fr, e.owner, "executeBurn", one))
	logs = e.logsSince(start)
	require.GreaterOrEqual(t, len(logs), 2)
	e.requireLog(logs[len(logs)-2], accumulator, fa, "FeesBurned", e.pool, n(feeTestPoolCut))
	e.requireLog(logs[len(logs)-1], router, fr, "FeesBurned", one, n(feeTestPoolCut))
	require.Equal(t, feeTestPoolCut, e.balance(e.usdl, common.HexToAddress(types.DeadAddress)))
	e.refused(types.ErrNoFeesAccumulated, fr, e.owner, "executeBurn", one)

	// executeLpRewards: the pool cut joins the curve's real quote balance.
	e.setStrategy(types.FeeStrategyBurn, types.FeeStrategyLpRewards)
	e.write(fa, e.pool, "recordFee", e.pool, n(feeTestFee))
	start = e.logCount()
	require.Equal(t, []interface{}{n(feeTestPoolCut)}, e.write(fr, e.owner, "executeLpRewards", one))
	logs = e.logsSince(start)
	require.Len(t, logs, 2)
	e.requireLog(logs[0], accumulator, fa, "LpRewardsSent", e.pool, n(feeTestPoolCut))
	e.requireLog(logs[1], router, fr, "LpRewardsExecuted", one, n(feeTestPoolCut))
	market, found := e.k.GetMarket(e.stateDB.Ctx(), e.denom)
	require.True(t, found)
	require.Equal(t, sdk.NewInt(feeTestPoolCut).String(), market.RealQuoteBalance.String())
	require.Equal(t, n(feeTestSurplus), e.view(fa, "totalLiabilities"))

	// executeAirdrop: the pool cut opens epoch 1 over the checkpointed supply
	// outside the pool at block.number-1.
	e.setStrategy(types.FeeStrategyLpRewards, types.FeeStrategyAirdrop)
	e.refused(types.ErrAirdropNotTriggered, fa, e.outsider, "claimAirdrop", e.pool, e.holder)
	e.write(fa, e.pool, "recordFee", e.pool, n(feeTestFee))
	start = e.logCount()
	require.Equal(t, []interface{}{n(feeTestPoolCut)}, e.write(fr, e.owner, "executeAirdrop", one))
	logs = e.logsSince(start)
	require.GreaterOrEqual(t, len(logs), 2)
	e.requireLog(logs[len(logs)-2], accumulator, fa, "AirdropTriggered", e.pool, n(feeTestPoolCut), one)
	e.requireLog(logs[len(logs)-1], router, fr, "AirdropExecuted", one, n(feeTestPoolCut))
	eligible := feeTestTokenSupply - feeTestPoolTokens
	require.Equal(t, one, e.view(fa, "getAirdropEpoch", e.pool))
	require.Equal(t, n(feeTestPoolCut), e.view(fa, "getAirdropBalance", e.pool))
	require.Equal(t, n(feeTestRunHeight-1), e.view(fa, "airdropSnapshotBlock", e.pool, one))
	require.Equal(t, n(eligible), e.view(fa, "airdropEligibleSupply", e.pool, one))
	require.Equal(t, n(feeTestPoolCut), e.view(fa, "airdropEpochRemaining", e.pool, one))
	same(t, n(0), e.view(fa, "airdropClaimedSupply", e.pool, one))
	require.Equal(t, [32]byte{}, e.view(fa, "legacyAirdropRoot", e.pool, one))
	same(t, n(0), e.view(fa, "legacyAirdropRemaining", e.pool, one))
	same(t, n(0), e.view(fa, "airdropEligibleSupply", e.pool, beyond))
	require.Equal(t, false, e.view(fa, "hasClaimedAirdrop", e.pool, e.holder, one))
	e.refused(types.ErrNoFeesAccumulated, fr, e.owner, "executeAirdrop", one)
	e.refused(types.ErrNoFeesAccumulated, fa, router, "triggerAirdrop", e.pool)

	// claimAirdrop through FeesRouter: any holder claims for themselves; the
	// only eligible holder takes the whole epoch.
	e.refused(types.ErrTokenNotFound, fr, e.holder, "claimAirdrop", n(2))
	e.refused(types.ErrTokenNotFound, fr, e.holder, "claimAirdropEpoch", beyond, one)
	e.refused(types.ErrAirdropNotTriggered, fr, e.holder, "claimAirdropEpoch", one, n(2))
	e.refused(types.ErrAirdropNotTriggered, fr, e.holder, "claimAirdropEpoch", one, beyond)
	e.refused(types.ErrUnauthorized, fa, e.outsider, "claimAirdropEpoch", e.pool, e.pool, one)
	start = e.logCount()
	require.Equal(t, []interface{}{n(feeTestPoolCut)}, e.write(fr, e.holder, "claimAirdrop", one))
	logs = e.logsSince(start)
	require.GreaterOrEqual(t, len(logs), 2)
	e.requireLog(logs[len(logs)-2], accumulator, fa, "AirdropClaimed", e.pool, e.holder, n(feeTestPoolCut), one)
	e.requireLog(logs[len(logs)-1], router, fr, "AirdropClaimed", one, e.holder, n(feeTestPoolCut))
	require.Equal(t, feeTestPoolCut, e.balance(e.usdl, e.holder))
	require.Equal(t, true, e.view(fa, "hasClaimedAirdrop", e.pool, e.holder, one))
	require.Equal(t, false, e.view(fa, "hasClaimedAirdrop", e.pool, e.holder, beyond))
	require.Equal(t, n(eligible), e.view(fa, "airdropClaimedSupply", e.pool, one))
	same(t, n(0), e.view(fa, "airdropEpochRemaining", e.pool, one))
	same(t, n(0), e.view(fa, "getAirdropBalance", e.pool))
	e.refused(types.ErrAlreadyClaimed, fr, e.holder, "claimAirdropEpoch", one, one)
	e.refused(types.ErrAlreadyClaimed, fa, e.outsider, "claimAirdrop", e.pool, e.holder)
	e.refused(types.ErrAlreadyClaimed, fa, e.outsider, "claimAirdropEpoch", e.pool, e.holder, one)
	e.refused(types.ErrAirdropNotTriggered, fa, e.outsider, "claimAirdropEpoch", e.pool, e.holder, n(2))
	e.refused(types.ErrAirdropNotTriggered, fa, e.outsider, "claimAirdropEpoch", e.pool, e.holder, beyond)

	// claimLegacyAirdrop: a claimed holder, a missing root and an epoch with
	// no root all refuse.
	e.refused(types.ErrAlreadyClaimed, fa, e.outsider, "claimLegacyAirdrop", e.pool, one, e.holder, one,
		[][32]byte{})
	e.refused(types.ErrUnauthorized, fa, e.outsider, "claimLegacyAirdrop", e.pool, one, e.outsider, one,
		[][32]byte{{7}})
	e.refused(types.ErrUnauthorized, fa, e.outsider, "claimLegacyAirdrop", e.pool, one, e.outsider, n(0),
		[][32]byte{})
	e.refused(types.ErrUnauthorized, fa, e.outsider, "claimLegacyAirdrop", e.pool, beyond, e.outsider, one,
		[][32]byte{})

	// authorizePool and authorizeOptical grant through the role table once;
	// renounceRole drops the caller's own role.
	newPool, optical := feeTestAddress("new pool N"), feeTestAddress("optical Q")
	start = e.logCount()
	require.Empty(t, e.write(fa, e.factory, "authorizePool", newPool))
	require.Empty(t, e.write(fa, e.factory, "authorizePool", newPool))
	require.Empty(t, e.write(fa, e.granter, "authorizeOptical", optical))
	logs = e.logsSince(start)
	require.Len(t, logs, 2)
	e.requireLog(logs[0], accumulator, fa, "RoleGranted", common.Hash(feeTestPoolRole), newPool, e.factory)
	e.requireLog(logs[1], accumulator, fa, "RoleGranted", common.Hash(feeTestOpticalClaimRole), optical, e.granter)
	require.Equal(t, true, e.view(fa, "hasRole", feeTestPoolRole, newPool))
	require.Equal(t, true, e.view(fa, "hasRole", feeTestOpticalClaimRole, optical))
	e.refused(types.ErrInvalidRoleAccount, fa, e.factory, "authorizePool", common.Address{})
	e.refused(types.ErrInvalidRoleAccount, fa, e.granter, "authorizeOptical", common.Address{})
	start = e.logCount()
	require.Empty(t, e.write(fa, newPool, "renounceRole", feeTestPoolRole, newPool))
	require.Empty(t, e.write(fa, newPool, "renounceRole", feeTestPoolRole, newPool))
	logs = e.logsSince(start)
	require.Len(t, logs, 1)
	e.requireLog(logs[0], accumulator, fa, "RoleRevoked", common.Hash(feeTestPoolRole), newPool, newPool)
	require.Equal(t, false, e.view(fa, "hasRole", feeTestPoolRole, newPool))
	require.Empty(t, e.write(fr, e.owner, "renounceRole", feeTestPoolRole, e.owner))

	// Every selector of both roles was served.
	for _, role := range []string{fr, fa} {
		for name := range e.abis[role].Methods {
			require.True(t, e.served[role+"."+name], "%s.%s was not served", role, name)
		}
	}
	require.Len(t, e.abis[fr].Methods, 20)
	require.Len(t, e.abis[fa].Methods, 54)
}

// setStrategy moves fee right 1 from old to next and checks the SidioraNFT and
// FeesRouter FeeStrategyChanged logs.
func (e *feeTestEnv) setStrategy(old, next types.FeeStrategy) {
	e.t.Helper()
	start := e.logCount()
	require.Empty(e.t, e.write(types.RoleFeesRouter, e.owner, "setFeeStrategy", big.NewInt(1), uint8(next)))
	logs := e.logsSince(start)
	require.Len(e.t, logs, 2)
	e.requireLog(logs[0], e.roles.SidioraNFT, types.RoleFeeRights, "FeeStrategyChanged", big.NewInt(1), uint8(old), uint8(next))
	e.requireLog(logs[1], e.roles.FeesRouter, types.RoleFeesRouter, "FeeStrategyChanged", big.NewInt(1), uint8(old),
		uint8(next))
	right, found, err := e.k.GetFeeRight(e.stateDB.Ctx(), e.roles.SidioraNFT, 1)
	require.NoError(e.t, err)
	require.True(e.t, found)
	require.Equal(e.t, next, right.Strategy)
	market, found := e.k.GetMarket(e.stateDB.Ctx(), e.denom)
	require.True(e.t, found)
	require.Equal(e.t, next, market.FeeStrategy)
}
