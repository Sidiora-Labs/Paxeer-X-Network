# renderer

## Overview

Chart overlays: Renderer orchestrates PositionLineService, OrderLineService, ExecutionService, TPSLService. brokerHostHandler patches host for silent orders and buy/sell visibility. Utilities: order.util, tpsl.util, tpslCal.service.

## Files

| File | Language | Description | Link |
|------|----------|-------------|------|
| `renderer.ts` | TypeScript | Renderer class: renderPositions, renderPendingOrders, renderFilledOrders, remove, chartReady, onDataLoaded | [renderer.md](./renderer.md) |
| `brokerHostHandler.ts` | TypeScript | preventDefaultRenderHack(host), forceSilentOrdersPlacement(instance, host) | [brokerHostHandler.md](./brokerHostHandler.md) |
| `positionLine.service.ts` | TypeScript | PositionLineService: draw position lines on chart | [positionLine.service.md](./positionLine.service.md) |
| `orderLine.service.ts` | TypeScript | OrderLineService: draw order lines, update positions | [orderLine.service.md](./orderLine.service.md) |
| `execution.service.ts` | TypeScript | ExecutionService: draw execution markers | [execution.service.md](./execution.service.md) |
| `tpsl.service.tsx` | TSX | TPSLService: TP/SL lines, updatePositions | [tpsl.service.md](./tpsl.service.md) |
| `tpslCal.service.ts` | TypeScript | TPSL calculation helpers | [tpslCal.service.md](./tpslCal.service.md) |
| `tpsl.util.ts` | TypeScript | TpslAlgoType, BracketAlgoType and related constants | [tpsl.util.md](./tpsl.util.md) |
| `order.util.ts` | TypeScript | Order mapping/format helpers | [order.util.md](./order.util.md) |
