package keeper

import (
	"encoding/json"
	"math/big"
	"os"
	"testing"

	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

func adlWord(t *testing.T, value string) uint256.Int {
	t.Helper()
	n, ok := new(big.Int).SetString(value, 10)
	require.True(t, ok)
	require.NotEqual(t, -1, n.Sign())
	out, overflow := uint256.FromBig(n)
	require.False(t, overflow)
	return *out
}

func adlUSD(value uint64) uint256.Int {
	var out uint256.Int
	out.Mul(uint256.NewInt(value), uint256.NewInt(1_000_000_000_000_000_000))
	return out
}

func adlTestKeeper(t *testing.T) (*Keeper, sdk.Context) {
	t.Helper()
	db := dbm.NewMemDB()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ms := store.NewCommitMultiStore(db)
	key := sdk.NewKVStoreKey(types.StoreKey)
	ms.MountStoreWithDB(key, sdk.StoreTypeIAVL, db)
	require.NoError(t, ms.LoadLatestVersion())
	ctx := sdk.NewContext(ms, tmproto.Header{Height: 2}, false)
	k := &Keeper{storeKey: key}
	params, err := json.Marshal(types.DefaultParams())
	require.NoError(t, err)
	ctx.KVStore(key).Set(types.ParamsKey, params)
	market := types.Market{
		ID: 0, Symbol: "BTC", Name: "Bitcoin", OracleSymbol: "BTC", SzDecimals: 5, PxDecimals: 1,
		MaxLeverage: 1000, TierClass: types.TierClassA, Enabled: true,
		ImpactNotionalUsd: sdk.NewIntWithDecimal(20_000, 18),
		OiCapUsd:          sdk.NewIntWithDecimal(1_000_000_000, 18), FundingCapPpmPerHour: 40_000,
	}
	require.NoError(t, market.Validate())
	raw, err := json.Marshal(market)
	require.NoError(t, err)
	ctx.KVStore(key).Set(types.MarketKey(0), raw)
	for _, name := range []string{types.InsuranceAccountName, types.FeesAccountName} {
		account := types.Account{Address: common.BytesToAddress(authtypes.NewModuleAddress(name)), Created: true}
		ctx.KVStore(key).Set(types.AccountKey(account.Address), account.Marshal())
	}
	ctx.KVStore(key).Set(types.VenueBooksKey, (types.VenueBooks{}).Marshal())
	ctx.KVStore(key).Set(types.MarketStateKey(0), (types.MarketState{MarketID: 0, MarkPrice: adlUSD(61_000), Fresh: true}).Marshal())
	return k, ctx
}

func adlSeedPosition(t *testing.T, k *Keeper, ctx sdk.Context, position types.Position, collateral, deficit uint256.Int) {
	t.Helper()
	store := ctx.KVStore(k.storeKey)
	position.ReferencePrice = position.EntryPrice
	cost, err := adlMulDiv(position.SizeBase, position.EntryPrice, adlUSD(1))
	require.NoError(t, err)
	position.CostBasisUsd = cost
	position.Leverage = adlUSD(1)
	store.Set(types.PositionKey(position.ID), position.Marshal())
	id := position.ID.Bytes32()
	store.Set(types.AccountMarketPositionKey(position.Account, position.MarketID), id[:])
	store.Set(types.AdlRankIndexKey(position.MarketID, types.SideOf(position.IsLong), position.RankScore, position.ID), []byte{})
	account := types.Account{Address: position.Account, Collateral: collateral, DeficitUsd: deficit, PositionCount: 1, Created: true}
	store.Set(types.AccountKey(position.Account), account.Marshal())
	store.Set(types.MarginRatioIndexKey(0, position.Account), []byte{})
	var state types.MarketState
	require.NoError(t, state.Unmarshal(store.Get(types.MarketStateKey(position.MarketID))))
	if position.IsLong {
		state.LongOpenInterest.Add(&state.LongOpenInterest, &position.SizeBase)
	} else {
		state.ShortOpenInterest.Add(&state.ShortOpenInterest, &position.SizeBase)
	}
	store.Set(types.MarketStateKey(position.MarketID), state.Marshal())
	var books types.VenueBooks
	require.NoError(t, books.Unmarshal(store.Get(types.VenueBooksKey)))
	books.DeficitSumUsd.Add(&books.DeficitSumUsd, &deficit)
	store.Set(types.VenueBooksKey, books.Marshal())
}

func adlPositionFixture(id uint64, long bool, size, entry, score uint256.Int) types.Position {
	return types.Position{ID: *uint256.NewInt(id), Account: common.BigToAddress(new(big.Int).SetUint64(id)),
		MarketID: 0, IsLong: long, SizeBase: size, EntryPrice: entry, RankScore: score}
}

func TestAdlRankOrder(t *testing.T) {
	k, ctx := adlTestKeeper(t)
	deficit := adlPositionFixture(99, true, adlUSD(1), adlUSD(70_000), uint256.Int{})
	adlSeedPosition(t, k, ctx, deficit, uint256.Int{}, adlUSD(2_000))
	for _, id := range []uint64{3, 2, 1} {
		entry, equity := adlUSD(75_000), adlUSD(30_000)
		want := adlWord(t, "2500000000000000000")
		if id == 3 {
			entry, equity = adlUSD(72_000), adlUSD(60_000)
			want = adlWord(t, "1200000000000000000")
		}
		position := adlPositionFixture(id, false, adlUSD(1), entry, want)
		score, profitable, err := ADLRankScore(position, adlUSD(60_000), equity)
		require.NoError(t, err)
		require.True(t, profitable)
		require.Equal(t, want, score)
		adlSeedPosition(t, k, ctx, position, *uint256.NewInt(1_000_000_000), uint256.Int{})
	}
	loser := adlPositionFixture(4, false, adlUSD(1), adlUSD(59_000), adlUSD(100))
	sameSide := adlPositionFixture(5, true, adlUSD(1), adlUSD(59_000), adlUSD(200))
	adlSeedPosition(t, k, ctx, loser, *uint256.NewInt(1_000_000_000), uint256.Int{})
	adlSeedPosition(t, k, ctx, sameSide, *uint256.NewInt(1_000_000_000), uint256.Int{})
	positions, err := k.RankADL(ctx, deficit.ID, adlUSD(60_000), 64)
	require.NoError(t, err)
	require.Len(t, positions, 3)
	for i := range positions {
		require.Equal(t, *uint256.NewInt(uint64(i + 1)), positions[i].ID)
	}
	positions, err = k.RankADL(ctx, deficit.ID, adlUSD(60_000), 2)
	require.NoError(t, err)
	require.Len(t, positions, 2)
	require.Equal(t, *uint256.NewInt(2), positions[1].ID)
}

func adlEvents(events []types.EventRecord, name string) []types.EventRecord {
	var selected []types.EventRecord
	for _, event := range events {
		if event.Name == name {
			selected = append(selected, event)
		}
	}
	return selected
}

func TestAdlExecutesAtPreviousMark(t *testing.T) {
	k, ctx := adlTestKeeper(t)
	deficit := adlPositionFixture(99, true, adlUSD(1), adlUSD(70_000), uint256.Int{})
	first := adlPositionFixture(1, false, adlWord(t, "600000000000000000"), adlUSD(66_000), adlUSD(5))
	second := adlPositionFixture(2, false, adlWord(t, "800000000000000000"), adlUSD(66_000), adlUSD(4))
	adlSeedPosition(t, k, ctx, deficit, uint256.Int{}, adlUSD(2_000))
	adlSeedPosition(t, k, ctx, first, *uint256.NewInt(12_000_000_000), uint256.Int{})
	adlSeedPosition(t, k, ctx, second, *uint256.NewInt(12_000_000_000), uint256.Int{})
	balancing := adlPositionFixture(98, true, adlWord(t, "400000000000000000"), adlUSD(60_000), uint256.Int{})
	adlSeedPosition(t, k, ctx, balancing, *uint256.NewInt(30_000_000_000), uint256.Int{})
	price := adlUSD(60_000)
	events, err := k.ExecuteADL(ctx, []uint256.Int{deficit.ID}, map[uint64]uint256.Int{0: price})
	require.NoError(t, err)
	matches := adlEvents(events, types.EventADLMatched)
	require.Equal(t, []types.EventRecord{
		{Name: types.EventADLMatched, Args: []any{big.NewInt(0), first.ID.ToBig(), first.Account, adlBig(36_000), price.ToBig(), first.RankScore.ToBig()}},
		{Name: types.EventADLMatched, Args: []any{big.NewInt(0), second.ID.ToBig(), second.Account, adlBig(24_000), price.ToBig(), second.RankScore.ToBig()}},
	}, matches)
	require.Equal(t, []types.EventRecord{{Name: types.EventADLExecuted, Args: []any{deficit.ID.ToBig(), adlBig(60_000)}}}, adlEvents(events, types.EventADLExecuted))
	_, err = adlPosition(ctx.KVStore(k.storeKey), deficit.ID)
	require.ErrorIs(t, err, types.ErrUnknownPosition)
	_, err = adlPosition(ctx.KVStore(k.storeKey), first.ID)
	require.ErrorIs(t, err, types.ErrUnknownPosition)
	remaining, err := adlPosition(ctx.KVStore(k.storeKey), second.ID)
	require.NoError(t, err)
	require.Equal(t, adlWord(t, "400000000000000000"), remaining.SizeBase)
	var state types.MarketState
	require.NoError(t, state.Unmarshal(ctx.KVStore(k.storeKey).Get(types.MarketStateKey(0))))
	require.Equal(t, adlWord(t, "400000000000000000"), state.LongOpenInterest)
	require.Equal(t, state.LongOpenInterest, state.ShortOpenInterest)
	require.Equal(t, adlUSD(61_000), state.MarkPrice)
}

func adlBig(value uint64) *big.Int {
	word := adlUSD(value)
	return word.ToBig()
}

func TestAdlOnlyAfterInsurance(t *testing.T) {
	k, ctx := adlTestKeeper(t)
	deficit := adlPositionFixture(99, true, adlUSD(1), adlUSD(70_000), uint256.Int{})
	adlSeedPosition(t, k, ctx, deficit, uint256.Int{}, adlUSD(2_000))
	insuranceAddress := common.BytesToAddress(authtypes.NewModuleAddress(types.InsuranceAccountName))
	insurance := types.Account{Address: insuranceAddress, Collateral: *uint256.NewInt(10_000_000_000), Created: true}
	ctx.KVStore(k.storeKey).Set(types.AccountKey(insuranceAddress), insurance.Marshal())
	before := ctx.KVStore(k.storeKey).Get(types.PositionKey(deficit.ID))
	events, err := k.ExecuteADL(ctx, []uint256.Int{deficit.ID}, map[uint64]uint256.Int{0: adlUSD(60_000)})
	require.NoError(t, err)
	require.Empty(t, events)
	require.Equal(t, before, ctx.KVStore(k.storeKey).Get(types.PositionKey(deficit.ID)))
	events, err = k.CoverDeficit(ctx, deficit.Account)
	require.NoError(t, err)
	require.Empty(t, adlEvents(events, types.EventADLExecuted))
	account, err := adlAccount(ctx.KVStore(k.storeKey), deficit.Account)
	require.NoError(t, err)
	require.Equal(t, uint256.Int{}, account.DeficitUsd)
	insurance, err = adlAccount(ctx.KVStore(k.storeKey), insuranceAddress)
	require.NoError(t, err)
	require.Equal(t, *uint256.NewInt(8_000_000_000), insurance.Collateral)
}

type adlGoldenCase struct {
	Name           string `json:"name"`
	DeficitID      uint64 `json:"deficit_id"`
	DeficitLong    bool   `json:"deficit_long"`
	DeficitSize    string `json:"deficit_size"`
	DeficitUsd     string `json:"deficit_usd"`
	DeficitEntry   string `json:"deficit_entry"`
	PreviousMark   string `json:"previous_mark"`
	CurrentMark    string `json:"current_mark"`
	Counterparties []struct {
		ID      uint64 `json:"id"`
		Size    string `json:"size"`
		Entry   string `json:"entry"`
		Rank    string `json:"rank"`
		Reduced string `json:"reduced"`
		Price   string `json:"price"`
	} `json:"counterparties"`
	Remaining string `json:"remaining"`
	Matches   int    `json:"matches"`
}

func TestAdlGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/adl_golden.json")
	require.NoError(t, err)
	var cases []adlGoldenCase
	require.NoError(t, json.Unmarshal(raw, &cases))
	require.Len(t, cases, 3)
	for _, vector := range cases {
		t.Run(vector.Name, func(t *testing.T) {
			k, ctx := adlTestKeeper(t)
			current := adlWord(t, vector.CurrentMark)
			ctx.KVStore(k.storeKey).Set(types.MarketStateKey(0), (types.MarketState{MarketID: 0, MarkPrice: current, Fresh: true}).Marshal())
			deficit := adlPositionFixture(vector.DeficitID, vector.DeficitLong, adlWord(t, vector.DeficitSize), adlWord(t, vector.DeficitEntry), uint256.Int{})
			adlSeedPosition(t, k, ctx, deficit, uint256.Int{}, adlWord(t, vector.DeficitUsd))
			for _, counterparty := range vector.Counterparties {
				p := adlPositionFixture(counterparty.ID, !vector.DeficitLong, adlWord(t, counterparty.Size), adlWord(t, counterparty.Entry), adlWord(t, counterparty.Rank))
				adlSeedPosition(t, k, ctx, p, *uint256.NewInt(100_000_000_000), uint256.Int{})
			}
			var totalCounterpartySize uint256.Int
			for _, counterparty := range vector.Counterparties {
				size := adlWord(t, counterparty.Size)
				totalCounterpartySize.Add(&totalCounterpartySize, &size)
			}
			if totalCounterpartySize != deficit.SizeBase {
				var difference uint256.Int
				long := deficit.IsLong
				entry := adlWord(t, vector.PreviousMark)
				if totalCounterpartySize.Gt(&deficit.SizeBase) {
					difference.Sub(&totalCounterpartySize, &deficit.SizeBase)
				} else {
					difference.Sub(&deficit.SizeBase, &totalCounterpartySize)
					long = !long
				}
				balancing := adlPositionFixture(98, long, difference, entry, uint256.Int{})
				adlSeedPosition(t, k, ctx, balancing, *uint256.NewInt(100_000_000_000), uint256.Int{})
			}
			events, err := k.ExecuteADL(ctx, []uint256.Int{deficit.ID}, map[uint64]uint256.Int{0: adlWord(t, vector.PreviousMark)})
			require.NoError(t, err)
			matched := adlEvents(events, types.EventADLMatched)
			require.Len(t, matched, vector.Matches)
			next := 0
			for _, counterparty := range vector.Counterparties {
				reduced := adlWord(t, counterparty.Reduced)
				original := adlWord(t, counterparty.Size)
				var expected uint256.Int
				expected.Sub(&original, &reduced)
				if !reduced.IsZero() {
					price := adlWord(t, counterparty.Price)
					notional, err := adlMulDiv(reduced, price, adlUSD(1))
					require.NoError(t, err)
					rank := adlWord(t, counterparty.Rank)
					require.Equal(t, types.EventRecord{Name: types.EventADLMatched,
						Args: []any{big.NewInt(0), new(big.Int).SetUint64(counterparty.ID), common.BigToAddress(new(big.Int).SetUint64(counterparty.ID)),
							notional.ToBig(), price.ToBig(), rank.ToBig()}}, matched[next])
					next++
				}
				position, err := adlPosition(ctx.KVStore(k.storeKey), *uint256.NewInt(counterparty.ID))
				if expected.IsZero() {
					require.ErrorIs(t, err, types.ErrUnknownPosition)
				} else {
					require.NoError(t, err)
					require.Equal(t, expected, position.SizeBase)
				}
			}
			wantRemaining := adlWord(t, vector.Remaining)
			position, err := adlPosition(ctx.KVStore(k.storeKey), deficit.ID)
			if wantRemaining.IsZero() {
				require.ErrorIs(t, err, types.ErrUnknownPosition)
			} else {
				require.NoError(t, err)
				require.Equal(t, wantRemaining, position.SizeBase)
			}
			if vector.Matches == adlCounterpartiesPerBlock {
				nextPrice := adlWord(t, vector.CurrentMark)
				nextEvents, err := k.ExecuteADL(ctx.WithBlockHeight(ctx.BlockHeight()+1), []uint256.Int{deficit.ID}, map[uint64]uint256.Int{0: nextPrice})
				require.NoError(t, err)
				nextMatches := adlEvents(nextEvents, types.EventADLMatched)
				require.Len(t, nextMatches, 1)
				require.Equal(t, big.NewInt(65), nextMatches[0].Args[1])
				require.Equal(t, nextPrice.ToBig(), nextMatches[0].Args[4])
				_, err = adlPosition(ctx.KVStore(k.storeKey), deficit.ID)
				require.ErrorIs(t, err, types.ErrUnknownPosition)
			}
		})
	}
}

func TestAdlRefusals(t *testing.T) {
	k, ctx := adlTestKeeper(t)
	id := *uint256.NewInt(99)
	_, err := k.RankADL(ctx, id, uint256.Int{}, 64)
	require.ErrorIs(t, err, types.ErrPriceStale)
	_, err = k.RankADL(ctx, id, adlUSD(60_000), 65)
	require.ErrorIs(t, err, types.ErrInvalidParams)
	_, err = k.RankADL(ctx, id, adlUSD(60_000), 64)
	require.ErrorIs(t, err, types.ErrUnknownPosition)
	p := adlPositionFixture(99, true, adlUSD(1), adlUSD(70_000), uint256.Int{})
	adlSeedPosition(t, k, ctx, p, uint256.Int{}, adlUSD(2_000))
	before := ctx.KVStore(k.storeKey).Get(types.PositionKey(id))
	_, err = k.ExecuteADL(ctx, []uint256.Int{id}, nil)
	require.ErrorIs(t, err, types.ErrPriceStale)
	require.Equal(t, before, ctx.KVStore(k.storeKey).Get(types.PositionKey(id)))
	_, err = k.ExecuteADL(ctx, []uint256.Int{id, id}, map[uint64]uint256.Int{0: adlUSD(60_000)})
	require.ErrorIs(t, err, types.ErrInvalidParams)
	candidate := adlPositionFixture(1, false, adlUSD(1), adlUSD(66_000), adlUSD(5))
	adlSeedPosition(t, k, ctx, candidate, *uint256.NewInt(12_000_000_000), uint256.Int{})
	ctx.KVStore(k.storeKey).Delete(types.PositionKey(candidate.ID))
	_, err = k.ExecuteADL(ctx, []uint256.Int{id}, map[uint64]uint256.Int{0: adlUSD(60_000)})
	require.ErrorIs(t, err, types.ErrUnknownPosition)
	require.Equal(t, before, ctx.KVStore(k.storeKey).Get(types.PositionKey(id)))
}

func TestAdlRankScoreBounds(t *testing.T) {
	p := adlPositionFixture(1, false, adlUSD(1), adlUSD(66_000), uint256.Int{})
	score, profitable, err := ADLRankScore(p, adlUSD(60_000), adlUSD(12_000))
	require.NoError(t, err)
	require.True(t, profitable)
	require.Equal(t, adlWord(t, "5500000000000000000"), score)
	_, profitable, err = ADLRankScore(p, adlUSD(60_000), uint256.Int{})
	require.ErrorIs(t, err, types.ErrInsufficientMargin)
	require.True(t, profitable)
	score, profitable, err = ADLRankScore(p, adlUSD(66_000), adlUSD(12_000))
	require.NoError(t, err)
	require.False(t, profitable)
	require.Equal(t, uint256.Int{}, score)
	p.IsLong = true
	p.EntryPrice = adlUSD(50_000)
	score, profitable, err = ADLRankScore(p, adlUSD(60_000), adlUSD(12_000))
	require.NoError(t, err)
	require.True(t, profitable)
	require.Equal(t, adlUSD(6), score)
	p.SizeBase.SetAllOne()
	_, _, err = ADLRankScore(p, adlUSD(60_000), adlUSD(12_000))
	require.ErrorIs(t, err, types.ErrOverflow)
}
