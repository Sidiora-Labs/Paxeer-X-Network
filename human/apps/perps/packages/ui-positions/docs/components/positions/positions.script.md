# positions.script

## Overview

Hook `usePositionsScript` fetches position stream, applies sort (with optional session storage via `useTabSort`), pagination, and exposes state for desktop/mobile positions UI.

## Exports

### usePositionsScript(props)

- **Parameters**: `PositionsProps`
- **Returns**: `PositionsState` – `dataSource`, `isLoading`, `pnlNotionalDecimalPrecision`, `sharePnLConfig`, `symbol`, `onSymbolChange`, `pagination`, `onSort`, `positionReverse`, `initialSort`.

### PositionsState

- **Type**: `ReturnType<typeof usePositionsScript>`

## Usage example

```tsx
const state = usePositionsScript({ symbol, onSymbolChange, enableSortingStorage: true });
<Positions {...state} />;
```
