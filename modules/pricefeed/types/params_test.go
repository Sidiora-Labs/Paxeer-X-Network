package types

import (
	"bytes"
	"testing"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	"github.com/stretchr/testify/require"
)

func TestParamsDefault(t *testing.T) {
	require.Equal(t, authtypes.NewModuleAddress(govtypes.ModuleName).String(), DefaultAuthority())
	params := DefaultParams(DefaultAuthority())
	require.NoError(t, params.Validate())
	require.Equal(t, DefaultAuthority(), params.Authority)
	require.Len(t, params.Markets, 18)
	for i, market := range params.Markets {
		require.Equal(t, uint64(i), market.ID)
		require.Equal(t, Markets[i].Symbol, market.Symbol)
		require.True(t, market.Enabled)
		require.Equal(t, "1", market.MinPrice.String())
		require.Equal(t, "1000000000000000000000000000000", market.MaxPrice.String())
	}
	require.Equal(t, uint64(10000), params.StalenessWindowMs)
	require.Equal(t, uint64(2000), params.MaxClockSkewMs)
	require.Equal(t, uint32(1), params.MinSources)
	require.Equal(t, uint32(50), params.MaxDivergenceBps)
	require.Equal(t, uint32(1000), params.MaxDeviationBps)
	require.Equal(t, uint32(32), params.MaxObservationsPerSubmission)
	require.Equal(t, uint32(8192), params.MaxSubmissionBytes)
	require.Equal(t, uint64(150000), params.MarkEmaTauMs)
	require.Equal(t, uint32(500), params.MaxMarkBasisBps)
	require.Equal(t, uint64(15000), params.MaxPriceAgeMs)

	params.Markets[0].Enabled = false
	require.True(t, Markets[0].Enabled, "DefaultParams must not alias the market table")

	genesis := DefaultGenesis()
	require.Equal(t, DefaultParams(DefaultAuthority()), genesis.Params)
	require.Empty(t, genesis.FeedKeys)
	require.Empty(t, genesis.Observations)
	require.Empty(t, genesis.Aggregates)
}

func TestParamsValidate(t *testing.T) {
	cases := map[string]func(p *Params){
		"empty authority":                  func(p *Params) { p.Authority = "" },
		"malformed authority":              func(p *Params) { p.Authority = "pax1xyz" },
		"zero staleness window":            func(p *Params) { p.StalenessWindowMs = 0; p.MaxClockSkewMs = 0 },
		"clock skew above staleness":       func(p *Params) { p.MaxClockSkewMs = 10001 },
		"zero min sources":                 func(p *Params) { p.MinSources = 0 },
		"zero divergence":                  func(p *Params) { p.MaxDivergenceBps = 0 },
		"divergence above 10000":           func(p *Params) { p.MaxDivergenceBps = 10001 },
		"zero deviation":                   func(p *Params) { p.MaxDeviationBps = 0 },
		"deviation above 10000":            func(p *Params) { p.MaxDeviationBps = 10001 },
		"zero observations per submission": func(p *Params) { p.MaxObservationsPerSubmission = 0 },
		"65 observations per submission":   func(p *Params) { p.MaxObservationsPerSubmission = 65 },
		"submission bytes 511":             func(p *Params) { p.MaxSubmissionBytes = 511 },
		"submission bytes 65537":           func(p *Params) { p.MaxSubmissionBytes = 65537 },
		"zero mark tau":                    func(p *Params) { p.MarkEmaTauMs = 0 },
		"zero mark basis":                  func(p *Params) { p.MaxMarkBasisBps = 0 },
		"mark basis above 10000":           func(p *Params) { p.MaxMarkBasisBps = 10001 },
		"price age 9999 below staleness":   func(p *Params) { p.MaxPriceAgeMs = 9999 },
		"market ids 0..16 and 18":          func(p *Params) { p.Markets[17].ID = 18 },
		"market ids not starting at 0":     func(p *Params) { p.Markets = p.Markets[1:] },
		"two markets named BTC":            func(p *Params) { p.Markets[1].Symbol = "BTC" },
		"empty symbol":                     func(p *Params) { p.Markets[2].Symbol = "" },
		"empty chainflow symbol":           func(p *Params) { p.Markets[2].ChainflowSymbol = "" },
		"min price equals max price":       func(p *Params) { p.Markets[3].MinPrice = p.Markets[3].MaxPrice },
		"min price above max price":        func(p *Params) { p.Markets[3].MinPrice = p.Markets[3].MaxPrice.AddRaw(1) },
		"zero min price":                   func(p *Params) { p.Markets[4].MinPrice = sdk.ZeroInt() },
		"negative min price":               func(p *Params) { p.Markets[4].MinPrice = sdk.NewInt(-1) },
		"nil min price":                    func(p *Params) { p.Markets[5].MinPrice = sdk.Int{} },
		"nil max price":                    func(p *Params) { p.Markets[5].MaxPrice = sdk.Int{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			params := DefaultParams(DefaultAuthority())
			mutate(&params)
			err := params.Validate()
			require.ErrorIs(t, err, ErrInvalidParams)
			requireLXPName(t, err, "LXP_PRICEFEED_INVALID_PARAMS")
		})
	}

	edges := map[string]func(p *Params){
		"clock skew equals staleness": func(p *Params) { p.MaxClockSkewMs = p.StalenessWindowMs },
		"price age equals staleness":  func(p *Params) { p.MaxPriceAgeMs = p.StalenessWindowMs },
		"bps at 10000":                func(p *Params) { p.MaxDivergenceBps, p.MaxDeviationBps, p.MaxMarkBasisBps = 10000, 10000, 10000 },
		"64 observations":             func(p *Params) { p.MaxObservationsPerSubmission = 64 },
		"submission bytes 512":        func(p *Params) { p.MaxSubmissionBytes = 512 },
		"submission bytes 65536":      func(p *Params) { p.MaxSubmissionBytes = 65536 },
		"disabled market":             func(p *Params) { p.Markets[6].Enabled = false },
		"listing the next id":         func(p *Params) { p.Markets = append(p.Markets, newMarket(18, "DOGE")) },
		"min price one below max":     func(p *Params) { p.Markets[7].MinPrice = p.Markets[7].MaxPrice.SubRaw(1) },
		"min sources 100":             func(p *Params) { p.MinSources = 100 },
	}
	for name, mutate := range edges {
		t.Run(name, func(t *testing.T) {
			params := DefaultParams(DefaultAuthority())
			mutate(&params)
			require.NoError(t, params.Validate())
		})
	}
}

func TestParamsStoreKeys(t *testing.T) {
	operator := sdk.ValAddress(bytes.Repeat([]byte{0xc3}, 20))
	account := sdk.AccAddress(bytes.Repeat([]byte{0xa1}, 20))
	marketThree := []byte{0, 0, 0, 0, 0, 0, 0, 3}

	require.Equal(t, "pricefeed", StoreKey)
	require.Equal(t, []byte{0x01}, ParamsKey)
	require.Equal(t, append([]byte{0x02}, operator...), FeedKeyKey(operator))
	require.Equal(t, append([]byte{0x03}, account...), FeedAccountKey(account))
	require.Equal(t, append(append([]byte{0x04}, marketThree...), operator...), ObservationKey(3, operator))
	require.Len(t, ObservationKey(3, operator), 29)
	require.Equal(t, append([]byte{0x05}, marketThree...), AggregateKey(3))
	require.Equal(t, append([]byte{0x06}, operator...), LastSubmitKey(operator))
	require.Equal(t, []byte{0x05, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}, AggregateKey(0x0102030405060708))

	prefixes := [][]byte{ParamsKey, FeedKeyPrefix, FeedAccountPrefix, ObservationPrefix, AggregatePrefix, LastSubmitPrefix}
	for i, prefix := range prefixes {
		require.Equal(t, []byte{byte(i + 1)}, prefix)
	}
}
