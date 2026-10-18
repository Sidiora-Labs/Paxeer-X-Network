package keeper

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store"
	storetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

type quotesFixture struct {
	keeper                          Keeper
	ctx                             sdk.Context
	perpsKey, feedKey, transientKey sdk.StoreKey
	account                         common.Address
}

func quotesNumber(t *testing.T, value string) uint256.Int {
	t.Helper()
	number, ok := new(big.Int).SetString(value, 10)
	require.True(t, ok)
	var word uint256.Int
	require.False(t, word.SetFromBig(number))
	return word
}

func quotesJSON(t *testing.T, ctx sdk.Context, key sdk.StoreKey, path []byte, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	ctx.KVStore(key).Set(path, encoded)
}

func newQuotesFixture(t *testing.T) quotesFixture {
	t.Helper()
	key := sdk.NewKVStoreKey(types.StoreKey)
	feedKey := sdk.NewKVStoreKey(pricefeedtypes.StoreKey)
	transientKey := sdk.NewTransientStoreKey("quotes_transient")
	db := dbm.NewMemDB()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ms := store.NewCommitMultiStore(db)
	ms.MountStoreWithDB(key, storetypes.StoreTypeIAVL, nil)
	ms.MountStoreWithDB(feedKey, storetypes.StoreTypeIAVL, nil)
	ms.MountStoreWithDB(transientKey, storetypes.StoreTypeTransient, nil)
	require.NoError(t, ms.LoadLatestVersion())
	ctx := sdk.NewContext(ms, tmproto.Header{Height: 20, Time: time.Unix(7200, 0)}, false)
	feed := pricefeedkeeper.NewKeeper(feedKey, nil, nil, nil)
	require.NoError(t, feed.SetParams(ctx, pricefeedtypes.DefaultParams(pricefeedtypes.DefaultAuthority())))
	k := Keeper{storeKey: key, pricefeed: feed}
	quotesJSON(t, ctx, key, types.ParamsKey, types.DefaultParams())
	for _, id := range []uint64{0, 5} {
		market := types.Market{ID: id, Symbol: "BTC", Name: "Bitcoin", OracleSymbol: "BTC", SzDecimals: 5, PxDecimals: 1,
			MaxLeverage: 1000, TierClass: types.TierClassA, ImpactNotionalUsd: sdk.NewIntWithDecimal(20000, 18),
			OiCapUsd: sdk.NewIntWithDecimal(1000000000, 18), FundingCapPpmPerHour: 40000, Enabled: true}
		quotesJSON(t, ctx, key, types.MarketKey(id), market)
		state := types.MarketState{MarketID: id, IndexPrice: quotesNumber(t, "60000000000000000000000"),
			MarkPrice: quotesNumber(t, "60000000000000000000000"), PricefeedMark: quotesNumber(t, "60000000000000000000000"),
			PriceTimestampMs: uint64(ctx.BlockTime().UnixMilli()), Fresh: true, LastIndexHour: 2,
			HourlyRate: quotesNumber(t, "12500000000000")}
		ctx.KVStore(key).Set(types.MarketStateKey(id), state.Marshal())
		quotesJSON(t, ctx, feedKey, pricefeedtypes.AggregateKey(id), pricefeedtypes.Aggregate{IndexPrice: sdk.NewIntWithDecimal(60000, 18),
			MarkPrice: sdk.NewIntWithDecimal(60000, 18), Basis: sdk.ZeroInt(), TimestampMs: uint64(ctx.BlockTime().UnixMilli()), Height: 20, Sources: 1})
	}
	for _, address := range []common.Address{{19: 1}, {19: 2}, {19: 3},
		common.BytesToAddress(authtypes.NewModuleAddress(types.FeesAccountName)), common.BytesToAddress(authtypes.NewModuleAddress(types.InsuranceAccountName))} {
		account := types.Account{Address: address, Created: true}
		if address != (common.Address{19: 1}) {
			account.Collateral = *uint256.NewInt(1000000000000)
		}
		ctx.KVStore(key).Set(types.AccountKey(address), account.Marshal())
	}
	orderID := uint256.NewInt(1).Bytes32()
	positionID := uint256.NewInt(7).Bytes32()
	placement := uint256.NewInt(1).Bytes32()
	ctx.KVStore(key).Set(types.SequenceKey(types.SequenceOrderID), orderID[:])
	ctx.KVStore(key).Set(types.SequenceKey(types.SequencePositionID), positionID[:])
	ctx.KVStore(key).Set(types.SequenceKey(types.SequencePlacement), placement[:])
	ctx.KVStore(key).Set(types.VenueBooksKey, (types.VenueBooks{}).Marshal())
	ctx.KVStore(transientKey).Set([]byte("receipt"), []byte("retained"))
	return quotesFixture{k, ctx, key, feedKey, transientKey, common.Address{19: 1}}
}

func (f quotesFixture) rest(t *testing.T, price string, isLong bool) {
	t.Helper()
	px := quotesNumber(t, price)
	size := new(big.Int).Quo(px.ToBig(), big.NewInt(100))
	sizeUsd := quotesNumber(t, size.String())
	_, err := f.keeper.PlaceOrder(f.ctx, common.Address{19: 2}, OrderParams{MarketID: 0, IsLong: isLong,
		OrderType: uint8(types.OrderTypeLimit), TimeInForce: uint8(types.TimeInForceGTC), LimitPrice: px,
		SizeUsd: sizeUsd, Leverage: quotesNumber(t, "10000000000000000000")})
	require.NoError(t, err)
}

func (f quotesFixture) open(t *testing.T, isLong bool) []types.EventRecord {
	t.Helper()
	_, err := f.keeper.Deposit(f.ctx, f.account, *uint256.NewInt(100000000))
	require.NoError(t, err)
	placed, err := f.keeper.PlaceOrder(f.ctx, f.account, OrderParams{MarketID: 0, IsLong: isLong,
		OrderType: uint8(types.OrderTypeMarket), TimeInForce: uint8(types.TimeInForceIOC),
		SizeUsd: quotesNumber(t, "990000000000000000000"), Leverage: quotesNumber(t, "10000000000000000000")})
	require.NoError(t, err)
	return placed.Events
}

func (f quotesFixture) positionID(t *testing.T) uint256.Int {
	t.Helper()
	data := f.ctx.KVStore(f.perpsKey).Get(types.AccountMarketPositionKey(f.account, 0))
	require.Len(t, data, 32)
	var id uint256.Int
	id.SetBytes(data)
	require.False(t, id.IsZero())
	return id
}

func quotesEventSum(t *testing.T, events []types.EventRecord, name string, account common.Address, index int) *big.Int {
	t.Helper()
	sum := new(big.Int)
	count := 0
	for _, event := range events {
		if event.Name != name {
			continue
		}
		require.Greater(t, len(event.Args), index)
		if event.Args[1] != account {
			continue
		}
		value, ok := event.Args[index].(*big.Int)
		require.True(t, ok)
		require.NotNil(t, value)
		sum.Add(sum, value)
		count++
	}
	require.Positive(t, count)
	return sum
}

func TestQuotesOpenEqualsExecution(t *testing.T) {
	f := newQuotesFixture(t)
	f.rest(t, "60000000000000000000000", false)
	f.rest(t, "60100000000000000000000", false)
	quote, err := f.keeper.QuoteOpen(f.ctx, f.account, 0, types.DefaultParams().CollateralToken,
		*uint256.NewInt(100000000), quotesNumber(t, "10000000000000000000"), true)
	require.NoError(t, err)
	require.Equal(t, "60039393939393939393939", quote.EntryPrice.String())
	require.Equal(t, "990650000000000000000", quote.SizeUsd.String())
	require.Equal(t, "100000000000000000000", quote.CollateralUsd.String())
	require.Equal(t, "10000000000000000000", quote.Leverage.String())
	require.Equal(t, "445793", quote.TradingFee.String())
	require.Equal(t, "445792500000000000", quote.TradingFeeUsd.String())
	require.Equal(t, "6", quote.PriceImpact.String())
	require.Equal(t, "5", quote.MaintenanceMarginBps.String())
	require.Equal(t, "-297000000000000000", quote.EstimatedFunding24h.String())
	events := f.open(t, true)
	require.Equal(t, quote.SizeUsd, quotesEventSum(t, events, types.EventTradeSettled, f.account, 4))
	require.Equal(t, quote.TradingFeeUsd, quotesEventSum(t, events, types.EventTradeSettled, f.account, 7))
	require.Equal(t, quote.TradingFee, quotesEventSum(t, events, types.EventFeeCollected, f.account, 5))
	var position types.Position
	require.NoError(t, position.Unmarshal(f.ctx.KVStore(f.perpsKey).Get(types.PositionKey(f.positionID(t)))))
	require.Equal(t, "16500000000000000", position.SizeBase.Dec())
	liquidationPrice, err := f.keeper.LiquidationPrice(f.ctx, position.ID)
	require.NoError(t, err)
	require.Equal(t, quote.LiquidationPrice, liquidationPrice.ToBig())
}

func TestQuotesCloseEqualsExecution(t *testing.T) {
	for _, partial := range []bool{false, true} {
		name := "full"
		if partial {
			name = "partial"
		}
		t.Run(name, func(t *testing.T) {
			f := newQuotesFixture(t)
			f.rest(t, "60000000000000000000000", false)
			f.rest(t, "60100000000000000000000", false)
			f.open(t, true)
			state, err := f.keeper.quoteState(f.ctx, 0)
			require.NoError(t, err)
			state.MarkPrice = quotesNumber(t, "61000000000000000000000")
			state.FundingIndex = quotesNumber(t, "1000000000000000000")
			f.ctx.KVStore(f.perpsKey).Set(types.MarketStateKey(0), state.Marshal())
			f.rest(t, "61000000000000000000000", true)
			f.rest(t, "61000000000000000000000", true)
			id := f.positionID(t)
			if partial {
				size := quotesNumber(t, "500000000000000000000")
				quote, err := f.keeper.QuotePartialClose(f.ctx, id, size)
				require.NoError(t, err)
				require.Equal(t, "61000000000000000000000", quote.ExitPrice.String())
				order, err := f.keeper.PartialClose(f.ctx, f.account, id, size)
				require.NoError(t, err)
				events, err := f.keeper.quotePlace(f.ctx, order)
				require.NoError(t, err)
				require.Equal(t, "499590000000000000000", quotesEventSum(t, events, types.EventTradeSettled, f.account, 4).String())
				require.Equal(t, quote.ClosedPnl, quotesEventSum(t, events, types.EventTradeSettled, f.account, 6))
				require.Equal(t, quote.Fee, quotesEventSum(t, events, types.EventFeeCollected, f.account, 5))
				require.Equal(t, "224816", quote.Fee.String())
				require.Equal(t, quote.EstimatedPayout, quotesEventSum(t, events, types.EventTradeSettled, f.account, 9))
			} else {
				quote, err := f.keeper.QuoteClose(f.ctx, id)
				require.NoError(t, err)
				require.Equal(t, "61000000000000000000000", quote.ExitPrice.String())
				require.Equal(t, "452925000000000000", quote.TradingFeeUsd.String())
				require.Equal(t, "452925", quote.TradingFee.String())
				require.Equal(t, "-16500000000000000", quote.FundingOwed.String())
				order, err := f.keeper.ClosePosition(f.ctx, f.account, id)
				require.NoError(t, err)
				events, err := f.keeper.quotePlace(f.ctx, order)
				require.NoError(t, err)
				require.Equal(t, quote.UnrealizedPnl, quotesEventSum(t, events, types.EventTradeSettled, f.account, 6))
				require.Equal(t, quote.FundingOwed, quotesEventSum(t, events, types.EventTradeSettled, f.account, 8))
				require.Equal(t, quote.TradingFeeUsd, quotesEventSum(t, events, types.EventTradeSettled, f.account, 7))
				require.Equal(t, quote.TradingFee, quotesEventSum(t, events, types.EventFeeCollected, f.account, 5))
				require.Equal(t, quote.EstimatedPayout, quotesEventSum(t, events, types.EventTradeSettled, f.account, 9))
				require.Equal(t, quote.NetPnl, new(big.Int).Sub(new(big.Int).Add(quote.UnrealizedPnl, quote.FundingOwed), quote.TradingFeeUsd))
				require.Nil(t, f.ctx.KVStore(f.perpsKey).Get(types.PositionKey(id)))
			}
		})
	}
}

func (f quotesFixture) hash(t *testing.T) []byte {
	t.Helper()
	h := sha256.New()
	for _, key := range []sdk.StoreKey{f.perpsKey, f.feedKey, f.transientKey} {
		it := f.ctx.KVStore(key).Iterator(nil, nil)
		for ; it.Valid(); it.Next() {
			for _, value := range [][]byte{it.Key(), it.Value()} {
				_, err := h.Write(binary.BigEndian.AppendUint64(nil, uint64(len(value))))
				require.NoError(t, err)
				_, err = h.Write(value)
				require.NoError(t, err)
			}
		}
		require.NoError(t, it.Close())
	}
	return h.Sum(nil)
}

func TestQuotesNoWrites(t *testing.T) {
	f := newQuotesFixture(t)
	f.rest(t, "60000000000000000000000", false)
	f.rest(t, "60100000000000000000000", false)
	f.open(t, true)
	f.rest(t, "60000000000000000000000", true)
	f.rest(t, "60000000000000000000000", true)
	f.rest(t, "60100000000000000000000", false)
	f.rest(t, "60100000000000000000000", false)
	id := f.positionID(t)
	before := f.hash(t)
	f.ctx = f.ctx.WithEventManager(sdk.NewEventManager())
	f.ctx.EventManager().EmitEvent(sdk.NewEvent("retained"))
	events := append(sdk.Events(nil), f.ctx.EventManager().Events()...)
	for i := 0; i < 100; i++ {
		switch i % 5 {
		case 0:
			_, err := f.keeper.QuoteOpen(f.ctx, common.Address{19: 3}, 0, types.DefaultParams().CollateralToken,
				*uint256.NewInt(100000000), quotesNumber(t, "10000000000000000000"), true)
			require.NoError(t, err)
		case 1:
			_, err := f.keeper.QuoteClose(f.ctx, id)
			require.NoError(t, err)
		case 2:
			_, err := f.keeper.QuotePartialClose(f.ctx, id, quotesNumber(t, "500000000000000000000"))
			require.NoError(t, err)
		case 3:
			_, err := f.keeper.QuoteMarket(f.ctx, 0)
			require.NoError(t, err)
		case 4:
			_, err := f.keeper.QuotePartialClose(f.ctx, id, quotesNumber(t, "10000000000000000000000"))
			require.ErrorIs(t, err, types.ErrCloseSizeExceedsPosition)
		}
		require.Equal(t, before, f.hash(t))
		require.Equal(t, events, f.ctx.EventManager().Events())
	}
}

func TestQuotesMarketStale(t *testing.T) {
	f := newQuotesFixture(t)
	for _, enabled := range []bool{true, false} {
		market, err := f.keeper.GetMarket(f.ctx, 5)
		require.NoError(t, err)
		market.Enabled = enabled
		quotesJSON(t, f.ctx, f.perpsKey, types.MarketKey(5), market)
		for _, age := range []uint64{pricefeedtypes.DefaultMaxPriceAgeMs, pricefeedtypes.DefaultMaxPriceAgeMs + 1} {
			ctx := f.ctx.WithBlockTime(f.ctx.BlockTime().Add(time.Duration(age) * time.Millisecond))
			before := f.hash(t)
			quote, err := f.keeper.QuoteMarket(ctx, 5)
			require.NoError(t, err)
			require.Equal(t, age > pricefeedtypes.DefaultMaxPriceAgeMs, quote.PriceStale)
			require.Equal(t, enabled, quote.Enabled)
			require.Equal(t, "60000000000000000000000", quote.IndexPrice.String())
			require.Equal(t, "60000000000000000000000", quote.MarkPrice.String())
			require.Equal(t, "60000000000000000000000", quote.OracleTWAP.String())
			require.Equal(t, "3472222222", quote.FundingRatePerSecond.String())
			require.Equal(t, "300000000000000", quote.FundingRate24h.String())
			require.Equal(t, "0", quote.LongOI.String())
			require.Equal(t, "0", quote.ShortOI.String())
			require.Equal(t, "1000", quote.MaxLeverage.String())
			require.Equal(t, "5", quote.MaintenanceMarginBps.String())
			require.Equal(t, before, f.hash(t))
		}
	}
	_, err := f.keeper.QuoteMarket(f.ctx, 999)
	require.ErrorIs(t, err, types.ErrUnknownMarket)
}
