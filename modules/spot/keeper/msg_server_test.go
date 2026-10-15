package keeper_test

import (
	"math/big"
	"testing"

	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

const syncDenom = "uspotsync"

var (
	governanceAuthority = authtypes.NewModuleAddress(govtypes.ModuleName).String()
	spotModuleAccount   = authtypes.NewModuleAddress(types.ModuleName)
	pecor               = common.HexToAddress(types.PECORAddress)
	pecorOrders         = common.HexToAddress(types.PECOROrdersAddress)
	pecorStopOrders     = common.HexToAddress(types.PECORStopOrdersAddress)
	pecorVault          = common.HexToAddress(types.PECORVaultAddress)
	syncPointer         = common.BytesToAddress(crypto.Keccak256([]byte("spot sync pointer"))[12:])
)

// newSpotEnv builds the spot keeper over the bank and EVM keepers of the test
// application and the real pricefeed keeper, on its own in-memory store at
// height 10, with syncPointer registered as the native pointer of syncDenom,
// and imports gs.
func newSpotEnv(t *testing.T, gs types.GenesisState) (*keeper.Keeper, sdk.Context) {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockHeight(10)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	pricefeed, ctx := pricefeedtestutil.NewKeeper(app, ctx)
	key := sdk.NewKVStoreKey(types.StoreKey)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, key)
	require.NoError(t, app.EvmKeeper.SetERC20NativePointer(ctx, syncDenom, syncPointer))
	k := keeper.NewKeeper(key, app.BankKeeper, &app.EvmKeeper, pricefeed)
	k.InitGenesis(ctx, gs)
	return k, ctx
}

func word(value int64) []byte { return common.LeftPadBytes(big.NewInt(value).Bytes(), 32) }

func requireLog(t *testing.T, l *ethtypes.Log, address common.Address, topics []common.Hash, words ...[]byte) {
	t.Helper()
	require.Equal(t, address, l.Address)
	require.Equal(t, topics, l.Topics)
	var data []byte
	for _, w := range words {
		data = append(data, w...)
	}
	require.Equal(t, data, l.Data)
}

func logsSince(ctx sdk.Context, before int) []*ethtypes.Log {
	return ctx.EVMEventManager().Events()[before:]
}

func getParams(t *testing.T, k *keeper.Keeper, ctx sdk.Context) types.Params {
	t.Helper()
	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	return params
}

func syncToken(capBps uint64, paused bool) types.TokenParams {
	return types.TokenParams{Address: syncPointer, Kind: types.TokenKindNative, Decimals: 6, MarketID: 12,
		Paused: paused, MaxUtilizationBps: capBps}
}

func TestParamChangeEmitsPauseLogs(t *testing.T) {
	require.Equal(t, common.HexToHash("0x62e78cea01bee320cd4e420270b5ea74000d11b0c9f74754ebdbfc544b05a258"), keeper.PausedTopic)
	require.Equal(t, common.HexToHash("0x5db9ee0a495bf2e6ff9c91a7834c1ba4fdd244a5e8aa4e537bd38aeae4b073aa"), keeper.UnpausedTopic)
	require.Equal(t, common.HexToHash("0x53c5e83e6ce0cd0796333da83f54e0738e25b57e301950baab492700fc969808"),
		keeper.TwapSanityUpdatedTopic)
	require.Equal(t, common.HexToHash("0x253b2f7aa4de4ccf23ab8e9c2078ab1cc589649293d26006ecdf802a5d6b451d"),
		keeper.TokenPauseUpdatedTopic)
	require.Equal(t, common.HexToHash("0xbcfb6f6e36e3fb105ffb3042d755b572cec796e9c4c634f0bb7ef3c8967331ac"),
		keeper.UtilizationCapUpdatedTopic)

	k, ctx := newSpotEnv(t, *types.DefaultGenesis())
	server := keeper.NewMsgServerImpl(k)
	governance := common.LeftPadBytes(types.GovernanceEVMAddress().Bytes(), 32)
	tokenTopic := common.BytesToHash(syncPointer.Bytes())
	update := func(params types.Params) ([]*ethtypes.Log, error) {
		before := len(ctx.EVMEventManager().Events())
		_, err := server.UpdateParams(sdk.WrapSDKContext(ctx), &types.MsgUpdateParams{Authority: governanceAuthority,
			Params: params})
		return logsSince(ctx, before), err
	}

	params := getParams(t, k, ctx)
	params.SwapPaused = true
	params.Tokens = []types.TokenParams{syncToken(9_000, false)}
	logs, err := update(params)
	require.NoError(t, err)
	require.Len(t, logs, 2)
	requireLog(t, logs[0], pecor, []common.Hash{keeper.PausedTopic}, governance)
	requireLog(t, logs[1], pecorVault, []common.Hash{keeper.UtilizationCapUpdatedTopic, tokenTopic}, word(9_000))
	require.True(t, getParams(t, k, ctx).SwapPaused)

	params.SwapPaused, params.OrdersPaused = false, true
	logs, err = update(params)
	require.NoError(t, err)
	require.Len(t, logs, 3)
	requireLog(t, logs[0], pecor, []common.Hash{keeper.UnpausedTopic}, governance)
	requireLog(t, logs[1], pecorOrders, []common.Hash{keeper.PausedTopic}, governance)
	requireLog(t, logs[2], pecorStopOrders, []common.Hash{keeper.PausedTopic}, governance)

	params.OrdersPaused = false
	params.TwapSanityEnabled, params.TwapSanityPeriod, params.MaxTwapDeviationBps = true, 600, 150
	params.Tokens = []types.TokenParams{syncToken(8_000, true)}
	logs, err = update(params)
	require.NoError(t, err)
	require.Len(t, logs, 5)
	requireLog(t, logs[0], pecorOrders, []common.Hash{keeper.UnpausedTopic}, governance)
	requireLog(t, logs[1], pecorStopOrders, []common.Hash{keeper.UnpausedTopic}, governance)
	requireLog(t, logs[2], pecor, []common.Hash{keeper.TwapSanityUpdatedTopic}, word(1), word(600), word(150))
	requireLog(t, logs[3], pecorVault, []common.Hash{keeper.TokenPauseUpdatedTopic, tokenTopic}, word(1))
	requireLog(t, logs[4], pecorVault, []common.Hash{keeper.UtilizationCapUpdatedTopic, tokenTopic}, word(8_000))

	logs, err = update(params)
	require.NoError(t, err)
	require.Empty(t, logs)

	stored := getParams(t, k, ctx)
	notPointer := common.BytesToAddress(crypto.Keccak256([]byte("not a pointer"))[12:])
	refused := params
	refused.SwapPaused = true
	refused.Tokens = append([]types.TokenParams{}, params.Tokens...)
	refused.Tokens = append(refused.Tokens, types.TokenParams{Address: notPointer, Kind: types.TokenKindNative,
		Decimals: 6, MarketID: 11, MaxUtilizationBps: 9_000})
	logs, err = update(refused)
	require.ErrorIs(t, err, types.ErrParamsUnsupportedToken)
	require.Empty(t, logs)
	require.Equal(t, stored, getParams(t, k, ctx))

	outsider, _ := testkeeper.MockAddressPair()
	before := len(ctx.EVMEventManager().Events())
	_, err = server.UpdateParams(sdk.WrapSDKContext(ctx), &types.MsgUpdateParams{Authority: outsider.String(),
		Params: refused})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.Empty(t, logsSince(ctx, before))
	require.Equal(t, stored, getParams(t, k, ctx))
}

func TestMsgSyncReserves(t *testing.T) {
	require.Equal(t, common.HexToHash("0x055fb2ac115b8a1258ed74cd751869f95f1fb10b2c889147acbaca28ba479e5f"),
		keeper.ReservesSyncTopic)
	require.Equal(t, common.HexToHash("0xa2c850dc6f3f3d2193ff68281de055559c5468fc0c89311bca2d25f8e50a38f5"),
		keeper.ReservesUpdatedTopic)

	user := common.BytesToAddress(crypto.Keccak256([]byte("spot sync user"))[12:])
	gs := types.DefaultGenesis()
	gs.Params.Tokens = []types.TokenParams{syncToken(9_000, false)}
	gs.Books = []types.TokenBook{{Token: syncPointer, Reserves: sdk.NewInt(1_000), TotalDeposited: sdk.NewInt(1_000),
		TotalWithdrawn: sdk.ZeroInt(), Decimals: 6, ShareDenom: "spot/share/" + syncPointer.Hex()}}
	gs.NextOrderID = 2
	gs.LimitOrders = []types.LimitOrder{{ID: 1, User: user, Stablecoin: common.HexToAddress(types.USDLAddress),
		Token: syncPointer, Amount: sdk.NewInt(200), TargetPrice: sdk.NewInt(1), OrderType: types.OrderTypeLimitSell,
		Status: types.OrderStatusPending, CreatedAt: 1}}
	gs.UserOrders = []types.UserOrder{{User: user, OrderID: 1, OrderType: types.OrderTypeLimitSell}}
	gs.ActiveLimitOrders = []uint64{1}
	gs.Escrows = []types.TokenEscrow{{Token: syncPointer, Amount: sdk.NewInt(200)}}
	gs.ActiveCounts = []types.ActiveOrderCount{{User: user, Count: 1}}
	k, ctx := newSpotEnv(t, *gs)
	server := keeper.NewMsgServerImpl(k)
	bank := testkeeper.EVMTestApp.BankKeeper
	coins := func(amount int64) sdk.Coins { return sdk.NewCoins(sdk.NewCoin(syncDenom, sdk.NewInt(amount))) }
	require.NoError(t, bank.MintCoins(ctx, "evm", coins(1_500)))
	require.NoError(t, bank.SendCoinsFromModuleToAccount(ctx, "evm", spotModuleAccount, coins(1_500)))

	tokenTopic := common.BytesToHash(syncPointer.Bytes())
	sync := func(authority string, tokens ...string) ([]*ethtypes.Log, error) {
		before := len(ctx.EVMEventManager().Events())
		_, err := server.SyncReserves(sdk.WrapSDKContext(ctx), &types.MsgSyncReserves{Authority: authority, Tokens: tokens})
		return logsSince(ctx, before), err
	}
	book := func() types.TokenBook {
		books := k.ExportGenesis(ctx).Books
		require.Len(t, books, 1)
		return books[0]
	}

	outsider, _ := testkeeper.MockAddressPair()
	logs, err := sync(outsider.String(), syncPointer.Hex())
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.Empty(t, logs)
	require.Equal(t, sdk.NewInt(1_000), book().Reserves)

	unregistered := common.BytesToAddress(crypto.Keccak256([]byte("unregistered"))[12:])
	logs, err = sync(governanceAuthority, syncPointer.Hex(), unregistered.Hex())
	require.ErrorIs(t, err, types.ErrTokenNotRegistered)
	require.Empty(t, logs)
	require.Equal(t, sdk.NewInt(1_000), book().Reserves)

	// custody 1500 - escrow 200 = 1300 over the book 1000 recovers 300.
	logs, err = sync(governanceAuthority, syncPointer.Hex())
	require.NoError(t, err)
	require.Len(t, logs, 2)
	requireLog(t, logs[0], pecorVault, []common.Hash{keeper.ReservesSyncTopic, tokenTopic}, word(1_000), word(1_300),
		word(300))
	requireLog(t, logs[1], pecorVault, []common.Hash{keeper.ReservesUpdatedTopic, tokenTopic}, word(1_000), word(1_300))
	require.Equal(t, sdk.NewInt(1_300), book().Reserves)
	require.Equal(t, sdk.NewInt(1_300), book().TotalDeposited)

	logs, err = sync(governanceAuthority, syncPointer.Hex())
	require.NoError(t, err)
	require.Empty(t, logs)

	// custody 1100 - escrow 200 = 900 under the book 1300: a deficit, recovered 0.
	require.NoError(t, bank.SendCoins(ctx, spotModuleAccount, outsider, coins(400)))
	logs, err = sync(governanceAuthority, syncPointer.Hex())
	require.NoError(t, err)
	require.Len(t, logs, 2)
	requireLog(t, logs[0], pecorVault, []common.Hash{keeper.ReservesSyncTopic, tokenTopic}, word(1_300), word(900),
		word(0))
	requireLog(t, logs[1], pecorVault, []common.Hash{keeper.ReservesUpdatedTopic, tokenTopic}, word(1_300), word(900))
	require.Equal(t, sdk.NewInt(900), book().Reserves)
	require.Equal(t, sdk.NewInt(1_300), book().TotalDeposited)

	require.NoError(t, bank.SendCoins(ctx, spotModuleAccount, outsider, coins(1_000)))
	logs, err = sync(governanceAuthority, syncPointer.Hex())
	require.ErrorIs(t, err, types.ErrInsufficientLiquidity)
	require.Empty(t, logs)
	require.Equal(t, sdk.NewInt(900), book().Reserves)
}
