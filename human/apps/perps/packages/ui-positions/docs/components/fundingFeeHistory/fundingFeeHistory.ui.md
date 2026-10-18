# fundingFeeHistory.ui

## Overview

`FundingFeeHistoryUI` shows symbol, total funding fee (USDC), and an infinite scroll list of funding fee history (time, rate, payment type, amount). Uses desktop DataTable or mobile ListView based on `useScreen()`.

## Props

| Prop | Type | Required | Description |
| ---- | ----- | -------- | ----------- |
| `total` | `number` | Yes | Total funding fee to display. |
| `symbol` | `string` | Yes | Symbol. |
| `start_t` | `string` | Yes | Start time for API. |
| `end_t` | `string` | Yes | End time for API. |

## Usage example

```tsx
<FundingFeeHistoryUI total={total} symbol={symbol} start_t={start_t} end_t={end_t} />
```
