# symbolProvider

## Overview

Provider component that reads symbol info via `useSymbolsInfo()[symbol]` and supplies `SymbolContextState` to children (e.g. position row cells).

## Props

| Prop | Type | Required | Description |
| ---- | ----- | -------- | ----------- |
| `symbol` | `string` | Yes | Symbol key for the current row. |
| `children` | `ReactNode` | Yes | Wrapped content that can use `useSymbolContext()`. |

## Usage example

```tsx
import { SymbolProvider } from "./symbolProvider";
<SymbolProvider symbol={record.symbol}>
  <PositionCell ... />
</SymbolProvider>
```
