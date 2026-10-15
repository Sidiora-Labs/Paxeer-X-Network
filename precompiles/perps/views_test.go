package perps_test

import (
	"math/big"
	"strings"
	"testing"
	"time"

	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	perpskeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	perpstypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
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
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
	tmdb "github.com/tendermint/tm-db"
)

const viewsNow = 1_800_001_500

var (
	viewsA = common.HexToAddress("0x00000000000000000000000000000000000000a1")
	viewsB = common.HexToAddress("0x00000000000000000000000000000000000000b2")
	viewsC = common.HexToAddress("0x00000000000000000000000000000000000000c3")
)

type viewsKeepers struct {
	utils.Keepers
	perps *perpskeeper.Keeper
}

func (k viewsKeepers) PerpsK() *perpskeeper.Keeper { return k.perps }

type viewsEnv struct {
	t          *testing.T
	ctx        sdk.Context
	keeper     *perpskeeper.Keeper
	abi        abi.ABI
	precompile *pcommon.Precompile
	evm        *vm.EVM
	caller     common.Address
	usdl       common.Address
}

func viewsMounted(parent storetypes.MultiStore, key storetypes.StoreKey) (store storetypes.KVStore, ok bool) {
	defer func() {
		if recover() != nil {
			store, ok = nil, false
		}
	}()
	return parent.GetKVStore(key), true
}

func viewsWithStore(ctx sdk.Context, key storetypes.StoreKey) sdk.Context {
	parent := ctx.MultiStore()
	stores := map[storetypes.StoreKey]storetypes.CacheWrapper{}
	keys := map[string]storetypes.StoreKey{}
	for _, existing := range parent.StoreKeys() {
		if store, ok := viewsMounted(parent, existing); ok {
			stores[existing] = store
			keys[existing.Name()] = existing
		}
	}
	stores[key] = dbadapter.Store{DB: tmdb.NewMemDB()}
	keys[key.Name()] = key
	return ctx.WithMultiStore(cachemulti.NewStore(tmdb.NewMemDB(), stores, keys, nil, nil, nil, 0))
}

func newViewsEnv(t *testing.T, build func(gs *perpstypes.GenesisState, k *perpskeeper.Keeper)) *viewsEnv {
	t.Helper()
	app := testkeeper.EVMTestApp
	base, _ := app.NewContext(false, tmtypes.Header{}).WithBlockHeight(11).WithBlockTime(time.Unix(viewsNow, 0)).CacheContext()
	key := sdk.NewKVStoreKey(perpstypes.StoreKey)
	ctx := viewsWithStore(base, key)
	k := perpskeeper.NewKeeper(key, &app.EvmKeeper, nil)
	require.NoError(t, perpskeeper.SeedV6120(ctx, *k))
	gs := k.ExportGenesis(ctx)
	gs.Counters = perpstypes.Counters{NextOrderID: *uint256.NewInt(100), NextPositionID: *uint256.NewInt(100), NextSequence: 100}
	if build != nil {
		build(gs, k)
	}
	k.InitGenesis(ctx, *gs)
	precompile, err := perps.NewPrecompile(viewsKeepers{Keepers: app.GetPrecompileKeepers(), perps: k})
	require.NoError(t, err)
	parsed, err := perpsabi.ABI()
	require.NoError(t, err)
	_, caller := testkeeper.MockAddressPair()
	stateDB := state.NewDBImpl(ctx.WithEventManager(sdk.NewEventManager()), &app.EvmKeeper, true)
	return &viewsEnv{t: t, ctx: ctx, keeper: k, abi: parsed, precompile: precompile, caller: caller,
		evm: &vm.EVM{StateDB: stateDB, TxContext: vm.TxContext{Origin: caller}}, usdl: gs.Params.CollateralToken}
}

func (e *viewsEnv) call(name string, args ...interface{}) ([]byte, error) {
	e.t.Helper()
	method, ok := e.abi.Methods[name]
	require.True(e.t, ok, name)
	input, err := method.Inputs.Pack(args...)
	require.NoError(e.t, err)
	return e.precompile.Run(e.evm, e.caller, e.caller, append(append([]byte{}, method.ID...), input...), nil, true, false, nil)
}

func (e *viewsEnv) expect(name string, args []interface{}, outputs ...interface{}) {
	e.t.Helper()
	res, err := e.call(name, args...)
	require.NoError(e.t, err, "%s reverted with %x", name, res)
	want, err := e.abi.Methods[name].Outputs.Pack(outputs...)
	require.NoError(e.t, err)
	require.Equal(e.t, hexutil.Encode(want), hexutil.Encode(res), name)
}

func (e *viewsEnv) expectRevert(name, custom string, args ...interface{}) {
	e.t.Helper()
	res, err := e.call(name, args...)
	require.ErrorIs(e.t, err, vm.ErrExecutionReverted)
	require.Equal(e.t, crypto.Keccak256([]byte(custom + "()"))[:4], res, name)
}

func viewsFixed(t *testing.T, decimal string) *big.Int {
	t.Helper()
	negative := strings.HasPrefix(decimal, "-")
	whole, fraction, _ := strings.Cut(strings.TrimPrefix(decimal, "-"), ".")
	require.LessOrEqual(t, len(fraction), 18, decimal)
	value, ok := new(big.Int).SetString(whole+fraction+strings.Repeat("0", 18-len(fraction)), 10)
	require.True(t, ok, decimal)
	if negative {
		value.Neg(value)
	}
	return value
}

func viewsWord(t *testing.T, decimal string) uint256.Int {
	t.Helper()
	value := viewsFixed(t, decimal)
	var word uint256.Int
	require.False(t, word.SetFromBig(new(big.Int).Abs(value)))
	if value.Sign() < 0 {
		word.Neg(&word)
	}
	return word
}

func viewsInt(value int64) *big.Int { return big.NewInt(value) }

func viewsID(id uint64) uint256.Int { return *uint256.NewInt(id) }

func viewsAccount(gs *perpstypes.GenesisState, address common.Address, usdl uint64) {
	gs.Accounts = append(gs.Accounts, perpstypes.Account{Address: address, Collateral: *uint256.NewInt(usdl), Created: true})
}

func viewsAccountOf(t *testing.T, gs *perpstypes.GenesisState, address common.Address) *perpstypes.Account {
	t.Helper()
	for i := range gs.Accounts {
		if gs.Accounts[i].Address == address {
			return &gs.Accounts[i]
		}
	}
	require.FailNow(t, "no account", address.Hex())
	return nil
}

func viewsPosition(t *testing.T, gs *perpstypes.GenesisState, position perpstypes.Position) {
	t.Helper()
	gs.Positions = append(gs.Positions, position)
	gs.AccountMarketPositions = append(gs.AccountMarketPositions, perpstypes.AccountMarketPositionEntry{
		Account: position.Account, MarketID: position.MarketID, PositionID: position.ID,
	})
	viewsAccountOf(t, gs, position.Account).PositionCount++
}

func viewsOrder(t *testing.T, gs *perpstypes.GenesisState, order perpstypes.Order) {
	t.Helper()
	gs.Orders = append(gs.Orders, order)
	if order.Status == perpstypes.OrderStatusResting || order.Status == perpstypes.OrderStatusUntriggered {
		gs.AccountOrders = append(gs.AccountOrders, perpstypes.AccountOrderEntry{Account: order.Account, OrderID: order.ID})
		viewsAccountOf(t, gs, order.Account).OpenOrderCount++
	}
}

func viewsState(t *testing.T, gs *perpstypes.GenesisState, state perpstypes.MarketState, mark string) *perpstypes.MarketState {
	t.Helper()
	price := viewsWord(t, mark)
	if state.IndexPrice.IsZero() {
		state.IndexPrice = price
	}
	if state.PricefeedMark.IsZero() {
		state.PricefeedMark = price
	}
	state.MarkPrice = price
	state.PriceTimestampMs = viewsNow * 1000
	state.LastIndexHour = viewsNow / 3600
	gs.MarketStates = append(gs.MarketStates, state)
	return &gs.MarketStates[len(gs.MarketStates)-1]
}

func viewsLong(t *testing.T, id uint64, account common.Address, marketID uint64, isLong bool, size, entry, leverage string) perpstypes.Position {
	t.Helper()
	cost := new(big.Int).Mul(viewsFixed(t, size), viewsFixed(t, entry))
	cost.Quo(cost, viewsFixed(t, "1"))
	var costWord uint256.Int
	require.False(t, costWord.SetFromBig(cost))
	return perpstypes.Position{
		ID: viewsID(id), Account: account, MarketID: marketID, IsLong: isLong, SizeBase: viewsWord(t, size),
		ReferencePrice: viewsWord(t, entry), CostBasisUsd: costWord, EntryPrice: viewsWord(t, entry),
		Leverage: viewsWord(t, leverage), OpenedAt: viewsNow - 600,
	}
}

func viewsLimit(t *testing.T, id uint64, account common.Address, marketID uint64, isLong bool, price, size string, sequence uint64) perpstypes.Order {
	t.Helper()
	sizeUsd := new(big.Int).Mul(viewsFixed(t, price), viewsFixed(t, size))
	sizeUsd.Quo(sizeUsd, viewsFixed(t, "1"))
	var sizeWord uint256.Int
	require.False(t, sizeWord.SetFromBig(sizeUsd))
	return perpstypes.Order{
		ID: viewsID(id), Account: account, MarketID: marketID, IsLong: isLong, Type: perpstypes.OrderTypeLimit,
		TimeInForce: perpstypes.TimeInForceGTC, Status: perpstypes.OrderStatusResting, LimitPrice: viewsWord(t, price),
		SizeUsd: sizeWord, SizeBase: viewsWord(t, size), Leverage: viewsWord(t, "20"), Sequence: sequence,
	}
}

type viewsAccountState struct {
	Collateral           *big.Int
	EquityUsd            *big.Int
	InitialMarginUsd     *big.Int
	MaintenanceMarginUsd *big.Int
	Withdrawable         *big.Int
	FeeTier              uint8
	Volume14dUsd         *big.Int
	PositionCount        *big.Int
	OpenOrderCount       *big.Int
}

type viewsPositionMargin struct {
	Token        common.Address
	LockedAmount *big.Int
	MarketId     *big.Int
	IsLong       bool
}

type viewsMarginTier struct {
	LowerBoundUsd  *big.Int
	MaxLeverage    *big.Int
	MaintenanceBps *big.Int
	DeductionUsd   *big.Int
}

type viewsOrderDetails struct {
	OrderId         *big.Int
	User            common.Address
	MarketId        *big.Int
	IsLong          bool
	OrderType       uint8
	TimeInForce     uint8
	Status          uint8
	ReduceOnly      bool
	LimitPrice      *big.Int
	TriggerPrice    *big.Int
	AcceptablePrice *big.Int
	SizeUsd         *big.Int
	SizeBase        *big.Int
	FilledBase      *big.Int
	Leverage        *big.Int
	Deadline        uint64
	Cloid           [16]byte
	Sequence        uint64
}

type viewsPositionDetails struct {
	PositionId       *big.Int
	User             common.Address
	MarketId         *big.Int
	IsLong           bool
	SizeBase         *big.Int
	SizeUsdAtMark    *big.Int
	EntryPrice       *big.Int
	Leverage         *big.Int
	UnrealizedPnlUsd *big.Int
	FundingOwedUsd   *big.Int
	LiquidationPrice *big.Int
	TakeProfitPrice  *big.Int
	StopLossPrice    *big.Int
	OpenedAt         *big.Int
}

type viewsMarketConfig struct {
	MarketId             *big.Int
	Symbol               string
	Name                 string
	OracleSymbol         string
	SzDecimals           uint8
	PxDecimals           uint8
	TickSize             *big.Int
	LotSize              *big.Int
	MaxLeverage          *big.Int
	TierClass            uint8
	ImpactNotionalUsd    *big.Int
	OiCapUsd             *big.Int
	FundingCapPpmPerHour *big.Int
	Enabled              bool
	ListedAt             uint64
}

func TestViewsGetVaultReturnsPrecompileOrZero(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) {
		viewsAccount(gs, viewsA, 0)
	})
	env.expect("getVault", []interface{}{viewsA}, common.HexToAddress(perps.PerpsAddress))
	env.expect("getVault", []interface{}{viewsB}, common.Address{})
}

func TestViewsGetAccountEquityFields(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) {
		viewsAccount(gs, viewsA, 250_000_000)
		viewsAccount(gs, viewsC, 100_000_000)
		viewsState(t, gs, perpstypes.MarketState{MarketID: 0, Fresh: true}, "60000")
		position := viewsLong(t, 7, viewsA, 0, true, "1", "60000", "1000")
		position.CostBasisUsd = viewsWord(t, "59987.5")
		viewsPosition(t, gs, position)
	})
	env.expect("getAccount", []interface{}{viewsA}, viewsAccountState{
		Collateral: viewsInt(250_000_000), EquityUsd: viewsFixed(t, "262.5"), InitialMarginUsd: viewsFixed(t, "60"),
		MaintenanceMarginUsd: viewsFixed(t, "30"), Withdrawable: viewsInt(0), FeeTier: 0, Volume14dUsd: viewsInt(0),
		PositionCount: viewsInt(1), OpenOrderCount: viewsInt(0),
	})
	env.expect("getAccount", []interface{}{viewsC}, viewsAccountState{
		Collateral: viewsInt(100_000_000), EquityUsd: viewsFixed(t, "100"), InitialMarginUsd: viewsInt(0),
		MaintenanceMarginUsd: viewsInt(0), Withdrawable: viewsInt(100_000_000), FeeTier: 0, Volume14dUsd: viewsInt(0),
		PositionCount: viewsInt(0), OpenOrderCount: viewsInt(0),
	})
	env.expect("getAccount", []interface{}{viewsB}, viewsAccountState{
		Collateral: viewsInt(0), EquityUsd: viewsInt(0), InitialMarginUsd: viewsInt(0), MaintenanceMarginUsd: viewsInt(0),
		Withdrawable: viewsInt(0), FeeTier: 0, Volume14dUsd: viewsInt(0), PositionCount: viewsInt(0), OpenOrderCount: viewsInt(0),
	})
}

func TestViewsGetPositionMarginIsInitialMargin(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) {
		viewsAccount(gs, viewsA, 500_000_000)
		viewsState(t, gs, perpstypes.MarketState{MarketID: 0, Fresh: true}, "50000")
		viewsPosition(t, gs, viewsLong(t, 7, viewsA, 0, true, "0.2", "50000", "100"))
	})
	env.expect("getPositionMargin", []interface{}{viewsInt(7)},
		viewsPositionMargin{Token: env.usdl, LockedAmount: viewsInt(100_000_000), MarketId: viewsInt(0), IsLong: true})
	env.expectRevert("getPositionMargin", "PerpsUnknownPosition", viewsInt(99))
}

func TestViewsGetMarginTiersMatchesSheet(t *testing.T) {
	env := newViewsEnv(t, nil)
	rows := []struct {
		lower     string
		leverage  int64
		bps       int64
		deduction string
	}{
		{"0", 1000, 5, "0"},
		{"1000000", 500, 10, "500"},
		{"5000000", 200, 25, "8000"},
		{"20000000", 100, 50, "58000"},
		{"50000000", 40, 125, "433000"},
		{"150000000", 20, 250, "2308000"},
	}
	tiers := make([]viewsMarginTier, len(rows))
	for i, row := range rows {
		tiers[i] = viewsMarginTier{LowerBoundUsd: viewsFixed(t, row.lower), MaxLeverage: viewsInt(row.leverage),
			MaintenanceBps: viewsInt(row.bps), DeductionUsd: viewsFixed(t, row.deduction)}
	}
	env.expect("getMarginTiers", []interface{}{viewsInt(0)}, tiers)
	env.expectRevert("getMarginTiers", "PerpsUnknownMarket", viewsInt(18))
}

func TestViewsCheckLiquidatableIsAccountLevel(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) {
		viewsAccount(gs, viewsA, 29_000_000)
		viewsAccount(gs, viewsB, 60_000_000)
		viewsState(t, gs, perpstypes.MarketState{MarketID: 0, Fresh: true}, "60000")
		viewsState(t, gs, perpstypes.MarketState{MarketID: 1, Fresh: true}, "3000")
		viewsPosition(t, gs, viewsLong(t, 7, viewsA, 0, true, "0.5", "60000", "100"))
		viewsPosition(t, gs, viewsLong(t, 8, viewsA, 1, true, "10", "3000", "100"))
		viewsPosition(t, gs, viewsLong(t, 9, viewsB, 0, true, "1", "60000", "100"))
	})
	env.expect("checkLiquidatable", []interface{}{viewsInt(7)}, true, viewsInt(9666))
	env.expect("checkLiquidatable", []interface{}{viewsInt(8)}, true, viewsInt(9666))
	env.expect("checkLiquidatable", []interface{}{viewsInt(9)}, false, viewsInt(20000))
	env.expectRevert("checkLiquidatable", "PerpsUnknownPosition", viewsInt(99))
}

func viewsOrderBook(t *testing.T, gs *perpstypes.GenesisState) {
	t.Helper()
	viewsAccount(gs, viewsA, 1_000_000_000)
	first := viewsLimit(t, 1, viewsA, 0, true, "59000", "0.10078", 5)
	first.Cloid = [16]byte{0xc1, 0x01}
	viewsOrder(t, gs, first)
	cancelled := viewsLimit(t, 2, viewsA, 0, true, "58000", "0.1", 6)
	cancelled.Status = perpstypes.OrderStatusCancelled
	viewsOrder(t, gs, cancelled)
	stop := viewsLimit(t, 4, viewsA, 0, true, "61100", "0.1", 7)
	stop.Type, stop.Status, stop.TriggerPrice = perpstypes.OrderTypeStopLimit, perpstypes.OrderStatusUntriggered, viewsWord(t, "61000")
	viewsOrder(t, gs, stop)
	viewsOrder(t, gs, viewsLimit(t, 9, viewsA, 1, false, "3100", "1", 8))
}

func TestViewsGetOrderCompatTuple(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) { viewsOrderBook(t, gs) })
	env.expect("getOrder", []interface{}{viewsInt(1)}, viewsA, viewsInt(0), true, uint8(0), viewsFixed(t, "59000"),
		viewsFixed(t, "59000"), viewsFixed(t, "5946.02"), viewsFixed(t, "20"), env.usdl, viewsInt(297_301_000), true)
	env.expect("getOrder", []interface{}{viewsInt(2)}, viewsA, viewsInt(0), true, uint8(0), viewsFixed(t, "58000"),
		viewsFixed(t, "58000"), viewsFixed(t, "5800"), viewsFixed(t, "20"), env.usdl, viewsInt(290_000_000), false)
	env.expect("getOrder", []interface{}{viewsInt(4)}, viewsA, viewsInt(0), true, uint8(1), viewsFixed(t, "61000"),
		viewsFixed(t, "61100"), viewsFixed(t, "6110"), viewsFixed(t, "20"), env.usdl, viewsInt(305_500_000), true)
	env.expectRevert("getOrder", "PerpsUnknownOrder", viewsInt(77))
}

func TestViewsGetOrderDetails(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) { viewsOrderBook(t, gs) })
	env.expect("getOrderDetails", []interface{}{viewsInt(1)}, viewsOrderDetails{
		OrderId: viewsInt(1), User: viewsA, MarketId: viewsInt(0), IsLong: true, OrderType: 0, TimeInForce: 0, Status: 1,
		ReduceOnly: false, LimitPrice: viewsFixed(t, "59000"), TriggerPrice: viewsInt(0), AcceptablePrice: viewsInt(0),
		SizeUsd: viewsFixed(t, "5946.02"), SizeBase: viewsFixed(t, "0.10078"), FilledBase: viewsInt(0),
		Leverage: viewsFixed(t, "20"), Deadline: 0, Cloid: [16]byte{0xc1, 0x01}, Sequence: 5,
	})
	env.expectRevert("getOrderDetails", "PerpsUnknownOrder", viewsInt(77))
}

func TestViewsGetUserOrderIdsOpenOnly(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) { viewsOrderBook(t, gs) })
	env.expect("getUserOrderIds", []interface{}{viewsA}, []*big.Int{viewsInt(1), viewsInt(4), viewsInt(9)})
	env.expect("getUserOrderIds", []interface{}{viewsB}, []*big.Int{})
}

func TestViewsGetPositionCompatTuple(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) {
		viewsAccount(gs, viewsA, 100_000_000)
		viewsState(t, gs, perpstypes.MarketState{MarketID: 0, Fresh: true}, "61000")
		viewsPosition(t, gs, viewsLong(t, 7, viewsA, 0, true, "0.0165", "60000", "10"))
	})
	env.expect("getPosition", []interface{}{viewsInt(7)}, viewsA, viewsInt(0), true, viewsFixed(t, "1006.5"),
		viewsFixed(t, "116.5"), env.usdl, viewsInt(116_500_000), viewsFixed(t, "60000"), viewsInt(viewsNow-600), true)
	env.expectRevert("getPosition", "PerpsUnknownPosition", viewsInt(99))
}

func TestViewsGetPositionDetails(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) {
		viewsAccount(gs, viewsA, 1_000_000_000)
		viewsState(t, gs, perpstypes.MarketState{MarketID: 0, Fresh: true, FundingIndex: viewsWord(t, "10.75")}, "61000")
		position := viewsLong(t, 7, viewsA, 0, true, "1", "60000", "10")
		position.FundingBasis = viewsWord(t, "10")
		viewsPosition(t, gs, position)
		for _, leg := range []struct {
			id      uint64
			typ     perpstypes.OrderType
			trigger string
		}{{11, perpstypes.OrderTypeTakeProfit, "66000"}, {12, perpstypes.OrderTypeStopLoss, "57000"}} {
			viewsOrder(t, gs, perpstypes.Order{
				ID: viewsID(leg.id), Account: viewsA, MarketID: 0, IsLong: false, Type: leg.typ,
				TimeInForce: perpstypes.TimeInForceIOC, Status: perpstypes.OrderStatusUntriggered, ReduceOnly: true,
				TriggerPrice: viewsWord(t, leg.trigger), SizeBase: viewsWord(t, "1"), Leverage: viewsWord(t, "10"),
				Sequence: 20 + leg.id,
			})
		}
		gs.AttachedTpSls = append(gs.AttachedTpSls, perpstypes.AttachedTpSl{
			PositionID: viewsID(7), TakeProfitOrderID: viewsID(11), StopLossOrderID: viewsID(12),
		})
	})
	liquidation, err := env.keeper.LiquidationPrice(env.ctx, viewsID(7))
	require.NoError(t, err)
	require.True(t, liquidation.Sign() > 0)
	env.expect("getPositionDetails", []interface{}{viewsInt(7)}, viewsPositionDetails{
		PositionId: viewsInt(7), User: viewsA, MarketId: viewsInt(0), IsLong: true, SizeBase: viewsFixed(t, "1"),
		SizeUsdAtMark: viewsFixed(t, "61000"), EntryPrice: viewsFixed(t, "60000"), Leverage: viewsFixed(t, "10"),
		UnrealizedPnlUsd: viewsFixed(t, "1000"), FundingOwedUsd: viewsFixed(t, "-0.75"), LiquidationPrice: liquidation.ToBig(),
		TakeProfitPrice: viewsFixed(t, "66000"), StopLossPrice: viewsFixed(t, "57000"), OpenedAt: viewsInt(viewsNow - 600),
	})
	env.expectRevert("getPositionDetails", "PerpsUnknownPosition", viewsInt(99))
}

func viewsTwoPositions(t *testing.T) *viewsEnv {
	t.Helper()
	return newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) {
		viewsAccount(gs, viewsA, 1_000_000_000)
		viewsState(t, gs, perpstypes.MarketState{MarketID: 0, Fresh: true}, "60000")
		viewsState(t, gs, perpstypes.MarketState{MarketID: 1, Fresh: true}, "3000")
		viewsPosition(t, gs, viewsLong(t, 7, viewsA, 0, true, "0.1", "60000", "10"))
		viewsPosition(t, gs, viewsLong(t, 9, viewsA, 1, false, "1", "3000", "10"))
	})
}

func TestViewsGetUserPositionIds(t *testing.T) {
	env := viewsTwoPositions(t)
	env.expect("getUserPositionIds", []interface{}{viewsA}, []*big.Int{viewsInt(7), viewsInt(9)})
	env.expect("getUserPositionIds", []interface{}{viewsB}, []*big.Int{})
}

func TestViewsGetUserMarketPosition(t *testing.T) {
	env := viewsTwoPositions(t)
	env.expect("getUserMarketPosition", []interface{}{viewsA, viewsInt(0)}, viewsInt(7))
	env.expect("getUserMarketPosition", []interface{}{viewsA, viewsInt(1)}, viewsInt(9))
	env.expect("getUserMarketPosition", []interface{}{viewsA, viewsInt(2)}, viewsInt(0))
	env.expectRevert("getUserMarketPosition", "PerpsUnknownMarket", viewsA, viewsInt(18))
}

func TestViewsGetMarketCompatTuple(t *testing.T) {
	env := newViewsEnv(t, nil)
	env.expect("getMarket", []interface{}{viewsInt(0)}, "Bitcoin", "BTC", viewsInt(1000), viewsInt(5), viewsFixed(t, "500000000"), true)
	env.expect("getMarket", []interface{}{viewsInt(11)}, "Paxeer Network", "PAX", viewsInt(1000), viewsInt(5), viewsFixed(t, "2000000"), true)
	env.expectRevert("getMarket", "PerpsUnknownMarket", viewsInt(18))
}

func TestViewsGetMarketConfig(t *testing.T) {
	env := newViewsEnv(t, nil)
	env.expect("getMarketConfig", []interface{}{viewsInt(11)}, viewsMarketConfig{
		MarketId: viewsInt(11), Symbol: "PAX", Name: "Paxeer Network", OracleSymbol: "PAX", SzDecimals: 0, PxDecimals: 5,
		TickSize: viewsFixed(t, "0.00001"), LotSize: viewsFixed(t, "1"), MaxLeverage: viewsInt(1000),
		TierClass: uint8(perpstypes.TierClassE), ImpactNotionalUsd: viewsFixed(t, "6000"), OiCapUsd: viewsFixed(t, "2000000"),
		FundingCapPpmPerHour: viewsInt(40000), Enabled: true, ListedAt: viewsNow,
	})
	env.expectRevert("getMarketConfig", "PerpsUnknownMarket", viewsInt(18))
}

func TestViewsGetActiveMarketIds(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) {
		for i := range gs.Markets {
			if gs.Markets[i].ID == 16 {
				gs.Markets[i].Enabled = false
			}
		}
	})
	ids := []*big.Int{}
	for id := int64(0); id < 18; id++ {
		if id != 16 {
			ids = append(ids, viewsInt(id))
		}
	}
	env.expect("getActiveMarketIds", nil, ids)
	env.expect("totalMarkets", nil, viewsInt(18))
}

func TestViewsTotalMarketsIsEighteen(t *testing.T) {
	newViewsEnv(t, nil).expect("totalMarkets", nil, viewsInt(18))
}

func TestViewsIsMarketActiveNeedsFreshPrice(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) {
		viewsState(t, gs, perpstypes.MarketState{MarketID: 0, Fresh: true}, "60000")
		viewsState(t, gs, perpstypes.MarketState{MarketID: 5, Fresh: false}, "250")
		viewsState(t, gs, perpstypes.MarketState{MarketID: 16, Fresh: true}, "10")
		for i := range gs.Markets {
			if gs.Markets[i].ID == 16 {
				gs.Markets[i].Enabled = false
			}
		}
	})
	env.expect("isMarketActive", []interface{}{viewsInt(0)}, true)
	env.expect("isMarketActive", []interface{}{viewsInt(5)}, false)
	env.expect("isMarketActive", []interface{}{viewsInt(16)}, false)
	env.expect("isMarketActive", []interface{}{viewsInt(3)}, false)
	env.expect("isMarketActive", []interface{}{viewsInt(18)}, false)
}

func TestViewsGetFeesTierZero(t *testing.T) {
	newViewsEnv(t, nil).expect("getFees", nil, viewsInt(450), viewsInt(150), viewsInt(0), viewsInt(0))
}

func TestViewsGetFeeTierWithRebate(t *testing.T) {
	day := uint64(viewsNow / 86400)
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) {
		viewsAccount(gs, viewsA, 0)
		gs.VolumeBuckets = append(gs.VolumeBuckets,
			perpstypes.VolumeBucket{Account: viewsA, Day: day - 3, TakerVolumeUsd: viewsWord(t, "20000000")},
			perpstypes.VolumeBucket{Account: viewsA, Day: day, TakerVolumeUsd: viewsWord(t, "9000000"), MakerVolumeUsd: viewsWord(t, "1000000")})
		gs.VenueVolumeBuckets = append(gs.VenueVolumeBuckets, perpstypes.VenueVolumeBucket{Day: day, VolumeUsd: viewsWord(t, "50000000")})
	})
	env.expect("getFeeTier", []interface{}{viewsA}, uint8(2), viewsInt(350), viewsInt(-20), viewsFixed(t, "30000000"))
	env.expect("getFeeTier", []interface{}{viewsB}, uint8(0), viewsInt(450), viewsInt(150), viewsInt(0))
}

func TestViewsGetProtocolAccounts(t *testing.T) {
	env := newViewsEnv(t, nil)
	require.NotEqual(t, env.keeper.FeesAccountAddress(), env.keeper.InsuranceAccountAddress())
	env.expect("getProtocolAccounts", nil, env.keeper.FeesAccountAddress(), env.keeper.InsuranceAccountAddress())
}

func viewsMedianState(t *testing.T, gs *perpstypes.GenesisState) {
	t.Helper()
	viewsAccount(gs, viewsA, 1_000_000_000)
	viewsAccount(gs, viewsB, 1_000_000_000)
	viewsOrder(t, gs, viewsLimit(t, 1, viewsA, 0, true, "59990", "0.1", 1))
	viewsOrder(t, gs, viewsLimit(t, 2, viewsB, 0, false, "60010", "0.1", 2))
	index, pricefeedMark, last, basis := viewsWord(t, "60000"), viewsWord(t, "60030"), viewsWord(t, "60000"), viewsWord(t, "12")
	mark := perpskeeper.ComputeMark(index, basis, viewsWord(t, "59990"), viewsWord(t, "60010"), last, pricefeedMark)
	require.Equal(t, viewsFixed(t, "60012"), mark.ToBig())
	state := viewsState(t, gs, perpstypes.MarketState{MarketID: 0, Fresh: true, IndexPrice: index, PricefeedMark: pricefeedMark,
		PremiumEma: basis, LastTradePrice: last}, "60012")
	require.Equal(t, mark, state.MarkPrice)
	viewsState(t, gs, perpstypes.MarketState{MarketID: 1, Fresh: false, IndexPrice: viewsWord(t, "3000")}, "3001")
}

func TestViewsGetMarkPriceSingleOutput(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) { viewsMedianState(t, gs) })
	env.expect("getMarkPrice", []interface{}{viewsInt(0)}, viewsFixed(t, "60012"))
	env.expectRevert("getMarkPrice", "PerpsUnknownMarket", viewsInt(18))
	env.expectRevert("getMarkPrice", "PerpsPriceStale", viewsInt(3))
}

func TestViewsGetMarkPriceDetailsMedian(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) { viewsMedianState(t, gs) })
	env.expect("getMarkPriceDetails", []interface{}{viewsInt(0)},
		viewsFixed(t, "60012"), viewsFixed(t, "60000"), viewsFixed(t, "60000"), true)
	env.expect("getMarkPriceDetails", []interface{}{viewsInt(1)},
		viewsFixed(t, "3001"), viewsFixed(t, "3000"), viewsFixed(t, "3000"), false)
	env.expectRevert("getMarkPriceDetails", "PerpsUnknownMarket", viewsInt(18))
}

func TestViewsGetCurrentFundingRateBaseline(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) {
		viewsState(t, gs, perpstypes.MarketState{MarketID: 0, Fresh: true}, "60000")
		viewsState(t, gs, perpstypes.MarketState{MarketID: 1, Fresh: true, PremiumSum: viewsWord(t, "0.002"), PremiumCount: 2}, "3000")
		viewsState(t, gs, perpstypes.MarketState{MarketID: 2, Fresh: true, PremiumSum: viewsWord(t, "-0.002"), PremiumCount: 2}, "150")
	})
	env.expect("getCurrentFundingRate", []interface{}{viewsInt(0)}, viewsInt(3_472_222_222))
	env.expect("getCurrentFundingRate", []interface{}{viewsInt(1)}, viewsInt(17_361_111_111))
	env.expect("getCurrentFundingRate", []interface{}{viewsInt(2)}, viewsInt(-17_361_111_111))
	env.expectRevert("getCurrentFundingRate", "PerpsUnknownMarket", viewsInt(18))
}

func TestViewsGetFundingStateDiamondTuple(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) {
		viewsState(t, gs, perpstypes.MarketState{MarketID: 0, Fresh: true, FundingIndex: viewsWord(t, "10.75"),
			HourlyRate: viewsWord(t, "0.0000125")}, "60000")
		viewsState(t, gs, perpstypes.MarketState{MarketID: 1, Fresh: true, FundingIndex: viewsWord(t, "-2.5"),
			HourlyRate: viewsWord(t, "-0.0000125")}, "3000")
	})
	env.expect("getFundingState", []interface{}{viewsInt(0)},
		viewsFixed(t, "10.75"), viewsFixed(t, "-10.75"), viewsInt(1_800_000_000), viewsInt(3_472_222_222))
	env.expect("getFundingState", []interface{}{viewsInt(1)},
		viewsFixed(t, "-2.5"), viewsFixed(t, "2.5"), viewsInt(1_800_000_000), viewsInt(-3_472_222_222))
	env.expectRevert("getFundingState", "PerpsUnknownMarket", viewsInt(18))
}

func TestViewsGetFundingDetails(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) {
		viewsState(t, gs, perpstypes.MarketState{MarketID: 0, Fresh: true, FundingIndex: viewsWord(t, "10.75"),
			HourlyRate: viewsWord(t, "0.0000125"), PremiumSum: viewsWord(t, "-0.006"), PremiumCount: 300}, "60000")
	})
	env.expect("getFundingDetails", []interface{}{viewsInt(0)}, viewsFixed(t, "10.75"), viewsInt(1_800_000_000),
		viewsInt(12_500_000_000_000), viewsFixed(t, "-0.006"), viewsInt(300))
	env.expectRevert("getFundingDetails", "PerpsUnknownMarket", viewsInt(18))
}

func TestViewsGetPositionFundingSign(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) {
		viewsAccount(gs, viewsA, 1_000_000_000)
		viewsAccount(gs, viewsB, 1_000_000_000)
		viewsState(t, gs, perpstypes.MarketState{MarketID: 0, Fresh: true, FundingIndex: viewsWord(t, "10.75")}, "60000")
		long := viewsLong(t, 7, viewsA, 0, true, "1", "60000", "10")
		long.FundingBasis = viewsWord(t, "10")
		viewsPosition(t, gs, long)
		short := viewsLong(t, 8, viewsB, 0, false, "1", "60000", "10")
		short.FundingBasis = viewsWord(t, "10")
		viewsPosition(t, gs, short)
	})
	env.expect("getPositionFunding", []interface{}{viewsInt(7)}, viewsFixed(t, "-0.75"))
	env.expect("getPositionFunding", []interface{}{viewsInt(8)}, viewsFixed(t, "0.75"))
	env.expectRevert("getPositionFunding", "PerpsUnknownPosition", viewsInt(99))
}

func TestViewsGetOpenInterestAtMark(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) {
		viewsState(t, gs, perpstypes.MarketState{MarketID: 0, Fresh: true, LongOpenInterest: viewsWord(t, "2"),
			ShortOpenInterest: viewsWord(t, "2")}, "60000")
	})
	env.expect("getOpenInterest", []interface{}{viewsInt(0)}, viewsFixed(t, "120000"), viewsFixed(t, "120000"))
	env.expectRevert("getOpenInterest", "PerpsUnknownMarket", viewsInt(18))
}

func TestViewsGetLiquidationPriceFormula(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) {
		viewsAccount(gs, viewsA, 630_000_000)
		viewsState(t, gs, perpstypes.MarketState{MarketID: 0, Fresh: true}, "60000")
		viewsPosition(t, gs, viewsLong(t, 7, viewsA, 0, true, "1", "60000", "100"))
	})
	want, ok := new(big.Int).SetString("59399699849924962481240", 10)
	require.True(t, ok)
	env.expect("getLiquidationPrice", []interface{}{viewsInt(7)}, want)
	env.expectRevert("getLiquidationPrice", "PerpsUnknownPosition", viewsInt(99))
}

func TestViewsGetInsuranceBalance(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, k *perpskeeper.Keeper) {
		viewsAccountOf(t, gs, k.InsuranceAccountAddress()).Collateral = *uint256.NewInt(5_000_000_000)
	})
	env.expect("getInsuranceBalance", []interface{}{env.usdl}, viewsInt(5_000_000_000))
	env.expect("getInsuranceBalance", []interface{}{viewsC}, viewsInt(0))
}

func TestViewsGetADLRank(t *testing.T) {
	env := newViewsEnv(t, func(gs *perpstypes.GenesisState, _ *perpskeeper.Keeper) {
		viewsAccount(gs, viewsA, 1_000_000_000)
		viewsAccount(gs, viewsB, 6_000_000_000)
		viewsState(t, gs, perpstypes.MarketState{MarketID: 0, Fresh: true}, "60000")
		short := viewsLong(t, 9, viewsB, 0, false, "1", "66000", "10")
		short.ReferencePrice = viewsWord(t, "60000")
		score, profitable, err := perpskeeper.ADLRankScore(short, viewsWord(t, "60000"), viewsWord(t, "12000"))
		require.NoError(t, err)
		require.True(t, profitable)
		require.Equal(t, viewsFixed(t, "5.5"), score.ToBig())
		short.RankScore = score
		viewsPosition(t, gs, short)
		viewsPosition(t, gs, viewsLong(t, 7, viewsA, 0, true, "1", "61000", "10"))
	})
	equity, err := env.keeper.Equity(env.ctx, viewsB)
	require.NoError(t, err)
	require.Equal(t, viewsFixed(t, "12000"), equity.BigInt())
	env.expect("getADLRank", []interface{}{viewsInt(9)}, viewsFixed(t, "5.5"), true)
	env.expect("getADLRank", []interface{}{viewsInt(7)}, viewsInt(0), false)
	env.expectRevert("getADLRank", "PerpsUnknownPosition", viewsInt(99))
}
