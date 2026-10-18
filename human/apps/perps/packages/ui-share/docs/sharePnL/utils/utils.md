# utils

## Overview

Builds the data object used by the poster (position, message, domain, updateTime, referral) and persists/retrieves user choices (PnL format, optional fields, background index, message) in localStorage under key `pnl_config_key`.

## Exports

### getPnLPosterData

Builds the poster data object from entity, message, domain, display format, selected options, decimals, and optional referral.

#### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| position | ShareEntity | Yes | Trading position. |
| message | string | Yes | User message. |
| domain | string | Yes | Domain string. |
| pnlType | PnLDisplayFormat | Yes | "roi_pnl" \| "roi" \| "pnl". |
| options | Set<ShareOptions> | Yes | Which optional fields to include. |
| baseDp | number | No | Base decimal places. |
| quoteDp | number | No | Quote decimal places. |
| referral | ReferralType | No | Referral code/link/slogan. |

#### Returns

Object with `position`, `updateTime`, `domain`, optional `message`, and optional `referral`. Position includes symbol, currency, side, pnl/ROI per format, leverage, and `informations` array (openPrice, closePrice, times, markPrice, quantity) based on `options`.

### savePnlInfo

Saves PnL UI state to localStorage.

| Parameter | Type | Description |
|-----------|------|-------------|
| format | PnLDisplayFormat | Display format. |
| options | Set<ShareOptions> | Selected optional fields. |
| bgIndex | number | Selected background index. |
| message | string | Custom message. |

### getPnlInfo

Reads PnL config from localStorage. Returns defaults if missing or invalid.

#### Returns

| Property | Type | Default |
|----------|------|---------|
| bgIndex | number | 0 |
| pnlFormat | PnLDisplayFormat | "roi_pnl" |
| options | ShareOptions[] | All options |
| message | string | "" |

## Usage example

```typescript
const data = getPnLPosterData(
  entity,
  "I am the paxeer-network KING.",
  "paxeer.network",
  "roi_pnl",
  new Set(["openPrice", "leverage"]),
  baseDp,
  quoteDp,
  referral
);
savePnlInfo("roi_pnl", options, 0, "My message");
const saved = getPnlInfo();
```
