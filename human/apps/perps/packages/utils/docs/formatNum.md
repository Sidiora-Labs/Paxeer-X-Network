# formatNum

## Overview

Formats numbers by semantic type (pnl, notional, roi, assetValue, collateral) with configurable decimal places and rounding. Uses `Decimal` internally. Exported as a function with namespace methods for each type.

## Exports

### formatNum (function and namespace)

Main formatter and namespace methods. Each method accepts optional `num` (`number | Decimal | string`) and returns `Decimal | undefined`.

| Method | Description | Default dp |
|--------|-------------|------------|
| `formatNum.pnl(num)` | PnL: round down if &gt; 0, round up if &lt; 0 | 2 |
| `formatNum.notional(num)` | Notional, round down | 2 |
| `formatNum.roi(num, dp?)` | ROI, round down/up by sign | dp ?? 4 |
| `formatNum.assetValue(num)` | Asset value, round down | 2 |
| `formatNum.collateral(num)` | Collateral, round down | 2 |

**Returns:** `Decimal | undefined` — formatted value, or `undefined` if parsing fails.

## Usage example

```typescript
import { formatNum, Decimal } from "@paxeer-network/utils";

formatNum.pnl(123.456);           // Decimal, 2 dp, rounding by sign
formatNum.notional("1,234.56");   // Decimal, 2 dp
formatNum.roi(0.01234, 4);       // Decimal, 4 dp
formatNum.assetValue(1000);      // Decimal, 2 dp
formatNum.collateral(500.1);      // Decimal, 2 dp
```
