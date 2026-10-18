# next — Directory Index

## Directory Responsibility

Next-generation order entry and related features: `useOrderEntry` hook, order store, position close, TPSL (take-profit/stop-loss) helpers, order book store, user orders, API status. This directory is the main entry for building order forms and closing positions.

## Subdirectories

| Directory | Description | Index |
|-----------|-------------|--------|
| [apiStatus](apiStatus/index.md) | API status store | [apiStatus/index.md](apiStatus/index.md) |
| [orderBook](orderBook/index.md) | Order book store | [orderBook/index.md](orderBook/index.md) |
| [positions](positions/index.md) | Position close hook | [positions/index.md](positions/index.md) |
| [tpsl](tpsl/index.md) | TPSL price checker, est. liq price | [tpsl/index.md](tpsl/index.md) |
| [useOrderEntry](useOrderEntry/index.md) | Order entry hook and store | [useOrderEntry/index.md](useOrderEntry/index.md) |
| [userOrders](userOrders/index.md) | User order store | [userOrders/index.md](userOrders/index.md) |

## Key Entities

| Entity | Location | Responsibility |
|--------|----------|----------------|
| useOrderEntry | next/useOrderEntry | Order entry state and submit |
| useOrderStore | next/useOrderEntry | Order entry store |
| usePositionClose | next/positions | Close position flow |
| useTpslPriceChecker / useGetEstLiqPrice / useEstLiqPriceBySymbol | next/tpsl | TPSL and liquidation price |
