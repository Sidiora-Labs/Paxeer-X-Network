# WebsocketService

## Overview

Singleton service wrapping WS instance. subscribeKline(subscribeId, symbol, resolution, onTickCallback) maps resolution via mapResolution, subscribes to `${symbol}@kline_${time}`, stores callbacks by klineKey; onMessage updates kline and invokes callbacks. unsubscribeKline(subscribeId) removes callback and unsubscribes topic when no callbacks left. subscribeSymbol(symbol) subscribes to trade topic for last-price updates. updateKlineByLastPrice/updateKline drive real-time bar updates.
