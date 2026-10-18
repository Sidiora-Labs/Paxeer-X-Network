# hooks

## Overview

React hooks for broker actions and chart renderer: useBroker (cancel, edit, close, send order, getSymbolInfo), useCreateRenderer (positions/orders/fills streams, createRenderer/removeRenderer), useEditOrder, useSendOrder, useCancelOrder, useLazyEffect.

## Files

| File | Language | Description | Link |
|------|----------|-------------|------|
| `useBroker.ts` | TypeScript | useBroker(closeConfirm, colorConfig, onToast, symbol, mode): cancelOrder, closePosition, editOrder, sendLimitOrder, sendMarketOrder, getSymbolInfo | [useBroker.md](./useBroker.md) |
| `useCreateRenderer.ts` | TypeScript | useCreateRenderer(symbol, displayControlSetting): [createRenderer, removeRenderer]; streams positions, pendingOrders, fillOrders; effects render positions/orders/fills | [useCreateRenderer.md](./useCreateRenderer.md) |
| `useEditOrder.ts` | TypeScript | useEditOrder(onToast): callback(order, lineValue) for TPSL/algo/limit updates | [useEditOrder.md](./useEditOrder.md) |
| `useSendOrder.ts` | TypeScript | useSendOrder(symbol): sendLimitOrder, sendMarketOrder (uses useOrderEntry_deprecated) | [useSendOrder.md](./useSendOrder.md) |
| `useCancelOrder.ts` | TypeScript | useCancelOrder(): callback(order) to cancel order or algo/TPLS child | [useCancelOrder.md](./useCancelOrder.md) |
| `useLazyEffect.ts` | TypeScript | Lazy effect hook (if present) | [useLazyEffect.md](./useLazyEffect.md) |
