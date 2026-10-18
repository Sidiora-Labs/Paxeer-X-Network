# stakeBooster.ui

## Overview

UI for stake booster: “Stake” link and two stat cards (average staked amount with ORDER/esORDER icon, and booster with rocket icon and “x” suffix).

## Exports

### StakeBooster (component)

Accepts `StakeBoosterReturns`: curEpochEstimate, stakeNow, booster. Uses `JumpIcon`, `PaxeerNetworkIcon`, `EsPaxeerNetworkIcon`, `RocketIcon`.

## Usage example

```tsx
<StakeBooster curEpochEstimate={est} stakeNow={stakeNow} booster="1.25" />
```
