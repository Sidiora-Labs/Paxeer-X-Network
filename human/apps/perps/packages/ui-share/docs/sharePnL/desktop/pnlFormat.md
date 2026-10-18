# pnlFormat

## Overview

Radio-style selector for PnL display format: ROI & PnL, ROI only, or PnL only. Uses i18n keys `share.pnl.displayFormat.*`.

## Component

### PnlFormatView

#### Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| type | PnLDisplayFormat | Yes | "roi_pnl" \| "roi" \| "pnl". |
| curType | PnLDisplayFormat | No | Currently selected format. |
| setPnlFormat | (format: PnLDisplayFormat) => void | Yes | Called when user selects. |

Renders a radio icon (selected/unselected) and localized label.

## Usage example

```tsx
<PnlFormatView
  type="roi_pnl"
  curType={pnlFormat}
  setPnlFormat={setPnlFormat}
/>
```
