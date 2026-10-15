package keeper_test

import (
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdktestutil "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const listingTime = int64(1_800_000_000)

func newKeeper(t *testing.T) (*keeper.Keeper, sdk.Context, sdk.StoreKey) {
	t.Helper()
	key := sdk.NewKVStoreKey(types.StoreKey)
	ctx := sdktestutil.DefaultContext(key, sdk.NewTransientStoreKey("transient_perps")).
		WithBlockTime(time.Unix(listingTime, 0))
	return keeper.NewKeeper(key, nil, nil), ctx, key
}

func govAuthority() string { return authtypes.NewModuleAddress(govtypes.ModuleName).String() }

func classEMarket(id uint64, symbol string) types.Market {
	return types.Market{
		ID:                   id,
		Symbol:               symbol,
		Name:                 symbol,
		OracleSymbol:         symbol,
		SzDecimals:           0,
		PxDecimals:           5,
		MaxLeverage:          1000,
		TierClass:            types.TierClassE,
		ImpactNotionalUsd:    sdk.NewIntWithDecimal(6000, types.USDDecimals),
		FundingCapPpmPerHour: 40_000,
		Enabled:              true,
	}
}

func requireUnauthorized(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, types.ErrUnauthorized)
	require.Contains(t, err.Error(), "LXP_PERPS_UNAUTHORIZED")
}

func TestKeeperUpdateParamsAuthority(t *testing.T) {
	k, ctx, _ := newKeeper(t)
	require.Equal(t, govAuthority(), k.Authority())

	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, types.DefaultParams(), params)

	updated := types.DefaultParams()
	updated.SweepPositionsPerBlock = 256
	updated.Paused = true
	require.NoError(t, k.UpdateParams(ctx, govAuthority(), updated))
	stored, err := k.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(256), stored.SweepPositionsPerBlock)
	require.True(t, stored.Paused)

	other := sdk.AccAddress(common.HexToAddress("0x00000000000000000000000000000000000b0001").Bytes()).String()
	changed := stored
	changed.SweepPositionsPerBlock = 1
	requireUnauthorized(t, k.UpdateParams(ctx, other, changed))
	afterRefusal, err := k.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, stored, afterRefusal)

	invalid := stored
	invalid.CollateralToken = common.Address{}
	err = k.UpdateParams(ctx, govAuthority(), invalid)
	require.ErrorIs(t, err, types.ErrInvalidParams)
	require.Contains(t, err.Error(), "LXP_PERPS_INVALID_PARAMS")
	afterInvalid, err := k.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, stored, afterInvalid)
}

func TestKeeperUpdateParamsRejectsSigner(t *testing.T) {
	k, ctx, key := newKeeper(t)
	signers := []string{
		sdk.AccAddress(common.HexToAddress("0x00000000000000000000000000000000000b0002").Bytes()).String(),
		k.ModuleAddress().String(),
		authtypes.NewModuleAddress(types.FeesAccountName).String(),
		"",
	}
	for _, signer := range signers {
		requireUnauthorized(t, k.UpdateParams(ctx, signer, types.DefaultParams()))
	}
	require.Nil(t, ctx.KVStore(key).Get(types.ParamsKey))
	params, err := k.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, types.DefaultParams(), params)
}

func TestKeeperSetMarketAuthority(t *testing.T) {
	k, ctx, _ := newKeeper(t)

	_, err := k.GetMarket(ctx, 2)
	require.ErrorIs(t, err, types.ErrUnknownMarket)

	market := classEMarket(2, "PAX")
	market.ListedAt = 7
	require.NoError(t, k.SetMarket(ctx, govAuthority(), market))
	stored, err := k.GetMarket(ctx, 2)
	require.NoError(t, err)
	want := classEMarket(2, "PAX")
	want.ListedAt = uint64(listingTime)
	require.True(t, stored.OiCapUsd.IsZero())
	want.OiCapUsd = stored.OiCapUsd
	require.Equal(t, want, stored)

	other := sdk.AccAddress(common.HexToAddress("0x00000000000000000000000000000000000b0003").Bytes()).String()
	changed := stored
	changed.Name = "Changed"
	changed.Enabled = false
	requireUnauthorized(t, k.SetMarket(ctx, other, changed))
	afterRefusal, err := k.GetMarket(ctx, 2)
	require.NoError(t, err)
	require.Equal(t, stored, afterRefusal)

	invalid := stored
	invalid.MaxLeverage = 1001
	err = k.SetMarket(ctx, govAuthority(), invalid)
	require.ErrorIs(t, err, types.ErrInvalidParams)
	require.Contains(t, err.Error(), "LXP_PERPS_INVALID_PARAMS")

	later := ctx.WithBlockTime(time.Unix(listingTime+40*86400, 0))
	renamed := stored
	renamed.Name = "Paxeer"
	renamed.ListedAt = 1
	require.NoError(t, k.SetMarket(later, govAuthority(), renamed))
	kept, err := k.GetMarket(later, 2)
	require.NoError(t, err)
	require.Equal(t, "Paxeer", kept.Name)
	require.Equal(t, uint64(listingTime), kept.ListedAt)

	disabled := kept
	disabled.Enabled = false
	require.NoError(t, k.SetMarket(later, govAuthority(), disabled))
	gotDisabled, err := k.GetMarket(later, 2)
	require.NoError(t, err)
	require.False(t, gotDisabled.Enabled)
	require.Equal(t, uint64(listingTime), gotDisabled.ListedAt)

	relistTime := listingTime + 100*86400
	relisted := gotDisabled
	relisted.Enabled = true
	require.NoError(t, k.SetMarket(ctx.WithBlockTime(time.Unix(relistTime, 0)), govAuthority(), relisted))
	gotRelisted, err := k.GetMarket(ctx, 2)
	require.NoError(t, err)
	require.True(t, gotRelisted.Enabled)
	require.Equal(t, uint64(relistTime), gotRelisted.ListedAt)

	require.NoError(t, k.SetMarket(ctx, govAuthority(), classEMarket(11, "SID")))
	require.NoError(t, k.SetMarket(ctx, govAuthority(), classEMarket(0, "TRUMP")))
	var visited []uint64
	require.NoError(t, k.IterateMarkets(ctx, func(m types.Market) bool {
		visited = append(visited, m.ID)
		return false
	}))
	require.Equal(t, []uint64{0, 2, 11}, visited)
	visited = nil
	require.NoError(t, k.IterateMarkets(ctx, func(m types.Market) bool {
		visited = append(visited, m.ID)
		return m.ID == 2
	}))
	require.Equal(t, []uint64{0, 2}, visited)
}

func TestKeeperIDCounters(t *testing.T) {
	k, ctx, key := newKeeper(t)
	for want := uint64(1); want <= 3; want++ {
		id, err := k.NextOrderID(ctx)
		require.NoError(t, err)
		require.Equal(t, *uint256.NewInt(want), id)
	}
	id, err := k.NextPositionID(ctx)
	require.NoError(t, err)
	require.Equal(t, *uint256.NewInt(1), id)

	var maxID uint256.Int
	maxID.SetAllOne()
	word := maxID.Bytes32()
	ctx.KVStore(key).Set(types.SequenceKey(types.SequencePositionID), word[:])
	_, err = k.NextPositionID(ctx)
	require.ErrorIs(t, err, types.ErrOverflow)
	require.Equal(t, word[:], ctx.KVStore(key).Get(types.SequenceKey(types.SequencePositionID)))

	require.Equal(t, common.BytesToAddress(authtypes.NewModuleAddress(types.FeesAccountName)), k.FeesAccountAddress())
	require.Equal(t, common.BytesToAddress(authtypes.NewModuleAddress(types.InsuranceAccountName)),
		k.InsuranceAccountAddress())
	require.Equal(t, common.BytesToAddress(authtypes.NewModuleAddress(types.ModuleName)), k.ModuleEVMAddress())
}
