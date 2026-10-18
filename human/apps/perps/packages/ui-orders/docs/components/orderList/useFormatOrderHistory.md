# useFormatOrderHistory

## Overview

Hook that normalizes order history for display: expands POSITIONAL_TP_SL / TP_SL parent orders into a flat list of child orders (activated with trigger_price, or filled/partial-filled), and passes through other orders as-is.

## Exports

### `useFormatOrderHistory(data: API.AlgoOrderExt[])`

Returns a memoized array of `API.AlgoOrder[]` suitable for table/list display. For TP_SL parent orders, pushes child orders (with `parent_algo_type` set) instead of the parent, depending on algo_status and trigger/execution state.

## Usage example

```typescript
const formattedData = useFormatOrderHistory(data ?? []);
// use formattedData as dataSource for table
```
