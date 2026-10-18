# content (mobile)

## Overview

Mobile Share PnL content component: carousel of posters (one per background image), format and optional-info toggles, message input (max 25 chars), and a share button that uses `navigator.share` with the poster image or falls back when unsupported. Persists user choices via `savePnlInfo`.

## Component

### MobileSharePnLContent

#### Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| entity | ShareEntity | Yes | Position to share. |
| hide | any | No | Callback (e.g. close sheet). |
| baseDp | number | No | Base decimals. |
| quoteDp | number | No | Quote decimals. |
| referral | ReferralType | No | Referral info. |
| shareOptions | SharePnLOptions | Yes | Backgrounds and layout/color options. |

#### UI behavior

- Carousel of posters with dot indicator; poster scale derived from container width (552×310 aspect).
- Display format: roi_pnl / roi / pnl (based on entity having roi and/or pnl).
- Optional info: checkable list (openPrice, closePrice, openTime, closeTime, markPrice, quantity, leverage) filtered by entity.
- Message input with clear button when focused; toast on max length (25).
- Share button exports current poster as PNG and uses `navigator.share` with the file, then calls `hide`.

## Usage example

```tsx
<MobileSharePnLContent
  entity={entity}
  hide={onClose}
  baseDp={baseDp}
  quoteDp={quoteDp}
  referral={referral}
  shareOptions={shareOptions}
/>
```
