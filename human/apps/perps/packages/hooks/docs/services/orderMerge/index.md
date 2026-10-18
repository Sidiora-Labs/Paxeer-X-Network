# services/orderMerge — Directory Index

## Directory Responsibility

Order merge handlers: merge incoming order updates with local state. Regular and algo order merge handlers extend a base merge handler. Used by order stream or order book to keep local order list in sync with server/WS.

## Files

| File | Language | Summary | Entry symbol(s) | Link |
|------|----------|---------|------------------|------|
| interface.ts | TS | Merge handler interface | (interface) | [interface.md](interface.md) |
| baseMergeHandler.ts | TS | Base merge handler | (base class) | [baseMergeHandler.md](baseMergeHandler.md) |
| regularOrderMergeHandler.ts | TS | Regular order merge | (handler) | [regularOrderMergeHandler.md](regularOrderMergeHandler.md) |
| algoOrderMergeHandler.ts | TS | Algo order merge | (handler) | [algoOrderMergeHandler.md](algoOrderMergeHandler.md) |
