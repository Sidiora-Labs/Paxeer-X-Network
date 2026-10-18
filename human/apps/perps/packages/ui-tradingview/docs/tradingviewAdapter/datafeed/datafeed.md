# Datafeed

## Overview

Concrete datafeed extending AbstractDatafeed. Subscribes to tickers and bbos via WebSocket; implements subscribeBars/unsubscribeBars using WebsocketService kline subscription; getQuotes and subscribeQuotes merge bbo + ticker data and push UDF-style QuoteData via eventBus (tickerUpdate). remove() unsubscribes all quote listeners.

## Constructor

Datafeed(apiUrl: string, ws: any). Sets up Requester, HistoryProvider, and ws handlers for tickers and bbos.

## Methods

- `subscribeBars(symbolInfo, resolution, onTick, listenerGuid, onResetCacheNeededCallback)`: Subscribes kline via _publicWs, stores onResetCacheNeededCallback on window.
- `unsubscribeBars(listenerGuid)`: Unsubscribes kline for that listener.
- `getQuotes(symbols, onDataCallback)`: One-shot quote delivery via eventBus subscription; maps bbosMap + tickersMap to _toUDFTicker.
- `subscribeQuotes(symbols, fastSymbols, onRealtimeCallback, listenerGuid)`: Subscribes to tickerUpdate and calls onRealtimeCallback with UDF tickers.
- `unsubscribeQuotes(listenerGuid)`: Removes subscription.
- `remove()`: Unsubscribes all quote subscriptions.
