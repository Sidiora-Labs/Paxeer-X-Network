# useAssetsAccountFilter.ts

## useAssetsAccountFilter.ts responsibility

Provides hooks and a pure function to filter assets (or asset-like data) by account and optionally by token. Used by assets and overview UIs that show per-account holdings and need account/asset selectors.

## useAssetsAccountFilter.ts exports

| Name | Type | Role | Description |
|------|------|------|--------------|
| useAssetsAccountFilter | hook | Account filter + state | selectedAccount, filteredData, onAccountFilter, setSelectedAccount |
| useAssetsMultiFilter | hook | Account + asset filter | selectedAccount, selectedAsset, filteredData, onFilter, setters |
| filterByAccount | function | Pure filter | Filters array by account given accountState and selectedAccount |

## useAssetsAccountFilter input and output

- **Input**: `data: T[]` where `T` has optional `account_id`.
- **Output**: `{ selectedAccount, filteredData, onAccountFilter, setSelectedAccount }`. Filter logic follows main/sub and ALL/MAIN/sub id same as useAccountFilter.

## useAssetsAccountFilter return value

| Property | Type | Description |
|----------|------|-------------|
| selectedAccount | string | Current selection (e.g. AccountType.ALL) |
| filteredData | T[] | Data filtered by selectedAccount |
| onAccountFilter | (filter: { name: string; value: string }) => void | Call when filter changes; if name === "account", sets selectedAccount |
| setSelectedAccount | (v: string) => void | Direct setter |

## useAssetsMultiFilter input and output

- **Input**: `data: T[]` where `T` has optional `account_id` and optional `children` (array with `token`).
- **Output**: Applies account filter first, then filters by `selectedAsset` (token) on `children`; returns `{ selectedAccount, selectedAsset, filteredData, onFilter, setSelectedAccount, setSelectedAsset }`.

## useAssetsMultiFilter return value

| Property | Type | Description |
|----------|------|-------------|
| selectedAccount | string | Account filter value |
| selectedAsset | string | Token filter ("all" or token symbol) |
| filteredData | T[] | Data filtered by account then by asset (on children) |
| onFilter | (filter: { name: string; value: string }) => void | name "account" or "asset" updates corresponding state |
| setSelectedAccount | (v: string) => void | Direct setter |
| setSelectedAsset | (v: string) => void | Direct setter |

## filterByAccount parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| data | T[] | Yes | Items with optional account_id |
| selectedAccount | string | Yes | ALL / MAIN / sub id |
| accountState | { isMainAccount, state: { mainAccountId?, accountId? } } | Yes | Same shape as useAccount |

Returns: `T[]`.

## useAssetsAccountFilter dependency and call relationship

- **Upstream**: `useAccount` from `@paxeer-network/hooks`, `AccountType` from assets page.
- **Downstream**: Assets page, overview assets tables that show account/asset filters.

## useAssetsAccountFilter Example

```typescript
const {
  selectedAccount,
  filteredData,
  onAccountFilter,
  setSelectedAccount,
} = useAssetsAccountFilter(accountsWithHoldings);

// Multi (account + asset)
const {
  selectedAccount,
  selectedAsset,
  filteredData,
  onFilter,
} = useAssetsMultiFilter(accountsWithChildren);

// Pure function
const filtered = filterByAccount(data, selectedAccount, { isMainAccount, state });
```
