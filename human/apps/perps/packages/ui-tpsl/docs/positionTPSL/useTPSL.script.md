# useTPSL.script

## Overview

Hook that builds and validates position TPSL state, handles submit with optional confirm dialog, and integrates with `useTPSLOrder`, position stream, and SL price checker. Returns all state and handlers needed by the TPSL UI.

## Exports

### TPSLBuilderOptions

| Name | Type | Required | Description |
|------|------|----------|-------------|
| symbol | `string` | Yes | Symbol |
| position | `API.Position` | No | Position (for sub-account or when not using main account) |
| order | `API.AlgoOrder` | No | Existing order when editing |
| onTPSLTypeChange | `(type: AlgoOrderRootType) => void` | No | Callback when TPSL type changes |
| isEditing | `boolean` | No | Edit mode |
| positionType | `PositionType` | No | Full or partial |
| onConfirm | `(order, options) => Promise<boolean>` | No | Custom confirm; if Promise rejects or returns false, submit is cancelled |
| close | `() => void` | No | Close callback |

### useTPSLBuilder(options)

**Parameters:** `TPSLBuilderOptions & { withTriggerPrice?, triggerPrice?, type?: "tp" \| "sl", qty? }`

**Returns (TPSLBuilderState):** isEditing, symbolInfo, maxQty, setQuantity, orderQuantity, TPSL_OrderEntity, setOrderValue, setPnL, setOrderPrice, onSubmit, slPriceError, estLiqPrice, metaState, errors, status (isCreateMutating, isUpdateMutating), position, setValues.

- Resolves position from main account stream or `options.position` (e.g. sub-account).
- Uses `useTPSLOrder` for order state and submit/delete/validate.
- Uses `useTpslPriceChecker` for SL price warning/error.
- Default confirm dialog shows `PositionTPSLConfirm`; can be overridden with `onConfirm`.
- If both TP and SL are empty on submit, shows cancel-order confirm.

## Usage example

```tsx
const state = useTPSLBuilder({
  symbol: position.symbol,
  position,
  positionType: PositionType.PARTIAL,
  onComplete: () => {},
});
// state.TPSL_OrderEntity, state.setOnOrderValue, state.onSubmit, etc.
```
