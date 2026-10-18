# usePriceInputContainer.ts

## Overview

Provides a ref for the price input container and its current width. Uses ResizeObserver to update width when container size changes (e.g. for BBO order type select dropdown width).

## Parameters

| Property | Type | Description |
|----------|------|-------------|
| `order_type_ext` | `OrderType` (optional) | Order type extension (used as dependency) |

## Returns

| Property | Type | Description |
|----------|------|-------------|
| `priceInputContainerRef` | `RefObject<HTMLDivElement>` | Ref to attach to container |
| `priceInputContainerWidth` | `number` | Current width in pixels |
