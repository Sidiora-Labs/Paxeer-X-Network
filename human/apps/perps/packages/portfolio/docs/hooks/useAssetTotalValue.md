# useAssetTotalValue.ts

## useAssetTotalValue.ts responsibility

Computes the total portfolio value in USD: main account holdings plus all sub-account holdings when user is main account, or only the current sub-account when in sub-account context. Uses index prices from `useIndexPricesStream` and holding data from `useCollateral` and `useAccountsData`.

## useAssetTotalValue.ts exports

| Name | Type | Role | Description |
|------|------|------|--------------|
| calculateTotalHolding | function | Pure calculator | Sums holding * indexPrice for SubAccount[] or holding[]; returns Decimal |
| useAssetTotalValue | hook | Total value | Returns total portfolio value as number (main+sub or current sub only) |

## calculateTotalHolding parameters and return

- **Input**: `data: SubAccount[] | SubAccount["holding"]`, `getIndexPrice: (token: string) => number`.
- **Output**: `Decimal` (sum of holding * getIndexPrice(token) for each holding). Handles both nested (SubAccount with holding array) and flat holding items.

## useAssetTotalValue input and output

- **Input**: None (uses useAccount, useCollateral, useIndexPricesStream, useAccountsData).
- **Output**: `number` – total portfolio value. If main account: mainTotalValue + subTotalValue; else value of current sub-account's children only.

## useAssetTotalValue dependency and call relationship

- **Upstream**: useAccount, useCollateral, useIndexPricesStream from `@paxeer-network/hooks`; useAccountsData from same package; Decimal from `@paxeer-network/utils`.
- **Downstream**: Overview and assets UIs that display total portfolio value.

## useAssetTotalValue execution flow

1. Read holding (main), subAccounts, getIndexPrice, allAccounts (useAccountsData).
2. mainTotalValue = calculateTotalHolding(holding, getIndexPrice).
3. subTotalValue = calculateTotalHolding(subAccounts, getIndexPrice).
4. If isMainAccount: totalValue = mainTotalValue + subTotalValue; else find current account in allAccounts and sum its children with getIndexPrice.
5. Return totalValue.toNumber().

## useAssetTotalValue errors and edge cases

| Scenario | Condition | Behavior | Handling |
|----------|-----------|----------|----------|
| Missing index price | getIndexPrice(token) undefined/NaN | Treated as 0 in multiplication | Ensure index prices stream is ready |
| No holdings | Empty holding/children | Returns 0 | Normal for new accounts |

## useAssetTotalValue Example

```typescript
const totalValue = useAssetTotalValue();
// totalValue: number (USD value)

// Standalone calculator
import { calculateTotalHolding } from "./useAssetTotalValue";
const total = calculateTotalHolding(subAccounts, (token) => indexPrices[token] ?? 0);
```
