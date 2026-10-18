# useAccountFilter.ts

## useAccountFilter.ts responsibility

Provides types and utilities to filter lists by the currently selected account (main account, sub-account, or "ALL"). Includes a generic factory `createAccountFilter`, hooks `useAccountFilter` and `useTransferHistoryAccountFilter`, and pre-defined `AccountFilters` for standard and transfer-history use cases.

## useAccountFilter.ts exports

| Name | Type | Role | Description |
|------|------|------|--------------|
| AccountFilterFunction | type | Filter function type | `(data, selectedAccount, accountState) => T[]` |
| createAccountFilter | function | Factory | Returns an account filter function; optional `getAccountId(item)` for custom key |
| useAccountFilter | hook | Filter by account_id | Filters `data` by `selectedAccount` using `useAccount` |
| useTransferHistoryAccountFilter | hook | Filter transfer history | Filters by `from_account_id` / `to_account_id` |
| AccountFilters | object | Pre-defined factories | `standard`, `transferHistory`, `customAccountId` |

## AccountFilterFunction input and output

- **Input**: `data: T[]`, `selectedAccount: string`, `accountState: ReturnType<typeof useAccount>`.
- **Output**: Filtered array `T[]` (items matching the selected account or ALL).

## createAccountFilter parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| getAccountId | `(item: T) => string \| undefined` | No | Extracts account id from item; default uses `item.account_id` |

Returns: `AccountFilterFunction<T>`.

## useAccountFilter parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| data | `T[]` | Yes | List of items with optional `account_id` |
| selectedAccount | string | Yes | `AccountType.ALL`, `AccountType.MAIN`, or sub-account id |
| getAccountId | `(item: T) => string \| undefined` | No | Custom account id getter |

Returns: `T[]` (memoized).

## useTransferHistoryAccountFilter parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| data | `T[]` (from_account_id, to_account_id) | Yes | Transfer history items |
| selectedAccount | string | Yes | Same as useAccountFilter |

Returns: `T[]` (memoized). Item kept if selected account is main and (from or to matches), or if sub-account and (from or to equals current account).

## useAccountFilter dependency and call relationship

- **Upstream**: `useAccount` from `@paxeer-network/hooks`; `AccountType` from `../pages/assets/assetsPage/assets.ui.desktop`.
- **Downstream**: Used by overview, assets, and transfer history components that need account-filtered lists.

## useAccountFilter execution flow

1. `useAccount()` provides `state` (mainAccountId, accountId, subAccounts) and `isMainAccount`.
2. If not main account: keep only items where account id equals `state.accountId`.
3. If main account: if selectedAccount is empty or ALL, keep all; if MAIN, keep where account id equals mainAccountId; else keep where account id equals selectedAccount.
4. Result is memoized on `[data, selectedAccount, accountState, getAccountId]` (or equivalent for transfer filter).

## useAccountFilter errors and edge cases

| Scenario | Condition | Behavior | Handling |
|----------|-----------|----------|----------|
| No account state | useAccount not ready | Filter still runs with current state | UI may show empty until account loads |
| selectedAccount unknown | Value not MAIN/ALL or sub id | Treated as sub-account id; exact match | Pass valid AccountType or id from sidebar |

## useAccountFilter Example

```typescript
import { useAccountFilter, createAccountFilter, AccountFilters } from "./hooks";

// Hook: filter list by selected account
const filtered = useAccountFilter(myList, selectedAccount);

// Factory for use outside React
const filterFn = createAccountFilter<{ account_id?: string }>();
const filteredList = filterFn(data, selectedAccount, accountState);

// Transfer history
const transferFiltered = useTransferHistoryAccountFilter(transfers, selectedAccount);

// Pre-defined
const std = AccountFilters.standard<MyItem>();
const byCustomKey = AccountFilters.customAccountId("myAccountId");
```
