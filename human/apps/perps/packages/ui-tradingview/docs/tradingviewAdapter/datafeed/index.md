# datafeed

## Overview

TradingView datafeed implementation: UDF-style config, symbol resolve/search, history bars (HistoryProvider + Requester), real-time bars via WebsocketService, and quotes (getQuotes, subscribeQuotes) from tickers and bbos WebSocket topics.

## Files

| File | Language | Description | Link |
|------|----------|-------------|------|
| `datafeed.ts` | TypeScript | Datafeed class: extends AbstractDatafeed, subscribeBars via WebsocketService, getQuotes/subscribeQuotes from bbos+tickers, remove | [datafeed.md](./datafeed.md) |
| `abstract-datafeed.ts` | TypeScript | AbstractDatafeed: getBars, onReady, searchSymbols, resolveSymbol, configuration from tv/config | [abstract-datafeed.md](./abstract-datafeed.md) |
| `history-provider.ts` | TypeScript | HistoryProvider: getBars from tv/history or v1/tv/kline_history, resolution mapping | [history-provider.md](./history-provider.md) |
| `requester.ts` | TypeScript | Requester: sendRequest to datafeed URL with optional params and headers | [requester.md](./requester.md) |
| `symbol-storage.ts` | TypeScript | SymbolsStorage: symbol search and resolve from API | [symbol-storage.md](./symbol-storage.md) |
| `websocket.service.ts` | TypeScript | WebsocketService: subscribeKline, unsubscribeKline, subscribeSymbol, kline/trade updates | [websocket.service.md](./websocket.service.md) |
| `helpers.ts` | TypeScript | RequestParams, UdfResponse, logMessage, getErrorMessage | [helpers.md](./helpers.md) |
| `eventBus.ts` | TypeScript | MultiBroadcastEventBus: subscribe, unsubscribe, publish | [eventBus.md](./eventBus.md) |
| `iRequester.ts` | TypeScript | IRequester interface for HTTP client | [iRequester.md](./iRequester.md) |
