package launchpad

import (
	"errors"
	"fmt"
	"math/big"
	"testing"

	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	ibctransferkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/interchain/modules/apps/transfer/keeper"
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	launchpadkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	codectypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/keeper"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	paramtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/params/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

const runPoolAddress = "0xCE20Da50F92D21d8BeE497910c782E9Aa25b6263"

var (
	runRouter   = launchpadkeeper.Generation2Surfaces[types.RoleRouter]
	runRegistry = launchpadkeeper.Generation2Surfaces[types.RolePoolRegistry]
	runPool     = common.HexToAddress(runPoolAddress)
	runCaller   = common.HexToAddress("0x00000000000000000000000000000000000A11CE")
	runOriginal = common.HexToAddress("0x0000000000000000000000000000000000000B0B")
)

// runEnv is the launchpad executor over a real keeper and a real EVM keeper
// on an in-memory store, with the generation 2 Router and PoolRegistry
// surfaces and one pool surface registered.
type runEnv struct {
	t        *testing.T
	ctx      sdk.Context
	executor *PrecompileExecutor
	keeper   *launchpadkeeper.Keeper
	denom    string
}

func newRunEnv(t *testing.T) *runEnv {
	t.Helper()
	key := sdk.NewKVStoreKey(types.StoreKey)
	evmKey := sdk.NewKVStoreKey("run_evm")
	transient := sdk.NewTransientStoreKey("run_transient")
	db := dbm.NewMemDB()
	cms := store.NewCommitMultiStore(db)
	cms.MountStoreWithDB(key, sdk.StoreTypeIAVL, db)
	cms.MountStoreWithDB(evmKey, sdk.StoreTypeIAVL, db)
	cms.MountStoreWithDB(transient, sdk.StoreTypeTransient, db)
	require.NoError(t, cms.LoadLatestVersion())
	ctx := sdk.NewContext(cms, tmproto.Header{}, false)
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	subspace := func(name string) paramtypes.Subspace {
		return paramtypes.NewSubspace(cdc, codec.NewLegacyAmino(), evmKey, transient, name)
	}
	accounts := authkeeper.NewAccountKeeper(cdc, evmKey, subspace("run_auth"), authtypes.ProtoBaseAccount,
		map[string][]string{authtypes.FeeCollectorName: nil})
	evm := evmkeeper.NewKeeper(evmKey, transient, subspace("run_evm"), nil, nil, &accounts, nil,
		ibctransferkeeper.Keeper{}, nil, nil, nil)
	k := launchpadkeeper.NewKeeper(key, nil, nil, evm)

	holder := sdk.AccAddress(common.HexToAddress("0x00000000000000000000000000000000000C0FFE").Bytes())
	snapshots := make([]sdk.Int, types.SnapshotSlots)
	for n := range snapshots {
		snapshots[n] = sdk.ZeroInt()
	}
	market := types.Market{
		Denom: "factory/launchpad/run", Index: 1, Name: "Run Token", Symbol: "RUN",
		Pointer: "0x0000000000000000000000000000000000000AbC", Creator: holder.String(), Guardian: holder.String(),
		FeeRightsHolder: holder.String(), FeeStrategy: types.FeeStrategyBurn, TotalSupply: sdk.NewInt(1_000),
		VirtualQuoteReserve: sdk.NewInt(10), RealQuoteBalance: sdk.ZeroInt(), TokenReserve: sdk.NewInt(1_000),
		CumulativeVolume: sdk.ZeroInt(), PriceSnapshots: snapshots, AccumulatedQuoteFees: sdk.ZeroInt(),
		AccumulatedTokenFees: sdk.ZeroInt(), AccumulatedFees: sdk.ZeroInt(), AirdropBalance: sdk.ZeroInt(),
		Generation: types.Generation2, PoolAddress: runPoolAddress,
		TokenAddress: common.BytesToAddress([]byte{0x7b, 0xf2}).Hex(), Optical: common.Address{}.Hex(),
		Factory:                   launchpadkeeper.Generation2Surfaces[types.RoleFactory].Hex(),
		FeeAccumulator:            launchpadkeeper.Generation2Surfaces[types.RoleFeeAccumulator].Hex(),
		ProtocolConfig:            launchpadkeeper.Generation2Surfaces[types.RoleProtocolConfig].Hex(),
		EventEmitter:              "0x622E1bb90E0E2B6E6e5e9e2DbE1555389E8A3EF6",
		CumulativeBuyQuoteVolume:  sdk.ZeroInt(),
		CumulativeSellQuoteVolume: sdk.ZeroInt(),
		FeeRightCollection:        launchpadkeeper.Generation2Surfaces[types.RoleFeeRights].Hex(),
		FeeRightID:                1,
	}
	gs := types.DefaultGenesis()
	gs.Markets = []types.Market{market}
	require.NoError(t, gs.Validate())
	k.InitGenesis(ctx, *gs)
	require.NoError(t, k.SetSurface(ctx, runRouter,
		launchpadkeeper.Surface{Role: types.RoleRouter, Generation: types.Generation2}))
	require.NoError(t, k.SetSurface(ctx, runRegistry,
		launchpadkeeper.Surface{Role: types.RolePoolRegistry, Generation: types.Generation2}))
	require.NoError(t, k.SetSurface(ctx, runPool,
		launchpadkeeper.Surface{Role: types.RolePool, Generation: types.Generation2, Market: market.Denom}))

	p, err := NewPrecompileWithKeeper(k, nil)
	require.NoError(t, err)
	return &runEnv{t: t, ctx: ctx, executor: p.executor, keeper: k, denom: market.Denom}
}

func roleMethod(t *testing.T, role, name string) *abi.Method {
	t.Helper()
	method, ok := roleABIs[role].Methods[name]
	require.True(t, ok, "%s.%s", role, name)
	return &method
}

func declaredBy(role, sig string) bool {
	for _, method := range roleABIs[role].Methods {
		if method.Sig == sig {
			return true
		}
	}
	return false
}

func calldata(t *testing.T, method *abi.Method, args ...interface{}) []byte {
	t.Helper()
	packed, err := method.Inputs.Pack(args...)
	require.NoError(t, err)
	return append(append([]byte(nil), method.ID...), packed...)
}

func withSuffix(input []byte, original common.Address) []byte {
	return append(append([]byte(nil), input...), original.Bytes()...)
}

// refusal resolves input sent by caller and serves it with value under the
// call kind of ctx and readOnly; every case reaches a refusal before any
// role handler, and the reason is the revert reason Run returns.
func (e *runEnv) refusal(ctx sdk.Context, caller common.Address, input []byte, value *big.Int, readOnly bool) string {
	e.t.Helper()
	c, err := e.executor.resolve(ctx, caller, input)
	require.NoError(e.t, err)
	c.readOnly = readOnly
	_, err = e.executor.serve(c, value)
	require.Error(e.t, err)
	reason, err := abi.UnpackRevert(runRevert(err))
	require.NoError(e.t, err)
	return reason
}

// emptyRevert asserts input sent by caller resolves to an unknown selector,
// which Run reverts with empty revert data.
func (e *runEnv) emptyRevert(caller common.Address, input []byte) {
	e.t.Helper()
	_, err := e.executor.resolve(e.ctx, caller, input)
	require.ErrorIs(e.t, err, errUnknownSelector)
	require.Empty(e.t, runRevert(err))
}

func TestCallKindRefusals(t *testing.T) {
	e := newRunEnv(t)
	ctx := e.ctx
	delegated := ctx.WithEVMPrecompileCalledFromDelegateCall(true)
	getPoolCount := roleMethod(t, types.RolePoolRegistry, "getPoolCount")
	buy := roleMethod(t, types.RoleRouter, "buy")
	upgrade := roleMethod(t, types.RoleRouter, "upgradeToAndCall")
	require.Equal(t, "payable", upgrade.StateMutability)

	const (
		delegateRefusal = "cannot delegatecall launchpad"
		valueRefusal    = "sending funds to a non-payable function"
		staticRefusal   = "cannot call a launchpad state change from staticcall"
	)
	for _, tc := range []struct {
		name     string
		ctx      sdk.Context
		method   *abi.Method
		value    *big.Int
		readOnly bool
		refusal  string
	}{
		{"delegatecall view", delegated, getPoolCount, nil, false, delegateRefusal},
		{"delegatecall view under staticcall", delegated, getPoolCount, nil, true, delegateRefusal},
		{"delegatecall write with value", delegated, buy, big.NewInt(1), false, delegateRefusal},
		{"value on the payable upgradeToAndCall", ctx, upgrade, big.NewInt(1), false, valueRefusal},
		{"value on a write", ctx, buy, big.NewInt(1), false, valueRefusal},
		{"value on a view", ctx, getPoolCount, big.NewInt(1), true, valueRefusal},
		{"write under staticcall", ctx, buy, nil, true, staticRefusal},
		{"upgradeToAndCall under staticcall", ctx, upgrade, big.NewInt(0), true, staticRefusal},
		{"view under staticcall", ctx, getPoolCount, nil, true, ""},
		{"view", ctx, getPoolCount, big.NewInt(0), false, ""},
		{"write", ctx, buy, nil, false, ""},
	} {
		err := callKindRefusal(tc.ctx, tc.method, tc.value, tc.readOnly)
		if tc.refusal == "" {
			require.NoError(t, err, tc.name)
			continue
		}
		require.EqualError(t, err, tc.refusal, tc.name)
	}

	getPoolCountInput := calldata(t, getPoolCount)
	buyInput := calldata(t, buy, common.HexToAddress("0x0000000000000000000000000000000000000AbC"),
		big.NewInt(1_000_000), big.NewInt(0), big.NewInt(1_900_000_000))
	upgradeInput := calldata(t, upgrade, common.HexToAddress("0x0000000000000000000000000000000000001234"), []byte{})
	marketCount := e.keeper.GetMarketCount(ctx)

	require.Equal(t, delegateRefusal, e.refusal(delegated, runCaller, getPoolCountInput, nil, false))
	require.Equal(t, delegateRefusal, e.refusal(delegated, runRegistry, withSuffix(getPoolCountInput, runOriginal), nil, true))
	require.Equal(t, staticRefusal, e.refusal(ctx, runRouter, withSuffix(buyInput, runOriginal), nil, true))
	require.Equal(t, staticRefusal, e.refusal(ctx, runCaller, buyInput, nil, true))
	require.Equal(t, valueRefusal, e.refusal(ctx, runCaller, upgradeInput, big.NewInt(1), false))
	require.Equal(t, valueRefusal, e.refusal(ctx, runRouter, withSuffix(upgradeInput, runOriginal), big.NewInt(1), false))
	require.Equal(t, valueRefusal, e.refusal(ctx, runCaller, buyInput, big.NewInt(1), false))
	require.Equal(t, marketCount, e.keeper.GetMarketCount(ctx))

	// Execute, the framework entry for direct calls, applies the same
	// refusals.
	_, err := e.executor.Execute(delegated, getPoolCount, runCaller, runCaller, []interface{}{}, nil, true, nil, nil)
	require.EqualError(t, err, delegateRefusal)
	_, err = e.executor.Execute(ctx, upgrade, runCaller, runCaller,
		[]interface{}{common.HexToAddress("0x0000000000000000000000000000000000001234"), []byte{}}, big.NewInt(1), false, nil, nil)
	require.EqualError(t, err, valueRefusal)
}

func TestDirectCallResolution(t *testing.T) {
	e := newRunEnv(t)
	ctx := e.ctx
	executor := e.executor

	direct := executor.abi
	require.Len(t, direct.Methods, 113)
	roleOrder := []string{"Router", "SidioraFactory", "Quoter", "PoolRegistry", "FeesRouter", "FeeAccumulator",
		"ProtocolConfig"}
	for _, m := range direct.Methods {
		role, method, err := executor.resolveDirect(m.ID)
		require.NoError(t, err, m.Sig)
		require.Equal(t, m.Sig, method.Sig)
		served := -1
		for n, candidate := range roleOrder {
			if declaredBy(candidate, m.Sig) {
				served = n
				break
			}
		}
		require.GreaterOrEqual(t, served, 0, m.Sig)
		require.Equal(t, roleOrder[served], role, m.Sig)
	}
	for sig, role := range map[string]string{
		"createMarket(string,string,uint8,address)": "Router",
		"DEFAULT_ADMIN_ROLE()":                      "Router",
		"upgradeToAndCall(address,bytes)":           "Router",
		"eventEmitter()":                            "SidioraFactory",
		"treasury()":                                "SidioraFactory",
		"getAllPools(uint256,uint256)":              "Quoter",
		"getPoolsByCreator(address)":                "Quoter",
		"initialize(address,address,address)":       "Quoter",
		"FACTORY_ROLE()":                            "PoolRegistry",
		"getPoolCount()":                            "PoolRegistry",
		"claimFees(uint256)":                        "FeesRouter",
		"totalLiabilities()":                        "FeeAccumulator",
		"setCreationFee(uint256)":                   "ProtocolConfig",
	} {
		require.Equal(t, role, directRole[sig], sig)
	}

	unknown := []byte{0xde, 0xad, 0xbe, 0xef}
	_, _, err := executor.resolveDirect(unknown)
	require.ErrorIs(t, err, errUnknownSelector)
	e.emptyRevert(runCaller, unknown)
	e.emptyRevert(runCaller, []byte{0xde, 0xad, 0xbe})
	e.emptyRevert(runCaller, nil)
	e.emptyRevert(runRouter, withSuffix(unknown, runOriginal))
	e.emptyRevert(runPool, withSuffix(unknown, runOriginal))
	e.emptyRevert(runPool, runOriginal.Bytes())
	// Pool, SidioraNFT and Treasury functions are not direct: at 0x1017 they
	// are unknown selectors.
	e.emptyRevert(runCaller, calldata(t, roleMethod(t, types.RolePool, "syncReserves")))
	e.emptyRevert(runCaller, calldata(t, roleMethod(t, types.RoleFeeRights, "ownerOf"), big.NewInt(1)))
	e.emptyRevert(runRouter, withSuffix(calldata(t, roleMethod(t, types.RolePool, "syncReserves")), runOriginal))

	buy := roleMethod(t, types.RoleRouter, "buy")
	token := common.HexToAddress("0x0000000000000000000000000000000000000AbC")
	buyInput := calldata(t, buy, token, big.NewInt(1_000_000), big.NewInt(7), big.NewInt(1_900_000_000))
	c, err := executor.resolve(ctx, runCaller, buyInput)
	require.NoError(t, err)
	require.Equal(t, types.RoleRouter, c.role)
	require.Equal(t, common.HexToAddress(LaunchpadAddress), c.target)
	require.Equal(t, runCaller, c.caller)
	require.Equal(t, "buy(address,uint256,uint256,uint256)", c.method.Sig)
	require.Equal(t, []interface{}{token, big.NewInt(1_000_000), big.NewInt(7), big.NewInt(1_900_000_000)}, c.args)
	require.Nil(t, c.market)

	// A caller that is not a surface cannot claim another caller by
	// appending 20 bytes: the calldata is decoded whole and refused.
	_, err = executor.resolve(ctx, runCaller, withSuffix(buyInput, runOriginal))
	require.EqualError(t, err, "launchpad: calldata of buy(address,uint256,uint256,uint256) is not its exact ABI encoding")
	reason, err := abi.UnpackRevert(runRevert(err))
	require.NoError(t, err)
	require.Equal(t, "launchpad: calldata of buy(address,uint256,uint256,uint256) is not its exact ABI encoding", reason)
	require.Equal(t, pcommon.RevertReason(errors.New(reason)), runRevert(errors.New(reason)))

	// A launchpad keeper error reverts with its Launch-x custom error and an
	// error that carries revert data reverts with that data.
	slippage, ok := types.ContractErrorOf(types.ErrSlippageExceeded)
	require.True(t, ok)
	require.Equal(t, crypto.Keccak256([]byte("SlippageExceeded()"))[:4], runRevert(types.ErrSlippageExceeded))
	require.Equal(t, slippage.Selector[:], runRevert(fmt.Errorf("swap: %w", types.ErrSlippageExceeded)))
	refusal := launchpadkeeper.AdminRefusal(runCaller)
	adminRevert, err := refusal.(interface{ Revert() ([]byte, error) }).Revert()
	require.NoError(t, err)
	require.Len(t, adminRevert, 4+64)
	require.Equal(t, adminRevert, runRevert(refusal))

	// From a surface the last 20 bytes are the original caller.
	c, err = executor.resolve(ctx, runRouter, withSuffix(buyInput, runOriginal))
	require.NoError(t, err)
	require.Equal(t, types.RoleRouter, c.role)
	require.Equal(t, runRouter, c.target)
	require.Equal(t, runOriginal, c.caller)
	require.Equal(t, []interface{}{token, big.NewInt(1_000_000), big.NewInt(7), big.NewInt(1_900_000_000)}, c.args)
	require.Nil(t, c.market)

	// One selector resolves per role: eventEmitter() is the SidioraFactory
	// function at 0x1017, the PoolRegistry function at the PoolRegistry
	// surface and the SidioraPool function, with its market, at a pool.
	eventEmitter := calldata(t, roleMethod(t, types.RolePool, "eventEmitter"))
	c, err = executor.resolve(ctx, runCaller, eventEmitter)
	require.NoError(t, err)
	require.Equal(t, types.RoleFactory, c.role)
	c, err = executor.resolve(ctx, runRegistry, withSuffix(eventEmitter, runOriginal))
	require.NoError(t, err)
	require.Equal(t, types.RolePoolRegistry, c.role)
	require.Equal(t, runOriginal, c.caller)
	require.Nil(t, c.market)
	c, err = executor.resolve(ctx, runPool, withSuffix(eventEmitter, runOriginal))
	require.NoError(t, err)
	require.Equal(t, types.RolePool, c.role)
	require.Equal(t, runPool, c.target)
	require.Equal(t, runOriginal, c.caller)
	require.Empty(t, c.args)
	require.NotNil(t, c.market)
	require.Equal(t, e.denom, c.market.Denom)
	require.Equal(t, runPoolAddress, c.market.PoolAddress)

	// A pool surface whose market is gone is refused.
	ghost := common.HexToAddress("0x00000000000000000000000000000000000060a7")
	require.NoError(t, e.keeper.SetSurface(ctx, ghost,
		launchpadkeeper.Surface{Role: types.RolePool, Generation: types.Generation2, Market: "factory/launchpad/ghost"}))
	_, err = executor.resolve(ctx, ghost, withSuffix(eventEmitter, runOriginal))
	require.ErrorIs(t, err, types.ErrUnknownMarket)
}
