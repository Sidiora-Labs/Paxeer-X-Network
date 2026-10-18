# @paxeer-network/utils

## Overview

This package provides shared utilities for the paxeer-network ecosystem: decimal math, number formatting, symbol formatting, order/TP-SL helpers, date/time, chain IDs, string helpers, and safe window/global access. All modules live under `packages/utils/src`.

## Module index

| File | Language | Description |
|------|----------|-------------|
| [version](version.md) | TypeScript | Package version and `window.__PAXEER_NETWORK_VERSION__` registration |
| [symbol](symbol.md) | TypeScript | Symbol formatting and display (`formatSymbol`, `optimizeSymbolDisplay`) |
| [order](order.md) | TypeScript | Order helpers: trailing stop price, BBO type, TP/SL direction |
| [formatNum](formatNum.md) | TypeScript | Number formatting by type (pnl, notional, roi, assetValue, collateral) |
| [window](window.md) | TypeScript | `windowGuard`, `getGlobalObject`, `getTimestamp` |
| [string](string.md) | TypeScript | String helpers: capitalize, symbol form, camelCase, longest common substring |
| [decimal](decimal.md) | TypeScript | Decimal.js re-export, commify, human-style numbers, precision, NaN check |
| [dateTime](dateTime.md) | TypeScript | Time conversion, timestamp to string, subtract days |
| [chain](chain.md) | TypeScript | Chain ID parsing (hex/int), testnet/Solana detection |
| [exports](exports.md) | TypeScript | Barrel file re-exporting all public APIs |

## Directory structure

```
src/
  index.ts      # Barrel exports
  version.ts
  symbol.ts
  order.ts
  formatNum.ts
  window.ts
  string.ts
  decimal.ts
  dateTime.ts
  chain.ts
```
