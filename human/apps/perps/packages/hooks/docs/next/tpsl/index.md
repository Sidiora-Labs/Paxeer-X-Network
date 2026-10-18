# next/tpsl — Directory Index

## Directory Responsibility

Take-profit/stop-loss (TPSL) helpers: price checker hook, estimated liquidation price by symbol, and error message codes. Used when setting or validating TPSL orders.

## Files

| File | Language | Summary | Entry symbol(s) | Link |
|------|----------|---------|------------------|------|
| index.ts | TS | Re-exports TPSL module | useTpslPriceChecker, ERROR_MSG_CODES, useEstLiqPriceBySymbol, useGetEstLiqPrice | [index.md](index.md) |
| useTpslPriceChecker.ts | TS | Hook to check TPSL prices | useTpslPriceChecker | [useTpslPriceChecker.md](useTpslPriceChecker.md) |
| useGetEstLiqPrice.ts | TS | Hook for estimated liq price | useGetEstLiqPrice | [useGetEstLiqPrice.md](useGetEstLiqPrice.md) |
| useEstLiqPriceBySymbol.ts | TS | Est. liq price by symbol | useEstLiqPriceBySymbol | [useEstLiqPriceBySymbol.md](useEstLiqPriceBySymbol.md) |
| errorMsgCodes.ts | TS | TPSL error codes | ERROR_MSG_CODES | [errorMsgCodes.md](errorMsgCodes.md) |
