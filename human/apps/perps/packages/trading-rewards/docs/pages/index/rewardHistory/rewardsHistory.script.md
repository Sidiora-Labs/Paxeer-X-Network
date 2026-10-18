# rewardsHistory.script

## Overview

Builds the reward history list by merging epoch info with wallet rewards and account rewards history. Computes claim state (Claimed / Claimable / Processing / Null) and optional `rewardsTooltip` per row. Uses pagination from `@paxeer-network/ui`.

## Exports

### ListType (type)

Extends `EpochInfoItem` with:

| Field | Type | Description |
|-------|------|-------------|
| info | WalletRewardsItem \| undefined | Wallet reward row for this epoch. |
| state | string \| undefined | "Claimed" \| "Claimable" \| "Processing" \| "Null". |
| rewardsTooltip | RewardsTooltipProps \| undefined | Broker breakdown for tooltip. |

### RewardsHistoryReturns (type)

`ReturnType<typeof useRewardsHistoryScript>`.

### useRewardsHistoryScript

Returns:

| Field | Type | Description |
|-------|------|-------------|
| data | ListType[] | Filtered list (ended epochs only), sorted desc by epoch_id. |
| originalData | ListType[] | Same as data (for ListView/DataTable). |
| pagination | from usePagination | Pagination state. |
| isLoading | boolean | From epochList loading. |

## Usage example

```tsx
const { data, pagination, isLoading } = useRewardsHistoryScript();
<RewardHistory data={data} originalData={data} pagination={pagination} isLoading={isLoading} />
```
