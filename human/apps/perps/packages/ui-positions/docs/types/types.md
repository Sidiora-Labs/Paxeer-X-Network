# types

## Overview

Defines `SortType` for table sorting and `PositionsProps` for the positions widgets (desktop, mobile, combine).

## Exports

### SortType

| Property | Type | Description |
| -------- | ----- | ----------- |
| `sortKey` | `string` | Column/key to sort by. |
| `sortOrder` | `SortOrder` | `"asc"` or `"desc"`. |

### PositionsProps

| Property | Type | Required | Description |
| -------- | ----- | -------- | ----------- |
| `pnlNotionalDecimalPrecision` | `number` | No | Decimal precision for notional/PnL display. |
| `sharePnLConfig` | `SharePnLConfig` | No | Config for share PnL (from `@paxeer-network/ui-share`). |
| `symbol` | `string` | No | Filter positions by symbol. |
| `calcMode` | `PriceMode` | No | Price mode for calculations. |
| `includedPendingOrder` | `boolean` | No | Include pending orders in position calc. |
| `selectedAccount` | `string` | No | For combine view: selected account filter. |
| `onSymbolChange` | `(symbol: API.Symbol) => void` | No | Callback when user changes symbol. |
| `enableSortingStorage` | `boolean` | No | Whether to persist sort in session storage (default true). |

## Usage example

```typescript
import type { PositionsProps, SortType } from "./types";
```
