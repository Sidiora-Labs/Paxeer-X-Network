# getBrokerAdapter

## Overview

Builds the broker adapter object for TradingView broker_factory. symbolInfo(symbol) returns qty min/max/step and pip from broker.getSymbolInfo. placeOrder(order) maps order.side and order.type (numeric) to SideType and OrderCombinationType, then calls broker.sendMarketOrder (for MARKET) or sendLimitOrder (for LIMIT). orders/positions/executions return empty arrays. connectionStatus() 1, isTradable() true, chartContextMenuActions delegates to host.defaultContextMenuActions. remove() unsubscribes host.silentOrdersPlacement.

## Parameters

host: IBrokerConnectionAdapterHost; broker: ReturnType<typeof useBroker>.

## Returns

Adapter object with symbolInfo, placeOrder, orders, positions, executions, connectionStatus, chartContextMenuActions, isTradable, accountManagerInfo, currentAccount, accountsMetainfo, remove.
