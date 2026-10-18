# orders.ts

## orders.ts responsibility

Provides order-related copy: order history, order status (pending, filled, canceled, rejected, etc.), toast titles for status changes, column labels (fill/quantity, order time, hidden), edit/cancel order dialogs and confirmations, cancel all (pending / TP-SL / by symbol), validation messages for price and quantity, and download tooltip.

## orders.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| orders | object | Key-value map | Keys under "orders.*", "order.edit.confirm.*" |
| Orders | type | typeof orders | Type export |

## orders.ts key groups (sample)

| Theme | Examples |
|-------|----------|
| Status | orders.status.pending, orders.status.filled, orders.status.canceled |
| Toasts | orders.status.opened.toast.title, orders.status.filled.toast.title |
| Actions | orders.editOrder, orders.cancelOrder, orders.cancelAll |
| Validation | orders.price.greaterThan, orders.quantity.lessThan |

## orders.ts Example

```typescript
t("orders.orderHistory");
t("orders.status.pending");
t("orders.cancelOrder.description");
```
