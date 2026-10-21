package l16

import (
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l17"
)

func ProjectPosition(wallet, token string, state Inventory, watermark l17.Watermark, mark *l17.Price, quality string) (l17.Position, error) {
	values, err := state.values()
	if err != nil {
		return l17.Position{}, err
	}
	realizedQuality := "complete"
	if state.IncompleteRealized {
		realizedQuality = "partial"
	}
	p := l17.Position{PositionID: "125:" + wallet + ":" + token, ChainID: "125", Wallet: wallet, TokenAddress: token, BalanceRaw: add(values[0], values[1]).String(), KnownQuantityRaw: state.Known, UnknownQuantityRaw: state.Unknown, KnownRemainingCostUsdlRaw: state.Cost, KnownRealizedSubtotalUsdlRaw: state.Realized, RealizedBasisQuality: realizedQuality, PricingQuality: quality, MarkPrice: mark, SourceWatermark: watermark}
	derived, _, err := l17.Derive(p)
	return derived, err
}
