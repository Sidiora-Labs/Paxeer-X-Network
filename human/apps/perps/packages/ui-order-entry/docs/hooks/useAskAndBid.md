# useAskAndBid.ts

## Overview

Subscribes to orderbook updates via event emitter and returns the current best ask and best bid as `[number, number]`. Used for "fill middle" (mid price) in limit order.

## Returns

`[number, number]` — `[bestAsk, bestBid]`, debounced 200ms.

## Usage example

```ts
const askAndBid = useAskAndBid();
const [bestAsk, bestBid] = askAndBid;
```
