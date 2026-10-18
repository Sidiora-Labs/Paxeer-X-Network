# middleware — Directory Index

## Directory Responsibility

SWR and app-level middleware: timestamp waiting (sync server time), IndexedDB persistence, signature injection, local time correction. Used to adapt SWR and private requests.

## Files

| File | Language | Summary | Entry symbol(s) | Link |
|------|----------|---------|------------------|------|
| timestampWaitingMiddleware.ts | TS | SWR middleware to wait for server timestamp | timestampWaitingMiddleware, resetTimestampOffsetState | [timestampWaitingMiddleware.md](timestampWaitingMiddleware.md) |
| persistIndexedDB.ts | TS | Persist SWR cache to IndexedDB | persistIndexedDB | [persistIndexedDB.md](persistIndexedDB.md) |
| indexedDBManager.ts | TS | IndexedDB manager and app DB init | indexedDBManager, initializeAppDatabase | [indexedDBManager.md](indexedDBManager.md) |
| signatureMiddleware.ts | TS | Middleware for request signing | (internal) | [signatureMiddleware.md](signatureMiddleware.md) |
| localTimeCorrectionMiddleware.ts | TS | Local time correction | (internal) | [localTimeCorrectionMiddleware.md](localTimeCorrectionMiddleware.md) |
