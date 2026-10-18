# useTransactionTime.ts

> Location: `packages/ui-transfer/src/contract/useTransactionTime.ts`

## Overview

React hook that returns the estimated transaction confirmation time (in seconds) for a given chain: `blockTime * confirmations`. Caches block time and confirmations per chain.

## Exports

### useTransactionTime(chainId?: number | string): number

| Parameter | Type             | Description                                         |
| --------- | ---------------- | --------------------------------------------------- |
| chainId   | number \| string | Optional chain ID; uses `useChains().findByChainId` |

**Returns:** Estimated time in seconds (0 if chain not ready or fetch failed).

## Usage example

```tsx
const transactionTime = useTransactionTime(chainId);
// e.g. show "Estimated: ~{transactionTime}s"
```
