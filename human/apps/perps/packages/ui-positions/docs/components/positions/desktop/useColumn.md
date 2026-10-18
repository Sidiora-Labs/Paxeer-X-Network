# useColumn

## Overview

Returns desktop table column config for positions: symbol, quantity, avg open, mark price, liq price, unrealized PnL (with share button), full/partial TP/SL, notional, margin, and close + optional reverse button.

## Parameters (ColumnConfig)

| Property | Type | Description |
| -------- | ----- | ----------- |
| `pnlNotionalDecimalPrecision` | `number?` | Notional/PnL precision. |
| `sharePnLConfig` | `SharePnLOptions?` | Share PnL dialog config. |
| `onSymbolChange` | `(symbol: API.Symbol) => void?` | Symbol click handler. |
| `positionReverse` | `boolean?` | Whether to show reverse position button. |

## Returns

- `Column<API.PositionTPSLExt>[]` for use with `AuthGuardDataTable` / `DataTable`.

## Usage example

```tsx
const columns = useColumn({ pnlNotionalDecimalPrecision, sharePnLConfig, onSymbolChange, positionReverse });
<DataTable columns={columns} dataSource={dataSource} ... />;
```
