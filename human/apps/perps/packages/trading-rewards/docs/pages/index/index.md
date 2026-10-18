# pages/index

## Overview

Trading Rewards home page: provider context, lazy-loaded widgets (title, curEpoch, availableToClaim, stakeBooster, rewardHistory), and the main `HomePage` composition. Stake booster is shown only when epoch status is active.

## Files

| File | Language | Description |
|------|----------|-------------|
| [page](page.md) | TSX | `HomePage` component and lazy widget composition. |
| [provider](provider.md) | TSX | `TradingRewardsProvider`, `TradingRewardsContext`, `useTradingRewardsContext`, and state type. |
| [curEpoch/](curEpoch/index.md) | — | Current epoch widget, UI, script, and tooltip. |
| [title/](title/index.md) | — | Title widget, UI, and config. |
| [availableToClaim/](availableToClaim/index.md) | — | Available to claim widget and script. |
| [stakeBooster/](stakeBooster/index.md) | — | Stake booster widget and script. |
| [rewardHistory/](rewardHistory/index.md) | — | Reward history widget, UI, and script. |
| [components/](components/index.md) | — | Icons: PaxeerNetworkIcon, EsPaxeerNetworkIcon, JumpIcon, RocketIcon. |
