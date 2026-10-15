package keeper_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// generation2Market is a migrated generation 2 market whose fee right is id
// of collection; its legacy holder field names legacyHolder.
func generation2Market(collection common.Address, id uint64, legacyHolder sdk.AccAddress) types.Market {
	snapshots := make([]sdk.Int, types.SnapshotSlots)
	for n := range snapshots {
		snapshots[n] = sdk.ZeroInt()
	}
	return types.Market{
		Denom: "factory/launchpad/feeright", Index: 1, Name: "Fee Right", Symbol: "FRT",
		Pointer: "0x0000000000000000000000000000000000000AbC", Creator: legacyHolder.String(),
		Guardian: legacyHolder.String(), FeeRightsHolder: legacyHolder.String(), FeeStrategy: types.FeeStrategyBurn,
		TotalSupply: i(1_000), VirtualQuoteReserve: i(10), RealQuoteBalance: sdk.ZeroInt(), TokenReserve: i(1_000),
		CumulativeVolume: sdk.ZeroInt(), PriceSnapshots: snapshots, AccumulatedQuoteFees: sdk.ZeroInt(),
		AccumulatedTokenFees: sdk.ZeroInt(), AccumulatedFees: i(500), AirdropBalance: sdk.ZeroInt(),
		Generation: types.Generation2, PoolAddress: "0xCE20Da50F92D21d8BeE497910c782E9Aa25b6263",
		TokenAddress: common.BytesToAddress([]byte{0x7b, 0xf2}).Hex(), Optical: common.Address{}.Hex(),
		Factory:                   "0x3993D004166738d1069B5C4e0a923f7B9D5D8830",
		FeeAccumulator:            "0x7b14808b72f61664cDb79D25E6d1587655571a66",
		ProtocolConfig:            "0xF3669eDc475Cb6E1F0d7635240ABD8e8FB7E20C6",
		EventEmitter:              "0x622E1bb90E0E2B6E6e5e9e2DbE1555389E8A3EF6",
		CumulativeBuyQuoteVolume:  sdk.ZeroInt(),
		CumulativeSellQuoteVolume: sdk.ZeroInt(),
		FeeRightCollection:        collection.Hex(),
		FeeRightID:                id,
	}
}

func TestFeeRightGate(t *testing.T) {
	e := newEnv(t)
	roles := e.k.Generation2()

	bz, err := os.ReadFile(filepath.Join(v6120FixtureDir, "usdl.json"))
	require.NoError(t, err)
	var usdl v6120USDLFixture
	require.NoError(t, json.Unmarshal(bz, &usdl))
	served := map[string]common.Address{
		"SidioraFactory": roles.SidioraFactory, "Router": roles.Router, "Quoter": roles.Quoter,
		"FeesRouter": roles.FeesRouter, "FeeAccumulator": roles.FeeAccumulator, "PoolRegistry": roles.PoolRegistry,
		"ProtocolConfig": roles.ProtocolConfig, "SidioraNFT": roles.SidioraNFT, "Treasury": roles.Treasury,
		"OpticalRegistry": roles.OpticalRegistry, "EventEmitter": roles.EventEmitter, "PoolBeacon": roles.PoolBeacon,
	}
	matched := 0
	for _, entry := range usdl.Balances {
		if entry.Generation != 2 || entry.Role == "SidioraPool" {
			continue
		}
		require.Contains(t, served, entry.Role)
		require.Equal(t, common.HexToAddress(entry.Address), served[entry.Role], entry.Role)
		matched++
	}
	require.Equal(t, len(served), matched)
	require.Equal(t, common.HexToAddress("0xBb82d74E071e03d8E68F1d78Fae9EFe8b6b7A723"), e.k.TreasuryContract())
	require.Equal(t, common.HexToAddress("0xe8DcC844a8006dbf64f5Bb851bCad90292a6409B"), roles.SidioraNFT)
	require.Equal(t, "0x9f2df0fed2c77648de5860a4cc508cd0818c85b8b8a1ab4ceeef8d981c8956a6", keeper.FeeRightMinterRole.Hex())
	require.Equal(t, "0x55ddad7e51dfd5c3f94e14941f1a52a4cdd1fb397b4d6787d4f6def3552ddb82",
		keeper.FeeRightStrategySetterRole.Hex())

	collection := roles.SidioraNFT
	generation1Collection := common.HexToAddress("0xDF73b354ed9dcB473cc9D01541c46f507591e190")
	x := common.BytesToAddress([]byte{0xA1})
	y := common.BytesToAddress([]byte{0xB2})
	spender := common.BytesToAddress([]byte{0xC3})
	operator := common.BytesToAddress([]byte{0xD4})
	minted := common.BytesToAddress([]byte{0xE5})
	pool := common.HexToAddress("0xCE20Da50F92D21d8BeE497910c782E9Aa25b6263")

	gs := types.DefaultGenesis()
	gs.Markets = []types.Market{generation2Market(collection, 9, sdk.AccAddress(y.Bytes()))}
	e.k.InitGenesis(e.ctx, *gs)
	denom := gs.Markets[0].Denom

	state, err := e.k.GetFeeRightCollection(e.ctx, collection)
	require.NoError(t, err)
	require.Equal(t, keeper.FeeRightCollectionState{NextTokenID: 1, PoolIndexReady: true}, state)

	// Ids 1 to 8 go to other pools, so the market's right is id 9 as a
	// generation 1 right imported at nextTokenId would be.
	for n := uint64(1); n <= 8; n++ {
		id, err := e.k.MintFeeRight(e.ctx, collection, minted, common.BytesToAddress([]byte{0xF0, byte(n)}),
			types.FeeStrategyClaim)
		require.NoError(t, err)
		require.Equal(t, n, id)
	}
	id, err := e.k.MintFeeRight(e.ctx, collection, x, pool, types.FeeStrategyClaim)
	require.NoError(t, err)
	require.Equal(t, uint64(9), id)

	_, err = e.k.MintFeeRight(e.ctx, collection, y, pool, types.FeeStrategyClaim)
	require.ErrorIs(t, err, types.ErrDuplicatePool)
	_, err = e.k.MintFeeRight(e.ctx, collection, common.Address{}, operator, types.FeeStrategyClaim)
	require.ErrorIs(t, err, types.ErrZeroAddress)
	_, err = e.k.MintFeeRight(e.ctx, collection, y, common.Address{}, types.FeeStrategyClaim)
	require.ErrorIs(t, err, types.ErrZeroAddress)
	_, err = e.k.MintFeeRight(e.ctx, collection, y, operator, types.FeeStrategy(4))
	require.ErrorIs(t, err, types.ErrInvalidStrategy)

	state, err = e.k.GetFeeRightCollection(e.ctx, collection)
	require.NoError(t, err)
	require.Equal(t, keeper.FeeRightCollectionState{NextTokenID: 10, PoolIndexReady: true, TotalSupply: 9}, state)

	// The gate reads the store keyed by collection and id, never the legacy
	// holder field, which names y.
	_, _, err = e.k.RequireMarketFeeRight(e.ctx, y, denom, types.FeeStrategyClaim)
	require.ErrorIs(t, err, types.ErrNotFeeRightsHolder)
	market, right, err := e.k.RequireMarketFeeRight(e.ctx, x, denom, types.FeeStrategyClaim)
	require.NoError(t, err)
	require.Equal(t, denom, market.Denom)
	require.Equal(t, types.FeeRight{ID: 9, Owner: x.Hex(), Strategy: types.FeeStrategyClaim, Pool: pool.Hex()}, right)
	_, _, err = e.k.RequireMarketFeeRight(e.ctx, x, denom, types.FeeStrategyBurn)
	require.ErrorIs(t, err, types.ErrWrongStrategy)
	_, _, err = e.k.RequireMarketFeeRight(e.ctx, x, "factory/launchpad/missing", types.FeeStrategyClaim)
	require.ErrorIs(t, err, types.ErrUnknownMarket)
	_, err = e.k.RequireFeeRightHolder(e.ctx, generation1Collection, x, 9)
	require.ErrorIs(t, err, types.ErrNotFeeRightsHolder)
	_, err = e.k.RequireFeeRightHolder(e.ctx, collection, x, 99)
	require.ErrorIs(t, err, types.ErrNotFeeRightsHolder)

	owner, err := e.k.FeeRightOwnerOf(e.ctx, collection, 9)
	require.NoError(t, err)
	require.Equal(t, x, owner)
	_, err = e.k.FeeRightOwnerOf(e.ctx, collection, 99)
	require.ErrorIs(t, err, types.ErrTokenNotFound)
	_, err = e.k.FeeRightOwnerOf(e.ctx, generation1Collection, 9)
	require.ErrorIs(t, err, types.ErrTokenNotFound)
	requireBalance := func(holder common.Address, expected uint64) {
		t.Helper()
		balance, err := e.k.FeeRightBalanceOf(e.ctx, collection, holder)
		require.NoError(t, err)
		require.Equal(t, expected, balance)
	}
	requireBalance(x, 1)
	requireBalance(minted, 8)
	requireBalance(y, 0)
	_, err = e.k.FeeRightBalanceOf(e.ctx, collection, common.Address{})
	require.ErrorIs(t, err, types.ErrZeroAddress)
	require.Equal(t, uint64(9), e.k.FeeRightPoolTokenID(e.ctx, collection, pool))
	require.Equal(t, uint64(0), e.k.FeeRightPoolTokenID(e.ctx, collection, operator))
	byIndex, err := e.k.FeeRightByIndex(e.ctx, collection, 8)
	require.NoError(t, err)
	require.Equal(t, uint64(9), byIndex)
	_, err = e.k.FeeRightByIndex(e.ctx, collection, 9)
	require.ErrorIs(t, err, types.ErrIndexOutOfBounds)
	ofOwner, err := e.k.FeeRightOfOwnerByIndex(e.ctx, collection, x, 0)
	require.NoError(t, err)
	require.Equal(t, uint64(9), ofOwner)
	_, err = e.k.FeeRightOfOwnerByIndex(e.ctx, collection, x, 1)
	require.ErrorIs(t, err, types.ErrIndexOutOfBounds)

	// approve: only the owner or an operator; a transfer clears it.
	require.ErrorIs(t, e.k.ApproveFeeRight(e.ctx, collection, y, y, 9), types.ErrNotApproved)
	require.ErrorIs(t, e.k.ApproveFeeRight(e.ctx, collection, x, spender, 99), types.ErrTokenNotFound)
	approved, err := e.k.FeeRightGetApproved(e.ctx, collection, 9)
	require.NoError(t, err)
	require.Equal(t, common.Address{}, approved)
	require.NoError(t, e.k.ApproveFeeRight(e.ctx, collection, x, spender, 9))
	approved, err = e.k.FeeRightGetApproved(e.ctx, collection, 9)
	require.NoError(t, err)
	require.Equal(t, spender, approved)
	_, err = e.k.FeeRightGetApproved(e.ctx, collection, 99)
	require.ErrorIs(t, err, types.ErrTokenNotFound)

	require.ErrorIs(t, e.k.TransferFeeRight(e.ctx, collection, y, x, y, 9), types.ErrNotApproved)
	require.ErrorIs(t, e.k.TransferFeeRight(e.ctx, collection, spender, y, spender, 9), types.ErrNotTokenOwner)
	require.ErrorIs(t, e.k.TransferFeeRight(e.ctx, collection, spender, x, common.Address{}, 9), types.ErrZeroAddress)
	require.ErrorIs(t, e.k.TransferFeeRight(e.ctx, collection, spender, x, y, 99), types.ErrTokenNotFound)
	require.NoError(t, e.k.TransferFeeRight(e.ctx, collection, spender, x, y, 9))
	owner, err = e.k.FeeRightOwnerOf(e.ctx, collection, 9)
	require.NoError(t, err)
	require.Equal(t, y, owner)
	approved, err = e.k.FeeRightGetApproved(e.ctx, collection, 9)
	require.NoError(t, err)
	require.Equal(t, common.Address{}, approved)
	requireBalance(x, 0)
	requireBalance(y, 1)
	_, err = e.k.FeeRightOfOwnerByIndex(e.ctx, collection, x, 0)
	require.ErrorIs(t, err, types.ErrIndexOutOfBounds)
	ofOwner, err = e.k.FeeRightOfOwnerByIndex(e.ctx, collection, y, 0)
	require.NoError(t, err)
	require.Equal(t, uint64(9), ofOwner)
	require.ErrorIs(t, e.k.TransferFeeRight(e.ctx, collection, spender, y, x, 9), types.ErrNotApproved)

	// The new holder passes the gate and the old one is refused.
	_, _, err = e.k.RequireMarketFeeRight(e.ctx, x, denom, types.FeeStrategyClaim)
	require.ErrorIs(t, err, types.ErrNotFeeRightsHolder)
	_, right, err = e.k.RequireMarketFeeRight(e.ctx, y, denom, types.FeeStrategyClaim)
	require.NoError(t, err)
	require.Equal(t, types.FeeRight{ID: 9, Owner: y.Hex(), Strategy: types.FeeStrategyClaim, Pool: pool.Hex()}, right)

	// Operators approve, transfer and set the strategy; strangers do not.
	require.False(t, e.k.FeeRightIsApprovedForAll(e.ctx, collection, y, operator))
	e.k.SetFeeRightApprovalForAll(e.ctx, collection, y, operator, true)
	require.True(t, e.k.FeeRightIsApprovedForAll(e.ctx, collection, y, operator))
	require.False(t, e.k.FeeRightIsApprovedForAll(e.ctx, collection, operator, y))
	require.NoError(t, e.k.ApproveFeeRight(e.ctx, collection, operator, spender, 9))
	approved, err = e.k.FeeRightGetApproved(e.ctx, collection, 9)
	require.NoError(t, err)
	require.Equal(t, spender, approved)
	_, err = e.k.SetFeeRightStrategy(e.ctx, collection, x, 9, types.FeeStrategyBurn)
	require.ErrorIs(t, err, types.ErrNotApproved)
	_, err = e.k.SetFeeRightStrategy(e.ctx, collection, operator, 9, types.FeeStrategy(4))
	require.ErrorIs(t, err, types.ErrInvalidStrategy)
	_, err = e.k.SetFeeRightStrategy(e.ctx, collection, operator, 99, types.FeeStrategyBurn)
	require.ErrorIs(t, err, types.ErrTokenNotFound)
	old, err := e.k.SetFeeRightStrategy(e.ctx, collection, operator, 9, types.FeeStrategyBurn)
	require.NoError(t, err)
	require.Equal(t, types.FeeStrategyClaim, old)
	_, _, err = e.k.RequireMarketFeeRight(e.ctx, y, denom, types.FeeStrategyClaim)
	require.ErrorIs(t, err, types.ErrWrongStrategy)
	_, right, err = e.k.RequireMarketFeeRight(e.ctx, y, denom, types.FeeStrategyBurn)
	require.NoError(t, err)
	require.Equal(t, types.FeeStrategyBurn, right.Strategy)
	e.k.SetFeeRightApprovalForAll(e.ctx, collection, y, operator, false)
	require.False(t, e.k.FeeRightIsApprovedForAll(e.ctx, collection, y, operator))
	require.ErrorIs(t, e.k.ApproveFeeRight(e.ctx, collection, operator, spender, 9), types.ErrNotApproved)

	// Owner enumeration moves the last owned id into a removed slot.
	require.NoError(t, e.k.TransferFeeRight(e.ctx, collection, minted, minted, y, 3))
	requireBalance(minted, 7)
	requireBalance(y, 2)
	for index, expected := range []uint64{1, 2, 8, 4, 5, 6, 7} {
		got, err := e.k.FeeRightOfOwnerByIndex(e.ctx, collection, minted, uint64(index))
		require.NoError(t, err)
		require.Equal(t, expected, got)
	}
	for index, expected := range []uint64{9, 3} {
		got, err := e.k.FeeRightOfOwnerByIndex(e.ctx, collection, y, uint64(index))
		require.NoError(t, err)
		require.Equal(t, expected, got)
	}
	for index := uint64(0); index < 9; index++ {
		got, err := e.k.FeeRightByIndex(e.ctx, collection, index)
		require.NoError(t, err)
		require.Equal(t, index+1, got)
	}

	// A self-transfer keeps the enumeration and clears the approval.
	require.NoError(t, e.k.ApproveFeeRight(e.ctx, collection, y, spender, 9))
	require.NoError(t, e.k.TransferFeeRight(e.ctx, collection, y, y, y, 9))
	requireBalance(y, 2)
	approved, err = e.k.FeeRightGetApproved(e.ctx, collection, 9)
	require.NoError(t, err)
	require.Equal(t, common.Address{}, approved)
	ofOwner, err = e.k.FeeRightOfOwnerByIndex(e.ctx, collection, y, 0)
	require.NoError(t, err)
	require.Equal(t, uint64(9), ofOwner)
}
