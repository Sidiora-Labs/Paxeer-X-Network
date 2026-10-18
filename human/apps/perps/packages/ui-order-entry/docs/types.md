# types.ts

## Overview

Enums and types for order entry input focus and quantity input behavior.

## Exports

### InputType (enum)

Identifies which input field is currently focused or relevant for order entry.

| Member | Description |
|--------|-------------|
| `NONE` | No input focused |
| `PRICE` | Price input focus |
| `TRIGGER_PRICE` | Trigger price input focus |
| `QUANTITY` | Quantity input focus |
| `QUANTITY_SLIDER` | Quantity slider input focus |
| `TOTAL` | Total input focus |
| `MARGIN` | Margin input focus |
| `START_PRICE` | Scaled order start price input focus |
| `END_PRICE` | Scaled order end price input focus |
| `TOTAL_ORDERS` | Scaled order total orders input focus |
| `SKEW` | Scaled order skew input focus |
| `ACTIVATED_PRICE` | Trailing stop activated price input focus |
| `CALLBACK_VALUE` | Trailing stop callback value input focus |
| `CALLBACK_RATE` | Trailing stop callback rate input focus |

## Usage example

```ts
import { InputType } from "@paxeer-network/ui-order-entry";

if (currentFocusInput.current === InputType.PRICE) {
  // focus is on price input
}
```
