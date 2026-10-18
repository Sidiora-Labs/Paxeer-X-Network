# useSendOrder

## Overview

Returns sendLimitOrder and sendMarketOrder. sendMarketOrder(data) calls useOrderEntry_deprecated onSubmit with symbol, side, order_quantity, order_type: MARKET, reduce_only: false; on error shows toast.error. sendLimitOrder is a stub (no-op).

## Parameters

symbol: string.

## Returns

{ sendLimitOrder, sendMarketOrder }.
