# tradingviewAdapter

## Overview

Adapter layer for the TradingView charting library: widget wrapper, options and persistence, datafeed (history + real-time), broker adapter, renderers (positions, orders, executions, TP/SL), hooks (useBroker, useCreateRenderer, useEditOrder, useSendOrder, useCancelOrder), and shared types/enums.

## Subdirectories

| Directory | Description |
|-----------|-------------|
| [widget](./widget/index.md) | Widget class, option merging (enabled/disabled features), persistUtils, util, chart_hack |
| [datafeed](./datafeed/index.md) | Datafeed, AbstractDatafeed, history-provider, requester, symbol-storage, websocket.service, helpers, eventBus, iRequester |
| [renderer](./renderer/index.md) | Renderer, positionLine, orderLine, execution, tpsl services, brokerHostHandler, tpsl.util, tpslCal.service, order.util |
| [hooks](./hooks/index.md) | useBroker, useCreateRenderer, useEditOrder, useSendOrder, useCancelOrder, useLazyEffect |
| [broker](./broker/index.md) | getBrokerAdapter, utils |

## Top-level files

| File | Language | Description | Link |
|------|----------|-------------|------|
| `type.ts` | TypeScript | Re-exports from charting_library; ChartOrder, enums, ChartPosition, OrderInterface, ColorConfigInterface | [type.md](./type.md) |
| `charting_library.d.ts` | TypeScript | Declarations for TradingView widget/broker/datafeed APIs | — |
