# useColumn

## Overview

Hook that returns table column definitions for the desktop order list. Columns vary by `TabType`: instrument (with optional type badges), side, fill/qty, price, trigger price, trailing callback, activated price, actions (cancel/edit/share), PnL, etc. Uses shared cells (PriceCell, QuantityCell, TriggerPriceCell, etc.) and symbol context.

## Exports

### `useOrderColumn(props)`

**Props:** `_type: TabType`, `onSymbolChange?`, `pnlNotionalDecimalPrecision?`, `sharePnLConfig?`, `symbolsInfo?`.

**Returns:** Array of column configs for `AuthGuardDataTable`. Columns include instrument (with `parseBadgesFor`), side, fill/quantity, price, trigger price, trailing callback, actived price, cancel/edit/share actions, avg price, bracket price, PnL, renew. Uses `useSymbolContext` and optional `ShareButtonWidget`.

## Usage example

Used internally by `DesktopOrderList`; not typically used directly.
