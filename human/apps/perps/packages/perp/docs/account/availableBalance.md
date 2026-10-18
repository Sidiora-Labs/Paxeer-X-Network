# availableBalance.ts

## availableBalance.ts Responsibilities

**availableBalance**: USDC holding + unsettlement PnL (simple sum). **availableBalanceForIsolatedMargin**: max(0, min(USDCHolding, freeCollateral − max(totalCrossUnsettledPnL, 0))). Used to determine how much USDC can be used for isolated margin (considering cross-margin free collateral and cross unsettled PnL).

## availableBalance.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| availableBalance | function | Formula | USDCHolding + unsettlementPnL. |
| availableBalanceForIsolatedMargin | function | Formula | max(0, min(USDC, freeCollateral - max(crossUpnl, 0))). |

## availableBalance Parameter Table

| Name | Type | Required | Description |
|------|------|----------|-------------|
| USDCHolding | number | Yes | USDC balance. |
| unsettlementPnL | number | Yes | Total unsettled PnL. |

## availableBalanceForIsolatedMargin Parameter Table

| Name | Type | Required | Description |
|------|------|----------|-------------|
| USDCHolding | number | Yes | USDC balance. |
| totalCrossUnsettledPnL | number | Yes | Cross margin unsettled PnL. |
| freeCollateral | number | Yes | From freeCollateral(). |

## Dependencies

- **Upstream**: `@paxeer-network/utils` (Decimal).

## Example

```typescript
import { availableBalance, availableBalanceForIsolatedMargin } from "@paxeer-network/perp";

const balance = availableBalance({ USDCHolding: 2000, unsettlementPnL: -18.34 });
const isoBalance = availableBalanceForIsolatedMargin({
  USDCHolding: 500,
  totalCrossUnsettledPnL: 100,
  freeCollateral: 300,
});
```
