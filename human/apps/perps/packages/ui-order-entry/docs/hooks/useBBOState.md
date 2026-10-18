# useBBOState.ts

## Overview

Manages BBO (best bid/offer) UI state: on/off/disabled, selected BBO type, and handlers to toggle and change type. Syncs with order type (e.g. disabled when TPSL or FOK/IOC/Post-only). Persists BBO type in localStorage.

## Parameters

| Property | Type | Description |
|----------|------|-------------|
| `tpslSwitch` | `boolean` | Whether TP/SL is enabled (disables BBO) |
| `order_type` | `OrderType` (optional) | Current order type |
| `order_type_ext` | `OrderType` (optional) | Order type ext (e.g. ASK, BID, POST_ONLY) |
| `side` | `OrderSide` (optional) | BUY or SELL |
| `setOrderValues` | `(values: Partial<PaxeerNetworkOrder>) => void` | Set order fields |

## Returns

| Property | Type | Description |
|----------|------|-------------|
| `bboStatus` | `BBOStatus` | ON, OFF, or DISABLED |
| `bboType` | `BBOOrderType \| undefined` | Current BBO type |
| `setBBOType` | `(v) => void` | Set BBO type |
| `onBBOChange` | `(value: BBOOrderType) => void` | Change handler |
| `toggleBBO` | `() => void` | Toggle BBO on/off |
