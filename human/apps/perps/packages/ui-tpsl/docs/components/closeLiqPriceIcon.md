# closeLiqPriceIcon

## Overview

Shows a warning icon when SL trigger price is close to liquidation (from `OrderValidationResult`). On desktop uses a tooltip; on mobile uses a modal alert on click.

## Exports

### CloseToLiqPriceIcon

#### Props

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| slPriceError | `OrderValidationResult \| null` | Yes | — | Validation result; warning when `sl_trigger_price` has `ERROR_MSG_CODES.SL_PRICE_WARNING` |
| className | `string` | No | — | Extra class for the icon |

Renders nothing if there is no SL price warning. Otherwise renders `ExclamationFillIcon` (size 14, warning color); on mobile wrapped in a button that opens `modal.alert` with the error message; on desktop wrapped in `Tooltip`.

## Usage example

```tsx
import { CloseToLiqPriceIcon } from "@paxeer-network/ui-tpsl";

<CloseToLiqPriceIcon slPriceError={slPriceError} className="oui-ml-1" />
```
