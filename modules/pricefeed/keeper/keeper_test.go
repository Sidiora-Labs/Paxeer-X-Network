package keeper_test

import (
	"testing"

	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/stretchr/testify/require"
)

func TestKeeper(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(8)
	k, ctx := pricefeedtestutil.NewKeeper(testApp, ctx)

	operator := sdk.ValAddress([]byte("pricefeed-operator--"))
	key := types.ObservationKey(3, operator)

	var missing types.ObservationRecord
	require.False(t, k.Get(ctx, key, &missing))
	require.Equal(t, types.ObservationRecord{}, missing)

	price, ok := sdk.NewIntFromString("64250120000000000000000")
	require.True(t, ok)
	record := types.ObservationRecord{
		Sequence:          41,
		IndexPrice:        price,
		MarkPrice:         price.AddRaw(7),
		SourceTimestampMs: 1_800_000_000_123,
		Height:            8,
	}
	k.Set(ctx, key, record)

	var got types.ObservationRecord
	require.True(t, k.Get(ctx, key, &got))
	require.Equal(t, uint64(41), got.Sequence)
	require.Equal(t, "64250120000000000000000", got.IndexPrice.String())
	require.Equal(t, "64250120000000000000007", got.MarkPrice.String())
	require.Equal(t, uint64(1_800_000_000_123), got.SourceTimestampMs)
	require.Equal(t, int64(8), got.Height)
	require.Equal(t,
		`{"sequence":41,"index_price":"64250120000000000000000","mark_price":"64250120000000000000007","source_timestamp_ms":1800000000123,"height":8}`,
		string(ctx.KVStore(k.StoreKey()).Get(key)))
}
