export const orders = {
  "orders.orderHistory": "Order history",

  "orders.status.pending": "Pending",
  "orders.status.filled": "Filled",
  "orders.status.partialFilled": "Partial filled",
  "orders.status.canceled": "Canceled",
  "orders.status.rejected": "Rejected",
  "orders.status.incomplete": "Incomplete",
  "orders.status.completed": "Completed",

  "orders.status.opened.toast.title": "Order opened",
  "orders.status.filled.toast.title": "Order filled",
  "orders.status.canceled.toast.title": "Order canceled",
  "orders.status.rejected.toast.title": "Order rejected",
  "orders.status.replaced.toast.title": "Order edited",

  "orders.status.scaledSubOrderOpened.toast.title":
    "Scale order: sub-order opened",
  "orders.trailingStop.activated": "Trailing order activated",

  "orders.column.fill&Quantity": "Filled / Quantity",
  "orders.column.orderTime": "Order time",
  "orders.column.hidden": "Hidden",

  "orders.editOrder": "Edit order",
  "order.edit.confirm.quantity":
    "You agree changing the quantity of {{base}}-PERP order to <0>{{value}}</0>.",
  "order.edit.confirm.price":
    "You agree changing the price of {{base}}-PERP order to <0>{{value}}</0>.",
  "order.edit.confirm.triggerPrice":
    "You agree changing the trigger price of {{base}}-PERP order to <0>{{value}}</0>.",
  "order.edit.confirm.callbackValue":
    "You agree changing the callback value of {{base}}-PERP order to <0>{{value}}</0>.",
  "order.edit.confirm.callbackRate":
    "You agree changing the callback rate of {{base}}-PERP order to <0>{{value}}</0>.",

  "orders.cancelOrder": "Cancel order",
  "orders.cancelOrder.description":
    "Are you sure you want to cancel your pending order.",

  "orders.cancelAll": "Cancel all",
  "orders.pending.cancelAll": "Cancel all pending orders",
  "orders.pending.cancelAll.description":
    "Are you sure you want to cancel all of your pending orders?",
  "orders.tpsl.cancelAll": "Cancel all TP/SL orders",
  "orders.tpsl.cancelAll.description":
    "Are you sure you want to cancel all of your TP/SL orders?",

  "orders.cancelAll.ofSymbol": "Cancel all of {{symbol}}",
  "orders.pending.cancelAll.forSymbol":
    "Cancel all pending orders for {{symbol}}",
  "orders.pending.cancelAll.forSymbol.description":
    "Are you sure you want to cancel all pending orders for {{symbol}}?",
  "orders.tpsl.cancelAll.forSymbol": "Cancel all TP/SL orders for {{symbol}}",
  "orders.tpsl.cancelAll.forSymbol.description":
    "Are you sure you want to cancel all TP/SL orders for {{symbol}}?",

  "orders.price.greaterThan": "Price can not be greater than {{max}} USDC.",
  "orders.price.lessThan": "Price can not be less than {{min}} USDC.",

  "orders.quantity.lessThan": "Quantity should be less than {{quantity}}",
  "orders.quantity.lessThanPosition":
    "Quantity should be less than position quantity : {{quantity}}",

  "orders.history.renew": "Renew",

  "orders.download.tooltip":
    "The downloaded data will reflect only the applied filters (e.g., status, time, side, and pagination) and may not include all records.",
};

export type Orders = typeof orders;
