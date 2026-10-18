# services/orderCreator — Directory Index

## Directory Responsibility

Order creation pipeline: factory to get creator by order type, base creators (regular, bracket, algo), concrete creators (market, limit, stop, FOK, IOC, post-only, scaled, trailing stop, TPSL), validation strategies and validators, and order builders. Used by order entry flow to build and validate orders before submit.

## Subdirectories

| Directory | Description | Index |
|-----------|-------------|--------|
| [builders](builders/index.md) | Order builders (Order, Algo, Bracket) | [builders/index.md](builders/index.md) |
| [validators](validators/index.md) | Validation strategies and validators | [validators/index.md](validators/index.md) |

## Key Files

| File | Language | Summary | Link |
|------|----------|---------|------|
| factory.ts | TS | Creator factory by order type | [factory.md](factory.md) |
| interface.ts | TS | Order creator interface and types | [interface.md](interface.md) |
| baseCreator.ts | TS | Base order creator | [baseCreator.md](baseCreator.md) |
| marketOrderCreator.ts | TS | Market order creator | [marketOrderCreator.md](marketOrderCreator.md) |
| limitOrderCreator.ts | TS | Limit order creator | [limitOrderCreator.md](limitOrderCreator.md) |
| orderValidation.ts | TS | Order validation orchestration | [orderValidation.md](orderValidation.md) |
| (others) | TS | Stop/limit, bracket, algo, TPSL, etc. | (see directory listing) |
