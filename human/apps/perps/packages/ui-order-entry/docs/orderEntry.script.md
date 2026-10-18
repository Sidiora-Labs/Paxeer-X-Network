# orderEntry.script.ts

## Overview

Hook that wires `useOrderEntry` with symbol, local order type/side, TPSL switch, BBO state, focus/blur, and orderbook click handling. Returns all state and handlers needed by `OrderEntry` UI.

## Exports

### OrderEntryScriptInputs

| Property | Type | Description |
|----------|------|-------------|
| `symbol` | `string` | Trading symbol (e.g. PERP_BTC_USDC) |

### PAXEER_NETWORK_ORDER_SOUND_ALERT_KEY

Local storage key for order filled sound alert preference.

### OrderEntryScriptReturn

Return type of `useOrderEntryScript` (inferred from hook return).

### useOrderEntryScript(inputs)

**Parameters:** `OrderEntryScriptInputs`

**Returns:** Object including:

- `formattedOrder`, `setOrderValue`, `setOrderValues`, `symbolInfo`, `side`, `type`, `level`
- `tpslSwitch`, `setTpslSwitch`
- `onFocus`, `onBlur`, `currentFocusInput`, `lastQuantityInputType`
- `priceInputRef`, `priceInputContainerRef`, `priceInputContainerWidth`, `triggerPriceInputRef`, `activatedPriceInputRef`
- `canTrade`, `bboStatus`, `bboType`, `onBBOChange`, `toggleBBO`
- `currentLtv`, `fillMiddleValue`, `symbol`, `soundAlert`, `setSoundAlert`
- `slPriceError`, `currentLeverage`, and other `useOrderEntry` state (e.g. `maxQty`, `helper`, `submit`, `metaState`, `isMutating`, etc.)

## Usage example

```ts
import { useOrderEntryScript } from "./orderEntry.script";

const state = useOrderEntryScript({ symbol: "PERP_BTC_USDC" });
```
