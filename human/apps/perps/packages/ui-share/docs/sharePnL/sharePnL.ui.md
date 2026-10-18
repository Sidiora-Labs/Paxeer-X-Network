# sharePnL.ui

## Overview

Desktop and mobile Share PnL content components. Both consume `SharePnLState` from `useSharePnLScript` and render the corresponding layout (desktop or mobile) with entity, decimals, referral, and share options.

## Components

### DesktopSharePnL

Renders desktop Share PnL content. Returns `null` if `shareOptions` or `entity` is missing.

#### Props (SharePnLState)

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| entity | from state | Yes | Share entity. |
| baseDp | number | No | Base decimal places. |
| quoteDp | number | No | Quote decimal places. |
| referralInfo | ReferralType | No | Referral code/link/slogan. |
| shareOptions | SharePnLOptions | Yes | Poster options (backgrounds, layout, etc.). |
| hide | () => void | No | Callback to close the dialog/sheet. |

### MobileSharePnL

Same props as `DesktopSharePnL`. Renders mobile Share PnL content; returns `null` when `shareOptions` or `entity` is missing.

## Usage example

```tsx
const state = useSharePnLScript({ pnl, hide });
return <DesktopSharePnL {...state} />;
// or
return <MobileSharePnL {...state} />;
```
