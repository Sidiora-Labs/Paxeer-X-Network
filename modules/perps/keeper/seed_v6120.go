package keeper

import (
	"errors"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

const (
	// seedFundingCapPpmPerHour is the 4 percent per hour funding cap of every
	// market.
	seedFundingCapPpmPerHour = 40_000

	seedImpactMajorUsd = 20_000
	seedImpactOtherUsd = 6_000
)

// seedMarket is one row of contracts/perps-markets.md section 2 in the order
// of the application's market list; oiCapUsd is 0 for class E, whose cap
// follows Params.ClassEOiCaps.
type seedMarket struct {
	symbol     string
	name       string
	szDecimals uint8
	pxDecimals uint8
	class      types.TierClass
	impactUsd  int64
	oiCapUsd   int64
}

var seedV6120Markets = [...]seedMarket{
	{"BTC", "Bitcoin", 5, 1, types.TierClassA, seedImpactMajorUsd, 500_000_000},
	{"ETH", "Ethereum", 4, 2, types.TierClassA, seedImpactMajorUsd, 500_000_000},
	{"SOL", "Solana", 2, 3, types.TierClassB, seedImpactOtherUsd, 50_000_000},
	{"AVAX", "Avalanche", 2, 3, types.TierClassB, seedImpactOtherUsd, 50_000_000},
	{"LINK", "Chainlink", 1, 3, types.TierClassF, seedImpactOtherUsd, 20_000_000},
	{"TSLA", "Tesla", 3, 2, types.TierClassD, seedImpactOtherUsd, 25_000_000},
	{"NVDA", "NVIDIA", 3, 2, types.TierClassD, seedImpactOtherUsd, 25_000_000},
	{"NAS100", "Nasdaq 100", 4, 1, types.TierClassC, seedImpactOtherUsd, 100_000_000},
	{"XAU", "Gold", 4, 1, types.TierClassC, seedImpactOtherUsd, 100_000_000},
	{"SPX500", "S&P 500", 4, 1, types.TierClassC, seedImpactOtherUsd, 100_000_000},
	{"GOOGL", "Alphabet", 3, 2, types.TierClassD, seedImpactOtherUsd, 25_000_000},
	{"PAX", "Paxeer Network", 0, 5, types.TierClassE, seedImpactOtherUsd, 0},
	{"SID", "Sidiora Markets", 0, 5, types.TierClassE, seedImpactOtherUsd, 0},
	{"HYPE", "Hyperliquid", 2, 3, types.TierClassB, seedImpactOtherUsd, 50_000_000},
	{"XRP", "XRP", 0, 4, types.TierClassB, seedImpactOtherUsd, 50_000_000},
	{"ASTER", "Aster", 0, 5, types.TierClassE, seedImpactOtherUsd, 0},
	{"TRUMP", "Trump", 1, 3, types.TierClassE, seedImpactOtherUsd, 0},
	{"BNB", "Binance Smart Chain", 3, 2, types.TierClassB, seedImpactOtherUsd, 50_000_000},
}

// seedV6120MarketList is the eighteen markets of contracts/perps-markets.md,
// ids 0 to 17, each enabled at the venue leverage cap.
func seedV6120MarketList() []types.Market {
	markets := make([]types.Market, len(seedV6120Markets))
	for i, row := range seedV6120Markets {
		markets[i] = types.Market{
			ID:                   uint64(i),
			Symbol:               row.symbol,
			Name:                 row.name,
			OracleSymbol:         row.symbol,
			SzDecimals:           row.szDecimals,
			PxDecimals:           row.pxDecimals,
			MaxLeverage:          types.VenueMaxLeverage,
			TierClass:            row.class,
			ImpactNotionalUsd:    sdk.NewIntWithDecimal(row.impactUsd, types.USDDecimals),
			OiCapUsd:             sdk.NewIntWithDecimal(row.oiCapUsd, types.USDDecimals),
			FundingCapPpmPerHour: seedFundingCapPpmPerHour,
			Enabled:              true,
		}
	}
	return markets
}

// SeedV6120 is the perps hook of the v6.12.0 upgrade handler: it stores
// DefaultParams, the store accounts perps_fees and perps_insurance and the
// eighteen markets listed at the block time. It writes only what is absent, so
// a second run leaves the store unchanged, and it refuses with
// LXP_PERPS_INVALID_PARAMS before any write when a market fails validation.
func SeedV6120(ctx sdk.Context, k Keeper) error {
	return seedV6120(ctx, &k, seedV6120MarketList())
}

func seedV6120(ctx sdk.Context, k *Keeper, markets []types.Market) error {
	for _, market := range markets {
		if err := market.Validate(); err != nil {
			return err
		}
	}
	if !k.store(ctx).Has(types.ParamsKey) {
		if err := k.setParams(ctx, types.DefaultParams()); err != nil {
			return err
		}
	}
	if _, _, err := k.EnsureAccount(ctx, k.FeesAccountAddress()); err != nil {
		return err
	}
	if _, _, err := k.EnsureAccount(ctx, k.InsuranceAccountAddress()); err != nil {
		return err
	}
	for _, market := range markets {
		_, err := k.GetMarket(ctx, market.ID)
		switch {
		case err == nil:
			continue
		case !errors.Is(err, types.ErrUnknownMarket):
			return err
		}
		if err := k.SetMarket(ctx, k.authority, market); err != nil {
			return err
		}
	}
	return nil
}
