# positions.widget

## Overview

Container components that wire script hooks to UI: `PositionsWidget` (desktop), `MobilePositionsWidget`, and `CombinePositionsWidget` (multi-account grouped table).

## Exports

### PositionsWidget

- **Props**: `PositionsProps`
- **Behavior**: Uses `usePositionsScript(props)` and renders `Positions` with the returned state.

### MobilePositionsWidget

- **Props**: `PositionsProps`
- **Behavior**: Same script, renders `MobilePositions`.

### CombinePositionsWidget

- **Props**: `PositionsProps`
- **Behavior**: Uses `useCombinePositionsScript(props)` and renders `CombinePositions` (grouped by account).

## Usage example

```tsx
import {
  PositionsWidget,
  MobilePositionsWidget,
  CombinePositionsWidget,
} from "@paxeer-network/ui-positions";

<PositionsWidget symbol={symbol} onSymbolChange={onSymbolChange} />
<MobilePositionsWidget symbol={symbol} />
<CombinePositionsWidget selectedAccount={account} />
```
