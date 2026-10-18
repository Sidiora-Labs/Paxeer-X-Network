# sharePnL.script

## Overview

Hook that prepares Share PnL state: resolves entity, symbol decimals (base_dp, quote_dp), referral info from params or `useReferralInfo`, and passes through share options and hide callback.

## Exports

### useSharePnLScript

Builds state for Share PnL UI from `pnl` (options + params) and optional `hide` callback.

#### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| props.pnl | SharePnLOptions & SharePnLParams | No | Options and entity/ref params. |
| props.hide | () => void | No | Called when share/copy is done or dialog is closed. |

#### Returns (SharePnLState)

| Property | Type | Description |
|----------|------|-------------|
| entity | ShareEntity \| undefined | From `pnl.entity`. |
| baseDp | number \| undefined | Base decimals from `useSymbolsInfo`. |
| quoteDp | number \| undefined | Quote decimals from `useSymbolsInfo`. |
| referralInfo | ReferralType \| undefined | From `pnl.refCode/refSlogan/refLink` or `getFirstRefCode()`. |
| shareOptions | SharePnLOptions | Same as `pnl` (options). |
| hide | () => void \| undefined | Pass-through. |

Logs a console warning when `entity` is null.

### SharePnLState

`ReturnType<typeof useSharePnLScript>`.

## Usage example

```tsx
const state = useSharePnLScript({
  pnl: { entity: myEntity, backgroundImages: [url], refCode: "X" },
  hide: () => close(),
});
<DesktopSharePnL {...state} />;
```
