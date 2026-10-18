# ui-tradingview

> Location: `packages/ui-tradingview/src`

## Overview

This package provides TradingView chart integration for the paxeer-network trading UI. It includes the main chart widget, datafeed for kline/history and real-time quotes, broker adapter for order/position handling, and renderers for positions, orders, executions, and TP/SL on the chart.

## Subdirectories

| Directory | Description |
|-----------|-------------|
| [components](./components/index.md) | React components: TradingviewWidget, TradingviewUI, script hook, top bar, time interval, line type, display control |
| [icons](./icons/index.md) | SVG icon components for chart toolbar |
| [utils](./utils/index.md) | Chart styling, resolution mapping, and localStorage keys |
| [tradingviewAdapter](./tradingviewAdapter/index.md) | Widget wrapper, datafeed, broker, renderers, and TradingView-specific types |

## Top-level files

| File | Language | Description | Link |
|------|----------|-------------|------|
| `index.ts` | TypeScript | Package entry: exports `TradingviewWidget`, `TradingviewUI`, `useTradingviewScript`, and re-exports from `./type` | — |
| `type.ts` | TypeScript | Public props and display-control types for the widget and UI | [type.md](./type.md) |
| `version.ts` | TypeScript | Package version registration on `window.__PAXEER_NETWORK_VERSION__` | [version.md](./version.md) |
