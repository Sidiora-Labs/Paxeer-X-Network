# liquidation.widget

## Overview

Container components for liquidation history: `LiquidationWidget` and `MobileLiquidationWidget` (optional `classNames`).

## Exports

### LiquidationProps

- `symbol?`, `enableLoadMore?`

### LiquidationWidget

- **Props**: `LiquidationProps`
- **Behavior**: Uses `useLiquidationScript`, renders `Liquidation`.

### MobileLiquidationWidget

- **Props**: `LiquidationProps & { classNames?: { root?; content?; cell? } }`
- **Behavior**: Same script, renders `MobileLiquidation` with `classNames`.

## Usage example

```tsx
<LiquidationWidget symbol={symbol} enableLoadMore />
```
