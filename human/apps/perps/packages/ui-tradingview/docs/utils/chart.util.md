# chart.util

## Overview

Chart theme and symbol helpers: default color config, overrides builder for pane/series/volume, exchange prefix for symbol (paxeer-network), and chart background constant.

## Exports

### defaultColorConfig

`ColorConfigInterface`: upColor, downColor, chartBG, pnlUpColor, pnlDownColor, pnlZeroColor, textColor, qtyTextColor, font, volumeUpColor, volumeDownColor, closeIconColor. The deprecated `pnlZoreColor` and `closeIcon` aliases remain supported for backward compatibility; the correctly spelled/new fields take precedence when both are provided.

### getOveriides(colorConfig, isMobile?)

Returns `{ overrides, studiesOverrides }` for TradingView (pane background, candle colors, grid, font size, volume colors). Uses smaller font and hides series title on mobile when `isMobile` is true.

### EXCHANGE

Constant `"paxeer-network"`.

### withoutExchangePrefix(symbol)

Returns symbol without `"paxeer-network:"` prefix (e.g. `paxeer-network:PERP_BTC_USDC` → `PERP_BTC_USDC`).

### withExchangePrefix(symbol)

Returns symbol with `"paxeer-network:"` prefix if not already present.

### chartBG

Background color constant `"#131519"`.
