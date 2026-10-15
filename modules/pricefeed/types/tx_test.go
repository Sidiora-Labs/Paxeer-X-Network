package types

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTxWireRoundTrip(t *testing.T) {
	msg := MsgSubmitPrices{
		Feeder: "pax1qyqszqgpqyqszqgpqyqszqgpqyqszqgpjnp7du",
		Observations: []Observation{
			{MarketId: 0, Sequence: 7, IndexPrice: "65000500000000000000000", MarkPrice: "65001000000000000000000", SourceTimestampMs: 1700000000123},
			{MarketId: 1, Sequence: 9, IndexPrice: "3400250000000000000000", MarkPrice: "3400300000000000000000", SourceTimestampMs: 1700000000456},
		},
	}
	bz, err := msg.Marshal()
	require.NoError(t, err)
	require.Len(t, bz, msg.Size())

	var got MsgSubmitPrices
	require.NoError(t, got.Unmarshal(bz))
	require.Equal(t, msg, got)
	again, err := got.Marshal()
	require.NoError(t, err)
	require.True(t, bytes.Equal(bz, again))

	params := MsgSetParams{
		Authority:                    "pax10d07y265gmmuvt4z0w9aw880jnsr700j5w6v4j",
		Markets:                      Markets,
		StalenessWindowMs:            10000,
		MaxClockSkewMs:               2000,
		MinSources:                   1,
		MaxDivergenceBps:             50,
		MaxDeviationBps:              1000,
		MaxObservationsPerSubmission: 32,
		MaxSubmissionBytes:           8192,
		MarkEmaTauMs:                 150000,
		MaxMarkBasisBps:              500,
		MaxPriceAgeMs:                15000,
	}
	pbz, err := params.Marshal()
	require.NoError(t, err)
	var gotParams MsgSetParams
	require.NoError(t, gotParams.Unmarshal(pbz))
	require.Len(t, gotParams.Markets, 18)
	require.Equal(t, uint64(17), gotParams.Markets[17].ID)
	require.Equal(t, "BNB", gotParams.Markets[17].Symbol)
	require.Equal(t, "1000000000000000000000000000000", gotParams.Markets[17].MaxPrice.String())
	require.Equal(t, uint64(15000), gotParams.MaxPriceAgeMs)
	pagain, err := gotParams.Marshal()
	require.NoError(t, err)
	require.True(t, bytes.Equal(pbz, pagain))
}
