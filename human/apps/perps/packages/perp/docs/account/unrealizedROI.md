# unrealizedROI.ts

## unrealizedROI.ts Responsibilities

Computes **Total Unrealized ROI**: totalUnrealizedPnL / (totalValue − totalUnrealizedPnL). Represents unrealized return relative to “realized” value (total value minus unrealized PnL).

## unrealizedROI.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| totalUnrealizedROI | function | Formula | totalUnrealizedPnL / (totalValue - totalUnrealizedPnL). |

## totalUnrealizedROI Parameter Table

| Name | Type | Required | Description |
|------|------|----------|-------------|
| totalUnrealizedPnL | number | Yes | Sum of unrealized PnL. |
| totalValue | number | Yes | From totalValue. |

## totalUnrealizedROI Dependencies

- **Upstream**: `@paxeer-network/utils` (Decimal).

## Example

```typescript
import { totalUnrealizedROI } from "@paxeer-network/perp";

const roi = totalUnrealizedROI({
  totalUnrealizedPnL: 200.53,
  totalValue: 2982.66,
});
// roi = 200.53 / (2982.66 - 200.53) ≈ 0.0721 (7.21%)
```
