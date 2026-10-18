# positions.ui

## Overview

Presentational components: desktop `Positions` (AuthGuardDataTable + columns), `MobilePositions` (ListView + PositionCellWidget), and `CombinePositions` (expandable rows by account).

## Components

### Positions

- **Props**: `PositionsState` (from `usePositionsScript`)
- **Behavior**: Renders `AuthGuardDataTable` with `useColumn`, `SymbolProvider` + `PositionsRowProvider` per row, pagination, and sort.

### MobilePositions

- **Props**: `PositionsState` & `PositionsProps`
- **Behavior**: `ListView` with `PositionCellWidget` per item, wrapped in `SymbolProvider` and `PositionsRowProvider`.

### CombinePositions

- **Props**: `CombinePositionsState`
- **Behavior**: DataTable with `getSubRows` for account grouping; group rows show Badge/address; position rows use same row provider and close/reverse actions.

## Usage example

```tsx
import { Positions, MobilePositions, CombinePositions } from "./positions.ui";
// Usually used via PositionsWidget / MobilePositionsWidget / CombinePositionsWidget
```
