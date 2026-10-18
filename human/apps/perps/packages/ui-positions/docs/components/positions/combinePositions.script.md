# combinePositions.script

## Overview

Hook that aggregates main + sub-account positions, groups by account for the combine view, and returns table data, loading state, and mutate callback.

## Exports

### AccountType (enum)

- `ALL`, `MAIN` – Filter options for account selection.

### useCombinePositionsScript(props)

- **Parameters**: `PositionsProps` (uses `selectedAccount`).
- **Returns**: `CombinePositionsState` – `tableData` (expanded + dataSource), `isLoading`, `pnlNotionalDecimalPrecision`, `sharePnLConfig`, `symbol`, `onSymbolChange`, `pagination`, `mutatePositions`.

### CombinePositionsState

- **Type**: `ReturnType<typeof useCombinePositionsScript>`.

## Usage example

```tsx
const state = useCombinePositionsScript({ symbol, selectedAccount });
<CombinePositions {...state} />;
```
