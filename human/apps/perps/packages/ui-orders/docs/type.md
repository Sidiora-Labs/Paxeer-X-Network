# type

## Overview

Defines the `EditType` enum used when editing order fields (e.g. in edit sheet or inline edit).

## Exports

### `EditType` (enum)

| Member | Description |
|--------|-------------|
| `quantity` | Edit order quantity. |
| `price` | Edit limit/order price. |
| `triggerPrice` | Edit trigger price (algo/TP/SL). |
| `callbackValue` | Edit trailing stop callback value. |
| `callbackRate` | Edit trailing stop callback rate. |

## Usage example

```typescript
import { EditType } from "@paxeer-network/ui-orders";

if (field === EditType.triggerPrice) {
  // show trigger price input
}
```
