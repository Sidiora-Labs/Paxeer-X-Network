# types

## Overview

Central type definitions for Share PnL: canvas layout info, poster layout config, share config/params/options, referral, PnL display format, share options, and share entity.

## Exports

### layoutInfo

Layout and style for a single element on the poster canvas.

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| width | number | No | Element width. |
| height | number | No | Element height. |
| fontSize | number | No | Font size. |
| color | string | No | Text color. |
| textAlign | CanvasTextAlign | No | Text alignment. |
| textBaseline | CanvasTextBaseline | No | Text baseline. |
| position | Partial<{ left, right, top, bottom }> | Yes | Position (number values). |

### PosterLayoutConfig

Layout configuration for poster sections (message, domain, position, unrealizedPnl, informations, updateTime). Each section can use `layoutInfo`; some extend it (e.g. `secondaryColor`, `labelColor`).

| Property | Type | Description |
|----------|------|-------------|
| message | layoutInfo | Optional. |
| domain | layoutInfo | Optional. |
| position | layoutInfo | Optional. |
| unrealizedPnl | layoutInfo & { secondaryColor, secondaryFontSize } | Optional. |
| informations | layoutInfo & { labelColor? } | Optional. |
| updateTime | layoutInfo | Optional. |

### SharePnLParams

Parameters for a share session (entity and referral).

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| entity | ShareEntity | Yes | Trading position to share. |
| refCode | string | No | Referral code. |
| refSlogan | string | No | Referral slogan. |
| refLink | string | No | Referral link. |

### SharePnLOptions

Options for poster appearance and data (fonts, backgrounds, layout, colors).

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| fontFamily | string | No | Default "Manrope". |
| backgroundImages | string[] | Yes | Background image URLs (cannot be empty). |
| layout | PosterLayoutConfig | No | Poster layout config. |
| color | string | No | Normal text color; default `"rgba(255, 255, 255, 0.98)"`. |
| profitColor | string | No | Profit text color; default `"rgb(0,181,159)"`. |
| lossColor | string | No | Loss text color; default `"rgb(255,103,194)"`. |
| brandColor | string | No | Brand color; default `"rgb(0,181,159)"`. |

### SharePnLConfig

Full config: `SharePnLOptions` and partial `SharePnLParams` (typically omitting `refCode` from params).

### ReferralType

| Property | Type | Description |
|----------|------|-------------|
| code | string | Optional. |
| link | string | Optional. |
| slogan | string | Optional. |

### PnLDisplayFormat

Display format for PnL on the poster: `"roi_pnl"` | `"roi"` | `"pnl"`.

### ShareOptions

Which optional fields to show: `"openPrice"` | `"closePrice"` | `"openTime"` | `"closeTime"` | `"markPrice"` | `"quantity"` | `"leverage"`.

### ShareEntity

Trading position data to share.

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| symbol | string | Yes | Symbol. |
| side | "LONG" \| "SHORT" | Yes | Side. |
| pnl | number | No | PnL value. |
| roi | number | No | ROI value. |
| openPrice | number | No | Open price. |
| closePrice | number | No | Close price. |
| openTime | number | No | Open time (timestamp). |
| closeTime | number | No | Close time (timestamp). |
| markPrice | number | No | Mark price. |
| quantity | number | No | Quantity. |
| leverage | number | No | Leverage. |

## Usage example

```typescript
import type {
  SharePnLConfig,
  SharePnLOptions,
  SharePnLParams,
  ShareEntity,
  PnLDisplayFormat,
  ShareOptions,
} from "@paxeer-network/ui-share";

const params: SharePnLParams = {
  entity: {
    symbol: "BTC-PERP",
    side: "LONG",
    pnl: 100,
    roi: 5.2,
    openPrice: 50000,
    quantity: 0.1,
    leverage: 10,
  },
  refCode: "MYCODE",
};

const options: SharePnLOptions = {
  backgroundImages: ["https://example.com/bg.png"],
  fontFamily: "Manrope",
};
```
