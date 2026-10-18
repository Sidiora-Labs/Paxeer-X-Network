# leverage.ts

## leverage.ts Responsibilities

Computes **current account leverage** from total margin ratio: 1 / totalMarginRatio. Returns 0 if totalMarginRatio is 0.

## leverage.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| currentLeverage | function | Formula | 1 / totalMarginRatio. |

## currentLeverage Parameter Table

| Name | Type | Required | Description |
|------|------|----------|-------------|
| totalMarginRatio | number | Yes | From totalMarginRatio(). |

## Example

```typescript
import { currentLeverage } from "@paxeer-network/perp";

const leverage = currentLeverage(0.1); // 10
```
