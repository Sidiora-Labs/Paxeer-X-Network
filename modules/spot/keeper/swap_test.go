package keeper_test

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	spotkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

type swapFixture struct {
	keeper  *spotkeeper.Keeper
	ctx     sdk.Context
	key     sdk.StoreKey
	user    common.Address
	account sdk.AccAddress
	stable  common.Address
	pax     common.Address
}

func newSwapFixture(t *testing.T, reserves int64, utilization uint64) swapFixture {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx, _ := app.GetContextForDeliverTx(nil).CacheContext()
	ctx = ctx.WithBlockHeight(8).WithBlockTime(time.Unix(100, 0))
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	spotKey := sdk.NewKVStoreKey(types.StoreKey)
	feedKey := sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	ctx = launchpadtest.WithStore(ctx, spotKey)
	ctx = launchpadtest.WithStore(ctx, feedKey)
	feed := pricefeedkeeper.NewKeeper(feedKey, app.StakingKeeper, app.AccountKeeper, &app.EvmKeeper)
	k := spotkeeper.NewKeeper(spotKey, app.BankKeeper, &app.EvmKeeper, feed)
	user := common.BytesToAddress([]byte{0x31})
	account := sdk.AccAddress(user.Bytes())
	app.EvmKeeper.SetAddressMapping(ctx, account, user)
	stable := common.BytesToAddress([]byte{0x41})
	pax := common.BytesToAddress([]byte{0x42})
	for _, token := range []struct {
		denom   string
		address common.Address
		display string
	}{
		{"uquote", stable, "quote"}, {"uhpx", pax, "pax"},
	} {
		app.BankKeeper.SetDenomMetaData(ctx, banktypes.Metadata{Base: token.denom, Display: token.display,
			DenomUnits: []*banktypes.DenomUnit{{Denom: token.denom, Exponent: 0}, {Denom: token.display, Exponent: 6}}})
		require.NoError(t, app.EvmKeeper.SetERC20NativePointer(ctx, token.denom, token.address))
	}
	app.AccountKeeper.SetModuleAccount(ctx, authtypes.NewEmptyModuleAccount(types.ModuleName))
	params := types.DefaultParams()
	params.SwapFeeBps, params.Tier1FeeBps, params.Tier2FeeBps = 30, 20, 50
	params.PriceImpactEnabled, params.PriceImpactScalarBps = true, 100
	params.Tokens = []types.TokenParams{
		{Address: stable, Kind: types.TokenKindNative, IsStablecoin: true, Decimals: 6, MaxUtilizationBps: 10000},
		{Address: pax, Kind: types.TokenKindNative, Decimals: 6, MarketID: types.PAXMarketID, MaxUtilizationBps: utilization},
	}
	putSwapJSON(t, ctx.KVStore(spotKey), types.ParamsKey, params)
	putSwapJSON(t, ctx.KVStore(feedKey), pricefeedtypes.ParamsKey, pricefeedtypes.DefaultParams(pricefeedtypes.DefaultAuthority()))
	price := sdk.NewIntWithDecimal(5, 17)
	putSwapJSON(t, ctx.KVStore(feedKey), pricefeedtypes.AggregateKey(types.PAXMarketID), pricefeedtypes.Aggregate{
		IndexPrice: price, MarkPrice: price, Basis: sdk.ZeroInt(), TimestampMs: 100000, Height: 8, Sources: 1,
	})
	for _, balance := range []struct {
		to     sdk.AccAddress
		denom  string
		amount int64
	}{
		{account, "uquote", 1000_000000},
		{spotkeeper.CustodyAddress(), "uhpx", reserves},
	} {
		coins := sdk.NewCoins(sdk.NewInt64Coin(balance.denom, balance.amount))
		require.NoError(t, app.BankKeeper.MintCoins(ctx, "evm", coins))
		require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", balance.to, coins))
	}
	for _, book := range []types.TokenBook{
		{Token: stable, Reserves: sdk.ZeroInt(), Decimals: 6, TotalDeposited: sdk.ZeroInt(), TotalWithdrawn: sdk.ZeroInt()},
		{Token: pax, Reserves: sdk.NewInt(reserves), Decimals: 6, TotalDeposited: sdk.NewInt(reserves), TotalWithdrawn: sdk.ZeroInt()},
	} {
		putSwapJSON(t, ctx.KVStore(spotKey), swapBookKey(book.Token), book)
	}
	return swapFixture{keeper: k, ctx: ctx, key: spotKey, user: user, account: account, stable: stable, pax: pax}
}

func putSwapJSON(t *testing.T, store sdk.KVStore, key []byte, value interface{}) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	store.Set(key, encoded)
}

func swapBookKey(token common.Address) []byte {
	return append(append([]byte(nil), types.BookPrefix...), token.Bytes()...)
}

func swapStoreSnapshot(t *testing.T, store sdk.KVStore) map[string]string {
	t.Helper()
	iterator := store.Iterator(nil, nil)
	defer func() { require.NoError(t, iterator.Close()) }()
	snapshot := make(map[string]string)
	for ; iterator.Valid(); iterator.Next() {
		snapshot[string(iterator.Key())] = string(iterator.Value())
	}
	return snapshot
}

func TestSwapLiquidityCheckedOnGross(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reserves int64
		cap      uint64
		accepted bool
	}{
		{"gross_equals_book", 2000_000000, 10000, true},
		{"net_fits_gross_exceeds_book", 1999_000000, 10000, false},
		{"net_fits_gross_exceeds_cap", 2000_000000, 9999, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSwapFixture(t, tc.reserves, tc.cap)
			store := f.ctx.KVStore(f.key)
			before := swapStoreSnapshot(t, store)
			bank := testkeeper.EVMTestApp.BankKeeper
			userBefore := bank.GetAllBalances(f.ctx, f.account)
			custodyBefore := bank.GetAllBalances(f.ctx, spotkeeper.CustodyAddress())
			result, err := f.keeper.MarketBuy(f.ctx, f.user, f.stable, f.pax, big.NewInt(1000_000000), new(big.Int), new(big.Int))
			if !tc.accepted {
				require.ErrorIs(t, err, types.ErrInsufficientLiquidity)
				var refusal *types.AbiError
				require.ErrorAs(t, err, &refusal)
				require.Equal(t, "bb55fd27", hex.EncodeToString(refusal.Data()))
				require.Equal(t, spotkeeper.SwapResult{}, result)
				require.Equal(t, before, swapStoreSnapshot(t, store))
				require.Equal(t, userBefore, bank.GetAllBalances(f.ctx, f.account))
				require.Equal(t, custodyBefore, bank.GetAllBalances(f.ctx, spotkeeper.CustodyAddress()))
				return
			}
			require.NoError(t, err)
			require.Equal(t, "1000000000", result.AmountIn.String())
			require.Equal(t, "2000000000", result.GrossOutput.String())
			require.Equal(t, "1974060000", result.AmountOut.String())
			impactEvent := result.Logs[1]
			require.Equal(t, common.HexToAddress(types.PECORAddress), impactEvent.Address)
			require.Equal(t, []common.Hash{
				crypto.Keccak256Hash([]byte("PriceImpactApplied(address,address,uint256,uint256)")),
				common.BytesToHash(f.user.Bytes()), common.BytesToHash(f.pax.Bytes()),
			}, impactEvent.Topics)
			require.Len(t, impactEvent.Data, 64)
			require.Equal(t, uint64(100), new(big.Int).SetBytes(impactEvent.Data[:32]).Uint64())
			require.Equal(t, "19940000", new(big.Int).SetBytes(impactEvent.Data[32:]).String())
			require.Equal(t, "0", bank.GetBalance(f.ctx, f.account, "uquote").Amount.String())
			require.Equal(t, "1974060000", bank.GetBalance(f.ctx, f.account, "uhpx").Amount.String())
			var inputBook, outputBook types.TokenBook
			require.NoError(t, json.Unmarshal(store.Get(swapBookKey(f.stable)), &inputBook))
			require.NoError(t, json.Unmarshal(store.Get(swapBookKey(f.pax)), &outputBook))
			require.Equal(t, "1000000000", inputBook.Reserves.String())
			require.Equal(t, "25940000", outputBook.Reserves.String())
			spendable, err := f.keeper.Spendable(f.ctx, f.pax)
			require.NoError(t, err)
			require.Zero(t, spendable.Sign())
			event := result.Logs[len(result.Logs)-1]
			require.Equal(t, common.HexToAddress(types.PECORAddress), event.Address)
			require.Equal(t, []common.Hash{
				crypto.Keccak256Hash([]byte("MarketOrderExecuted(address,address,address,uint256,uint256,bool)")),
				common.BytesToHash(f.user.Bytes()), common.BytesToHash(f.stable.Bytes()), common.BytesToHash(f.pax.Bytes()),
			}, event.Topics)
			require.Len(t, event.Data, 96)
			require.Equal(t, "1000000000", new(big.Int).SetBytes(event.Data[:32]).String())
			require.Equal(t, "1974060000", new(big.Int).SetBytes(event.Data[32:64]).String())
			require.Equal(t, uint64(1), new(big.Int).SetBytes(event.Data[64:]).Uint64())
		})
	}
}

func TestSwapImpactRoundedToZeroDoesNotEmitEvent(t *testing.T) {
	f := newSwapFixture(t, 100, 10000)
	amount := sdk.OneInt()
	quote, err := f.keeper.Price(f.ctx, common.HexToAddress(types.PECORAddress), f.stable, f.pax, amount)
	require.NoError(t, err)
	require.Greater(t, quote.ImpactBps, uint64(0))
	require.True(t, quote.Deduction.IsZero())
	require.True(t, quote.Net.IsPositive())

	result, err := f.keeper.SwapExactIn(f.ctx, f.user, f.stable, f.pax, amount.BigInt(), new(big.Int), new(big.Int))
	require.NoError(t, err)
	require.Equal(t, quote.Net.BigInt(), result.AmountOut)
	impactTopic := crypto.Keccak256Hash([]byte("PriceImpactApplied(address,address,uint256,uint256)"))
	for _, event := range result.Logs {
		require.NotEmpty(t, event.Topics)
		require.NotEqual(t, impactTopic, event.Topics[0])
	}
}

func TestSwapRefusalsAreAtomic(t *testing.T) {
	for _, tc := range []struct {
		name    string
		invoke  func(swapFixture) (spotkeeper.SwapResult, error)
		refusal *types.AbiError
	}{
		{"expired", func(f swapFixture) (spotkeeper.SwapResult, error) {
			return f.keeper.MarketBuy(f.ctx, f.user, f.stable, f.pax, big.NewInt(1000_000000), new(big.Int), big.NewInt(99))
		}, types.ErrExpired},
		{"slippage", func(f swapFixture) (spotkeeper.SwapResult, error) {
			return f.keeper.MarketBuy(f.ctx, f.user, f.stable, f.pax, big.NewInt(1000_000000), big.NewInt(2000_000000), new(big.Int))
		}, types.ErrInsufficientOutput},
		{"same_token", func(f swapFixture) (spotkeeper.SwapResult, error) {
			return f.keeper.SwapExactIn(f.ctx, f.user, f.pax, f.pax, big.NewInt(1), new(big.Int), new(big.Int))
		}, types.ErrSameToken},
		{"zero", func(f swapFixture) (spotkeeper.SwapResult, error) {
			return f.keeper.SwapExactIn(f.ctx, f.user, f.stable, f.pax, new(big.Int), new(big.Int), new(big.Int))
		}, types.ErrZeroAmount},
		{"native_deposit", func(f swapFixture) (spotkeeper.SwapResult, error) {
			return f.keeper.SwapNative(f.ctx, f.user, f.pax, big.NewInt(1), new(big.Int), new(big.Int), true)
		}, types.ErrUseWethDeposit},
		{"native_withdraw", func(f swapFixture) (spotkeeper.SwapResult, error) {
			return f.keeper.SwapNative(f.ctx, f.user, f.pax, big.NewInt(1), new(big.Int), new(big.Int), false)
		}, types.ErrUseWethWithdraw},
		{"excessive_input", func(f swapFixture) (spotkeeper.SwapResult, error) {
			return f.keeper.SwapExactOut(f.ctx, f.user, f.stable, f.pax, big.NewInt(1000_000000), big.NewInt(1), new(big.Int))
		}, types.ErrExcessiveInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSwapFixture(t, 2000_000000, 10000)
			before := swapStoreSnapshot(t, f.ctx.KVStore(f.key))
			bank := testkeeper.EVMTestApp.BankKeeper
			userBefore := bank.GetAllBalances(f.ctx, f.account)
			custodyBefore := bank.GetAllBalances(f.ctx, spotkeeper.CustodyAddress())
			result, err := tc.invoke(f)
			require.ErrorIs(t, err, tc.refusal)
			var refusal *types.AbiError
			require.ErrorAs(t, err, &refusal)
			require.Equal(t, tc.refusal.Data(), refusal.Data())
			require.Empty(t, result.Logs)
			require.Equal(t, before, swapStoreSnapshot(t, f.ctx.KVStore(f.key)))
			require.Equal(t, userBefore, bank.GetAllBalances(f.ctx, f.account))
			require.Equal(t, custodyBefore, bank.GetAllBalances(f.ctx, spotkeeper.CustodyAddress()))
		})
	}
}
