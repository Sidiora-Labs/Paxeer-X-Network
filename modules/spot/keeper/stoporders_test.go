package keeper_test

import (
	"encoding/binary"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"math/big"
	"strconv"
	"testing"
	"time"

	feedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	feedtest "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	feedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

type stopFixture struct {
	t                         *testing.T
	ctx                       sdk.Context
	k                         *keeper.Keeper
	key, feedKey              sdk.StoreKey
	user, other, stable, base common.Address
	params                    types.Params
}

func stopTestKey(prefix []byte, ids ...uint64) []byte {
	key := append([]byte(nil), prefix...)
	for _, id := range ids {
		key = binary.BigEndian.AppendUint64(key, id)
	}
	return key
}

func stopTestAddress(value string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte(value)))
}

func stopTestPut(t *testing.T, store sdk.KVStore, key []byte, value interface{}) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	store.Set(key, encoded)
}

func stopTestPrice(value int64) sdk.Int { return sdk.NewInt(value).Mul(sdk.NewIntWithDecimal(1, 16)) }

func newStopFixture(t *testing.T) *stopFixture {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx(nil).WithBlockHeight(8).WithBlockTime(time.Unix(1000, 0).UTC()).WithEventManager(sdk.NewEventManager())
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	key, feedKey := sdk.NewKVStoreKey(types.StoreKey), sdk.NewKVStoreKey(feedtypes.StoreKey)
	ctx = feedtest.WithPriceFeedStore(ctx, feedKey)
	ctx = feedtest.WithPriceFeedStore(ctx, key)
	feed := feedkeeper.NewKeeper(feedKey, app.StakingKeeper, app.AccountKeeper, &app.EvmKeeper)
	k := keeper.NewKeeper(key, app.BankKeeper, &app.EvmKeeper, feed)
	f := &stopFixture{t: t, ctx: ctx, k: k, key: key, feedKey: feedKey, user: stopTestAddress("stop order user"), other: stopTestAddress("stop order other"), base: stopTestAddress("stop order native token")}

	source, err := parser.ParseFile(token.NewFileSet(), "../../launchpad/keeper/custody_test.go", nil, 0)
	require.NoError(t, err)
	var creation string
	ast.Inspect(source, func(n ast.Node) bool {
		declaration, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range declaration.Names {
			if name.Name != "sidioraERC20Creation" {
				continue
			}
			literal, ok := declaration.Values[i].(*ast.BasicLit)
			require.True(t, ok)
			creation, err = strconv.Unquote(literal.Value)
			require.NoError(t, err)
		}
		return true
	})
	require.NotEmpty(t, creation)
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	uintType, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(t, err)
	args, err := abi.Arguments{{Type: stringType}, {Type: stringType}, {Type: uintType}, {Type: addressType}}.Pack("Quote Token", "QUOTE", big.NewInt(2_000_000_000_000), f.user)
	require.NoError(t, err)
	nonce := app.EvmKeeper.GetNonce(ctx, f.user)
	_, err = app.EvmKeeper.CallEVM(ctx, f.user, nil, nil, append(common.FromHex(creation), args...))
	require.NoError(t, err)
	f.stable = crypto.CreateAddress(f.user, nonce)
	require.NotEmpty(t, app.EvmKeeper.GetCode(ctx, f.stable))
	require.NoError(t, app.EvmKeeper.SetERC20NativePointer(ctx, "uhpx", f.base))
	app.BankKeeper.SetDenomMetaData(ctx, banktypes.Metadata{Base: "uhpx", Display: "pax", Name: "Paxeer X Network", Symbol: "PAX", DenomUnits: []*banktypes.DenomUnit{{Denom: "uhpx", Exponent: 0}, {Denom: "pax", Exponent: 6}}})
	f.params = types.DefaultParams()
	f.params.OrderFeeBps = 10
	f.params.OrderTier1FeeBps = 20
	f.params.OrderTier2FeeBps = 50
	f.params.Tokens = []types.TokenParams{
		{Address: f.stable, Kind: types.TokenKindERC20, IsStablecoin: true, Decimals: 6, MaxUtilizationBps: 9000},
		{Address: f.base, Kind: types.TokenKindNative, MarketID: 11, Decimals: 6, MaxUtilizationBps: 9000},
	}
	f.setParams()
	f.setPrice(50)
	custody := f.k.CustodyEVMAddress(ctx)
	f.tokenCall("transfer(address,uint256)", custody, big.NewInt(500_000_000_000))
	f.tokenCall("approve(address,uint256)", common.HexToAddress(types.PECORStopOrdersAddress), big.NewInt(1_000_000_000_000))
	coins := sdk.NewCoins(sdk.NewInt64Coin("uhpx", 2_000_000_000_000))
	require.NoError(t, app.BankKeeper.MintCoins(ctx, "evm", coins))
	require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", app.EvmKeeper.GetPaxAddressOrDefault(ctx, f.user), coins))
	require.NoError(t, app.BankKeeper.SendCoins(ctx, app.EvmKeeper.GetPaxAddressOrDefault(ctx, f.user), keeper.CustodyAddress(), sdk.NewCoins(sdk.NewInt64Coin("uhpx", 1_000_000_000_000))))
	for _, entry := range []struct {
		token    common.Address
		reserves int64
	}{{f.stable, 500_000_000_000}, {f.base, 1_000_000_000_000}} {
		stopTestPut(t, ctx.KVStore(key), append(append([]byte(nil), types.BookPrefix...), entry.token.Bytes()...), types.TokenBook{Token: entry.token, Reserves: sdk.NewInt(entry.reserves), TotalDeposited: sdk.NewInt(entry.reserves), TotalWithdrawn: sdk.ZeroInt(), Decimals: 6, ShareDenom: "spot/share/" + common.Bytes2Hex(entry.token.Bytes()), MinimumSharesLocked: true})
	}
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	return f
}

func (f *stopFixture) setParams() {
	f.t.Helper()
	require.NoError(f.t, f.params.Validate())
	stopTestPut(f.t, f.ctx.KVStore(f.key), types.ParamsKey, f.params)
}

func (f *stopFixture) setPrice(value int64) {
	f.t.Helper()
	price := stopTestPrice(value)
	stopTestPut(f.t, f.ctx.KVStore(f.feedKey), feedtypes.AggregateKey(11), feedtypes.Aggregate{IndexPrice: price, MarkPrice: price, Basis: sdk.ZeroInt(), TimestampMs: uint64(f.ctx.BlockTime().UnixMilli()), Height: f.ctx.BlockHeight(), Sources: 4})
}

func (f *stopFixture) tokenCall(signature string, address common.Address, amount *big.Int) {
	f.t.Helper()
	data := append(crypto.Keccak256([]byte(signature))[:4], common.LeftPadBytes(address.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(amount.Bytes(), 32)...)
	result, err := testkeeper.EVMTestApp.EvmKeeper.CallEVM(f.ctx, f.user, &f.stable, nil, data)
	require.NoError(f.t, err)
	require.Equal(f.t, common.LeftPadBytes(big.NewInt(1).Bytes(), 32), result)
}

func (f *stopFixture) quoteBalance() sdk.Int {
	f.t.Helper()
	data := append(crypto.Keccak256([]byte("balanceOf(address)"))[:4], common.LeftPadBytes(f.user.Bytes(), 32)...)
	result, err := testkeeper.EVMTestApp.EvmKeeper.StaticCallEVM(f.ctx, keeper.CustodyAddress(), &f.stable, data)
	require.NoError(f.t, err)
	require.Len(f.t, result, 32)
	return sdk.NewIntFromBigInt(new(big.Int).SetBytes(result))
}

func (f *stopFixture) baseBalance() sdk.Int {
	app := testkeeper.EVMTestApp
	return app.BankKeeper.GetBalance(f.ctx, app.EvmKeeper.GetPaxAddressOrDefault(f.ctx, f.user), "uhpx").Amount
}

func (f *stopFixture) event(name string) sdk.Event {
	f.t.Helper()
	var found []sdk.Event
	for _, event := range f.ctx.EventManager().Events() {
		if event.Type == name {
			found = append(found, event)
		}
	}
	require.Len(f.t, found, 1)
	return found[0]
}

func (f *stopFixture) field(event sdk.Event, name, value string) {
	f.t.Helper()
	for _, attribute := range event.Attributes {
		if string(attribute.Key) == name {
			require.Equal(f.t, value, string(attribute.Value))
			return
		}
	}
	f.t.Fatalf("missing event attribute %s", name)
}

func (f *stopFixture) assertEscrow(token common.Address, expected string) {
	f.t.Helper()
	raw := f.ctx.KVStore(f.key).Get(append(append([]byte(nil), types.EscrowPrefix...), token.Bytes()...))
	var escrow types.TokenEscrow
	require.NoError(f.t, json.Unmarshal(raw, &escrow))
	require.Equal(f.t, expected, escrow.Amount.String())
}

func TestStopLimitActivatesThenFills(t *testing.T) {
	f := newStopFixture(t)
	before := f.baseBalance()
	quoteBefore := f.quoteBalance()
	id, err := f.k.PlaceStopLimit(f.ctx, f.user, f.stable, f.base, sdk.NewInt(100_000_000), stopTestPrice(55), stopTestPrice(60), 0, types.OrderTypeStopLimitBuy)
	require.NoError(t, err)
	require.Equal(t, uint64(1), id)
	require.Equal(t, "100000000", quoteBefore.Sub(f.quoteBalance()).String())
	f.assertEscrow(f.stable, "100000000")
	f.field(f.event("StopLimitOrderCreated"), "rail", types.PECORStopOrdersAddress)
	require.ErrorIs(t, f.k.ExecuteStopLimit(f.ctx, id), types.ErrNotActivated)
	activated, err := f.k.ActivateStopLimit(f.ctx, id)
	require.NoError(t, err)
	require.False(t, activated)
	f.setPrice(56)
	activated, err = f.k.ActivateStopLimit(f.ctx, id)
	require.NoError(t, err)
	require.True(t, activated)
	order, err := f.k.GetStopLimitOrder(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActivated, order.Status)
	f.field(f.event("StopLimitActivated"), "activationPrice", "560000000000000000")
	require.NoError(t, f.k.ExecuteStopLimit(f.ctx, id))
	order, err = f.k.GetStopLimitOrder(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusExecuted, order.Status)
	require.Equal(t, "178392857", f.baseBalance().Sub(before).String())
	f.assertEscrow(f.stable, "0")
	count, err := f.k.GetActiveStopLimitOrderCount(f.ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	f.field(f.event("StopLimitExecuted"), "executionPrice", "560000000000000000")
	require.ErrorIs(t, f.k.ExecuteStopLimit(f.ctx, id), types.ErrNotActivated)
	require.ErrorIs(t, f.k.CancelStopLimit(f.ctx, f.user, id), types.ErrCannotCancel)
}

func TestActivatedWaitsForLimit(t *testing.T) {
	f := newStopFixture(t)
	id, err := f.k.PlaceStopLimit(f.ctx, f.user, f.stable, f.base, sdk.NewInt(1_000_000_000), stopTestPrice(45), stopTestPrice(40), 0, types.OrderTypeStopLimitSell)
	require.NoError(t, err)
	f.setPrice(39)
	activated, err := f.k.ActivateStopLimit(f.ctx, id)
	require.NoError(t, err)
	require.True(t, activated)
	before := f.quoteBalance()
	events := len(f.ctx.EventManager().Events())
	require.ErrorIs(t, f.k.ExecuteStopLimit(f.ctx, id), types.ErrPriceBelowLimit)
	require.Equal(t, events, len(f.ctx.EventManager().Events()))
	require.Equal(t, before.String(), f.quoteBalance().String())
	order, err := f.k.GetStopLimitOrder(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusActivated, order.Status)
	f.assertEscrow(f.base, "1000000000")
	f.setPrice(40)
	require.NoError(t, f.k.ExecuteStopLimit(f.ctx, id))
	require.Equal(t, "399600000", f.quoteBalance().Sub(before).String())

	t.Run("batch preserves activation on failed fill", func(t *testing.T) {
		f := newStopFixture(t)
		id, err := f.k.PlaceStopLimit(f.ctx, f.user, f.stable, f.base, sdk.NewInt(100_000_000), stopTestPrice(55), stopTestPrice(60), 0, types.OrderTypeStopLimitBuy)
		require.NoError(t, err)
		f.setPrice(61)
		activated, executed, err := f.k.BatchCheckAndExecuteStopLimits(f.ctx, f.other, []uint64{id, id, 999})
		require.NoError(t, err)
		require.Equal(t, uint64(1), activated)
		require.Zero(t, executed)
		require.ErrorIs(t, f.k.ExecuteStopLimit(f.ctx, id), types.ErrPriceAboveLimit)
		require.ErrorIs(t, f.k.CancelStopLimit(f.ctx, f.other, id), types.ErrNotOrderOwner)
		f.params.OrdersPaused = true
		f.setParams()
		require.NoError(t, f.k.CancelStopLimit(f.ctx, f.user, id))
		order, err := f.k.GetStopLimitOrder(f.ctx, id)
		require.NoError(t, err)
		require.Equal(t, types.OrderStatusCancelled, order.Status)
	})
}

func TestStopLimitPriceRange(t *testing.T) {
	cases := []struct {
		name        string
		kind        types.OrderType
		stop, limit sdk.Int
		expected    error
	}{
		{"sell limit above stop", types.OrderTypeStopLimitSell, stopTestPrice(45), stopTestPrice(50), types.ErrLimitMustBeBelowStopSell},
		{"buy limit below stop", types.OrderTypeStopLimitBuy, stopTestPrice(55), stopTestPrice(50), types.ErrLimitMustBeAboveStopBuy},
		{"zero stop", types.OrderTypeStopLimitBuy, sdk.ZeroInt(), stopTestPrice(60), types.ErrInvalidPrices},
		{"zero limit", types.OrderTypeStopLimitSell, stopTestPrice(45), sdk.ZeroInt(), types.ErrInvalidPrices},
		{"nil stop", types.OrderTypeStopLimitBuy, sdk.Int{}, stopTestPrice(60), types.ErrInvalidPrices},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newStopFixture(t)
			baseBefore, quoteBefore := f.baseBalance(), f.quoteBalance()
			id, err := f.k.PlaceStopLimit(f.ctx, f.user, f.stable, f.base, sdk.NewInt(1_000_000_000), tc.stop, tc.limit, 0, tc.kind)
			require.ErrorIs(t, err, tc.expected)
			require.Zero(t, id)
			require.Equal(t, baseBefore.String(), f.baseBalance().String())
			require.Equal(t, quoteBefore.String(), f.quoteBalance().String())
			require.Empty(t, f.ctx.EventManager().Events())
			require.Nil(t, f.ctx.KVStore(f.key).Get(types.NextOrderIDKey))
		})
	}
	t.Run("placement bounds", func(t *testing.T) {
		f := newStopFixture(t)
		f.params.MaxActiveOrdersPerUser = 1
		f.setParams()
		_, err := f.k.PlaceStopLoss(f.ctx, f.user, f.base, f.stable, sdk.NewInt(1_000_000), stopTestPrice(40), 0)
		require.ErrorIs(t, err, types.ErrOrderBelowMinimumNotional)
		_, err = f.k.PlaceStopLimit(f.ctx, f.user, f.stable, f.base, sdk.NewInt(100_000_000), stopTestPrice(55), stopTestPrice(60), uint64(f.ctx.BlockTime().Unix()), types.OrderTypeStopLimitBuy)
		require.ErrorIs(t, err, types.ErrInvalidExpiry)
		_, err = f.k.PlaceStopLoss(f.ctx, f.user, f.base, f.stable, sdk.NewInt(1_000_000_000), stopTestPrice(40), 0)
		require.NoError(t, err)
		before := f.quoteBalance()
		_, err = f.k.PlaceStopLimit(f.ctx, f.user, f.stable, f.base, sdk.NewInt(100_000_000), stopTestPrice(55), stopTestPrice(60), 0, types.OrderTypeStopLimitBuy)
		require.ErrorIs(t, err, types.ErrTooManyActiveOrders)
		require.Equal(t, before.String(), f.quoteBalance().String())
	})
}

func stopTestViews(t *testing.T) {
	f := newStopFixture(t)
	f.params.MaxActiveOrdersPerUser = 150
	f.setParams()
	for i := 0; i < 120; i++ {
		_, err := f.k.PlaceStopLimit(f.ctx, f.user, f.stable, f.base, sdk.NewInt(100_000_000), stopTestPrice(55), stopTestPrice(60), 0, types.OrderTypeStopLimitBuy)
		require.NoError(t, err)
	}
	f.setPrice(56)
	ids, err := f.k.GetActivatableStopLimits(f.ctx, 200)
	require.NoError(t, err)
	require.Len(t, ids, 100)
	for i, id := range ids {
		require.Equal(t, uint64(i+1), id)
	}
	empty, err := f.k.GetActivatableStopLimits(f.ctx, 0)
	require.NoError(t, err)
	require.Empty(t, empty)
	for _, id := range ids {
		changed, err := f.k.ActivateStopLimit(f.ctx, id)
		require.NoError(t, err)
		require.True(t, changed)
	}
	executable, err := f.k.GetExecutableStopLimits(f.ctx, 200)
	require.NoError(t, err)
	require.Equal(t, ids, executable)
	f.setPrice(61)
	executable, err = f.k.GetExecutableStopLimits(f.ctx, 200)
	require.NoError(t, err)
	require.Empty(t, executable)
	f.params.KeeperOpen = false
	f.setParams()
	_, _, err = f.k.BatchCheckAndExecuteStopLimits(f.ctx, f.other, []uint64{1})
	require.ErrorIs(t, err, types.ErrNotAuthorized)
	_, err = f.k.BatchExecuteStopLossOrders(f.ctx, f.other, []uint64{1})
	require.ErrorIs(t, err, types.ErrNotAuthorized)
}

func TestStopLossRecord(t *testing.T) {
	t.Run("stop-limit discovery", stopTestViews)
	f := newStopFixture(t)
	var ids []uint64
	for i := 0; i < 3; i++ {
		id, err := f.k.PlaceStopLoss(f.ctx, f.user, f.base, f.stable, sdk.NewInt(1_000_000_000), stopTestPrice(40), 0)
		require.NoError(t, err)
		ids = append(ids, id)
	}
	require.Equal(t, []uint64{1, 2, 3}, ids)
	order, err := f.k.GetStopLossOrder(f.ctx, ids[0])
	require.NoError(t, err)
	require.Equal(t, types.StopLossOrder{ID: 1, User: f.user, Stablecoin: f.stable, Token: f.base, Amount: sdk.NewInt(1_000_000_000), TriggerPrice: stopTestPrice(40), Status: types.OrderStatusPending, CreatedAt: uint64(f.ctx.BlockTime().Unix())}, order)
	require.NotNil(t, f.ctx.KVStore(f.key).Get(stopTestKey(types.StopLossOrderPrefix, ids[0])))
	require.Nil(t, f.ctx.KVStore(f.key).Get(stopTestKey(types.LimitOrderPrefix, ids[0])))
	require.Nil(t, f.ctx.KVStore(f.key).Get(stopTestKey(types.ActiveLimitPrefix, ids[0])))
	require.ErrorIs(t, f.k.CancelStopLoss(f.ctx, f.other, ids[1]), types.ErrNotOrderOwner)
	require.NoError(t, f.k.CancelStopLoss(f.ctx, f.user, ids[1]))
	count, err := f.k.GetActiveStopLossOrderCount(f.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), count)
	first, err := f.k.ActiveStopLossOrderIDs(f.ctx, 0)
	require.NoError(t, err)
	last, err := f.k.ActiveStopLossOrderIDs(f.ctx, 1)
	require.NoError(t, err)
	require.Equal(t, []uint64{1, 3}, []uint64{first, last})
	history, err := f.k.GetUserStopLossOrders(f.ctx, f.user)
	require.NoError(t, err)
	require.Equal(t, ids, history)
	limitID, err := f.k.PlaceStopLimit(f.ctx, f.user, f.stable, f.base, sdk.NewInt(100_000_000), stopTestPrice(55), stopTestPrice(60), 0, types.OrderTypeStopLimitBuy)
	require.NoError(t, err)
	require.Equal(t, uint64(4), limitID)
	zero, err := f.k.GetStopLossOrder(f.ctx, limitID)
	require.NoError(t, err)
	require.Equal(t, types.StopLossOrder{Amount: sdk.ZeroInt(), TriggerPrice: sdk.ZeroInt()}, zero)
	require.Equal(t, uint64(5), binary.BigEndian.Uint64(f.ctx.KVStore(f.key).Get(types.NextOrderIDKey)))
	require.ErrorIs(t, f.k.CancelStopLoss(f.ctx, f.user, ids[1]), types.ErrNotPending)
}

func TestStopLossFillsAtTrigger(t *testing.T) {
	f := newStopFixture(t)
	id, err := f.k.PlaceStopLoss(f.ctx, f.user, f.base, f.stable, sdk.NewInt(1_000_000_000), stopTestPrice(40), 0)
	require.NoError(t, err)
	before := f.quoteBalance()
	require.ErrorIs(t, f.k.ExecuteStopLoss(f.ctx, id), types.ErrPriceAboveTrigger)
	f.setPrice(40)
	require.NoError(t, f.k.ExecuteStopLoss(f.ctx, id))
	require.Equal(t, "399600000", f.quoteBalance().Sub(before).String())
	order, err := f.k.GetStopLossOrder(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusExecuted, order.Status)
	f.assertEscrow(f.base, "0")
	f.field(f.event("StopLossOrderExecuted"), "executionPrice", "400000000000000000")
	require.ErrorIs(t, f.k.ExecuteStopLoss(f.ctx, id), types.ErrNotPending)
	require.ErrorIs(t, f.k.CancelStopLoss(f.ctx, f.user, id), types.ErrNotPending)

	t.Run("failed output leaves all state unchanged", func(t *testing.T) {
		f := newStopFixture(t)
		id, err := f.k.PlaceStopLoss(f.ctx, f.user, f.base, f.stable, sdk.NewInt(1_000_000_000), stopTestPrice(40), 0)
		require.NoError(t, err)
		f.setPrice(40)
		stopTestPut(t, f.ctx.KVStore(f.key), stopTestKey(types.OrderMinOutputPrefix, id), types.OrderMinOutput{OrderID: id, MinOutput: sdk.NewInt(400_000_000)})
		before := f.quoteBalance()
		events := len(f.ctx.EventManager().Events())
		require.ErrorIs(t, f.k.ExecuteStopLoss(f.ctx, id), types.ErrInsufficientOutput)
		require.Equal(t, before.String(), f.quoteBalance().String())
		require.Equal(t, events, len(f.ctx.EventManager().Events()))
		f.assertEscrow(f.base, "1000000000")
		order, err := f.k.GetStopLossOrder(f.ctx, id)
		require.NoError(t, err)
		require.Equal(t, types.OrderStatusPending, order.Status)
		second, err := f.k.PlaceStopLoss(f.ctx, f.user, f.base, f.stable, sdk.NewInt(1_000_000_000), stopTestPrice(40), 0)
		require.NoError(t, err)
		filled, err := f.k.BatchExecuteStopLossOrders(f.ctx, f.other, []uint64{id, second, second, 999})
		require.NoError(t, err)
		require.Equal(t, uint64(1), filled)
		require.Equal(t, "399600000", f.quoteBalance().Sub(before).String())
	})
}

func TestStopLossExpiry(t *testing.T) {
	f := newStopFixture(t)
	expiry := uint64(f.ctx.BlockTime().Unix()) + 10
	before := f.baseBalance()
	id, err := f.k.PlaceStopLoss(f.ctx, f.user, f.base, f.stable, sdk.NewInt(1_000_000_000), stopTestPrice(40), expiry)
	require.NoError(t, err)
	require.ErrorIs(t, f.k.ExpireStopLoss(f.ctx, id), types.ErrInvalidExpiry)
	stopTestPut(t, f.ctx.KVStore(f.key), stopTestKey(types.FillBackoffPrefix, id), types.FillBackoff{OrderID: id, Failures: 1, NextRetryHeight: 10})
	f.ctx = f.ctx.WithBlockTime(time.Unix(int64(expiry), 0))
	require.ErrorIs(t, f.k.ExecuteStopLoss(f.ctx, id), types.ErrOrderExpired)
	require.NoError(t, f.k.ExpireStopLoss(f.ctx, id))
	order, err := f.k.GetStopLossOrder(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.OrderStatusExpired, order.Status)
	require.Equal(t, before.String(), f.baseBalance().String())
	f.assertEscrow(f.base, "0")
	require.Nil(t, f.ctx.KVStore(f.key).Get(stopTestKey(types.ExpiryIndexPrefix, expiry, id)))
	require.Nil(t, f.ctx.KVStore(f.key).Get(stopTestKey(types.FillBackoffPrefix, id)))
	count, err := f.k.GetActiveStopLossOrderCount(f.ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	f.field(f.event("StopLossOrderCancelled"), "orderId", "1")
	require.ErrorIs(t, f.k.ExpireStopLoss(f.ctx, id), types.ErrNotPending)

	t.Run("activated stop-limit expiry", func(t *testing.T) {
		f := newStopFixture(t)
		expiry := uint64(f.ctx.BlockTime().Unix()) + 10
		before := f.quoteBalance()
		id, err := f.k.PlaceStopLimit(f.ctx, f.user, f.stable, f.base, sdk.NewInt(100_000_000), stopTestPrice(55), stopTestPrice(60), expiry, types.OrderTypeStopLimitBuy)
		require.NoError(t, err)
		f.setPrice(61)
		activated, err := f.k.ActivateStopLimit(f.ctx, id)
		require.NoError(t, err)
		require.True(t, activated)
		f.ctx = f.ctx.WithBlockTime(time.Unix(int64(expiry), 0))
		require.ErrorIs(t, f.k.ExecuteStopLimit(f.ctx, id), types.ErrOrderExpired)
		require.NoError(t, f.k.ExpireStopLimit(f.ctx, id))
		order, err := f.k.GetStopLimitOrder(f.ctx, id)
		require.NoError(t, err)
		require.Equal(t, types.OrderStatusExpired, order.Status)
		require.Equal(t, before.String(), f.quoteBalance().String())
		f.field(f.event("StopLimitCancelled"), "orderId", "1")
	})
}
