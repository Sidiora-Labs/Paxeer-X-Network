# useAccountsData.ts

## useAccountsData.ts responsibility

Transforms raw account and collateral data into a display-oriented list: each account has `account_id` and `children` (holdings with `account_id` attached). Main account is included at the top when user is main account. Used by assets and overview to render account-grouped holdings.

## useAccountsData.ts exports

| Name | Type | Role | Description |
|------|------|------|--------------|
| AccountWithChildren | interface | Data shape | account_id, id?, description?, children (holdings with account_id) |
| useAccountsData | hook | Data transformer | Returns AccountWithChildren[] from useAccount + useCollateral |

## AccountWithChildren fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| account_id | string | Yes | Account id |
| id | string | No | Same as account_id (from sub account id) |
| description | string | No | e.g. "Main account" from i18n |
| children | Array<API.Holding & { account_id: string }> | Yes | Holdings for this account; empty uses single USDC 0 holding |

## useAccountsData input and output

- **Input**: None (reads from useAccount and useCollateral).
- **Output**: `AccountWithChildren[]`. Main account first (if isMainAccount), then sub-accounts; each sub's `holding` array is mapped to `children` with `account_id` set.

## useAccountsData dependency and call relationship

- **Upstream**: `useAccount`, `useCollateral` from `@paxeer-network/hooks`; `useTranslation` from `@paxeer-network/i18n`; `API.Holding` from `@paxeer-network/types`.
- **Downstream**: useAssetTotalValue, assets/overview components that display accounts and holdings.

## useAccountsData implementation notes

- Uses `produce` (immer) on `subAccounts` to build draft; adds main account with `t("common.mainAccount")` and main account holdings from `holding`.
- Removes original `holding` from sub account objects to avoid confusion.

## useAccountsData Example

```typescript
const accounts = useAccountsData();
// accounts[0].account_id, accounts[0].description, accounts[0].children (holdings)
accounts.forEach((acc) => {
  acc.children.forEach((h) => {
    // h.token, h.holding, h.frozen, h.account_id
  });
});
```
