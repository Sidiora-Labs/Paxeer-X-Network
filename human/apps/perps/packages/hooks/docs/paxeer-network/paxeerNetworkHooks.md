# paxeerNetworkHooks

## Overview

Barrel file that re-exports all paxeer-network hooks: orderbook stream, symbols info, symbol info, account info, markets stream/market/markets store, mark/index prices and streams, leverage, Odos quote, computed LTV, ticker stream, funding rate/details/rates/history, position stream, order stream, sub-account algo order stream, market trade stream, collateral, max qty, margin ratio, chains, storage chain, chain/chain info, withdraw, deposit, convert, transfer, internal transfer, max withdrawal, holding stream, wallet/balance/settle subscriptions, private data observer, symbol price range, TPSL order, max/symbol leverage, statistics hooks, maintenance status, and stores (mark price by symbol, position actions, storage ledger address, tokens info, symbols info store, RWA symbols, funding rates store, portfolio, funding rate by symbol).

## Usage example

```ts
import {
  useTickerStream,
  useMarkPrice,
  useOrderbookStream,
  usePositionStream,
} from "@paxeer-network/hooks";

// Use any exported paxeer-network hook
const ticker = useTickerStream("BTC_PERP");
const markPrice = useMarkPrice("BTC_PERP");
```
