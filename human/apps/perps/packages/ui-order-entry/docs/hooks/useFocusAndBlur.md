# useFocusAndBlur.ts

## Overview

Tracks which order entry input is focused and provides onFocus/onBlur handlers. Updates currentFocusInput, lastScaledOrderPriceInput (for scaled order), and lastQuantityInputType. On blur of quantity input, formats quantity by base_tick.

## Parameters (FocusAndBlurProps)

| Property | Type | Description |
|----------|------|-------------|
| `base_tick` | `number` | Quantity tick size |
| `order_type` | `OrderType` (optional) | Order type (scaled skips quantity format) |
| `order_quantity` | `string` (optional) | Current quantity |
| `setValue` | `OrderEntryReturn["setValue"]` | Set single order value |

## Returns

| Property | Type | Description |
|----------|------|-------------|
| `currentFocusInput` | `RefObject<InputType>` | Currently focused input |
| `lastScaledOrderPriceInput` | `RefObject<InputType>` | Last focused scaled price (start/end) |
| `lastQuantityInputType` | `RefObject<InputType>` | Last quantity-related input |
| `onFocus` | `(type: InputType) => FocusEventHandler` | Focus handler |
| `onBlur` | `(type: InputType) => FocusEventHandler` | Blur handler (with 300ms delay for currentFocusInput reset) |
