package launchpad_test

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
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
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

var (
	viewNow      = time.Unix(1_900_000_000, 0).UTC()
	viewQuoter   = launchpadkeeper.Generation2Surfaces[types.RoleQuoter]
	viewRegistry = launchpadkeeper.Generation2Surfaces[types.RolePoolRegistry]
	viewConfig   = launchpadkeeper.Generation2Surfaces[types.RoleProtocolConfig]
	viewEmitter  = common.HexToAddress("0x622E1bb90E0E2B6E6e5e9e2DbE1555389E8A3EF6")
	viewWad      = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	viewBps      = big.NewInt(10_000)
)

// viewDirectOrder is the order in which a call sent to 0x1017 resolves its
// selector to a role.
var viewDirectOrder = []string{
	types.RoleRouter, types.RoleFactory, types.RoleQuoter, types.RolePoolRegistry, types.RoleFeesRouter,
	types.RoleFeeAccumulator, types.RoleProtocolConfig,
}

// viewEnv is the launchpad precompile over a real keeper whose genesis holds
// one native market and four pools of generations 1 and 2, with the Quoter,
// PoolRegistry and ProtocolConfig surfaces registered.
type viewEnv struct {
	t          *testing.T
	stateDB    *state.DBImpl
	evm        *vm.EVM
	precompile vm.PrecompiledContract
	keeper     *launchpadkeeper.Keeper
	abis       map[string]abi.ABI
	owners     map[string]string
	served     map[string]map[string]bool
	markets    map[common.Address]types.Market
}

type viewPool struct {
	pool, token common.Address
	market      types.Market
}

func viewInt(value int64) sdk.Int { return sdk.NewInt(value) }

func viewMarket(index uint64, generation types.Generation, creator sdk.AccAddress, pool, token common.Address,
	virtual, real, reserve, supply int64) types.Market {
	snapshots := make([]sdk.Int, types.SnapshotSlots)
	for n := range snapshots {
		snapshots[n] = sdk.ZeroInt()
	}
	collection := launchpadkeeper.Generation2Surfaces[types.RoleFeeRights]
	if generation == types.Generation1 {
		collection = common.HexToAddress("0xDF73b354ed9dcB473cc9D01541c46f507591e190")
	}
	return types.Market{
		Denom: types.ModuleName + "/" + pool.Hex(), Index: index, Name: fmt.Sprintf("View %d", index),
		Symbol: fmt.Sprintf("VW%d", index), Creator: creator.String(), Guardian: creator.String(),
		FeeRightsHolder: creator.String(), FeeStrategy: types.FeeStrategyBurn, TotalSupply: viewInt(supply),
		VirtualQuoteReserve: viewInt(virtual), RealQuoteBalance: viewInt(real), TokenReserve: viewInt(reserve),
		CumulativeVolume: viewInt(3_000_000 * int64(index)), PriceSnapshots: snapshots,
		AccumulatedQuoteFees: sdk.ZeroInt(), AccumulatedTokenFees: sdk.ZeroInt(), AccumulatedFees: sdk.ZeroInt(),
		AirdropBalance: sdk.ZeroInt(), Generation: generation, PoolAddress: pool.Hex(), TokenAddress: token.Hex(),
		Optical: common.Address{}.Hex(), Factory: launchpadkeeper.Generation2Surfaces[types.RoleFactory].Hex(),
		FeeAccumulator: launchpadkeeper.Generation2Surfaces[types.RoleFeeAccumulator].Hex(),
		ProtocolConfig: viewConfig.Hex(), EventEmitter: viewEmitter.Hex(),
		CumulativeBuyQuoteVolume: sdk.ZeroInt(), CumulativeSellQuoteVolume: sdk.ZeroInt(),
		FeeRightCollection: collection.Hex(), FeeRightID: 2*index + 1,
	}
}

func viewNative(index uint64, creator sdk.AccAddress) types.Market {
	snapshots := make([]sdk.Int, types.SnapshotSlots)
	for n := range snapshots {
		snapshots[n] = sdk.ZeroInt()
	}
	return types.Market{
		Denom: "factory/launchpad/native", Index: index, Name: "Native View", Symbol: "NVW",
		Pointer: common.BytesToAddress([]byte{0x0a, 0xbc}).Hex(), Creator: creator.String(),
		Guardian: creator.String(), FeeRightsHolder: creator.String(), FeeStrategy: types.FeeStrategyBurn,
		TotalSupply: viewInt(1_000), VirtualQuoteReserve: viewInt(10), RealQuoteBalance: sdk.ZeroInt(),
		TokenReserve: viewInt(1_000), CumulativeVolume: sdk.ZeroInt(), PriceSnapshots: snapshots,
		AccumulatedQuoteFees: sdk.ZeroInt(), AccumulatedTokenFees: sdk.ZeroInt(), AccumulatedFees: sdk.ZeroInt(),
		AirdropBalance: sdk.ZeroInt(),
	}
}

func newViewEnv(t *testing.T, markets []types.Market, members []types.RoleMember,
	mappings map[string]common.Address) *viewEnv {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockTime(viewNow).WithBlockHeight(77)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	k, ctx := launchpadtest.NewKeeper(app, ctx)
	for bech32, address := range mappings {
		app.EvmKeeper.SetAddressMapping(ctx, sdk.MustAccAddressFromBech32(bech32), address)
	}
	gs := types.DefaultGenesis()
	gs.Markets = markets
	gs.RoleMembers = members
	byPool := map[common.Address]types.Market{}
	for _, market := range markets {
		if market.Generation != types.GenerationNative {
			gs.Surfaces = append(gs.Surfaces, types.Surface{Address: market.PoolAddress, Role: types.RolePool})
			gs.PoolMetadata = append(gs.PoolMetadata, types.PoolMetadata{Pool: market.PoolAddress,
				Creator: mappings[market.Creator].Hex(), Token: market.TokenAddress, Optical: market.Optical,
				NFTID: market.FeeRightID, CreatedAt: market.CreationTime, CreatedBlock: 77})
			byPool[common.HexToAddress(market.PoolAddress)] = market
		}
	}
	for _, role := range []string{types.RoleQuoter, types.RolePoolRegistry, types.RoleProtocolConfig} {
		gs.Surfaces = append(gs.Surfaces,
			types.Surface{Address: launchpadkeeper.Generation2Surfaces[role].Hex(), Role: role})
	}
	require.NoError(t, gs.Validate())
	k.InitGenesis(ctx, *gs)

	precompile, err := launchpad.NewPrecompileWithKeeper(k, &app.EvmKeeper)
	require.NoError(t, err)
	e := &viewEnv{t: t, keeper: k, precompile: precompile, abis: map[string]abi.ABI{}, owners: map[string]string{},
		served: map[string]map[string]bool{}, markets: byPool}
	e.stateDB = state.NewDBImpl(ctx, &app.EvmKeeper, false)
	blockCtx, err := app.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
	require.NoError(t, err)
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(app.EvmKeeper.ChainID(ctx))
	e.evm = vm.NewEVM(*blockCtx, e.stateDB, cfg, vm.Config{}, app.EvmKeeper.CustomPrecompiles(ctx))
	for _, role := range viewDirectOrder {
		bz, err := os.ReadFile("abi/" + role + ".json")
		require.NoError(t, err)
		parsed, err := abi.JSON(bytes.NewReader(bz))
		require.NoError(t, err)
		e.abis[role] = parsed
		for _, method := range parsed.Methods {
			if _, taken := e.owners[method.Sig]; !taken {
				e.owners[method.Sig] = role
			}
		}
	}
	return e
}

// outcome is the return data or revert data of one call.
type outcome struct {
	ret      []byte
	reverted bool
}

func (e *viewEnv) run(from common.Address, input []byte) outcome {
	e.t.Helper()
	ret, err := e.precompile.Run(e.evm, from, from, input, nil, false, false, nil)
	if err != nil {
		require.ErrorIs(e.t, err, vm.ErrExecutionReverted)
		return outcome{ret: ret, reverted: true}
	}
	return outcome{ret: ret}
}

// call sends name of role from caller through the role surface and, when the
// selector resolves to role at 0x1017, directly to 0x1017; both answers are
// the same. It returns the surface answer.
func (e *viewEnv) call(role string, caller common.Address, name string, args ...interface{}) outcome {
	e.t.Helper()
	method, ok := e.abis[role].Methods[name]
	require.True(e.t, ok, "%s.%s", role, name)
	if e.served[role] == nil {
		e.served[role] = map[string]bool{}
	}
	e.served[role][name] = true
	packed, err := method.Inputs.Pack(args...)
	require.NoError(e.t, err)
	input := append(append([]byte(nil), method.ID...), packed...)
	surface := launchpadkeeper.Generation2Surfaces[role]
	viaSurface := e.run(surface, append(append([]byte(nil), input...), caller.Bytes()...))
	if e.owners[method.Sig] == role && method.IsConstant() {
		require.Equal(e.t, viaSurface, e.run(caller, input), "%s.%s at 0x1017", role, name)
	}
	return viaSurface
}

func (e *viewEnv) values(role string, caller common.Address, name string, args ...interface{}) []interface{} {
	e.t.Helper()
	got := e.call(role, caller, name, args...)
	require.False(e.t, got.reverted, "%s.%s reverted: %x", role, name, got.ret)
	out, err := e.abis[role].Methods[name].Outputs.Unpack(got.ret)
	require.NoError(e.t, err)
	return out
}

func (e *viewEnv) one(role string, caller common.Address, name string, args ...interface{}) interface{} {
	e.t.Helper()
	out := e.values(role, caller, name, args...)
	require.Len(e.t, out, 1)
	return out[0]
}

// refused asserts the call reverts with the reason of want, carrying detail.
func (e *viewEnv) refused(want *sdkerrors.Error, detail string, role string, caller common.Address, name string,
	args ...interface{}) {
	e.t.Helper()
	got := e.call(role, caller, name, args...)
	require.True(e.t, got.reverted, "%s.%s did not revert", role, name)
	// A contract error reverts with its custom error, as the contract does;
	// detail is checked against its arguments, and an error without
	// arguments is the bare selector.
	if contractError, ok := types.ContractErrorOf(want); ok {
		require.GreaterOrEqual(e.t, len(got.ret), 4, "%s.%s", role, name)
		require.Equal(e.t, contractError.Selector[:], got.ret[:4], "%s.%s", role, name)
		if len(contractError.Inputs) == 0 {
			require.Len(e.t, got.ret, 4, "%s.%s", role, name)
			return
		}
		values, err := contractError.Inputs.Unpack(got.ret[4:])
		require.NoError(e.t, err)
		require.Contains(e.t, fmt.Sprint(values...), detail, "%s.%s", role, name)
		return
	}
	reason, err := abi.UnpackRevert(got.ret)
	require.NoError(e.t, err)
	require.Contains(e.t, reason, want.Error(), "%s.%s", role, name)
	require.Contains(e.t, reason, detail, "%s.%s", role, name)
}

func (e *viewEnv) adminRefused(role string, caller common.Address, name string, args ...interface{}) {
	e.t.Helper()
	refusal := launchpadkeeper.AdminRefusal(caller)
	var roleErr *launchpadkeeper.RoleError
	require.True(e.t, errors.As(refusal, &roleErr))
	revert, err := roleErr.Revert()
	require.NoError(e.t, err)
	require.Equal(e.t, []byte{0xe2, 0x51, 0x7d, 0x3f}, revert[:4])
	// AccessControl reverts with the custom error
	// AccessControlUnauthorizedAccount(account, neededRole), not Error(string).
	got := e.call(role, caller, name, args...)
	require.True(e.t, got.reverted, "%s.%s did not revert", role, name)
	require.Equal(e.t, revert, got.ret, "%s.%s", role, name)
}

func viewMulDiv(a, b, c *big.Int) *big.Int { return new(big.Int).Div(new(big.Int).Mul(a, b), c) }

func viewPrice(effective, reserve *big.Int) *big.Int { return viewMulDiv(effective, viewWad, reserve) }

// viewLeg is the Quoter arithmetic of one swap on market at feeBps,
// written out from Launch-x Quoter.quoteExactInput and quoteMultihop.
func viewLeg(market types.Market, feeBps, amountIn *big.Int, isBuy, multihopSell bool) (out, fee, impact *big.Int) {
	fee = viewMulDiv(amountIn, feeBps, viewBps)
	after := new(big.Int).Sub(amountIn, fee)
	effective := new(big.Int).Add(market.VirtualQuoteReserve.BigInt(), market.RealQuoteBalance.BigInt())
	reserve := market.TokenReserve.BigInt()
	var newEffective, newReserve *big.Int
	if isBuy {
		out = viewMulDiv(reserve, after, new(big.Int).Add(effective, after))
		newEffective, newReserve = new(big.Int).Add(effective, after), new(big.Int).Sub(reserve, out)
	} else {
		out = viewMulDiv(effective, after, new(big.Int).Add(reserve, after))
		newEffective, newReserve = new(big.Int).Sub(effective, out), new(big.Int).Add(reserve, amountIn)
	}
	impact = new(big.Int)
	if newReserve.Sign() > 0 && (!multihopSell || newEffective.Sign() > 0) {
		before := viewPrice(effective, reserve)
		impact = viewMulDiv(new(big.Int).Abs(new(big.Int).Sub(viewPrice(newEffective, newReserve), before)), viewBps,
			before)
	}
	return out, fee, impact
}

func TestViewRolesServeEverySelector(t *testing.T) {
	creatorAcc, creator := testkeeper.MockAddressPair()
	otherAcc, other := testkeeper.MockAddressPair()
	_, holder := testkeeper.MockAddressPair()
	_, stranger := testkeeper.MockAddressPair()
	address := func(seed byte) common.Address { return common.BytesToAddress([]byte{0x5e, seed}) }
	poolA, poolB, poolC, poolD := address(0xa1), address(0xb1), address(0xc1), address(0xd1)
	tokenA, tokenB, tokenC, tokenD := address(0xa2), address(0xb2), address(0xc2), address(0xd2)
	notPool := address(0xee)

	marketA := viewMarket(1, types.Generation2, creatorAcc, poolA, tokenA, 1_000_000_000_000, 250_000_000_000,
		800_000_000_000, 1_000_000_000_000)
	marketA.CreationTime = viewNow.Unix() - 7_200
	native := viewNative(2, creatorAcc)
	marketB := viewMarket(3, types.Generation1, creatorAcc, poolB, tokenB, 500_000_000_000, 100_000_000_000,
		900_000_000_000, 1_000_000_000_000)
	marketB.CreationTime = viewNow.Unix() - 100
	marketC := viewMarket(4, types.Generation2, otherAcc, poolC, tokenC, 1_000_000, 0, 1_000_000, 1_000_000)
	marketC.Paused = true
	marketD := viewMarket(5, types.Generation2, otherAcc, poolD, tokenD, 1_000_000, 0, 1_000_000, 1_000_000)
	marketD.Optical = address(0x0f).Hex()
	e := newViewEnv(t, []types.Market{marketA, native, marketB, marketC, marketD},
		[]types.RoleMember{{Contract: viewRegistry.Hex(), Role: launchpad.FactoryRole.Hex(), Account: holder.Hex()}},
		map[string]common.Address{creatorAcc.String(): creator, otherAcc.String(): other})
	ctx := e.stateDB.Ctx()
	params := e.keeper.GetParams(ctx)
	feeA, err := e.keeper.FeeBps(ctx, marketA)
	require.NoError(t, err)
	feeB, err := e.keeper.FeeBps(ctx, marketB)
	require.NoError(t, err)
	effA := new(big.Int).Add(marketA.VirtualQuoteReserve.BigInt(), marketA.RealQuoteBalance.BigInt())
	priceA := viewPrice(effA, marketA.TokenReserve.BigInt())
	capA := viewMulDiv(priceA, marketA.TotalSupply.BigInt(), viewWad)
	all := []common.Address{poolA, poolB, poolC, poolD}
	zero := common.Address{}
	n := big.NewInt

	for _, role := range []string{types.RoleQuoter, types.RolePoolRegistry, types.RoleProtocolConfig} {
		contract := launchpadkeeper.Generation2Surfaces[role]
		require.Equal(t, [32]byte{}, e.one(role, stranger, "DEFAULT_ADMIN_ROLE"))
		require.Equal(t, [32]byte{}, e.one(role, stranger, "getRoleAdmin", [32]byte(launchpad.FactoryRole)))
		same(t, n(0), e.one(role, stranger, "registeredDefaultAdminCount"))
		require.Equal(t, role == types.RolePoolRegistry,
			e.one(role, stranger, "hasRole", [32]byte(launchpad.FactoryRole), holder))
		require.Equal(t, false, e.one(role, stranger, "hasRole", [32]byte{}, holder))
		e.adminRefused(role, stranger, "grantRole", [32]byte(launchpad.FactoryRole), stranger)
		e.adminRefused(role, holder, "revokeRole", [32]byte(launchpad.FactoryRole), holder)
		e.adminRefused(role, stranger, "upgradeToAndCall", contract, []byte{0x01})
		e.refused(types.ErrUUPSNotThroughActiveProxy, "", role, stranger, "proxiableUUID")
		initialize := e.abis[role].Methods["initialize"]
		initArgs := make([]interface{}, len(initialize.Inputs))
		for i := range initArgs {
			initArgs[i] = stranger
		}
		e.refused(types.ErrAlreadyInitialized, "", role, stranger, "initialize", initArgs...)

		// MissingRole(msg.sender, role) (Launch-x:contracts/contracts/base/AccessControl.sol:68-71).
		e.refused(types.ErrMissingRole, stranger.Hex(), role, stranger, "renounceRole", [32]byte(launchpad.FactoryRole),
			holder)
		logs := len(e.stateDB.GetAllLogs())
		require.Empty(t, e.values(role, stranger, "renounceRole", [32]byte(launchpad.FactoryRole), stranger))
		require.Len(t, e.stateDB.GetAllLogs(), logs)
	}

	// Quoter.
	require.Equal(t, viewRegistry, e.one(types.RoleQuoter, stranger, "poolRegistry"))
	require.Equal(t, viewConfig, e.one(types.RoleQuoter, stranger, "protocolConfig"))
	require.Equal(t, all, e.one(types.RoleQuoter, stranger, "getAllPools", n(0), n(10)))
	require.Equal(t, []common.Address{poolB, poolC}, e.one(types.RoleQuoter, stranger, "getAllPools", n(1), n(2)))
	require.Equal(t, []common.Address{poolA, poolB}, e.one(types.RoleQuoter, stranger, "getPoolsByCreator", creator))
	require.Equal(t, priceA, e.one(types.RoleQuoter, stranger, "getPoolPrice", poolA))
	e.refused(types.ErrUnknownMarket, notPool.Hex(), types.RoleQuoter, stranger, "getPoolPrice", notPool)
	require.Equal(t, capA, e.one(types.RoleQuoter, stranger, "getMarketCap", poolA))
	e.refused(types.ErrUnknownMarket, notPool.Hex(), types.RoleQuoter, stranger, "getMarketCap", notPool)

	stats := abi.ConvertType(e.one(types.RoleQuoter, stranger, "getPoolStats", poolA),
		new(launchpad.QuoterPoolStats)).(*launchpad.QuoterPoolStats)
	require.Equal(t, launchpad.QuoterPoolStats{VirtualUsdl: marketA.VirtualQuoteReserve.BigInt(),
		RealUsdl: marketA.RealQuoteBalance.BigInt(), TokenReserve: marketA.TokenReserve.BigInt(),
		CumulativeVolume: n(3_000_000), CurrentFeeBps: feeA, PoolAge: n(7_200), MarketCap: capA, Price: priceA}, *stats)
	e.refused(types.ErrUnknownMarket, notPool.Hex(), types.RoleQuoter, stranger, "getPoolStats", notPool)

	quote := func(pool common.Address, amountIn *big.Int, isBuy bool) launchpad.QuoterQuoteResult {
		t.Helper()
		return *abi.ConvertType(e.one(types.RoleQuoter, stranger, "quoteExactInput", pool, amountIn, isBuy),
			new(launchpad.QuoterQuoteResult)).(*launchpad.QuoterQuoteResult)
	}
	for _, isBuy := range []bool{true, false} {
		out, fee, impact := viewLeg(marketA, feeA, n(1_000_000_000), isBuy, false)
		require.Positive(t, impact.Sign())
		require.Equal(t, launchpad.QuoterQuoteResult{AmountOut: out, FeeAmount: fee, PriceImpactBps: impact},
			quote(poolA, n(1_000_000_000), isBuy))
	}
	e.refused(types.ErrUnexecutableQuote, "real USDL", types.RoleQuoter, stranger, "quoteExactInput", poolA,
		n(800_000_000_000), false)
	e.refused(types.ErrInsufficientInput, "", types.RoleQuoter, stranger, "quoteExactInput", poolA, n(0), true)
	e.refused(types.ErrUnexecutableQuote, "zero output", types.RoleQuoter, stranger, "quoteExactInput", poolA, n(1),
		true)
	e.refused(types.ErrUnexecutableQuote, "paused", types.RoleQuoter, stranger, "quoteExactInput", poolC, n(1_000),
		true)
	e.refused(types.ErrOpticalQuoteUnsupported, poolD.Hex(), types.RoleQuoter, stranger, "quoteExactInput", poolD,
		n(1_000), true)
	e.refused(types.ErrUnexecutableQuote, "not a registered pool", types.RoleQuoter, stranger, "quoteExactInput",
		notPool, n(1_000), true)

	sellOut, sellFee, sellImpact := viewLeg(marketA, feeA, n(1_000_000_000), false, true)
	buyOut, buyFee, buyImpact := viewLeg(marketB, feeB, sellOut, true, false)
	hop := abi.ConvertType(e.one(types.RoleQuoter, stranger, "quoteMultihop", tokenA, tokenB, n(1_000_000_000)),
		new(launchpad.QuoterMultihopResult)).(*launchpad.QuoterMultihopResult)
	require.Equal(t, launchpad.QuoterMultihopResult{AmountOut: buyOut, IntermediateUsdl: sellOut,
		SellFeeAmount: sellFee, BuyFeeAmount: buyFee, SellPriceImpactBps: sellImpact, BuyPriceImpactBps: buyImpact,
		CombinedPriceImpactBps: new(big.Int).Add(sellImpact, buyImpact), PoolA: poolA, PoolB: poolB}, *hop)
	e.refused(types.ErrZeroAddress, "", types.RoleQuoter, stranger, "quoteMultihop", zero, tokenB, n(1_000))
	e.refused(types.ErrZeroAddress, "", types.RoleQuoter, stranger, "quoteMultihop", tokenA, tokenA, n(1_000))
	e.refused(types.ErrZeroAddress, "no pool", types.RoleQuoter, stranger, "quoteMultihop", tokenA, notPool, n(1_000))
	e.refused(types.ErrUnexecutableQuote, "paused", types.RoleQuoter, stranger, "quoteMultihop", tokenA, tokenC,
		n(1_000_000_000))
	e.refused(types.ErrUnexecutableQuote, "real USDL", types.RoleQuoter, stranger, "quoteMultihop", tokenA, tokenB,
		n(800_000_000_000))

	// PoolRegistry.
	reg := types.RolePoolRegistry
	require.Equal(t, [32]byte(crypto.Keccak256Hash([]byte("FACTORY_ROLE"))), e.one(reg, stranger, "FACTORY_ROLE"))
	require.Equal(t, viewEmitter, e.one(reg, stranger, "eventEmitter"))
	require.Equal(t, all, e.one(reg, stranger, "getAllPools", n(0), new(big.Int).Lsh(n(1), 255)))
	require.Equal(t, []common.Address{poolD}, e.one(reg, stranger, "getAllPools", n(3), n(5)))
	require.Equal(t, []common.Address{}, e.one(reg, stranger, "getAllPools", n(4), n(1)))
	require.Equal(t, []common.Address{}, e.one(reg, stranger, "getAllPools", new(big.Int).Lsh(n(1), 200), n(1)))
	same(t, n(4), e.one(reg, stranger, "getPoolCount"))
	same(t, n(2), e.one(reg, stranger, "getPoolCountByCreator", other))
	same(t, n(0), e.one(reg, stranger, "getPoolCountByCreator", stranger))
	require.Equal(t, []common.Address{poolC, poolD}, e.one(reg, stranger, "getPoolsByCreator", other))
	require.Equal(t, []common.Address{}, e.one(reg, stranger, "getPoolsByCreator", stranger))
	require.Equal(t, []common.Address{poolD}, e.one(reg, stranger, "getPoolsByCreatorPage", other, n(1), n(5)))
	require.Equal(t, []common.Address{poolA}, e.one(reg, stranger, "getPoolsByCreatorPage", creator, n(0), n(1)))
	require.Equal(t, []common.Address{}, e.one(reg, stranger, "getPoolsByCreatorPage", other, n(2), n(1)))
	require.Equal(t, poolB, e.one(reg, stranger, "getPoolByToken", tokenB))
	require.Equal(t, zero, e.one(reg, stranger, "getPoolByToken", notPool))
	require.Equal(t, zero, e.one(reg, stranger, "getPoolByToken", zero))
	require.Equal(t, true, e.one(reg, stranger, "isRegisteredPool", poolC))
	require.Equal(t, false, e.one(reg, stranger, "isRegisteredPool", notPool))
	require.Equal(t, false, e.one(reg, stranger, "isRegisteredPool", viewQuoter))
	same(t, n(int64(marketA.FeeRightID)), e.one(reg, stranger, "getNftIdByPool", poolA))
	same(t, n(int64(marketB.FeeRightID)), e.one(reg, stranger, "getNftIdByPool", poolB))
	same(t, n(0), e.one(reg, stranger, "getNftIdByPool", notPool))
	metadata := abi.ConvertType(e.one(reg, stranger, "getPoolMetadata", notPool),
		new(launchpad.RegistryPoolMetadata)).(*launchpad.RegistryPoolMetadata)
	same(t, launchpad.RegistryPoolMetadata{NftId: n(0), CreatedAt: n(0), CreatedBlock: n(0)}, *metadata)
	metadata = abi.ConvertType(e.one(reg, stranger, "getPoolMetadata", poolA),
		new(launchpad.RegistryPoolMetadata)).(*launchpad.RegistryPoolMetadata)
	same(t, launchpad.RegistryPoolMetadata{Creator: creator, Token: tokenA, Optical: zero,
		NftId: n(int64(marketA.FeeRightID)), CreatedAt: n(marketA.CreationTime), CreatedBlock: n(77)}, *metadata)
	ctx.KVStore(sdk.NewKVStoreKey(types.StoreKey)).Delete(append([]byte{0x51}, poolA.Bytes()...))
	e.refused(types.ErrInvalidMarket, "no registry metadata", reg, stranger, "getPoolMetadata", poolA)

	newPool, newToken := address(0x77), address(0x78)
	e.refused(types.ErrMissingRole, stranger.Hex(), reg, stranger, "register", newPool, newToken, creator, zero, n(1))
	e.refused(types.ErrZeroAddress, "", reg, holder, "register", zero, newToken, creator, zero, n(1))
	e.refused(types.ErrZeroAddress, "", reg, holder, "register", newPool, zero, creator, zero, n(1))
	e.refused(types.ErrZeroAddress, "", reg, holder, "register", newPool, newToken, zero, zero, n(1))
	e.refused(types.ErrDuplicateToken, tokenA.Hex(), reg, holder, "register", newPool, tokenA, creator, zero, n(1))
	e.refused(types.ErrDuplicatePool, poolB.Hex(), reg, holder, "register", poolB, newToken, creator, zero, n(1))
	e.refused(types.ErrInvalidConfiguration, newPool.Hex(), reg, holder, "register", newPool, newToken, creator, zero,
		n(1))
	same(t, n(4), e.one(reg, stranger, "getPoolCount"))

	logs := len(e.stateDB.GetAllLogs())
	require.Empty(t, e.values(reg, holder, "renounceRole", [32]byte(launchpad.FactoryRole), holder))
	allLogs := e.stateDB.GetAllLogs()
	require.Len(t, allLogs, logs+1)
	revoked := allLogs[logs]
	holderTopic := common.BytesToHash(holder.Bytes())
	require.Equal(t, viewRegistry, revoked.Address)
	require.Equal(t, []common.Hash{e.abis[reg].Events["RoleRevoked"].ID, launchpad.FactoryRole, holderTopic,
		holderTopic}, revoked.Topics)
	require.Empty(t, revoked.Data)
	require.Equal(t, false, e.one(reg, stranger, "hasRole", [32]byte(launchpad.FactoryRole), holder))
	e.refused(types.ErrMissingRole, holder.Hex(), reg, holder, "register", newPool, newToken, creator, zero, n(1))

	// ProtocolConfig.
	cfg := types.RoleProtocolConfig
	u := func(value uint64) *big.Int { return new(big.Int).SetUint64(value) }
	require.Equal(t, common.HexToAddress(params.QuoteToken), e.one(cfg, stranger, "usdlAddress"))
	require.Equal(t, common.HexToAddress(types.DefaultQuoteToken), e.one(cfg, stranger, "usdlAddress"))
	same(t, params.VirtualQuoteDefault.BigInt(), e.one(cfg, stranger, "virtualUsdlDefault"))
	same(t, params.VirtualTokenDefault.BigInt(), e.one(cfg, stranger, "virtualTokenDefault"))
	same(t, u(params.MinFeeBps), e.one(cfg, stranger, "minFeeBps"))
	same(t, u(params.MaxFeeBps), e.one(cfg, stranger, "maxFeeBps"))
	same(t, u(params.BaseFeeBps), e.one(cfg, stranger, "baseFeeBps"))
	same(t, u(params.ProtocolFeeBps), e.one(cfg, stranger, "protocolFeeBps"))
	same(t, u(params.FeeDecayRate), e.one(cfg, stranger, "feeDecayRate"))
	same(t, u(params.VolatilityWeight), e.one(cfg, stranger, "volatilityWeight"))
	same(t, u(params.ConcentrationWeight), e.one(cfg, stranger, "concentrationWeight"))
	same(t, params.CreationFee.BigInt(), e.one(cfg, stranger, "creationFee"))
	require.Equal(t, viewEmitter, e.one(cfg, stranger, "eventEmitter"))
	e.adminRefused(cfg, stranger, "setBaseFeeBps", u(params.BaseFeeBps))
	e.adminRefused(cfg, holder, "setCreationFee", n(1))
	e.adminRefused(cfg, stranger, "setFeeWeights", n(1), n(2), n(0))
	e.adminRefused(cfg, stranger, "setProtocolFeeBps", n(100))
	e.adminRefused(cfg, stranger, "setVirtualDefaults", n(1), n(1))
	require.Equal(t, params, e.keeper.GetParams(e.stateDB.Ctx()))

	for _, role := range []string{types.RoleQuoter, types.RolePoolRegistry, types.RoleProtocolConfig} {
		for name := range e.abis[role].Methods {
			require.True(t, e.served[role][name], "%s.%s is not exercised", role, name)
		}
	}
}
