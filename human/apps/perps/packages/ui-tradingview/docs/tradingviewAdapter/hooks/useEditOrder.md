# useEditOrder

## Overview

Returns a callback(order, lineValue) that edits an order: for TPSL/algo types calls updateTPSLOrder or updateAlgoOrder; for limit orders uses updateOrder. Uses useOrderStream and useEventEmitter; on failure calls onToast.error. lineValue holds the new value (e.g. trigger_price).

## Parameters

onToast: any (toast API for errors).

## Returns

(order, lineValue) => Promise (edit request).
