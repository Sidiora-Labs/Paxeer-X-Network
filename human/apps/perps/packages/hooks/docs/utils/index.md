# utils — Directory Index

## Directory Responsibility

Shared utilities: fetcher (for SWR), JSON parse, createGetter, order helpers (createOrder, orderEntryHelper, order price, scaled order), parseHolding, SWR helpers, WS helpers, dev helpers. Used by hooks and services for HTTP, formatting, and order math.

## Files

| File | Language | Summary | Entry symbol(s) | Link |
|------|----------|---------|------------------|------|
| index.ts | TS | Re-exports utils (fetcher, order helpers, etc.) | fetcher, noCacheConfig, useQueryOptions, findTPSLFromOrder, cleanStringStyle, formatNumber, priceToPnl, calcTPSL_ROI, getPositionBySymbol | [index.md](index.md) |
| fetcher.ts | TS | SWR fetcher and options | fetcher, noCacheConfig, useQueryOptions | [fetcher.md](fetcher.md) |
| json.ts | TS | JSON parse utility | parseJSON | [json.md](json.md) |
| createGetter.ts | TS | Create getter utility | (createGetter) | [createGetter.md](createGetter.md) |
| createOrder.ts | TS | Order creation helpers (notional, etc.) | checkNotional, getMinNotional | [createOrder.md](createOrder.md) |
| orderEntryHelper.ts | TS | Order entry string/style helpers | cleanStringStyle, formatNumber | [orderEntryHelper.md](orderEntryHelper.md) |
| parseHolding.ts | TS | Parse holding data | (parseHolding) | [parseHolding.md](parseHolding.md) |
| swr.ts | TS | SWR helpers (e.g. getPositionBySymbol) | getPositionBySymbol | [swr.md](swr.md) |
| ws.ts | TS | WebSocket helpers | (ws) | [ws.md](ws.md) |
| dev.ts | TS | Dev-only utilities | (dev) | [dev.md](dev.md) |
| order/orderPrice.ts | TS | Order price helpers | (orderPrice) | [order/orderPrice.md](order/orderPrice.md) |
| order/scaledOrder.ts | TS | Scaled order helpers | (scaledOrder) | [order/scaledOrder.md](order/scaledOrder.md) |
