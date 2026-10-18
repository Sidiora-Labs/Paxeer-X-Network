# useCancelOrder

## Overview

Returns a callback(order) that cancels an order: if algo_order_id, dispatches to cancelTPSLChildOrder or cancelAlgoOrder depending on root type; otherwise cancelOrder(order_id, symbol). Uses useOrderStream (INCOMPLETE) and toast on failure.

## Returns

(order) => Promise (cancel request).
