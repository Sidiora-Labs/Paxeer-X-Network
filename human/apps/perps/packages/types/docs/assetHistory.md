# assetHistory

## Overview

Enums for asset history (deposit/withdraw) record status and side. Used when displaying or filtering asset history rows.

## Exports

### AssetHistoryStatusEnum

| Value | Description |
|-------|-------------|
| NEW | Deprecated, not used |
| PENDING | Pending |
| CONFIRM | Confirmed |
| PROCESSING | Processing |
| COMPLETED | Completed |
| FAILED | Failed |
| PENDING_REBALANCE | Pending rebalance |

### AssetHistorySideEnum

| Value | Description |
|-------|-------------|
| DEPOSIT | Deposit |
| WITHDRAW | Withdraw |

## Usage example

```typescript
import { AssetHistoryStatusEnum, AssetHistorySideEnum } from "@paxeer-network/types";
if (row.side === AssetHistorySideEnum.DEPOSIT && row.trans_status === AssetHistoryStatusEnum.COMPLETED) {
  // ...
}
```
