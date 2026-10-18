# availableToClaim.script

## Overview

Computes remaining ORDER and esORDER rewards (lifetime/pending minus claimed) and provides a `goToClaim` handler that opens the app’s trading rewards URL. Uses `useDataTap` for display values.

## Exports

### AvailableReturns (type)

| Field | Type | Description |
|-------|------|-------------|
| order | number \| undefined | Remaining ORDER to claim. |
| esOrder | number \| undefined | Remaining esORDER to claim. |
| goToClaim | (e: any) => void | Opens app trading rewards page in new tab. |

### useAvailableScript

Uses context: totalOrderClaimedReward, totalEsOrderClaimedReward, walletRewardsHistory. Uses chain namespace to pick lifetime vs pending fields. Returns `AvailableReturns`.

## Usage example

```tsx
const { order, esOrder, goToClaim } = useAvailableScript();
<AvailableToClaim order={order} esOrder={esOrder} goToClaim={goToClaim} />
```
