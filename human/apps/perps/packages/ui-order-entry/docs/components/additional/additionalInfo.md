# additionalInfo.tsx (AdditionalInfo)

## Overview

Renders checkboxes for Post-only, IOC, FOK, order confirm, and visible quantity (hidden). Uses `onValueChange` to update order fields (`order_type_ext`, `visible_quantity`) and local state for confirm/hidden.

## Props (AdditionalInfoProps)

| Prop | Type | Description |
|------|------|-------------|
| `pinned` | `boolean` | Whether additional panel is pinned |
| `setPinned` | `(value: boolean) => void` | Set pinned state |
| `needConfirm` | `boolean` | Whether to show order confirm dialog |
| `setNeedConfirm` | `(value: boolean) => void` | Set confirm preference |
| `hidden` | `boolean` | Visible quantity 0 vs 1 |
| `setHidden` | `(value: boolean) => void` | Set hidden state |
| `onValueChange` | `(key, value) => void` (optional) | Update order field |
| `orderTypeExtra` | `OrderType` (optional) | Current order_type_ext |
| `showExtra` | `boolean` | Whether to show Post-only/IOC/FOK (e.g. limit order, no TPSL) |
