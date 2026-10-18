# curEpoch.script

## Overview

Hook that builds current-epoch view state from `TradingRewardsContext`: epoch list, estimate, hide/connect flags, rewards tooltip (per-broker breakdown), status, and optional pause countdown.

## Exports

### useCurEpochScript

Returns:

| Field | Type | Description |
|-------|------|-------------|
| epochList | from context | Current epoch list and loading state. |
| estimate | CurrentEpochEstimate \| null \| undefined | Cur epoch estimate. |
| hideData | boolean | True when not connected / wrong network / disabled. |
| notConnected | boolean | True when not connected or disabled. |
| connect | function | Wallet connect from useWalletConnector. |
| rewardsTooltip | RewardsTooltipProps \| undefined | Broker name, cur/other rewards (undefined when hideData). |
| statusInfo | StatusInfo \| undefined | Epoch status. |
| showEpochPauseCountdown | boolean | Whether to show pause countdown. |

### CurEpochReturns (type)

`ReturnType<typeof useCurEpochScript>`.

### RewardsTooltipProps (from rewardsTooltip)

Used by script: `brokerName`, `curRewards`, `otherRewards`.

## Usage example

```tsx
const state = useCurEpochScript();
<CurEpoch {...state} />
```
