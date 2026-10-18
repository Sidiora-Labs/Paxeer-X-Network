# hooks

## Overview

Hooks used by the order entry script and UI: BBO state, focus/blur handling, price input container width, and best ask/bid from orderbook.

## Files

| File | Description |
|------|-------------|
| [useAskAndBid.md](useAskAndBid.md) | Returns current best ask and bid from orderbook stream (for mid price) |
| [useBBOState.md](useBBOState.md) | BBO on/off state, type, toggle and change handlers |
| [useFocusAndBlur.md](useFocusAndBlur.md) | currentFocusInput, lastScaledOrderPriceInput, lastQuantityInputType, onFocus/onBlur |
| [usePriceInputContainer.md](usePriceInputContainer.md) | priceInputContainerRef and width (ResizeObserver) for BBO dropdown |
