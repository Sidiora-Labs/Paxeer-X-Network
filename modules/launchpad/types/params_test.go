package types_test

import (
	"encoding/json"
	"math/big"
	"reflect"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/stretchr/testify/require"
)

func TestDefaultParamsMatchProtocolConfig(t *testing.T) {
	params := types.DefaultParams()

	require.Equal(t, "0x85FcD13735F4309833A503EE804ea32395851479", params.QuoteToken)
	require.Equal(t, "10000000000", params.VirtualQuoteDefault.String())
	require.Equal(t, "1000000000000000", params.VirtualTokenDefault.String())
	require.Equal(t, uint64(10), params.MinFeeBps)
	require.Equal(t, uint64(300), params.MaxFeeBps)
	require.Equal(t, uint64(30), params.BaseFeeBps)
	require.Equal(t, uint64(1_000), params.ProtocolFeeBps)
	require.Equal(t, uint64(500), params.FeeDecayRate)
	require.Equal(t, uint64(100), params.VolatilityWeight)
	require.Equal(t, uint64(0), params.ConcentrationWeight)
	require.Equal(t, "1000000", params.CreationFee.String())
	require.NoError(t, params.Validate())

	encoded, err := params.Marshal()
	require.NoError(t, err)
	var decoded types.Params
	require.NoError(t, decoded.Unmarshal(encoded))
	require.Equal(t, params.QuoteToken, decoded.QuoteToken)
	require.NoError(t, decoded.Validate())
}

func TestParamsHaveNoQuoteDenom(t *testing.T) {
	_, found := reflect.TypeOf(types.Params{}).FieldByName("QuoteDenom")
	require.False(t, found)

	params := types.DefaultParams()
	encodedJSON, err := json.Marshal(params)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encodedJSON, &fields))
	require.NotContains(t, fields, "quote_denom")
	require.Equal(t, `"0x85FcD13735F4309833A503EE804ea32395851479"`, string(fields["quote_token"]))

	encoded, err := params.Marshal()
	require.NoError(t, err)
	// Field 1 is reserved: a record that still carries the retired
	// quote_denom string "uusdl" decodes to the same Params and encodes back
	// without it.
	retired := append([]byte{0x0a, 0x05, 'u', 'u', 's', 'd', 'l'}, encoded...)
	var decoded types.Params
	require.NoError(t, decoded.Unmarshal(retired))
	require.Equal(t, params, decoded)
	reencoded, err := decoded.Marshal()
	require.NoError(t, err)
	require.Equal(t, encoded, reencoded)
	require.NotEqual(t, byte(0x0a), reencoded[0])
	require.NoError(t, decoded.Validate())
}

func TestValidateMatchesProtocolConfigBounds(t *testing.T) {
	maxUint112 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 112), big.NewInt(1))
	aboveUint112 := new(big.Int).Lsh(big.NewInt(1), 112)

	refused := []struct {
		name   string
		mutate func(*types.Params)
	}{
		{"concentration_weight_one", func(p *types.Params) { p.ConcentrationWeight = 1 }},
		{"fee_decay_rate_above_10000", func(p *types.Params) { p.FeeDecayRate = 10_001 }},
		{"volatility_weight_above_10000", func(p *types.Params) { p.VolatilityWeight = 10_001 }},
		{"virtual_quote_above_uint112", func(p *types.Params) { p.VirtualQuoteDefault = sdk.NewIntFromBigInt(aboveUint112) }},
		{"virtual_token_above_uint112", func(p *types.Params) { p.VirtualTokenDefault = sdk.NewIntFromBigInt(aboveUint112) }},
		{"virtual_quote_zero", func(p *types.Params) { p.VirtualQuoteDefault = sdk.ZeroInt() }},
		{"virtual_token_zero", func(p *types.Params) { p.VirtualTokenDefault = sdk.ZeroInt() }},
		{"base_fee_below_min", func(p *types.Params) { p.BaseFeeBps = p.MinFeeBps - 1 }},
		{"base_fee_above_max", func(p *types.Params) { p.BaseFeeBps = p.MaxFeeBps + 1 }},
		{"protocol_fee_above_5000", func(p *types.Params) { p.ProtocolFeeBps = types.MaxProtocolFeeBps + 1 }},
		{"min_fee_above_max_fee", func(p *types.Params) { p.MinFeeBps = p.MaxFeeBps + 1 }},
		{"max_fee_at_denominator", func(p *types.Params) { p.MaxFeeBps = types.BpsDenominator }},
		{"creation_fee_negative", func(p *types.Params) { p.CreationFee = sdk.NewInt(-1) }},
		{"quote_token_empty", func(p *types.Params) { p.QuoteToken = "" }},
		{"quote_token_unprefixed", func(p *types.Params) { p.QuoteToken = "85FcD13735F4309833A503EE804ea32395851479" }},
		{"quote_token_short", func(p *types.Params) { p.QuoteToken = "0x85FcD13735F4309833A503EE804ea323958514" }},
		{"quote_token_long", func(p *types.Params) { p.QuoteToken = "0x85FcD13735F4309833A503EE804ea3239585147900" }},
		{"quote_token_not_hex", func(p *types.Params) { p.QuoteToken = "0x85FcD13735F4309833A503EE804ea3239585147g" }},
		{"quote_token_zero", func(p *types.Params) { p.QuoteToken = "0x0000000000000000000000000000000000000000" }},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			params := types.DefaultParams()
			tc.mutate(&params)
			err := params.Validate()
			require.ErrorIs(t, err, types.ErrInvalidParams)
			require.Contains(t, err.Error(), "LXP_LAUNCHPAD_INVALID_PARAMS")
		})
	}

	accepted := []struct {
		name   string
		mutate func(*types.Params)
	}{
		{"fee_decay_rate_10000", func(p *types.Params) { p.FeeDecayRate = 10_000 }},
		{"volatility_weight_10000", func(p *types.Params) { p.VolatilityWeight = 10_000 }},
		{"virtual_defaults_uint112_max", func(p *types.Params) {
			p.VirtualQuoteDefault = sdk.NewIntFromBigInt(maxUint112)
			p.VirtualTokenDefault = sdk.NewIntFromBigInt(maxUint112)
		}},
		{"base_fee_at_min", func(p *types.Params) { p.BaseFeeBps = p.MinFeeBps }},
		{"base_fee_at_max", func(p *types.Params) { p.BaseFeeBps = p.MaxFeeBps }},
		{"protocol_fee_5000", func(p *types.Params) { p.ProtocolFeeBps = types.MaxProtocolFeeBps }},
		{"creation_fee_zero", func(p *types.Params) { p.CreationFee = sdk.ZeroInt() }},
		{"quote_token_lowercase", func(p *types.Params) { p.QuoteToken = "0x85fcd13735f4309833a503ee804ea32395851479" }},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			params := types.DefaultParams()
			tc.mutate(&params)
			require.NoError(t, params.Validate())
		})
	}
}
