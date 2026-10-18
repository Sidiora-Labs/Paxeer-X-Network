# content (desktop)

## Overview

Desktop Share PnL content: single poster (current background), format and optional-info selectors, optional message with checkbox, background image carousel, and bottom Download/Copy buttons. Copy uses poster ref `copy()` and shows toast; Download uses `download("Poster.png")` and then hides.

## Component

### DesktopSharePnLContent

#### Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| entity | ShareEntity | Yes | Position to share. |
| hide | any | No | Callback (e.g. close dialog). |
| baseDp | number | No | Base decimals. |
| quoteDp | number | No | Quote decimals. |
| referral | ReferralType | No | Referral info. |
| shareOptions | SharePnLOptions | Yes | Backgrounds and layout/color options. |

#### UI behavior

- Poster with ref for copy/download; message only included when checkbox is checked.
- Display format and optional info same as mobile (PnlFormatView, ShareOption).
- Message component: checkbox to enable custom message, input (max 25 chars).
- CarouselBackgroundImage for selecting background.
- BottomButtons: Download (poster ref download), Copy (poster ref copy + toast).

## Usage example

```tsx
<DesktopSharePnLContent
  entity={entity}
  hide={onClose}
  baseDp={baseDp}
  quoteDp={quoteDp}
  referral={referral}
  shareOptions={shareOptions}
/>
```
