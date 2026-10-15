package types

import (
	"strings"
	"testing"

	cdctypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/stretchr/testify/require"
)

func testAccount(t *testing.T, prefix string, first byte) (sdk.AccAddress, string) {
	t.Helper()
	account := sdk.AccAddress(append([]byte{first}, make([]byte, 19)...))
	encoded, err := sdk.Bech32ifyAddressBytes(prefix, account)
	require.NoError(t, err)
	return account, encoded
}

func validObservation(marketID uint64) Observation {
	return Observation{
		MarketId:          marketID,
		Sequence:          7,
		IndexPrice:        "65000500000000000000000",
		MarkPrice:         "65010000000000000000000",
		SourceTimestampMs: 1699999999500,
	}
}

func requireLXPName(t *testing.T, err error, want string) {
	t.Helper()
	require.Error(t, err)
	name, ok := LXPName(err)
	require.True(t, ok, "error %v carries no LXP name", err)
	require.Equal(t, want, name)
}

func TestValidateBasicRefusesInvalid(t *testing.T) {
	feederAccount, feeder := testAccount(t, AccountAddressPrefix, 0xa1)
	valid := MsgSubmitPrices{Feeder: feeder, Observations: []Observation{validObservation(0), validObservation(1)}}
	require.NoError(t, valid.ValidateBasic())
	require.Equal(t, []sdk.AccAddress{feederAccount}, valid.GetSigners())

	_, foreignPrefix := testAccount(t, "cosmos", 0xa1)
	withObservation := func(edit func(*Observation)) MsgSubmitPrices {
		observation := validObservation(0)
		edit(&observation)
		return MsgSubmitPrices{Feeder: feeder, Observations: []Observation{observation}}
	}
	invalid := map[string]MsgSubmitPrices{
		"malformed feeder":   {Feeder: "pax1xyz", Observations: []Observation{validObservation(0)}},
		"empty feeder":       {Feeder: "", Observations: []Observation{validObservation(0)}},
		"foreign prefix":     {Feeder: foreignPrefix, Observations: []Observation{validObservation(0)}},
		"no observations":    {Feeder: feeder},
		"zero sequence":      withObservation(func(o *Observation) { o.Sequence = 0 }),
		"zero timestamp":     withObservation(func(o *Observation) { o.SourceTimestampMs = 0 }),
		"leading zero index": withObservation(func(o *Observation) { o.IndexPrice = "012" }),
		"zero mark":          withObservation(func(o *Observation) { o.MarkPrice = "0" }),
		"empty index":        withObservation(func(o *Observation) { o.IndexPrice = "" }),
		"signed mark":        withObservation(func(o *Observation) { o.MarkPrice = "+1" }),
		"fractional index":   withObservation(func(o *Observation) { o.IndexPrice = "65000.5" }),
		"exponent mark":      withObservation(func(o *Observation) { o.MarkPrice = "1e18" }),
		"spaced index":       withObservation(func(o *Observation) { o.IndexPrice = " 1" }),
		"forty digit mark":   withObservation(func(o *Observation) { o.MarkPrice = "1" + strings.Repeat("0", 39) }),
	}
	for name, msg := range invalid {
		t.Run(name, func(t *testing.T) {
			err := msg.ValidateBasic()
			require.ErrorIs(t, err, ErrInvalidObservation)
			requireLXPName(t, err, "LXP_PRICEFEED_INVALID_OBSERVATION")
		})
	}
	require.Equal(t, []sdk.AccAddress{}, invalid["malformed feeder"].GetSigners())
	require.Equal(t, []sdk.AccAddress{}, invalid["foreign prefix"].GetSigners())

	authorityAccount, authority := testAccount(t, AccountAddressPrefix, 0xb2)
	params := MsgSetParams{Authority: authority, Markets: Markets}
	require.NoError(t, params.ValidateBasic())
	require.Equal(t, []sdk.AccAddress{authorityAccount}, params.GetSigners())
	for _, bad := range []string{"", "pax1xyz", foreignPrefix} {
		err := MsgSetParams{Authority: bad, Markets: Markets}.ValidateBasic()
		require.ErrorIs(t, err, ErrUnauthorized)
		requireLXPName(t, err, "LXP_PRICEFEED_UNAUTHORIZED")
		require.Equal(t, []sdk.AccAddress{}, MsgSetParams{Authority: bad}.GetSigners())
	}
}

func TestValidateBasicBounds(t *testing.T) {
	_, feeder := testAccount(t, AccountAddressPrefix, 0xa1)

	duplicate := MsgSubmitPrices{Feeder: feeder, Observations: []Observation{validObservation(1), validObservation(1)}}
	err := duplicate.ValidateBasic()
	require.ErrorIs(t, err, ErrDuplicateMarket)
	requireLXPName(t, err, "LXP_PRICEFEED_DUPLICATE_MARKET")

	observations := make([]Observation, MaxObservationsPerMessage+1)
	for i := range observations {
		observations[i] = validObservation(uint64(i))
	}
	err = MsgSubmitPrices{Feeder: feeder, Observations: observations}.ValidateBasic()
	require.ErrorIs(t, err, ErrTooManyObservations)
	requireLXPName(t, err, "LXP_PRICEFEED_TOO_MANY_OBSERVATIONS")

	require.NoError(t, MsgSubmitPrices{Feeder: feeder, Observations: observations[:MaxObservationsPerMessage]}.ValidateBasic())
	require.NoError(t, MsgSubmitPrices{Feeder: feeder, Observations: observations[:1]}.ValidateBasic())

	widest := validObservation(0)
	widest.IndexPrice = strings.Repeat("9", MaxPriceDigits)
	widest.MarkPrice = "1"
	require.NoError(t, MsgSubmitPrices{Feeder: feeder, Observations: []Observation{widest}}.ValidateBasic())
}

func TestValidateBasicMessagesRegistered(t *testing.T) {
	registry := cdctypes.NewInterfaceRegistry()
	RegisterInterfaces(registry)
	for typeURL, want := range map[string]sdk.Msg{
		"/paxprotocol.paxchain.pricefeed.MsgSubmitPrices": &MsgSubmitPrices{},
		"/paxprotocol.paxchain.pricefeed.MsgSetParams":    &MsgSetParams{},
	} {
		resolved, err := registry.Resolve(typeURL)
		require.NoError(t, err)
		require.IsType(t, want, resolved)
		require.Equal(t, typeURL, sdk.MsgTypeURL(want))
	}
}
