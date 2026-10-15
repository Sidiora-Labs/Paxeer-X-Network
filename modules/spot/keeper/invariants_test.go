package keeper_test

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"math/rand"
	"sort"
	"testing"
	"time"

	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtest "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/utils"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// invUSDLBook and invPAXBook are the fixture books: 500,000e6 USDL and
// 1,000,000e6 PAX.
const (
	invUSDLBook int64 = 500_000_000_000
	invPAXBook  int64 = 1_000_000_000_000
)

var (
	invSwapRail   = common.HexToAddress(types.PECORAddress)
	invOrdersRail = common.HexToAddress(types.PECOROrdersAddress)
	invStopsRail  = common.HexToAddress(types.PECORStopOrdersAddress)
	invVaultRail  = common.HexToAddress(types.PECORVaultAddress)
	invPAXPrice   = sdk.NewIntWithDecimal(5, 17)
)

func invAddress(name string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte(name))[12:])
}

func invWord(value *big.Int) []byte { return common.LeftPadBytes(value.Bytes(), 32) }

// invEnv is the spot keeper over the test application's bank and EVM
// keepers at the fixture prices (USDL 1e18, PAX 0.5e18) and fixture fee
// parameters, with USDL an ERC20 stablecoin, PAX the uhpx native pointer, a
// share token for each pool and a funded trader that approved the three
// trading rails.
type invEnv struct {
	t                         *testing.T
	ctx                       sdk.Context
	key                       sdk.StoreKey
	k                         *keeper.Keeper
	usdl, pax, holder, trader common.Address
}

func newInvEnv(t *testing.T) *invEnv {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx, _ := app.GetContextForDeliverTx(nil).CacheContext()
	ctx = ctx.WithBlockHeight(8).WithBlockTime(time.Unix(1000, 0).UTC())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	key, feedKey := sdk.NewKVStoreKey(types.StoreKey), sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	ctx = pricefeedtest.WithPriceFeedStore(pricefeedtest.WithPriceFeedStore(ctx, feedKey), key)
	feed := pricefeedkeeper.NewKeeper(feedKey, app.StakingKeeper, app.AccountKeeper, &app.EvmKeeper)
	require.NoError(t, feed.SetParams(ctx, pricefeedtypes.DefaultParams(pricefeedtypes.DefaultAuthority())))
	raw, err := json.Marshal(pricefeedtypes.Aggregate{IndexPrice: invPAXPrice, MarkPrice: invPAXPrice, Basis: sdk.ZeroInt(),
		TimestampMs: uint64(ctx.BlockTime().UnixMilli()), Height: ctx.BlockHeight(), Sources: 1})
	require.NoError(t, err)
	ctx.KVStore(feedKey).Set(pricefeedtypes.AggregateKey(types.PAXMarketID), raw)
	e := &invEnv{t: t, ctx: ctx, key: key, k: keeper.NewKeeper(key, app.BankKeeper, &app.EvmKeeper, feed),
		holder: invAddress("invariant provider"), trader: invAddress("invariant trader")}

	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(t, err)
	args, err := abi.Arguments{{Type: stringType}, {Type: stringType}, {Type: uintType}, {Type: addressType}}.
		Pack("USD Ledger", "USDL", big.NewInt(30_000_000_000_000), e.holder)
	require.NoError(t, err)
	deployer := invAddress("invariant usdl deployer")
	nonce := app.EvmKeeper.GetNonce(ctx, deployer)
	_, err = app.EvmKeeper.CallEVM(ctx, deployer, nil, nil, append(common.FromHex(limitTokenCreation), args...))
	require.NoError(t, err)
	e.usdl = crypto.CreateAddress(deployer, nonce)
	require.NotEmpty(t, app.EvmKeeper.GetCode(ctx, e.usdl))
	pax, _, exists := app.EvmKeeper.GetERC20NativePointer(ctx, paxDenom)
	if !exists {
		require.NoError(t, app.EvmKeeper.RunWithOneOffEVMInstance(ctx, func(evm *vm.EVM) error {
			var upsertErr error
			pax, upsertErr = app.EvmKeeper.UpsertERCNativePointer(ctx, evm, paxDenom,
				utils.ERCMetadata{Name: "PAX", Symbol: "PAX", Decimals: 6})
			return upsertErr
		}, func(string, string) {}))
	}
	e.pax = pax

	params := types.DefaultParams()
	params.SwapFeeBps, params.Tier1FeeBps, params.Tier2FeeBps = 30, 20, 50
	params.PriceImpactEnabled, params.PriceImpactScalarBps = true, 100
	params.OrderFeeBps, params.OrderTier1FeeBps, params.OrderTier2FeeBps = 10, 20, 50
	params.OrderPriceImpactEnabled, params.OrderPriceImpactScalarBps = true, 100
	params.KeeperOpen = true
	params.Tokens = []types.TokenParams{
		{Address: e.usdl, Kind: types.TokenKindERC20, IsStablecoin: true, Decimals: types.OpeningTokenDecimals,
			MaxUtilizationBps: types.OpeningTokenMaxUtilizationBps},
		{Address: e.pax, Kind: types.TokenKindNative, Decimals: types.OpeningTokenDecimals, MarketID: types.PAXMarketID,
			MaxUtilizationBps: types.OpeningTokenMaxUtilizationBps},
	}
	require.NoError(t, e.k.UpdateParams(ctx, e.k.Authority(), params))

	require.NoError(t, app.EvmKeeper.RunWithOneOffEVMInstance(ctx, func(evm *vm.EVM) error {
		for _, pool := range []struct {
			token  common.Address
			symbol string
		}{{e.usdl, "USDL"}, {e.pax, "PAX"}} {
			if _, _, exists := app.EvmKeeper.GetERC20NativePointer(ctx, e.k.ShareDenom(pool.token)); exists {
				continue
			}
			if _, err := e.k.CreateShareToken(ctx, evm, pool.token, pool.symbol, 6); err != nil {
				return err
			}
		}
		return nil
	}, func(string, string) {}))

	e.usdlCall(e.holder, "transfer(address,uint256)", e.trader, 100_000_000_000)
	e.fundPAX(e.trader, 200_000_000_000)
	for _, spender := range []common.Address{invSwapRail, invOrdersRail, invStopsRail} {
		e.usdlCall(e.trader, "approve(address,uint256)", spender, 100_000_000_000)
	}
	return e
}

func (e *invEnv) usdlCall(from common.Address, signature string, to common.Address, amount int64) {
	e.t.Helper()
	input := append(append(crypto.Keccak256([]byte(signature))[:4], common.LeftPadBytes(to.Bytes(), 32)...),
		invWord(big.NewInt(amount))...)
	ret, err := testkeeper.EVMTestApp.EvmKeeper.CallEVM(e.ctx, from, &e.usdl, nil, input)
	require.NoError(e.t, err)
	require.Equal(e.t, invWord(big.NewInt(1)), ret)
}

func (e *invEnv) account(holder common.Address) sdk.AccAddress {
	return testkeeper.EVMTestApp.EvmKeeper.GetPaxAddressOrDefault(e.ctx, holder)
}

// fundPAX mints amount uhpx to the bank account of holder.
func (e *invEnv) fundPAX(holder common.Address, amount int64) {
	e.t.Helper()
	bank := testkeeper.EVMTestApp.BankKeeper
	coins := sdk.NewCoins(sdk.NewCoin(paxDenom, sdk.NewInt(amount)))
	require.NoError(e.t, bank.MintCoins(e.ctx, "evm", coins))
	require.NoError(e.t, bank.SendCoinsFromModuleToAccount(e.ctx, "evm", e.account(holder), coins))
}

func (e *invEnv) paxBalance(holder common.Address) int64 {
	e.t.Helper()
	amount := testkeeper.EVMTestApp.BankKeeper.GetBalance(e.ctx, e.account(holder), paxDenom).Amount
	require.True(e.t, amount.IsInt64())
	return amount.Int64()
}

// provide deposits the fixture books from the provider.
func (e *invEnv) provide() {
	e.t.Helper()
	e.usdlCall(e.holder, "approve(address,uint256)", invVaultRail, invUSDLBook)
	_, _, err := e.k.Deposit(e.ctx, e.holder, e.usdl, sdk.NewInt(invUSDLBook))
	require.NoError(e.t, err)
	e.fundPAX(e.holder, invPAXBook)
	_, _, err = e.k.Deposit(e.ctx, e.holder, e.pax, sdk.NewInt(invPAXBook))
	require.NoError(e.t, err)
	require.Equal(e.t, sdk.NewInt(invUSDLBook), e.reserves(e.ctx, e.usdl))
	require.Equal(e.t, sdk.NewInt(invPAXBook), e.reserves(e.ctx, e.pax))
}

func (e *invEnv) reserves(ctx sdk.Context, token common.Address) sdk.Int {
	e.t.Helper()
	book, err := e.k.GetBook(ctx, token)
	require.NoError(e.t, err)
	return book.Reserves
}

// supply is the bank supply of the share denom of token, the totalSupply
// its share pointer reads.
func (e *invEnv) supply(ctx sdk.Context, token common.Address) sdk.Int {
	return testkeeper.EVMTestApp.BankKeeper.GetSupply(ctx, e.k.ShareDenom(token)).Amount
}

func (e *invEnv) shares(ctx sdk.Context, holder sdk.AccAddress, token common.Address) sdk.Int {
	return testkeeper.EVMTestApp.BankKeeper.GetBalance(ctx, holder, e.k.ShareDenom(token)).Amount
}

func (e *invEnv) escrow(ctx sdk.Context, token common.Address) sdk.Int {
	e.t.Helper()
	escrow, err := e.k.GetEscrow(ctx, token)
	require.NoError(e.t, err)
	return escrow
}

func (e *invEnv) fees(ctx sdk.Context, rail, token common.Address) sdk.Int {
	e.t.Helper()
	fees, err := e.k.AccumulatedFees(ctx, rail, token)
	require.NoError(e.t, err)
	return fees
}

// put writes record at prefix + parts in the spot store of ctx.
func (e *invEnv) put(ctx sdk.Context, record interface{}, prefix []byte, parts ...[]byte) {
	e.t.Helper()
	key := append([]byte(nil), prefix...)
	for _, part := range parts {
		key = append(key, part...)
	}
	if record == nil {
		ctx.KVStore(e.key).Set(key, []byte{})
		return
	}
	raw, err := json.Marshal(record)
	require.NoError(e.t, err)
	ctx.KVStore(e.key).Set(key, raw)
}

// requireInvariant runs invariant on ctx and requires its exact message and
// broken flag; an empty message means no violation.
func requireInvariant(t *testing.T, ctx sdk.Context, invariant sdk.Invariant, name, message string) {
	t.Helper()
	got, broken := invariant(ctx)
	require.Equal(t, sdk.FormatInvariant(types.ModuleName, name, message), got)
	require.Equal(t, message != "", broken)
}

// invRegistry records the routes RegisterInvariants registers.
type invRegistry struct{ routes []string }

func (r *invRegistry) RegisterRoute(module, route string, _ sdk.Invariant) {
	r.routes = append(r.routes, module+"/"+route)
}

func TestSolvencyInvariant(t *testing.T) {
	e := newInvEnv(t)
	registry := &invRegistry{}
	keeper.RegisterInvariants(registry, e.k)
	require.Equal(t, []string{"spot/spot-solvency", "spot/spot-share-backing", "spot/spot-escrow"}, registry.routes)

	solvency := keeper.SolvencyInvariant(e.k)
	check := func() {
		t.Helper()
		requireInvariant(t, e.ctx, solvency, keeper.SolvencyInvariantName, "")
	}
	check()
	e.provide()
	check()

	_, err := e.k.MarketBuy(e.ctx, e.trader, e.usdl, e.pax, big.NewInt(1_000_000_000), new(big.Int), new(big.Int))
	require.NoError(t, err)
	check()
	_, err = e.k.MarketSell(e.ctx, e.trader, e.pax, e.usdl, big.NewInt(400_000_000), new(big.Int), new(big.Int))
	require.NoError(t, err)
	check()

	buy, _, err := e.k.PlaceLimit(e.ctx, e.trader, e.usdl, e.pax, sdk.NewInt(100_000_000), invPAXPrice, 0, types.OrderTypeLimitBuy)
	require.NoError(t, err)
	check()
	_, err = e.k.ExecuteLimit(e.ctx, buy)
	require.NoError(t, err)
	check()
	sell, _, err := e.k.PlaceLimit(e.ctx, e.trader, e.usdl, e.pax, sdk.NewInt(400_000_000), sdk.NewIntWithDecimal(6, 17), 0,
		types.OrderTypeLimitSell)
	require.NoError(t, err)
	require.Equal(t, sdk.NewInt(400_000_000), e.escrow(e.ctx, e.pax))
	check()
	_, err = e.k.CancelLimit(e.ctx, e.trader, sell)
	require.NoError(t, err)
	check()
	stop, err := e.k.PlaceStopLoss(e.ctx, e.trader, e.pax, e.usdl, sdk.NewInt(200_000_000), invPAXPrice, 0)
	require.NoError(t, err)
	check()
	require.NoError(t, e.k.ExecuteStopLoss(e.ctx, stop))
	check()
	_, _, err = e.k.Withdraw(e.ctx, e.holder, e.usdl, sdk.NewInt(1_000_000_000))
	require.NoError(t, err)
	check()
	// Custody above book plus escrow is solvent.
	e.usdlCall(e.holder, "transfer(address,uint256)", e.k.CustodyEVMAddress(e.ctx), 5_000_000)
	check()
	require.True(t, e.fees(e.ctx, invSwapRail, e.pax).IsPositive())
	require.True(t, e.fees(e.ctx, invOrdersRail, e.pax).IsPositive())

	// A book above custody less escrow is a violation.
	branch, _ := e.ctx.CacheContext()
	_, custody, err := e.k.CustodyBalance(branch, e.usdl)
	require.NoError(t, err)
	book, err := e.k.GetBook(branch, e.usdl)
	require.NoError(t, err)
	book.Reserves = custody.AddRaw(1)
	require.NoError(t, e.k.SetBook(branch, book))
	requireInvariant(t, branch, solvency, keeper.SolvencyInvariantName, fmt.Sprintf(
		"token %s: custody %s below book %s plus escrow 0\n", e.usdl.Hex(), custody, custody.AddRaw(1)))

	// Escrow counts against custody too.
	branch, _ = e.ctx.CacheContext()
	_, custody, err = e.k.CustodyBalance(branch, e.pax)
	require.NoError(t, err)
	paxReserves := e.reserves(branch, e.pax)
	excess := custody.Sub(paxReserves).AddRaw(1)
	e.put(branch, types.TokenEscrow{Token: e.pax, Amount: excess}, types.EscrowPrefix, e.pax.Bytes())
	requireInvariant(t, branch, solvency, keeper.SolvencyInvariantName, fmt.Sprintf(
		"token %s: custody %s below book %s plus escrow %s\n", e.pax.Hex(), custody, paxReserves, excess))

	// A book below its fee accruals is a violation.
	branch, _ = e.ctx.CacheContext()
	reserved := e.fees(branch, invSwapRail, e.pax).Add(e.fees(branch, invOrdersRail, e.pax))
	require.True(t, reserved.GT(sdk.OneInt()))
	book, err = e.k.GetBook(branch, e.pax)
	require.NoError(t, err)
	book.Reserves = sdk.OneInt()
	require.NoError(t, e.k.SetBook(branch, book))
	requireInvariant(t, branch, solvency, keeper.SolvencyInvariantName, fmt.Sprintf(
		"token %s: book 1 below fee accruals %s\n", e.pax.Hex(), reserved))
}

func TestShareBackingInvariant(t *testing.T) {
	e := newInvEnv(t)
	shareBacking := keeper.ShareBackingInvariant(e.k)
	app := testkeeper.EVMTestApp

	// Before any deposit both pools have supply 0.
	require.True(t, e.supply(e.ctx, e.usdl).IsZero())
	require.True(t, e.supply(e.ctx, e.pax).IsZero())
	requireInvariant(t, e.ctx, shareBacking, keeper.ShareBackingInvariantName, "")

	// Shares without a deposit are a violation.
	branch, _ := e.ctx.CacheContext()
	require.NoError(t, app.EvmKeeper.RunWithOneOffEVMInstance(branch, func(evm *vm.EVM) error {
		return e.k.MintShares(branch, evm, e.k.ShareDenom(e.usdl), e.trader, sdk.NewInt(5))
	}, func(string, string) {}))
	requireInvariant(t, branch, shareBacking, keeper.ShareBackingInvariantName, fmt.Sprintf(
		"token %s: share supply 5 without a deposit\n", e.usdl.Hex()))

	e.provide()
	require.Equal(t, sdk.NewInt(invUSDLBook), e.supply(e.ctx, e.usdl))
	require.Equal(t, sdk.NewInt(keeper.MinimumShares), e.shares(e.ctx, keeper.CustodyAddress(), e.usdl))
	requireInvariant(t, e.ctx, shareBacking, keeper.ShareBackingInvariantName, "")

	// The provider withdraws every share it holds: supply 1000, all held by
	// the spot module account.
	held := e.shares(e.ctx, e.account(e.holder), e.usdl)
	require.Equal(t, sdk.NewInt(invUSDLBook-keeper.MinimumShares), held)
	paid, _, err := e.k.Withdraw(e.ctx, e.holder, e.usdl, held)
	require.NoError(t, err)
	require.Equal(t, sdk.NewInt(invUSDLBook-keeper.MinimumShares), paid)
	require.True(t, e.shares(e.ctx, e.account(e.holder), e.usdl).IsZero())
	require.Equal(t, sdk.NewInt(keeper.MinimumShares), e.supply(e.ctx, e.usdl))
	require.Equal(t, sdk.NewInt(keeper.MinimumShares), e.shares(e.ctx, keeper.CustodyAddress(), e.usdl))
	require.Equal(t, sdk.NewInt(keeper.MinimumShares), e.reserves(e.ctx, e.usdl))
	requireInvariant(t, e.ctx, shareBacking, keeper.ShareBackingInvariantName, "")

	burn := func(amount int64) sdk.Context {
		t.Helper()
		branch, _ := e.ctx.CacheContext()
		require.NoError(t, app.EvmKeeper.RunWithOneOffEVMInstance(branch, func(evm *vm.EVM) error {
			return e.k.BurnShares(branch, evm, e.k.ShareDenom(e.usdl), e.k.CustodyEVMAddress(branch), sdk.NewInt(amount))
		}, func(string, string) {}))
		return branch
	}
	// Burning a locked share leaves the pool below MINIMUM_SHARES.
	requireInvariant(t, burn(1), shareBacking, keeper.ShareBackingInvariantName, fmt.Sprintf(
		"token %[1]s: share supply 999 below MINIMUM_SHARES 1000 after the first deposit\n"+
			"token %[1]s: spot module account holds 999 shares, below MINIMUM_SHARES 1000\n", e.usdl.Hex()))
	// A deposited pool at supply 0 is a violation.
	requireInvariant(t, burn(keeper.MinimumShares), shareBacking, keeper.ShareBackingInvariantName, fmt.Sprintf(
		"token %[1]s: share supply 0 below MINIMUM_SHARES 1000 after the first deposit\n"+
			"token %[1]s: spot module account holds 0 shares, below MINIMUM_SHARES 1000\n", e.usdl.Hex()))
}

// invOrder is an active order of TestEscrowInvariant.
type invOrder struct {
	kind   types.OrderType
	token  common.Address
	amount int64
}

func TestEscrowInvariant(t *testing.T) {
	e := newInvEnv(t)
	e.provide()
	escrowInvariant := keeper.EscrowInvariant(e.k)
	requireInvariant(t, e.ctx, escrowInvariant, keeper.EscrowInvariantName, "")

	rng := rand.New(rand.NewSource(4263))
	active := map[uint64]invOrder{}
	finished := map[types.OrderType]uint64{}
	performed := map[string]int{}
	ids := func() []uint64 {
		out := make([]uint64, 0, len(active))
		for id := range active {
			out = append(out, id)
		}
		sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
		return out
	}
	for step := 0; step < 50; step++ {
		action := rng.Intn(4)
		if len(active) == 0 {
			action = 0
		}
		switch action {
		case 0, 1:
			usdlAmount := int64(20+rng.Intn(181)) * 1_000_000
			paxAmount := int64(40+rng.Intn(361)) * 1_000_000
			var (
				id    uint64
				err   error
				order invOrder
			)
			switch rng.Intn(4) {
			case 0:
				order = invOrder{types.OrderTypeLimitBuy, e.usdl, usdlAmount}
				id, _, err = e.k.PlaceLimit(e.ctx, e.trader, e.usdl, e.pax, sdk.NewInt(usdlAmount), invPAXPrice, 0, order.kind)
			case 1:
				order = invOrder{types.OrderTypeLimitSell, e.pax, paxAmount}
				id, _, err = e.k.PlaceLimit(e.ctx, e.trader, e.usdl, e.pax, sdk.NewInt(paxAmount), invPAXPrice, 0, order.kind)
			case 2:
				order = invOrder{types.OrderTypeStopLimitBuy, e.usdl, usdlAmount}
				id, err = e.k.PlaceStopLimit(e.ctx, e.trader, e.usdl, e.pax, sdk.NewInt(usdlAmount), invPAXPrice,
					sdk.NewIntWithDecimal(6, 17), 0, order.kind)
			default:
				order = invOrder{types.OrderTypeStopLoss, e.pax, paxAmount}
				id, err = e.k.PlaceStopLoss(e.ctx, e.trader, e.pax, e.usdl, sdk.NewInt(paxAmount), invPAXPrice, 0)
			}
			require.NoError(t, err)
			active[id] = order
			performed["place"]++
		case 2:
			list := ids()
			id := list[rng.Intn(len(list))]
			var err error
			switch active[id].kind {
			case types.OrderTypeLimitBuy, types.OrderTypeLimitSell:
				_, err = e.k.CancelLimit(e.ctx, e.trader, id)
			case types.OrderTypeStopLimitBuy:
				err = e.k.CancelStopLimit(e.ctx, e.trader, id)
			default:
				err = e.k.CancelStopLoss(e.ctx, e.trader, id)
			}
			require.NoError(t, err)
			finished[active[id].kind] = id
			delete(active, id)
			performed["cancel"]++
		default:
			list := ids()
			id := list[rng.Intn(len(list))]
			var err error
			switch active[id].kind {
			case types.OrderTypeLimitBuy, types.OrderTypeLimitSell:
				_, err = e.k.ExecuteLimit(e.ctx, id)
			case types.OrderTypeStopLimitBuy:
				activated, activateErr := e.k.ActivateStopLimit(e.ctx, id)
				require.NoError(t, activateErr)
				require.True(t, activated)
				err = e.k.ExecuteStopLimit(e.ctx, id)
			default:
				err = e.k.ExecuteStopLoss(e.ctx, id)
			}
			require.NoError(t, err)
			finished[active[id].kind] = id
			delete(active, id)
			performed["fill"]++
		}
		requireInvariant(t, e.ctx, escrowInvariant, keeper.EscrowInvariantName, "")
	}
	require.Equal(t, 50, performed["place"]+performed["cancel"]+performed["fill"])
	require.Positive(t, performed["cancel"])
	require.Positive(t, performed["fill"])

	sums := map[common.Address]int64{}
	for _, order := range active {
		sums[order.token] += order.amount
	}
	require.Positive(t, sums[e.usdl])
	require.Equal(t, sdk.NewInt(sums[e.usdl]), e.escrow(e.ctx, e.usdl))
	require.Equal(t, sdk.NewInt(sums[e.pax]), e.escrow(e.ctx, e.pax))
	count, err := e.k.GetActiveOrderCount(e.ctx, e.trader)
	require.NoError(t, err)
	require.Equal(t, uint64(len(active)), count)

	// Escrow off the active sum by one.
	branch, _ := e.ctx.CacheContext()
	e.put(branch, types.TokenEscrow{Token: e.usdl, Amount: sdk.NewInt(sums[e.usdl] + 1)}, types.EscrowPrefix, e.usdl.Bytes())
	requireInvariant(t, branch, escrowInvariant, keeper.EscrowInvariantName, fmt.Sprintf(
		"escrow of %s is %d, its active orders sum to %d\n", e.usdl.Hex(), sums[e.usdl]+1, sums[e.usdl]))

	// An active index entry without a record.
	branch, _ = e.ctx.CacheContext()
	e.put(branch, nil, types.ActiveLimitPrefix, binary.BigEndian.AppendUint64(nil, 999_999))
	requireInvariant(t, branch, escrowInvariant, keeper.EscrowInvariantName,
		"active limit index entry 999999 has no limit record\n")

	// An active index entry whose order is finished.
	finishedLimit, ok := finished[types.OrderTypeLimitBuy]
	if !ok {
		finishedLimit, ok = finished[types.OrderTypeLimitSell]
	}
	require.True(t, ok)
	order, err := e.k.GetLimitOrder(e.ctx, finishedLimit)
	require.NoError(t, err)
	branch, _ = e.ctx.CacheContext()
	e.put(branch, nil, types.ActiveLimitPrefix, binary.BigEndian.AppendUint64(nil, finishedLimit))
	requireInvariant(t, branch, escrowInvariant, keeper.EscrowInvariantName, fmt.Sprintf(
		"active limit index entry %d has status %d\n", finishedLimit, order.Status))

	// An active count off the active orders.
	branch, _ = e.ctx.CacheContext()
	e.put(branch, types.ActiveOrderCount{User: e.trader, Count: count + 1}, types.ActiveCountPrefix, e.trader.Bytes())
	requireInvariant(t, branch, escrowInvariant, keeper.EscrowInvariantName, fmt.Sprintf(
		"active count of %s is %d, it has %d active orders\n", e.trader.Hex(), count+1, count))
}

func TestCrossPoolConservation(t *testing.T) {
	e := newInvEnv(t)
	e.provide()

	// The fixture marketBuy of 1,000e6 USDL.
	inBefore, outBefore := e.reserves(e.ctx, e.usdl), e.reserves(e.ctx, e.pax)
	paxBefore := e.paxBalance(e.trader)
	result, err := e.k.MarketBuy(e.ctx, e.trader, e.usdl, e.pax, big.NewInt(1_000_000_000), new(big.Int), new(big.Int))
	require.NoError(t, err)
	require.Equal(t, big.NewInt(1_000_000_000), result.AmountIn)
	require.Equal(t, big.NewInt(2_000_000_000), result.GrossOutput)
	require.Equal(t, big.NewInt(1_994_000_000), result.AmountOut)
	inAfter, outAfter := e.reserves(e.ctx, e.usdl), e.reserves(e.ctx, e.pax)
	require.Equal(t, inBefore.AddRaw(1_000_000_000), inAfter)
	require.Equal(t, outBefore.SubRaw(1_994_000_000), outAfter)
	require.Equal(t, sdk.NewInt(6_000_000), e.fees(e.ctx, invSwapRail, e.pax))
	require.Equal(t, paxBefore+1_994_000_000, e.paxBalance(e.trader))
	valueIn, err := keeper.VolumeUSD(sdk.NewInt(1_000_000_000), sdk.NewIntWithDecimal(1, 18), types.OpeningTokenDecimals)
	require.NoError(t, err)
	valueOut, err := keeper.VolumeUSD(sdk.NewInt(2_000_000_000), invPAXPrice, types.OpeningTokenDecimals)
	require.NoError(t, err)
	require.Equal(t, sdk.NewIntWithDecimal(1_000, 18), valueIn)
	require.Equal(t, valueIn, valueOut)

	swap := keeper.Settlement{TokenIn: e.usdl, TokenOut: e.pax, AmountIn: sdk.NewIntFromBigInt(result.AmountIn),
		Gross: sdk.NewIntFromBigInt(result.GrossOutput), Net: sdk.NewIntFromBigInt(result.AmountOut),
		InBefore: inBefore, InAfter: inAfter, OutBefore: outBefore, OutAfter: outAfter}
	requireInvariant(t, e.ctx, keeper.ConservationInvariant(e.k, swap), keeper.ConservationInvariantName, "")

	// The limit buy fill of 100e6 USDL at 0.5e18: gross 200e6, fee 0.2e6,
	// net 199.8e6.
	id, _, err := e.k.PlaceLimit(e.ctx, e.trader, e.usdl, e.pax, sdk.NewInt(100_000_000), invPAXPrice, 0, types.OrderTypeLimitBuy)
	require.NoError(t, err)
	inBefore, outBefore = e.reserves(e.ctx, e.usdl), e.reserves(e.ctx, e.pax)
	paxBefore = e.paxBalance(e.trader)
	_, err = e.k.ExecuteLimit(e.ctx, id)
	require.NoError(t, err)
	require.Equal(t, sdk.NewInt(200_000), e.fees(e.ctx, invOrdersRail, e.pax))
	require.Equal(t, paxBefore+199_800_000, e.paxBalance(e.trader))
	fill := keeper.Settlement{TokenIn: e.usdl, TokenOut: e.pax, AmountIn: sdk.NewInt(100_000_000), Gross: sdk.NewInt(200_000_000),
		Net: sdk.NewInt(199_800_000), InBefore: inBefore, InAfter: e.reserves(e.ctx, e.usdl), OutBefore: outBefore,
		OutAfter: e.reserves(e.ctx, e.pax)}
	requireInvariant(t, e.ctx, keeper.ConservationInvariant(e.k, fill), keeper.ConservationInvariantName, "")

	// A net output off the output book.
	wrongNet := swap
	wrongNet.Net = sdk.NewInt(1_993_000_000)
	requireInvariant(t, e.ctx, keeper.ConservationInvariant(e.k, wrongNet), keeper.ConservationInvariantName, fmt.Sprintf(
		"output book of %s fell by 1994000000, net output 1993000000\n", e.pax.Hex()))
	// An input book off amountIn.
	wrongIn := swap
	wrongIn.InAfter = inAfter.SubRaw(1)
	requireInvariant(t, e.ctx, keeper.ConservationInvariant(e.k, wrongIn), keeper.ConservationInvariantName, fmt.Sprintf(
		"input book of %s moved by 999999999, amountIn 1000000000\n", e.usdl.Hex()))
	// A net output above gross.
	aboveGross := swap
	aboveGross.Net, aboveGross.OutAfter = sdk.NewInt(2_000_000_001), swap.OutBefore.SubRaw(2_000_000_001)
	requireInvariant(t, e.ctx, keeper.ConservationInvariant(e.k, aboveGross), keeper.ConservationInvariantName,
		"net output 2000000001 above gross 2000000000\n")
	// A gross worth more than amountIn.
	overpaid := swap
	overpaid.Gross = sdk.NewInt(2_000_000_001)
	requireInvariant(t, e.ctx, keeper.ConservationInvariant(e.k, overpaid), keeper.ConservationInvariantName, fmt.Sprintf(
		"USD value 1000000000000000000000 of amountIn 1000000000 of %s differs from USD value "+
			"1000000000500000000000 of gross 2000000001 of %s beyond the rounding of gross\n", e.usdl.Hex(), e.pax.Hex()))
	// A gross worth less than amountIn by more than its rounding.
	underpaid := swap
	underpaid.Gross, underpaid.Net = sdk.NewInt(1_999_999_999), sdk.NewInt(1_994_000_000)
	requireInvariant(t, e.ctx, keeper.ConservationInvariant(e.k, underpaid), keeper.ConservationInvariantName, fmt.Sprintf(
		"USD value 1000000000000000000000 of amountIn 1000000000 of %s differs from USD value "+
			"999999999500000000000 of gross 1999999999 of %s beyond the rounding of gross\n", e.usdl.Hex(), e.pax.Hex()))
	// A settlement of one token against itself, and amounts outside uint256.
	self := swap
	self.TokenOut = e.usdl
	requireInvariant(t, e.ctx, keeper.ConservationInvariant(e.k, self), keeper.ConservationInvariantName, fmt.Sprintf(
		"settlement trades %s against itself\n", e.usdl.Hex()))
	negative := swap
	negative.AmountIn = sdk.NewInt(-1)
	requireInvariant(t, e.ctx, keeper.ConservationInvariant(e.k, negative), keeper.ConservationInvariantName,
		"settlement amounts must be uint256 integers\n")
	requireInvariant(t, e.ctx, keeper.ConservationInvariant(e.k, keeper.Settlement{TokenIn: e.usdl, TokenOut: e.pax}),
		keeper.ConservationInvariantName, "settlement amounts must be uint256 integers\n")
	// An unregistered token.
	unregistered := swap
	unregistered.TokenOut = invAddress("unregistered output")
	requireInvariant(t, e.ctx, keeper.ConservationInvariant(e.k, unregistered), keeper.ConservationInvariantName, fmt.Sprintf(
		"token %s: %v\n", unregistered.TokenOut.Hex(), types.ErrTokenNotRegistered))
}
