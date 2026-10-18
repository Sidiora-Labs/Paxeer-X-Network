package keeper_test

import (
	"bytes"
	"fmt"
	"sort"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	keeperv610 "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/legacy/v610/keeper"
	typesv610 "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/legacy/v610/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	tokenfactorykeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/tokenfactory/keeper"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// upgradeError wraps a migration error the way the v6.12.0 upgrade handler
// wraps the launchpad hook.
func upgradeError(err error) error { return fmt.Errorf("upgrade v6.12.0: launchpad: %w", err) }

// newRecordEnv is newEnv with an event manager of its own, so the migration
// log of one test does not carry the events of another.
func newRecordEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	e.ctx = e.ctx.WithEventManager(sdk.NewEventManager())
	return e
}

func fixtureDeltas(t *testing.T) ([]types.BookHeldDelta, sdk.Int) {
	t.Helper()
	var pools v6120PoolsFixture
	decodeV6120Fixture(t, "pools.json", &pools)
	out := make([]types.BookHeldDelta, len(pools.Deltas))
	for n, d := range pools.Deltas {
		out[n] = types.BookHeldDelta{Pool: d.Pool, Asset: d.AssetAddress,
			Book:  sdk.NewIntFromBigInt(requireAmount(t, d.Book, false)),
			Held:  sdk.NewIntFromBigInt(requireAmount(t, d.Held, false)),
			Delta: sdk.NewIntFromBigInt(requireAmount(t, d.Delta, true))}
	}
	return out, sdk.NewIntFromBigInt(requireAmount(t, pools.Tolerance, false))
}

func storeEntries(ctx sdk.Context, key sdk.StoreKey) [][2][]byte {
	iterator := ctx.KVStore(key).Iterator(nil, nil)
	defer iterator.Close()
	out := [][2][]byte{}
	for ; iterator.Valid(); iterator.Next() {
		out = append(out, [2][]byte{append([]byte(nil), iterator.Key()...), append([]byte(nil), iterator.Value()...)})
	}
	return out
}

func eventsOf(ctx sdk.Context, eventType string) []map[string]string {
	out := []map[string]string{}
	for _, event := range ctx.EventManager().Events() {
		if event.Type != eventType {
			continue
		}
		attributes := map[string]string{}
		for _, attribute := range event.Attributes {
			attributes[string(attribute.Key)] = string(attribute.Value)
		}
		out = append(out, attributes)
	}
	return out
}

// TestBookHeldDeltaRecord records the five known deltas of pools.json and
// reads them back through the delta query and the migration log.
func TestBookHeldDeltaRecord(t *testing.T) {
	e := newRecordEnv(t)
	deltas, tolerance := fixtureDeltas(t)
	require.Len(t, deltas, 5)
	require.NoError(t, keeper.CheckDeltaTolerance(deltas, deltas, tolerance))
	for _, delta := range deltas {
		require.NoError(t, e.k.RecordBookHeldDelta(e.ctx, delta))
	}

	want := append([]types.BookHeldDelta(nil), deltas...)
	sort.Slice(want, func(x, y int) bool {
		return bytes.Compare(types.BookHeldDeltaKey(common.HexToAddress(want[x].Pool), common.HexToAddress(want[x].Asset)),
			types.BookHeldDeltaKey(common.HexToAddress(want[y].Pool), common.HexToAddress(want[y].Asset))) < 0
	})
	got, err := e.k.BookHeldDeltas(e.ctx)
	require.NoError(t, err)
	sameJSON(t, want, got)

	usdl := common.HexToAddress("0x85FcD13735F4309833A503EE804ea32395851479")
	one, found, err := e.k.GetBookHeldDelta(e.ctx, common.HexToAddress("0x26Fd86Ef00F40e204dF12f652d0C0527a17c49EA"), usdl)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "108968467255", one.Book.String())
	require.Equal(t, "107459616307", one.Held.String())
	require.Equal(t, "-1508850948", one.Delta.String())
	token, found, err := e.k.GetBookHeldDelta(e.ctx, common.HexToAddress("0x7bF2f96443A6D7b122DD73Ce79205c85eEdae738"),
		common.HexToAddress("0xd16EF71b702E1f503BA21C5f52BcF97154Ab1388"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "-5000000000000", token.Delta.String())
	_, found, err = e.k.GetBookHeldDelta(e.ctx, common.HexToAddress("0x7bF2f96443A6D7b122DD73Ce79205c85eEdae738"), usdl)
	require.NoError(t, err)
	require.False(t, found)

	logged := eventsOf(e.ctx, keeper.EventTypeMigrationBookHeldDelta)
	require.Len(t, logged, 5)
	for n, delta := range deltas {
		require.Equal(t, map[string]string{keeper.AttributeKeyPool: delta.Pool, keeper.AttributeKeyAsset: delta.Asset,
			keeper.AttributeKeyBook: delta.Book.String(), keeper.AttributeKeyHeld: delta.Held.String(),
			keeper.AttributeKeyDelta: delta.Delta.String()}, logged[n])
	}

	require.ErrorIs(t, e.k.RecordBookHeldDelta(e.ctx, deltas[0]), keeper.ErrInvalidMigrationRecord)
	zero := types.BookHeldDelta{Pool: deltas[0].Pool, Asset: deltas[3].Asset, Book: sdk.NewInt(7), Held: sdk.NewInt(7),
		Delta: sdk.ZeroInt()}
	require.ErrorIs(t, e.k.RecordBookHeldDelta(e.ctx, zero), keeper.ErrInvalidMigrationRecord)
	wrong := types.BookHeldDelta{Pool: deltas[0].Pool, Asset: deltas[3].Asset, Book: sdk.NewInt(7), Held: sdk.NewInt(5),
		Delta: sdk.NewInt(2)}
	require.ErrorIs(t, e.k.RecordBookHeldDelta(e.ctx, wrong), keeper.ErrInvalidMigrationRecord)
	lower := deltas[0]
	lower.Pool = "0x90665cf00c471060dfc8db4c3447e50e1092fc8b"
	require.ErrorIs(t, e.k.RecordBookHeldDelta(e.ctx, lower), keeper.ErrInvalidMigrationRecord)
	got, err = e.k.BookHeldDeltas(e.ctx)
	require.NoError(t, err)
	require.Len(t, got, 5)
}

// TestUnlistedDeltaRefuses checks a pool absent from pools.json that is short
// by 1 USDL against the fixture tolerance.
func TestUnlistedDeltaRefuses(t *testing.T) {
	e := newRecordEnv(t)
	known, tolerance := fixtureDeltas(t)
	require.True(t, tolerance.IsZero())
	unlisted := types.BookHeldDelta{Pool: "0xCE20Da50F92D21d8BeE497910c782E9Aa25b6263",
		Asset: "0x85FcD13735F4309833A503EE804ea32395851479", Book: sdk.NewInt(5_000_000), Held: sdk.NewInt(4_000_000),
		Delta: sdk.NewInt(-1_000_000)}
	read := append(append([]types.BookHeldDelta(nil), known...), unlisted)

	err := keeper.CheckDeltaTolerance(read, known, tolerance)
	require.ErrorIs(t, err, keeper.ErrUnlistedBookHeldDelta)
	require.EqualError(t, upgradeError(err), "upgrade v6.12.0: launchpad: unlisted book-held delta")
	got, err := e.k.BookHeldDeltas(e.ctx)
	require.NoError(t, err)
	require.Empty(t, got)
	require.Empty(t, eventsOf(e.ctx, keeper.EventTypeMigrationBookHeldDelta))

	surplus := unlisted
	surplus.Held, surplus.Delta = sdk.NewInt(6_000_000), sdk.NewInt(1_000_000)
	require.ErrorIs(t, keeper.CheckDeltaTolerance([]types.BookHeldDelta{surplus}, known, tolerance),
		keeper.ErrUnlistedBookHeldDelta)
	require.NoError(t, keeper.CheckDeltaTolerance(read, known, sdk.NewInt(1_000_000)))
	require.ErrorIs(t, keeper.CheckDeltaTolerance(read, known, sdk.NewInt(999_999)), keeper.ErrUnlistedBookHeldDelta)

	otherAsset := known[0]
	otherAsset.Asset = known[3].Asset
	require.ErrorIs(t, keeper.CheckDeltaTolerance([]types.BookHeldDelta{otherAsset}, known, tolerance),
		keeper.ErrUnlistedBookHeldDelta)
	require.ErrorIs(t, keeper.CheckDeltaTolerance(read, known, sdk.NewInt(-1)), keeper.ErrInvalidMigrationRecord)
	require.ErrorIs(t, keeper.CheckDeltaTolerance(read, known, sdk.Int{}), keeper.ErrInvalidMigrationRecord)
}

// TestNativeMarketGuard creates one native market through the v6.10 keeper
// over the launchpad store key and requires the guard to refuse without a
// write.
func TestNativeMarketGuard(t *testing.T) {
	app := testkeeper.EVMTestApp
	key := sdk.NewKVStoreKey(types.StoreKey)
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockTime(genesisTime)
	ctx = launchpadtest.WithStore(ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx)), key)
	k := keeper.NewKeeper(key, app.BankKeeper, tokenfactorykeeper.NewMsgServerImpl(app.TokenFactoryKeeper), &app.EvmKeeper)
	legacy := keeperv610.NewKeeper(key, app.BankKeeper, tokenfactorykeeper.NewMsgServerImpl(app.TokenFactoryKeeper),
		&app.EvmKeeper)
	legacy.InitGenesis(ctx, *typesv610.DefaultGenesis())
	require.NoError(t, k.CheckNoNativeMarkets(ctx))

	creator, _ := testkeeper.MockAddressPair()
	coins := sdk.NewCoins(sdk.NewCoin(typesv610.DefaultQuoteDenom, sdk.NewInt(250_000_000)))
	require.NoError(t, app.BankKeeper.MintCoins(ctx, "evm", coins))
	require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", creator, coins))
	_, err := legacy.CreateMarket(ctx, nil, creator, "Kindle Token", "KNDL", typesv610.FeeStrategyClaim)
	require.NoError(t, err)
	require.Equal(t, uint64(1), k.GetMarketCount(ctx))

	before := storeEntries(ctx, key)
	err = k.CheckNoNativeMarkets(ctx)
	require.ErrorIs(t, err, keeper.ErrNativeMarketsExist)
	require.EqualError(t, upgradeError(err), "upgrade v6.12.0: launchpad: native markets exist")
	require.Equal(t, before, storeEntries(ctx, key))
}

// retiredSection21 is the retired list of contracts/launchpad.md section 21.
func retiredSection21() []types.RetiredAddress {
	return []types.RetiredAddress{
		{Address: "0x8a1A09CEe72c1D39dF33B8284E38baeF8371f465", Role: types.RoleFactory,
			Replacement: "0x3993D004166738d1069B5C4e0a923f7B9D5D8830"},
		{Address: "0xCC7298801112682e10ee14b8a520309caD80336d", Role: types.RoleRouter,
			Replacement: "0x78FB06f525cD964388f2305A489035263E31C6e4"},
		{Address: "0xB768e183b6EfDeDf8b2AA7af732039D1C3c452d0", Role: types.RoleQuoter,
			Replacement: "0x793006f4F321b8863F26DBD75b11bAa01DA181dc"},
		{Address: "0xEeDF5409cFD30bd14D0399318c7d2150265575e5", Role: types.RoleProtocolConfig,
			Replacement: "0xF3669eDc475Cb6E1F0d7635240ABD8e8FB7E20C6"},
		{Address: "0x50C69dF6637b3DCE6a7407C5A4b4F99E68514A76", Role: types.RoleFeeAccumulator,
			Replacement: "0x7b14808b72f61664cDb79D25E6d1587655571a66"},
		{Address: "0x7684382c89f79104574D8EF9b31eFf2eD2C2BA0b", Role: types.RolePoolRegistry,
			Replacement: "0x1dC357d2B7B850332a91F06A9E66634F46fC4Ba7"},
		{Address: "0xDF73b354ed9dcB473cc9D01541c46f507591e190", Role: types.RoleFeeRights,
			Replacement: "0xe8DcC844a8006dbf64f5Bb851bCad90292a6409B"},
		{Address: "0x02Df12a44F2658080E76fbcF7D6B34Baa97843b6", Role: types.RoleFeesRouter,
			Replacement: "0x05681dA6f611C2dfFD3E92611F6dE2d247647927"},
		{Address: "0x15405D535ce533BfFb98c83e42f4DD242AA5e079", Role: types.RoleTreasury,
			Replacement: "0xBb82d74E071e03d8E68F1d78Fae9EFe8b6b7A723"},
		{Address: "0x4CdA6e48632d51Ee4Fa735D81BF09F7543f644a1", Role: types.RoleOpticalRegistry,
			Replacement: "0x0FB5E510372Bd3EddFED16A5Dd766d4473CffcF0"},
	}
}

// TestRetiredListRecorded records the ten retired addresses and requires the
// retired-address query and the migration log to equal section 21.
func TestRetiredListRecorded(t *testing.T) {
	e := newRecordEnv(t)
	var layout v6120LayoutFixture
	decodeV6120Fixture(t, "layout.json", &layout)
	section21 := retiredSection21()
	require.Equal(t, layout.Treasury, section21[8].Replacement)
	for _, entry := range section21 {
		require.NoError(t, e.k.RecordRetired(e.ctx, entry))
	}

	want := append([]types.RetiredAddress(nil), section21...)
	sort.Slice(want, func(x, y int) bool {
		return bytes.Compare(common.HexToAddress(want[x].Address).Bytes(), common.HexToAddress(want[y].Address).Bytes()) < 0
	})
	got, err := e.k.RetiredAddresses(e.ctx)
	require.NoError(t, err)
	require.Len(t, got, 10)
	require.Equal(t, want, got)

	entry, found, err := e.k.GetRetired(e.ctx, common.HexToAddress("0xCC7298801112682e10ee14b8a520309caD80336d"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, section21[1], entry)
	_, found, err = e.k.GetRetired(e.ctx, common.HexToAddress("0x78FB06f525cD964388f2305A489035263E31C6e4"))
	require.NoError(t, err)
	require.False(t, found)

	logged := eventsOf(e.ctx, keeper.EventTypeMigrationRetired)
	require.Len(t, logged, 10)
	for n, entry := range section21 {
		require.Equal(t, map[string]string{keeper.AttributeKeyAddress: entry.Address, keeper.AttributeKeyRole: entry.Role,
			keeper.AttributeKeyReplacement: entry.Replacement}, logged[n])
	}

	require.ErrorIs(t, e.k.RecordRetired(e.ctx, section21[0]), keeper.ErrInvalidMigrationRecord)
	require.ErrorIs(t, e.k.RecordRetired(e.ctx, types.RetiredAddress{Address: "0x0000000000000000000000000000000000000001",
		Role: types.RoleRouter, Replacement: section21[1].Replacement}), keeper.ErrInvalidMigrationRecord)
	require.ErrorIs(t, e.k.RecordRetired(e.ctx, types.RetiredAddress{Address: "0x0000000000000000000000000000000000000002",
		Role: types.RolePool, Replacement: section21[1].Replacement}), keeper.ErrInvalidMigrationRecord)
	require.ErrorIs(t, e.k.RecordRetired(e.ctx, types.RetiredAddress{Address: section21[1].Replacement,
		Role: types.RoleRouter, Replacement: section21[1].Replacement}), keeper.ErrInvalidMigrationRecord)
	got, err = e.k.RetiredAddresses(e.ctx)
	require.NoError(t, err)
	require.Len(t, got, 10)
}

// TestFeeRightIDMapRecorded records generation 1 fee right 3 minted again at
// nextTokenId 5 of the generation 2 collection (criterion 2.25.6).
func TestFeeRightIDMapRecorded(t *testing.T) {
	e := newRecordEnv(t)
	require.NoError(t, e.k.RecordFeeRightID(e.ctx, 3, 5))

	id, found, err := e.k.GetFeeRightID(e.ctx, 3)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, uint64(5), id)
	_, found, err = e.k.GetFeeRightID(e.ctx, 5)
	require.NoError(t, err)
	require.False(t, found)
	mappings, err := e.k.FeeRightIDMap(e.ctx)
	require.NoError(t, err)
	require.Equal(t, []types.FeeRightIDMapping{{Generation1ID: 3, Generation2ID: 5}}, mappings)
	require.Equal(t, []map[string]string{{keeper.AttributeKeyGeneration1ID: "3", keeper.AttributeKeyGeneration2ID: "5"}},
		eventsOf(e.ctx, keeper.EventTypeMigrationFeeRightID))

	require.ErrorIs(t, e.k.RecordFeeRightID(e.ctx, 3, 6), keeper.ErrInvalidMigrationRecord)
	require.ErrorIs(t, e.k.RecordFeeRightID(e.ctx, 4, 5), keeper.ErrInvalidMigrationRecord)
	require.ErrorIs(t, e.k.RecordFeeRightID(e.ctx, 0, 6), keeper.ErrInvalidMigrationRecord)
	require.ErrorIs(t, e.k.RecordFeeRightID(e.ctx, 4, 0), keeper.ErrInvalidMigrationRecord)

	require.NoError(t, e.k.RecordFeeRightID(e.ctx, 1, 6))
	mappings, err = e.k.FeeRightIDMap(e.ctx)
	require.NoError(t, err)
	require.Equal(t, []types.FeeRightIDMapping{{Generation1ID: 1, Generation2ID: 6}, {Generation1ID: 3, Generation2ID: 5}},
		mappings)
}
