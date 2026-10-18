# initialMargin.ts

## initialMargin.ts Responsibilities

Computes **total initial margin with orders** for **cross margin only**: sum over symbols of (position notional with orders × IMR). Filters positions and orders by margin mode (excludes ISOLATED). Used for free collateral and max qty inputs.

## initialMargin.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| totalInitialMarginWithQty | function | Formula | Sum of cross-margin initial margin per symbol. |

## totalInitialMarginWithQty Parameter Table

| Name | Type | Required | Description |
|------|------|----------|-------------|
| positions | API.Position[] | Yes | All positions (filtered to cross). |
| orders | API.Order[] | Yes | All orders (filtered to cross). |
| markPrices | Record<string, number> | Yes | Mark price by symbol. |
| symbolInfo | any | Yes | Accessor for base_imr etc. by symbol. |
| IMR_Factors | Record<string, number> | Yes | IMR factor by symbol. |
| maxLeverageBySymbol | Record<string, number> | No | Symbol leverage when no position. |

## totalInitialMarginWithQty Execution Flow

1. Filter positions/orders to cross margin only (margin_mode !== ISOLATED).
2. Build symbol set from positions and orders.
3. For each symbol: get position qty, buy/sell order qtys, mark price; compute positionQtyWithOrders and positionNotionalWithOrders; compute IMR; add notional × IMR to sum.
4. Return sum.

## Dependencies

- **Upstream**: `@paxeer-network/types` (API, OrderSide, MarginMode), `@paxeer-network/utils` (Decimal, zero), `../constants` (IMRFactorPower), positionNotional (positionQtyWithOrders_by_symbol, positionNotionalWithOrder_by_symbol), positionUtils (getQtyFromPositions, getQtyFromOrdersBySide).

## Example

```typescript
import { totalInitialMarginWithQty } from "@paxeer-network/perp";

const totalIM = totalInitialMarginWithQty({
  positions: crossPositions,
  orders: crossOrders,
  markPrices: { "BTC-PERP": 50000 },
  symbolInfo: getSymbolInfo,
  IMR_Factors: { "BTC-PERP": 0.0000002512 },
  maxLeverageBySymbol: { "BTC-PERP": 10 },
});
```
