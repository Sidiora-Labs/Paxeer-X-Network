# hooks – directory index

## Directory responsibility

Hooks for account-based filtering, accounts data aggregation, and portfolio total value. Used by overview, assets, and history UIs to filter by account and compute totals.

## Key entities

| Entity | File | Responsibility | Dependency |
|--------|------|----------------|------------|
| useAccountFilter | useAccountFilter.ts | Filter list by selected account (main/sub/ALL) | useAccount |
| useTransferHistoryAccountFilter | useAccountFilter.ts | Filter transfer history by from/to account | useAccount |
| createAccountFilter | useAccountFilter.ts | Factory for custom account filter functions | - |
| AccountFilters | useAccountFilter.ts | Pre-defined filter factories (standard, transferHistory, customAccountId) | - |
| useAssetsAccountFilter | useAssetsAccountFilter.ts | Account filter + selectedAccount state for assets | useAccount |
| useAssetsMultiFilter | useAssetsAccountFilter.ts | Account + asset (token) combined filter for assets | useAccount |
| useAccountsData | useAccountsData.ts | List of accounts with holdings for display | useAccount, useCollateral |
| useAssetTotalValue | useAssetTotalValue.ts | Total portfolio value (main + sub) | useAccount, useCollateral, useIndexPricesStream, useAccountsData |

## Files

| File | Language | Summary | Entry symbol(s) | Link |
|------|----------|---------|-----------------|------|
| index.ts | TypeScript | Re-exports all hooks | (re-exports) | [index.md](index.md) |
| useAccountFilter.ts | TypeScript | Account filter types, factory, and hooks | AccountFilterFunction, createAccountFilter, useAccountFilter, useTransferHistoryAccountFilter, AccountFilters | [useAccountFilter.md](useAccountFilter.md) |
| useAssetsAccountFilter.ts | TypeScript | Assets account filter and multi-filter hooks | useAssetsAccountFilter, useAssetsMultiFilter, filterByAccount | [useAssetsAccountFilter.md](useAssetsAccountFilter.md) |
| useAccountsData.ts | TypeScript | Account list with children (holdings) | AccountWithChildren, useAccountsData | [useAccountsData.md](useAccountsData.md) |
| useAssetTotalValue.ts | TypeScript | Total portfolio value calculation | calculateTotalHolding, useAssetTotalValue | [useAssetTotalValue.md](useAssetTotalValue.md) |

## Subdirectories

None.
