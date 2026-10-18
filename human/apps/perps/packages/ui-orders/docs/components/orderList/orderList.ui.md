# orderList.ui

## Overview

Presentational desktop and mobile order list components. Desktop uses `useOrderColumn`, `OrderListProvider`, `SymbolProvider` (per row), `AuthGuardDataTable`, and optional `DataFilter` + Cancel All. Mobile uses `ListView`, `OrderCellWidget`, filters, and load-more.

## Exports

### `DesktopOrderList`

`FC<OrdersBuilderState & { testIds?: { tableBody?: string } }>`. Renders filter bar (when `filterItems.length > 0`), Cancel All for pending/tp_sl, and a table with columns from `useOrderColumn`. Rows are wrapped with `SymbolProvider` and optionally `TPSLOrderRowProvider`.

### `MobileOrderList`

Receives script state plus optional `classNames`, `showFilter`, `filterConfig`. Renders mobile list with order cells and load-more.

## Usage example

Used internally by `DesktopOrderListWidget` and `MobileOrderListWidget`; typically not used directly.
