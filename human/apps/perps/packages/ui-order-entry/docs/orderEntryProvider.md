# orderEntryProvider.tsx

## Overview

React context provider that supplies order entry state to children: errors, symbolInfo, focus/blur handlers, getErrorMsg, setOrderValue/setOrderValues, currentFocusInput, and refs (priceInputRef, priceInputContainerRef, triggerPriceInputRef, activatedPriceInputRef, lastQuantityInputType), plus leverage.

## Props

All props are passed through as `OrderEntryContextState` (see orderEntryContext.tsx): `errors`, `errorMsgVisible`, `symbolInfo`, `onFocus`, `onBlur`, `getErrorMsg`, `setOrderValue`, `setOrderValues`, `currentFocusInput`, `priceInputRef`, `priceInputContainerRef`, `triggerPriceInputRef`, `activatedPriceInputRef`, `lastQuantityInputType`, `leverage`.

## Usage example

```tsx
<OrderEntryProvider {...contextState}>
  <OrderInput ... />
</OrderEntryProvider>
```
