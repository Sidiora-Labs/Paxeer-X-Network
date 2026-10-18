# orderEntryContext.tsx

## Overview

React context for order entry: validation errors, visibility of error messages, symbol info, focus/blur handlers, error message getter, order value setters, current focus input, and refs for price/trigger/activated inputs and last quantity input type. Consumed by input components and TPSL UI.

## Exports

### OrderEntryContextState

| Property | Type | Description |
|----------|------|-------------|
| `errors` | `OrderValidationResult \| null` | Validation errors keyed by field |
| `errorMsgVisible` | `boolean` | Whether to show error messages |
| `symbolInfo` | `API.SymbolExt` | Symbol metadata (base, quote, dps, etc.) |
| `onFocus` | `(type: InputType) => FocusEventHandler` | Focus handler factory |
| `onBlur` | `(type: InputType) => FocusEventHandler` | Blur handler factory |
| `getErrorMsg` | `(key, customValue?) => string` | Get error message for a field |
| `setOrderValue` | `(key, value) => void` | Set single order field |
| `setOrderValues` | `(values: Partial<PaxeerNetworkOrder>) => void` | Set multiple order fields |
| `currentFocusInput` | `InputType` | Currently focused input type |
| `priceInputRef` | `RefObject<HTMLInputElement>` | Price input ref |
| `priceInputContainerRef` | `RefObject<HTMLDivElement>` | Price input container ref |
| `triggerPriceInputRef` | `RefObject<HTMLInputElement>` | Trigger price input ref |
| `activatedPriceInputRef` | `RefObject<HTMLInputElement>` | Trailing stop activated price ref |
| `lastQuantityInputType` | `MutableRefObject<InputType>` | Last quantity-related input type |
| `leverage` | `number` (optional) | Symbol leverage |

### OrderEntryContext

React context instance.

### useOrderEntryContext()

Hook that returns the current `OrderEntryContextState`.

## Usage example

```ts
import { useOrderEntryContext } from "./orderEntryContext";

const { setOrderValue, getErrorMsg } = useOrderEntryContext();
```
