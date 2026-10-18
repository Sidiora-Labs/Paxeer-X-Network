# subAccount.ts

## subAccount.ts Responsibility

Defines the `SubAccount` type used by Account state: sub-account id, description, and holding (balance) list from the API.

## subAccount.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| SubAccount | type | Data shape | id, description, holding (API.Holding[]) |

## SubAccount Fields

| Field | Type | Description |
|-------|------|-------------|
| id | string | Sub-account ID |
| description | string | User-defined description |
| holding | API.Holding[] | Balance/holding list from paxeer-network API |

## subAccount.ts Dependencies and Call Relationships

- **Upstream**: @paxeer-network/types (API.Holding).
- **Downstream**: account.ts (AccountState.subAccounts, _restoreSubAccount, createSubAccount, updateSubAccount, switchAccount).

## subAccount.ts Example

```typescript
import type { SubAccount } from "@paxeer-network/core";

const sub: SubAccount = { id: "0", description: "Trading", holding: [] };
```
