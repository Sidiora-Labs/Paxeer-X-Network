package keeper_test

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/big"
	"testing"
	"time"

	minttypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/mint/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtest "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

type limitEnv struct {
	t                   *testing.T
	ctx                 sdk.Context
	k                   *keeper.Keeper
	key, priceKey       sdk.StoreKey
	user, stable, token common.Address
}

func limitKey(prefix []byte, parts ...[]byte) []byte {
	key := append([]byte(nil), prefix...)
	for _, part := range parts {
		key = append(key, part...)
	}
	return key
}

func limitID(id uint64) []byte   { return binary.BigEndian.AppendUint64(nil, id) }
func limitInt(n int64) sdk.Int   { return sdk.NewInt(n) }
func limitPrice(n int64) sdk.Int { return sdk.NewInt(n).MulRaw(1_000_000_000_000_000) }
func limitAddress(label string) common.Address {
	return common.BytesToAddress(crypto.Keccak256([]byte(label)))
}
func limitWord(n int64) []byte { return common.LeftPadBytes(big.NewInt(n).Bytes(), 32) }

func (e *limitEnv) put(key []byte, value interface{}) {
	e.t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(e.t, err)
	e.ctx.KVStore(e.key).Set(key, raw)
}

func (e *limitEnv) price(price sdk.Int) {
	e.t.Helper()
	raw, err := json.Marshal(pricefeedtypes.Aggregate{IndexPrice: price, MarkPrice: price, Basis: sdk.ZeroInt(),
		TimestampMs: uint64(e.ctx.BlockTime().UnixMilli()), Height: e.ctx.BlockHeight(), Sources: 1})
	require.NoError(e.t, err)
	e.ctx.KVStore(e.priceKey).Set(pricefeedtypes.AggregateKey(types.PAXMarketID), raw)
}

func (e *limitEnv) tokenCall(from common.Address, signature string, words ...[]byte) {
	e.t.Helper()
	input := crypto.Keccak256([]byte(signature))[:4]
	for _, word := range words {
		input = append(input, word...)
	}
	output, err := testkeeper.EVMTestApp.EvmKeeper.CallEVM(e.ctx, from, &e.stable, nil, input)
	require.NoError(e.t, err)
	require.Equal(e.t, limitWord(1), output)
}

func (e *limitEnv) balance(token, user common.Address) sdk.Int {
	e.t.Helper()
	app := testkeeper.EVMTestApp
	if token == e.token {
		return app.BankKeeper.GetBalance(e.ctx, app.EvmKeeper.GetPaxAddressOrDefault(e.ctx, user), "uhpx").Amount
	}
	input := append(crypto.Keccak256([]byte("balanceOf(address)"))[:4], common.LeftPadBytes(user.Bytes(), 32)...)
	raw, err := app.EvmKeeper.StaticCallEVM(e.ctx, keeper.CustodyAddress(), &token, input)
	require.NoError(e.t, err)
	require.Len(e.t, raw, 32)
	return sdk.NewIntFromBigInt(new(big.Int).SetBytes(raw))
}

func newLimitEnv(t *testing.T) *limitEnv {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx(nil).WithBlockTime(time.Unix(1000, 0).UTC()).WithBlockHeight(10)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	key, priceKey := sdk.NewKVStoreKey(types.StoreKey), sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	ctx = pricefeedtest.WithPriceFeedStore(pricefeedtest.WithPriceFeedStore(ctx, priceKey), key)
	feed := pricefeedkeeper.NewKeeper(priceKey, app.StakingKeeper, app.AccountKeeper, &app.EvmKeeper)
	require.NoError(t, feed.SetParams(ctx, pricefeedtypes.DefaultParams(pricefeedtypes.DefaultAuthority())))
	k := keeper.NewKeeper(key, app.BankKeeper, &app.EvmKeeper, feed)
	e := &limitEnv{t: t, ctx: ctx, k: k, key: key, priceKey: priceKey, user: limitAddress("limit owner"), token: limitAddress("native pointer")}
	app.BankKeeper.SetDenomMetaData(ctx, banktypes.Metadata{Base: "uhpx", Display: "PAX", DenomUnits: []*banktypes.DenomUnit{{Denom: "uhpx", Exponent: 0}, {Denom: "PAX", Exponent: 6}}})
	require.NoError(t, app.EvmKeeper.SetERC20NativePointer(ctx, "uhpx", e.token))
	coin := sdk.NewCoins(sdk.NewInt64Coin("uhpx", 2_000_000_000_000))
	require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, coin))
	require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, app.EvmKeeper.GetPaxAddressOrDefault(ctx, e.user), coin))
	require.NoError(t, app.BankKeeper.SendCoins(ctx, app.EvmKeeper.GetPaxAddressOrDefault(ctx, e.user), keeper.CustodyAddress(), sdk.NewCoins(sdk.NewInt64Coin("uhpx", 1_000_000_000_000))))
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	intType, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	addressType, err := abi.NewType("address", "", nil)
	require.NoError(t, err)
	args, err := (abi.Arguments{{Type: stringType}, {Type: stringType}, {Type: intType}, {Type: addressType}}).Pack("USD Ledger", "USDL", big.NewInt(1_500_000_000_000), e.user)
	require.NoError(t, err)
	deployer := limitAddress("limit token deployer")
	nonce := app.EvmKeeper.GetNonce(ctx, deployer)
	_, err = app.EvmKeeper.CallEVM(ctx, deployer, nil, nil, append(common.FromHex(limitTokenCreation), args...))
	require.NoError(t, err)
	e.stable = crypto.CreateAddress(deployer, nonce)
	require.NotEmpty(t, app.EvmKeeper.GetCode(ctx, e.stable))
	e.tokenCall(e.user, "transfer(address,uint256)", common.LeftPadBytes(k.CustodyEVMAddress(ctx).Bytes(), 32), limitWord(500_000_000_000))
	e.tokenCall(e.user, "approve(address,uint256)", common.LeftPadBytes(common.HexToAddress(types.PECOROrdersAddress).Bytes(), 32), limitWord(1_000_000_000_000))
	params := types.DefaultParams()
	params.SwapFeeBps, params.Tier1FeeBps, params.Tier2FeeBps = 30, 20, 50
	params.OrderFeeBps, params.OrderTier1FeeBps, params.OrderTier2FeeBps = 10, 20, 50
	params.Tokens = []types.TokenParams{
		{Address: e.stable, Kind: types.TokenKindERC20, IsStablecoin: true, Decimals: 6, MaxUtilizationBps: 9000},
		{Address: e.token, Kind: types.TokenKindNative, MarketID: types.PAXMarketID, Decimals: 6, MaxUtilizationBps: 9000},
	}
	require.NoError(t, params.Validate())
	e.put(types.ParamsKey, params)
	for _, entry := range []struct {
		token    common.Address
		reserves int64
	}{{e.stable, 500_000_000_000}, {e.token, 1_000_000_000_000}} {
		e.put(limitKey(types.BookPrefix, entry.token.Bytes()), types.TokenBook{Token: entry.token, Reserves: limitInt(entry.reserves), TotalDeposited: limitInt(entry.reserves), TotalWithdrawn: sdk.ZeroInt(), Decimals: 6, ShareDenom: "spot/share/" + common.Bytes2Hex(entry.token.Bytes()), MinimumSharesLocked: true})
	}
	e.price(limitPrice(500))
	return e
}

func (e *limitEnv) place(kind types.OrderType, amount, target int64, expiry uint64) uint64 {
	e.t.Helper()
	id, events, err := e.k.PlaceLimit(e.ctx, e.user, e.stable, e.token, limitInt(amount), limitPrice(target), expiry, kind)
	require.NoError(e.t, err)
	require.Equal(e.t, []keeper.LimitOrderEvent{{Address: common.HexToAddress(types.PECOROrdersAddress), Name: "LimitOrderCreated", Args: []interface{}{new(big.Int).SetUint64(id), e.user, uint8(kind), big.NewInt(amount), limitPrice(target).BigInt()}}}, events)
	return id
}

func (e *limitEnv) order(id uint64) types.LimitOrder {
	e.t.Helper()
	order, err := e.k.GetLimitOrder(e.ctx, id)
	require.NoError(e.t, err)
	return order
}

func (e *limitEnv) snapshot() map[string]string {
	e.t.Helper()
	out := map[string]string{}
	it := e.ctx.KVStore(e.key).Iterator(nil, nil)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		out[string(it.Key())] = string(it.Value())
	}
	for _, token := range []common.Address{e.stable, e.token} {
		out[token.Hex()+"/user"] = e.balance(token, e.user).String()
		out[token.Hex()+"/custody"] = e.balance(token, e.k.CustodyEVMAddress(e.ctx)).String()
	}
	return out
}

func limitError(t *testing.T, err error, expected *types.AbiError) {
	t.Helper()
	var actual *types.AbiError
	require.True(t, errors.As(err, &actual), "expected ABI error, got %v", err)
	require.Equal(t, expected.Data(), actual.Data())
}

func TestPlaceLimitEscrows(t *testing.T) {
	e := newLimitEnv(t)
	id := e.place(types.OrderTypeLimitBuy, 100_000_000, 500, 1100)
	require.Equal(t, uint64(1), id)
	require.Equal(t, types.LimitOrder{ID: 1, User: e.user, Stablecoin: e.stable, Token: e.token, Amount: limitInt(100_000_000), TargetPrice: limitPrice(500), OrderType: types.OrderTypeLimitBuy, Status: types.OrderStatusPending, CreatedAt: 1000, ExpiresAt: 1100}, e.order(id))
	require.Equal(t, "999900000000", e.balance(e.stable, e.user).String())
	escrow, err := e.k.GetEscrow(e.ctx, e.stable)
	require.NoError(t, err)
	require.Equal(t, "100000000", escrow.String())
	minimum, err := e.k.GetOrderMinOutput(e.ctx, id)
	require.NoError(t, err)
	require.Equal(t, "0", minimum.String())
	count, err := e.k.GetActiveOrderCount(e.ctx, e.user)
	require.NoError(t, err)
	require.Equal(t, uint64(1), count)
	require.True(t, e.ctx.KVStore(e.key).Has(limitKey(types.ExpiryIndexPrefix, limitID(1100), limitID(1))))
	require.Equal(t, uint64(2), e.place(types.OrderTypeLimitSell, 1_000_000_000, 550, 0))
	require.Equal(t, "999000000000", e.balance(e.token, e.user).String())
	cases := []struct {
		name          string
		amount, price sdk.Int
		expiry        uint64
		kind          types.OrderType
		want          *types.AbiError
	}{
		{"zero amount", sdk.ZeroInt(), limitPrice(500), 0, types.OrderTypeLimitBuy, types.ErrZeroAmount},
		{"negative amount", limitInt(-1), limitPrice(500), 0, types.OrderTypeLimitBuy, types.ErrZeroAmount},
		{"zero price", limitInt(100_000_000), sdk.ZeroInt(), 0, types.OrderTypeLimitBuy, types.ErrInvalidPrice},
		{"expiry boundary", limitInt(100_000_000), limitPrice(500), 1000, types.OrderTypeLimitBuy, types.ErrInvalidExpiry},
		{"stop loss kind", limitInt(100_000_000), limitPrice(500), 0, types.OrderTypeStopLoss, types.ErrOrderNotPending},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := e.snapshot()
			id, events, err := e.k.PlaceLimit(e.ctx, e.user, e.stable, e.token, tc.amount, tc.price, tc.expiry, tc.kind)
			limitError(t, err, tc.want)
			require.Zero(t, id)
			require.Empty(t, events)
			require.Equal(t, before, e.snapshot())
		})
	}
}

func TestLimitStatusTransitions(t *testing.T) {
	t.Run("batch isolation", limitBatchIsolation)
	t.Run("failed payout", limitFailedPayout)
	t.Run("modify refusals", limitModifyRefusals)
	e := newLimitEnv(t)
	id := e.place(types.OrderTypeLimitBuy, 100_000_000, 400, 1100)
	before := e.snapshot()
	events, err := e.k.CancelLimit(e.ctx, limitAddress("other owner"), id)
	limitError(t, err, types.ErrNotOrderOwner)
	require.Empty(t, events)
	require.Equal(t, before, e.snapshot())
	events, err = e.k.ExecuteLimit(e.ctx, id)
	limitError(t, err, types.ErrPriceAboveTarget)
	require.Empty(t, events)
	require.Equal(t, before, e.snapshot())
	ok, reason, err := e.k.CanExecuteLimitOrder(e.ctx, id)
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, "Price above target", reason)
	events, err = e.k.ModifyLimit(e.ctx, e.user, id, limitPrice(550), 1200)
	require.NoError(t, err)
	require.Equal(t, "LimitOrderModified", events[0].Name)
	require.Equal(t, []interface{}{big.NewInt(1), limitPrice(550).BigInt(), big.NewInt(1200)}, events[0].Args)
	require.Equal(t, "999900000000", e.balance(e.stable, e.user).String())
	require.False(t, e.ctx.KVStore(e.key).Has(limitKey(types.ExpiryIndexPrefix, limitID(1100), limitID(id))))
	require.True(t, e.ctx.KVStore(e.key).Has(limitKey(types.ExpiryIndexPrefix, limitID(1200), limitID(id))))
	events, err = e.k.ExecuteLimit(e.ctx, id)
	require.NoError(t, err)
	require.Len(t, events, 3)
	require.Equal(t, "LimitOrderExecuted", events[2].Name)
	require.Equal(t, []interface{}{big.NewInt(1), limitPrice(500).BigInt()}, events[2].Args)
	require.Equal(t, "1000199800000", e.balance(e.token, e.user).String())
	require.Equal(t, types.OrderStatusExecuted, e.order(id).Status)
	require.Equal(t, []interface{}{e.stable, big.NewInt(500_000_000_000), big.NewInt(500_100_000_000)}, events[0].Args)
	require.Equal(t, []interface{}{e.token, big.NewInt(1_000_000_000_000), big.NewInt(999_800_200_000)}, events[1].Args)
	escrow, err := e.k.GetEscrow(e.ctx, e.stable)
	require.NoError(t, err)
	require.Equal(t, "0", escrow.String())
	count, err := e.k.GetActiveOrderCount(e.ctx, e.user)
	require.NoError(t, err)
	require.Zero(t, count)
	require.False(t, e.ctx.KVStore(e.key).Has(limitKey(types.ExpiryIndexPrefix, limitID(1200), limitID(id))))
	before = e.snapshot()
	_, err = e.k.CancelLimit(e.ctx, e.user, id)
	limitError(t, err, types.ErrCannotCancel)
	_, err = e.k.ModifyLimit(e.ctx, e.user, id, limitPrice(500), 0)
	limitError(t, err, types.ErrOrderNotPending)
	_, err = e.k.ExecuteLimit(e.ctx, id)
	limitError(t, err, types.ErrOrderNotPending)
	require.Equal(t, before, e.snapshot())
	id = e.place(types.OrderTypeLimitSell, 1_000_000_000, 550, 0)
	_, err = e.k.ExecuteLimit(e.ctx, id)
	limitError(t, err, types.ErrPriceBelowTarget)
	params, err := e.k.GetParams(e.ctx)
	require.NoError(t, err)
	params.OrdersPaused = true
	e.put(types.ParamsKey, params)
	events, err = e.k.CancelLimit(e.ctx, e.user, id)
	require.NoError(t, err)
	require.Equal(t, "LimitOrderCancelled", events[0].Name)
	require.Equal(t, types.OrderStatusCancelled, e.order(id).Status)
	require.Equal(t, "1000199800000", e.balance(e.token, e.user).String())
	unknown, err := e.k.GetLimitOrder(e.ctx, 999)
	require.NoError(t, err)
	require.Equal(t, types.LimitOrder{Amount: sdk.ZeroInt(), TargetPrice: sdk.ZeroInt()}, unknown)
	ok, reason, err = e.k.CanExecuteLimitOrder(e.ctx, 999)
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, "Order not found", reason)
}

func limitModifyRefusals(t *testing.T) {
	e := newLimitEnv(t)
	id := e.place(types.OrderTypeLimitSell, 1_000_000_000, 550, 1100)
	for _, tc := range []struct {
		user   common.Address
		target sdk.Int
		expiry uint64
		want   *types.AbiError
	}{
		{limitAddress("other owner"), limitPrice(600), 1200, types.ErrNotOrderOwner},
		{e.user, sdk.ZeroInt(), 1200, types.ErrInvalidPrice},
		{e.user, limitPrice(600), 1000, types.ErrInvalidExpiry},
	} {
		before := e.snapshot()
		events, err := e.k.ModifyLimit(e.ctx, tc.user, id, tc.target, tc.expiry)
		limitError(t, err, tc.want)
		require.Empty(t, events)
		require.Equal(t, before, e.snapshot())
	}
	_, err := e.k.ModifyLimit(e.ctx, e.user, id, limitPrice(600), 0)
	require.NoError(t, err)
	require.False(t, e.ctx.KVStore(e.key).Has(limitKey(types.ExpiryIndexPrefix, limitID(1100), limitID(id))))
	escrow, err := e.k.GetEscrow(e.ctx, e.token)
	require.NoError(t, err)
	require.Equal(t, "1000000000", escrow.String())
}

func limitBatchIsolation(t *testing.T) {
	e := newLimitEnv(t)
	e.place(types.OrderTypeLimitBuy, 100_000_000, 400, 0)
	e.place(types.OrderTypeLimitSell, 1_000_000_000, 500, 0)
	e.place(types.OrderTypeLimitBuy, 100_000_000, 500, 0)
	count, events := e.k.BatchExecuteLimit(e.ctx, []uint64{999, 1, 2, 2, 3})
	require.Equal(t, uint64(2), count)
	require.Len(t, events, 6)
	require.Equal(t, types.OrderStatusPending, e.order(1).Status)
	require.Equal(t, types.OrderStatusExecuted, e.order(2).Status)
	require.Equal(t, types.OrderStatusExecuted, e.order(3).Status)
	require.Equal(t, []interface{}{big.NewInt(2), limitPrice(500).BigInt()}, events[2].Args)
	require.Equal(t, []interface{}{big.NewInt(3), limitPrice(500).BigInt()}, events[5].Args)
	require.Equal(t, "1000299500000", e.balance(e.stable, e.user).String())
	require.Equal(t, "999199800000", e.balance(e.token, e.user).String())
}

func limitFailedPayout(t *testing.T) {
	e := newLimitEnv(t)
	id := e.place(types.OrderTypeLimitBuy, 100_000_000, 500, 0)
	order := e.order(id)
	order.User = e.k.CustodyEVMAddress(e.ctx)
	require.NoError(t, order.Validate())
	e.put(limitKey(types.LimitOrderPrefix, limitID(id)), order)
	store := e.ctx.KVStore(e.key)
	store.Delete(limitKey(types.UserOrderPrefix, e.user.Bytes(), limitID(id)))
	e.put(limitKey(types.UserOrderPrefix, order.User.Bytes(), limitID(id)), types.UserOrder{User: order.User, OrderID: id, OrderType: order.OrderType})
	store.Delete(limitKey(types.ActiveCountPrefix, e.user.Bytes()))
	e.put(limitKey(types.ActiveCountPrefix, order.User.Bytes()), types.ActiveOrderCount{User: order.User, Count: 1})
	before := e.snapshot()
	events, err := e.k.ExecuteLimit(e.ctx, id)
	limitError(t, err, types.ErrUnsupportedTransfer)
	require.Empty(t, events)
	require.Equal(t, before, e.snapshot())
	require.Equal(t, types.OrderStatusPending, e.order(id).Status)
	events, err = e.k.CancelLimit(e.ctx, order.User, id)
	limitError(t, err, types.ErrUnsupportedTransfer)
	require.Empty(t, events)
	require.Equal(t, before, e.snapshot())
}

func TestEscrowIsReserved(t *testing.T) {
	e := newLimitEnv(t)
	e.place(types.OrderTypeLimitBuy, 100_000_000, 500, 0)
	for _, tc := range []struct {
		amount int64
		want   bool
	}{{450_000_000_000, true}, {450_000_000_001, false}} {
		ok, err := e.k.HasLiquidity(e.ctx, e.stable, limitInt(tc.amount))
		require.NoError(t, err)
		require.Equal(t, tc.want, ok)
	}
	e.put(limitKey(types.BookPrefix, e.token.Bytes()), types.TokenBook{Token: e.token, Reserves: limitInt(199_900_000), TotalDeposited: limitInt(199_900_000), TotalWithdrawn: sdk.ZeroInt(), Decimals: 6, ShareDenom: "spot/share/" + common.Bytes2Hex(e.token.Bytes())})
	params, err := e.k.GetParams(e.ctx)
	require.NoError(t, err)
	params.Tokens[1].MaxUtilizationBps = 10000
	params.OrderPriceImpactEnabled = false
	e.put(types.ParamsKey, params)
	before := e.snapshot()
	events, err := e.k.ExecuteLimit(e.ctx, 1)
	limitError(t, err, types.ErrInsufficientLiquidity)
	require.Empty(t, events)
	require.Equal(t, before, e.snapshot())
}

func TestActiveListSwapAndPop(t *testing.T) {
	e := newLimitEnv(t)
	for i := 0; i < 3; i++ {
		e.place(types.OrderTypeLimitBuy, 100_000_000, 500, 0)
	}
	_, err := e.k.ExecuteLimit(e.ctx, 1)
	require.NoError(t, err)
	for i, id := range []uint64{3, 2} {
		actual, err := e.k.ActiveLimitOrderID(e.ctx, uint64(i))
		require.NoError(t, err)
		require.Equal(t, id, actual)
	}
	ids, err := e.k.GetUserLimitOrders(e.ctx, e.user)
	require.NoError(t, err)
	require.Equal(t, []uint64{1, 2, 3}, ids)
	require.Equal(t, types.OrderStatusExecuted, e.order(1).Status)
	_, err = e.k.CancelLimit(e.ctx, e.user, 3)
	require.NoError(t, err)
	actual, err := e.k.ActiveLimitOrderID(e.ctx, 0)
	require.NoError(t, err)
	require.Equal(t, uint64(2), actual)
	_, err = e.k.CancelLimit(e.ctx, e.user, 2)
	require.NoError(t, err)
	count, err := e.k.GetActiveLimitOrderCount(e.ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	params, err := e.k.GetParams(e.ctx)
	require.NoError(t, err)
	params.MaxActiveOrdersPerUser = 200
	e.put(types.ParamsKey, params)
	for i := 0; i < 150; i++ {
		e.place(types.OrderTypeLimitBuy, 100_000_000, 500, 0)
	}
	ids, err = e.k.GetExecutableLimitOrders(e.ctx, 500)
	require.NoError(t, err)
	require.Len(t, ids, 100)
	for i, id := range ids {
		require.Equal(t, uint64(i+4), id)
	}
	ids, err = e.k.GetExecutableLimitOrders(e.ctx, 0)
	require.NoError(t, err)
	require.Empty(t, ids)
}

func TestMinOrderNotional(t *testing.T) {
	for _, tc := range []struct {
		name           string
		kind           types.OrderType
		below, minimum int64
	}{{"buy", types.OrderTypeLimitBuy, 9_000_000, 10_000_000}, {"sell", types.OrderTypeLimitSell, 19_999_999, 20_000_000}} {
		t.Run(tc.name, func(t *testing.T) {
			e := newLimitEnv(t)
			before := e.snapshot()
			id, events, err := e.k.PlaceLimit(e.ctx, e.user, e.stable, e.token, limitInt(tc.below), limitPrice(500), 0, tc.kind)
			limitError(t, err, types.ErrOrderBelowMinimumNotional)
			require.Equal(t, []byte{0x9b, 0x2b, 0xea, 0x5b}, types.ErrOrderBelowMinimumNotional.Data())
			require.Zero(t, id)
			require.Empty(t, events)
			require.Equal(t, before, e.snapshot())
			require.Equal(t, uint64(1), e.place(tc.kind, tc.minimum, 500, 0))
		})
	}
}

func TestMaxActiveOrdersPerUser(t *testing.T) {
	e := newLimitEnv(t)
	params, err := e.k.GetParams(e.ctx)
	require.NoError(t, err)
	params.MaxActiveOrdersPerUser = 2
	e.put(types.ParamsKey, params)
	e.place(types.OrderTypeLimitBuy, 100_000_000, 500, 0)
	stop := types.StopLossOrder{ID: 2, User: e.user, Stablecoin: e.stable, Token: e.token, Amount: limitInt(1_000_000_000), TriggerPrice: limitPrice(400), Status: types.OrderStatusPending, CreatedAt: 1000}
	require.NoError(t, stop.Validate())
	app := testkeeper.EVMTestApp
	require.NoError(t, app.BankKeeper.SendCoins(e.ctx, app.EvmKeeper.GetPaxAddressOrDefault(e.ctx, e.user), keeper.CustodyAddress(), sdk.NewCoins(sdk.NewInt64Coin("uhpx", 1_000_000_000))))
	e.put(limitKey(types.StopLossOrderPrefix, limitID(2)), stop)
	e.put(limitKey(types.UserOrderPrefix, e.user.Bytes(), limitID(2)), types.UserOrder{User: e.user, OrderID: 2, OrderType: types.OrderTypeStopLoss})
	e.ctx.KVStore(e.key).Set(limitKey(types.ActiveStopLossPrefix, limitID(2)), []byte{})
	e.put(limitKey(types.EscrowPrefix, e.token.Bytes()), types.TokenEscrow{Token: e.token, Amount: stop.Amount})
	e.put(limitKey(types.ActiveCountPrefix, e.user.Bytes()), types.ActiveOrderCount{User: e.user, Count: 2})
	e.ctx.KVStore(e.key).Set(types.NextOrderIDKey, limitID(3))
	before := e.snapshot()
	id, events, err := e.k.PlaceLimit(e.ctx, e.user, e.stable, e.token, limitInt(100_000_000), limitPrice(500), 0, types.OrderTypeLimitBuy)
	limitError(t, err, types.ErrTooManyActiveOrders)
	require.Equal(t, []byte{0x28, 0xd7, 0xa6, 0x41}, types.ErrTooManyActiveOrders.Data())
	require.Zero(t, id)
	require.Empty(t, events)
	require.Equal(t, before, e.snapshot())
	_, err = e.k.ExecuteLimit(e.ctx, 2)
	limitError(t, err, types.ErrOrderNotPending)
	ids, err := e.k.GetUserLimitOrders(e.ctx, e.user)
	require.NoError(t, err)
	require.Equal(t, []uint64{1}, ids)
	_, err = e.k.CancelLimit(e.ctx, e.user, 1)
	require.NoError(t, err)
	require.Equal(t, uint64(3), e.place(types.OrderTypeLimitBuy, 100_000_000, 500, 0))
}

func TestOrderMinOutput(t *testing.T) {
	e := newLimitEnv(t)
	id := e.place(types.OrderTypeLimitSell, 1_000_000_000, 550, 0)
	e.price(limitPrice(550))
	minimum, err := e.k.GetOrderMinOutput(e.ctx, id)
	require.NoError(t, err)
	require.Equal(t, "0", minimum.String())
	gs := types.DefaultGenesis()
	gs.Params, err = e.k.GetParams(e.ctx)
	require.NoError(t, err)
	gs.NextOrderID = 2
	gs.LimitOrders = []types.LimitOrder{e.order(id)}
	gs.UserOrders = []types.UserOrder{{User: e.user, OrderID: id, OrderType: types.OrderTypeLimitSell}}
	gs.ActiveLimitOrders = []uint64{id}
	gs.Escrows = []types.TokenEscrow{{Token: e.token, Amount: limitInt(1_000_000_000)}}
	gs.ActiveCounts = []types.ActiveOrderCount{{User: e.user, Count: 1}}
	gs.OrderMinOutputs = []types.OrderMinOutput{{OrderID: id, MinOutput: limitInt(600_000_000)}}
	for _, token := range []common.Address{e.stable, e.token} {
		var book types.TokenBook
		require.NoError(t, json.Unmarshal(e.ctx.KVStore(e.key).Get(limitKey(types.BookPrefix, token.Bytes())), &book))
		gs.Books = append(gs.Books, book)
	}
	require.NoError(t, gs.Validate())
	e.k.InitGenesis(e.ctx, *gs)
	before := e.snapshot()
	events, err := e.k.ExecuteLimit(e.ctx, id)
	limitError(t, err, types.ErrInsufficientOutput)
	require.Empty(t, events)
	require.Equal(t, before, e.snapshot())
	require.Equal(t, types.OrderStatusPending, e.order(id).Status)
	e.put(limitKey(types.OrderMinOutputPrefix, limitID(id)), types.OrderMinOutput{OrderID: id, MinOutput: limitInt(549_450_000)})
	events, err = e.k.ExecuteLimit(e.ctx, id)
	require.NoError(t, err)
	require.Equal(t, "LimitOrderExecuted", events[len(events)-1].Name)
	require.Equal(t, "1000549450000", e.balance(e.stable, e.user).String())
}

func TestLimitExpire(t *testing.T) {
	e := newLimitEnv(t)
	id := e.place(types.OrderTypeLimitBuy, 100_000_000, 500, 1001)
	e.put(limitKey(types.FillBackoffPrefix, limitID(id)), types.FillBackoff{OrderID: id, Failures: 1, NextRetryHeight: 12})
	before := e.snapshot()
	_, err := e.k.ExpireLimit(e.ctx, id)
	limitError(t, err, types.ErrInvalidExpiry)
	require.Equal(t, before, e.snapshot())
	perpetual := e.place(types.OrderTypeLimitBuy, 100_000_000, 500, 0)
	e.ctx = e.ctx.WithBlockTime(time.Unix(1001, 0).UTC())
	_, err = e.k.ExecuteLimit(e.ctx, id)
	limitError(t, err, types.ErrOrderExpired)
	count, events := e.k.BatchExpire(e.ctx, []uint64{999, perpetual, id, id})
	require.Equal(t, uint64(1), count)
	require.Equal(t, []keeper.LimitOrderEvent{{Address: common.HexToAddress(types.PECOROrdersAddress), Name: "LimitOrderExpired", Args: []interface{}{big.NewInt(1)}}}, events)
	require.Equal(t, types.OrderStatusExpired, e.order(id).Status)
	require.Equal(t, "999900000000", e.balance(e.stable, e.user).String())
	escrow, err := e.k.GetEscrow(e.ctx, e.stable)
	require.NoError(t, err)
	require.Equal(t, "100000000", escrow.String())
	active, err := e.k.GetActiveOrderCount(e.ctx, e.user)
	require.NoError(t, err)
	require.Equal(t, uint64(1), active)
	require.False(t, e.ctx.KVStore(e.key).Has(limitKey(types.ExpiryIndexPrefix, limitID(1001), limitID(id))))
	require.False(t, e.ctx.KVStore(e.key).Has(limitKey(types.FillBackoffPrefix, limitID(id))))
	_, err = e.k.CancelLimit(e.ctx, e.user, id)
	limitError(t, err, types.ErrCannotCancel)
}

const limitTokenCreation = "0x6101208060405234610648576116b3803803809161001d8285610668565b833981016080828203126106485781516001600160401b038111610648578161004791840161068b565b602083015190916001600160401b0382116106485761006791840161068b565b91606060408201519101519160018060a01b0383168093036106485780516001600160401b03811161055557600054600181811c9116801561063e575b602082101461053557601f81116105da575b50806020601f82116001146105765760009161056b575b508160011b916000199060031b1c1916176000555b83516001600160401b03811161055557600154600181811c9116801561054b575b602082101461053557601f81116104d0575b50602094601f82116001146104695794819293949560009261045e575b50508160011b916000199060031b1c1916176001555b6006608052602081519101208060e0524660c05260405160208101917f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f835260408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a081526101cd60c082610668565b51902060a0523361010052811561044d576000906101ed816002546106fa565b6002558282526003602052604082206102078282546106fa565b9055828252600660205260408220838352600360205260408320549080549081151580610419575b156103b45760001982019182116103a05760019161024c9161071d565b5001555b6002546007548015158061037f575b156102f35760001981019081116102df5791602091600161029060008051602061169383398151915295600761071d565b5001555b604051908152a3604051610f4390816107508239608051816106d5015260a051816109bd015260c05181610997015260e05181610a0c01526101005181818161043b01526105d80152f35b634e487b7160e01b84526011600452602484fd5b90604051916103018361064d565b438352602083019182526801000000000000000081101561036b5780600161032e9201600755600761071d565b929092610357575182555160019091015560008051602061169383398151915290602090610294565b634e487b7160e01b85526004859052602485fd5b634e487b7160e01b85526041600452602485fd5b5060001981018181116103a05761039790600761071d565b5054431461025f565b634e487b7160e01b85526011600452602485fd5b9190604051926103c38461064d565b438452602084019283526801000000000000000082101561040557906103ee9160018201815561071d565b929092610357579060019151835551910155610250565b634e487b7160e01b86526041600452602486fd5b50600019820182811161043957610430908261071d565b5054431461022f565b634e487b7160e01b86526011600452602486fd5b63d92e233d60e01b60005260046000fd5b015190503880610132565b601f198216956001600052806000209160005b8881106104b85750836001959697981061049f575b505050811b01600155610148565b015160001960f88460031b161c19169055388080610491565b9192602060018192868501518155019401920161047c565b60016000527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6601f830160051c8101916020841061052b575b601f0160051c01905b81811061051f5750610115565b60008155600101610512565b9091508190610509565b634e487b7160e01b600052602260045260246000fd5b90607f1690610103565b634e487b7160e01b600052604160045260246000fd5b9050820151386100cd565b60008080528181209250601f198416905b8181106105c2575090836001949392106105a9575b5050811b016000556100e2565b84015160001960f88460031b161c19169055388061059c565b91926020600181928689015181550194019201610587565b600080527f290decd9548b62a8d60345a988386fc84ba6bc95484008f6362f93160ef3e563601f830160051c81019160208410610634575b601f0160051c01905b81811061062857506100b6565b6000815560010161061b565b9091508190610612565b90607f16906100a4565b600080fd5b604081019081106001600160401b0382111761055557604052565b601f909101601f19168101906001600160401b0382119082101761055557604052565b81601f82011215610648578051906001600160401b03821161055557604051926106bf601f8401601f191660200185610668565b828452602083830101116106485760005b8281106106e557505060206000918301015290565b806020809284010151828287010152016106d0565b9190820180921161070757565b634e487b7160e01b600052601160045260246000fd5b80548210156107395760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fdfe6080604052600436101561001257600080fd5b60003560e01c806306fdde0314610849578063095ea7b3146108235780630df869991461066457806316f0115b146107fa57806318160ddd146107dc57806323b872dd1461073457806330adf81f146106f9578063313ce567146106bb5780633644e515146106a05780633a46b1a8146106645780635a73fae2146105bc57806370a08231146105825780637ecebe00146105485780638e539e8c1461046a5780638ea261cc1461042557806395d89b411461031a578063a9059cbb146102e9578063d505accf146101435763dd62ed3e146100ed57600080fd5b3461013e57604036600319011261013e57610106610968565b61010e61097e565b6001600160a01b039182166000908152600460209081526040808320949093168252928352819020549051908152f35b600080fd5b3461013e5760e036600319011261013e5761015c610968565b61016461097e565b6044359060643560843560ff811680910361013e578142116102d8576001600160a01b038516600081815260056020526040902080549193919260001984146102c25783600093608093600160209701905560405190868201927f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c9845288604084015260018060a01b038a1660608401528a8684015260a083015260c082015260c0815261021360e0826108e7565b51902061021e610994565b90604051908682019261190160f01b845260228301526042820152604281526102486062826108e7565b519020906040519182528482015260a435604082015260c435606082015282805260015afa156102b6576000516001600160a01b03168015919082156102ab575b505061029a5761029892610a77565b005b63ddafbaef60e01b60005260046000fd5b141590508480610289565b6040513d6000823e3d90fd5b634e487b7160e01b600052601160045260246000fd5b63068568f360e21b60005260046000fd5b3461013e57604036600319011261013e5761030f610305610968565b6024359033610ba9565b602060405160018152f35b3461013e57600036600319011261013e5760006040519080600154908160011c9160018116801561041b575b602084108114610407578386529081156103e05750600114610383575b61037f84610373818603826108e7565b6040519182918261091f565b0390f35b600181527fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6939250905b8082106103c65750909150810160200161037382610363565b9192600181602092548385880101520191019092916103ad565b60ff191660208087019190915292151560051b850190920192506103739150839050610363565b634e487b7160e01b83526022600452602483fd5b92607f1692610346565b3461013e57600036600319011261013e576040517f00000000000000000000000000000000000000000000000000000000000000006001600160a01b03168152602090f35b3461013e57602036600319011261013e576004356000438210156105395780916007545b8084106104e1575050816104a957602091505b604051908152f35b60001982019182116104cd575060016104c56020926007610aeb565b5001546104a1565b634e487b7160e01b81526011600452602490fd5b6104f76104ee8583610ad1565b60011c85610ade565b9082610504836007610aeb565b50541115610512575061048e565b935060018101809111610525579261048e565b634e487b7160e01b83526011600452602483fd5b63014bf66160e11b8152600490fd5b3461013e57602036600319011261013e576001600160a01b03610569610968565b1660005260056020526020604060002054604051908152f35b3461013e57602036600319011261013e576001600160a01b036105a3610968565b1660005260036020526020604060002054604051908152f35b3461013e57602036600319011261013e576105d5610968565b337f00000000000000000000000000000000000000000000000000000000000000006001600160a01b031614801590610650575b8015610647575b61063657600880546001600160a01b0319166001600160a01b0392909216919091179055005b633ddf3e5b60e21b60005260046000fd5b50803b15610610565b506008546001600160a01b03161515610609565b3461013e57604036600319011261013e576001600160a01b03610685610968565b16600052600660205260206104a16024356040600020610b1d565b3461013e57600036600319011261013e5760206104a1610994565b3461013e57600036600319011261013e57602060405160ff7f0000000000000000000000000000000000000000000000000000000000000000168152f35b3461013e57600036600319011261013e5760206040517f6e71edae12b1b97f4d1f60370fef10105fa2faae0126114a169c64845d6126c98152f35b3461013e57606036600319011261013e5761074d610968565b61075561097e565b6001600160a01b03821660008181526004602090815260408083203384529091529020549092604435929160018101610794575b5061030f9350610ba9565b8381106107cb5761030f946000526004602052604060002060018060a01b0333166000526020528360406000209103905584610789565b6313be252b60e01b60005260046000fd5b3461013e57600036600319011261013e576020600254604051908152f35b3461013e57600036600319011261013e576008546040516001600160a01b039091168152602090f35b3461013e57604036600319011261013e5761030f61083f610968565b6024359033610a77565b3461013e57600036600319011261013e5760405160008054600181811c918391811680156108dd575b602084108114610407578386529081156103e0575060011461089e5761037f84610373818603826108e7565b80805260208120939250905b8082106108c35750909150810160200161037382610363565b9192600181602092548385880101520191019092916108aa565b92607f1692610872565b90601f8019910116810190811067ffffffffffffffff82111761090957604052565b634e487b7160e01b600052604160045260246000fd5b91909160208152825180602083015260005b818110610952575060409293506000838284010152601f8019910116010190565b8060208092870101516040828601015201610931565b600435906001600160a01b038216820361013e57565b602435906001600160a01b038216820361013e57565b467f0000000000000000000000000000000000000000000000000000000000000000036109df577f000000000000000000000000000000000000000000000000000000000000000090565b60405160208101907f8b73c3c69bb8fe3d512ecc4cf759cc79239f7b179b0ffacaa9a75d522b39400f82527f000000000000000000000000000000000000000000000000000000000000000060408201527fc89efdaa54c0f20c7adf612882df0950f5a951637e0307cdcb4c672f298b8bc660608201524660808201523060a082015260a08152610a7160c0826108e7565b51902090565b6001600160a01b0390811660008181526004602090815260408083209590941680835294815290839020859055915193845291927f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b9259190a3565b919082039182116102c257565b919082018092116102c257565b8054821015610b075760005260206000209060011b0190600090565b634e487b7160e01b600052603260045260246000fd5b43821015610b985760009181545b808410610b5e57505081610b40575050600090565b60001982019182116102c257600191610b5891610aeb565b50015490565b610b6b6104ee8583610ad1565b9082610b778386610aeb565b50541115610b855750610b2b565b9350600181018091116102c25792610b2b565b63014bf66160e11b60005260046000fd5b60085460009391906001600160a01b031680610cc6575b506001600160a01b0316918215610cb7576001600160a01b0316928315610ca857828152600360205281604082205410610c99577fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9181846020935260038352604081208281540390558581526003835260408120610c40838254610ade565b905584815260068352610c626040822086835260038552604083205490610e4e565b858503610c74575b50604051908152a3565b85815260068352604080822087835260038552912054610c9391610e4e565b38610c6a565b631e9acf1760e31b8152600490fd5b63d92e233d60e01b8152600490fd5b63d92e233d60e01b8452600484fd5b60405163a2d0292760e01b8152602081600481855afa908115610e43578691610e01575b506001600160a01b03169081151580610d85575b610d09575b50610bc0565b813b15610d81576040516301bdf3ad60e51b815260048101919091526001600160a01b03838116602483015285166044820152606481018490529085908290608490829084905af18015610d7657610d62575b80610d03565b84610d6f919592956108e7565b9238610d5c565b6040513d87823e3d90fd5b8580fd5b50604051636cc7149d60e01b8152602081600481865afa8015610df6578790610db5575b60109150161515610cfe565b506020813d602011610dee575b81610dcf602093836108e7565b81010312610dea575160ff81168103610dea57601090610da9565b8680fd5b3d9150610dc2565b6040513d89823e3d90fd5b90506020813d602011610e3b575b81610e1c602093836108e7565b81010312610d8157516001600160a01b0381168103610d815738610cea565b3d9150610e0f565b6040513d88823e3d90fd5b80549081151580610eed575b15610e7c5760001982019182116102c257600191610e7791610aeb565b500155565b604051916040830183811067ffffffffffffffff82111761090957604052438352602083019384526801000000000000000081101561090957610ec491600182018155610aeb565b929092610ed75760019151835551910155565b634e487b7160e01b600052600060045260246000fd5b5060001982018281116102c257610f049082610aeb565b50544314610e5a56fea2646970667358221220470832d0bf3f454c98d354da71961ef6b0260869197b1e39758d6291ee0444f464736f6c634300081b0033ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
