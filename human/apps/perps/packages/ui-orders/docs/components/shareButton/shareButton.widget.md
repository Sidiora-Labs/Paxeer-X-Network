# shareButton.widget

## Overview

Share PnL button widget: composes `useShareButtonScript` and `ShareButton`. Used in order list rows when share is enabled.

## Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| `order` | `any` | Yes | Order entity (symbol, realized_pnl, side, average_executed_price, updated_time, quantity). |
| `sharePnLConfig` | `SharePnLConfig` | No | Config for share PnL modal. |
| `modalId` | `string` | Yes | Modal ID to open (e.g. SharePnLDialogId). |
| `iconSize` | `number` | No | Icon size. |

## Usage example

```tsx
<ShareButtonWidget order={order} sharePnLConfig={config} modalId={SharePnLDialogId} />
```
