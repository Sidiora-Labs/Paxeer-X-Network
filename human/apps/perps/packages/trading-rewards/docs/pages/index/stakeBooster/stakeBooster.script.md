# stakeBooster.script

## Overview

Provides current epoch estimate (via `useDataTap`), `stakeNow` handler that opens app staking URL, and computed `booster` value from `est_stake_boost` (formula: value / 10^0.15, 2 decimal places).

## Exports

### useStakeBoosterScript

Returns:

| Field | Type | Description |
|-------|------|-------------|
| curEpochEstimate | CurrentEpochEstimate \| undefined | Tapped estimate. |
| stakeNow | (e: any) => void | Opens staking page in new tab. |
| booster | string \| number \| undefined | Display booster (e.g. "1.25"). |

### StakeBoosterReturns (type)

`ReturnType<typeof useStakeBoosterScript>`.

## Usage example

```tsx
const { curEpochEstimate, stakeNow, booster } = useStakeBoosterScript();
<StakeBooster curEpochEstimate={curEpochEstimate} stakeNow={stakeNow} booster={booster} />
```
