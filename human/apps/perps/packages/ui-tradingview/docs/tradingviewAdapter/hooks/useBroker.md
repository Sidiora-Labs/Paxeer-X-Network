# useBroker

## Overview

Returns a stable broker object (ref) with cancelOrder, closePosition, editOrder, sendLimitOrder, sendMarketOrder (wrapped), getSymbolInfo, colorConfig, mode. Uses useCancelOrder, useEditOrder, useSendOrder, useSymbolsInfo; closePosition calls closeConfirm. Effects keep broker.current in sync with latest handlers and symbol data.

## Parameters

| Name | Type | Description |
|------|------|-------------|
| closeConfirm | (position) => void | Callback to show close-position confirm and submit |
| colorConfig | ColorConfigInterface | Chart colors for renderer |
| onToast | any | Toast for errors (e.g. editOrder) |
| symbol | string | Current symbol |
| mode | ChartMode | Optional chart mode |

## Returns

Broker object: cancelOrder, closePosition, editOrder, colorConfig, sendLimitOrder, getSymbolInfo, sendMarketOrder, mode.
