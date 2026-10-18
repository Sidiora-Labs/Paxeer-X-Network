# broker

## Overview

Broker adapter for TradingView: getBrokerAdapter(host, broker) returns the object expected by the chart (symbolInfo, placeOrder, orders, positions, executions, connectionStatus, chartContextMenuActions, isTradable, accountManagerInfo, currentAccount, accountsMetainfo, remove). placeOrder maps order type to OrderCombinationType and calls broker.sendMarketOrder or sendLimitOrder. utils (if present) contain shared helpers.

## Files

| File | Language | Description | Link |
|------|----------|-------------|------|
| `getBrokerAdapter.ts` | TypeScript | getBrokerAdapter(host, broker): adapter object for TradingView widget | [getBrokerAdapter.md](./getBrokerAdapter.md) |
| `utils.ts` | TypeScript | Broker-related utilities | [utils.md](./utils.md) |
