# page

## Overview

Composes the Trading Rewards home page: `TradingRewardsProvider`, lazy-loaded title, curEpoch, availableToClaim, stakeBooster (conditional on epoch active), and reward history. Uses `useTradingRewardsStatus` and `EpochStatus` to show stake booster only when the epoch is active.

## Exports

### HomePage (component)

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| titleConfig | TitleConfig | No | — | Passed to provider. |
| className | string | No | — | Root Flex className. |
| showEpochPauseCountdown | boolean | No | — | Passed to provider for pause countdown. |

## Usage example

```tsx
import { TradingRewards } from "@paxeer-network/trading-rewards";

<TradingRewards.HomePage
  titleConfig={{ title: "Trading Rewards", docOpenOptions: { url: "...", target: "_blank" } }}
  showEpochPauseCountdown
/>
```
