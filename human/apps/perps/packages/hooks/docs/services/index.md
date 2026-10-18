# services — Directory Index

## Directory Responsibility

Back-end and business logic services: order creator (all order types, validators, builders), order merge handlers, painter (poster/canvas), amplitude tracker, data center service. Used by hooks to submit orders, merge order updates, draw posters, and track events.

## Subdirectories

| Directory | Description | Index |
|-----------|-------------|--------|
| [orderCreator](orderCreator/index.md) | Order creation: creators, validators, builders, factory | [orderCreator/index.md](orderCreator/index.md) |
| [orderMerge](orderMerge/index.md) | Order merge handlers (regular, algo) | [orderMerge/index.md](orderMerge/index.md) |
| [painter](painter/index.md) | Canvas painter for poster/layout | [painter/index.md](painter/index.md) |

## Top-Level Files

| File | Language | Summary | Link |
|------|----------|---------|------|
| dataCenter.ts | TS | Data center service | [dataCenter.md](dataCenter.md) |
| amplitudeTracker.ts | TS | Amplitude analytics | [amplitudeTracker.md](amplitudeTracker.md) |
