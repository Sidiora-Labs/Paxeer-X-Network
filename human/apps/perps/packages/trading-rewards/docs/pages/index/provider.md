# provider

## Overview

Provides global state for the Trading Rewards page: broker info, epoch list, claimed rewards, curEpoch estimate, wallet rewards history, title config, and status. Consumes hooks from `@paxeer-network/hooks` and exposes everything via `TradingRewardsContext`.

## Exports

### TradingRewardsState (type)

| Field | Type | Description |
|-------|------|-------------|
| type | TWType | normal / mm. |
| brokerId | string | From config. |
| brokerName | string \| undefined | Resolved from brokers. |
| brokers | Brokers \| undefined | From useAllBrokers. |
| epochList | EpochInfoType | From useEpochInfo. |
| totalOrderClaimedReward | [number \| undefined, { refresh }] | From useGetClaimed (order). |
| totalEsOrderClaimedReward | [number \| undefined, { refresh }] | From useGetClaimed (esOrder). |
| curEpochEstimate | CurrentEpochEstimate \| undefined | From useCurEpochEstimate. |
| walletRewardsHistory | WalletRewardsHistoryReturns | From useWalletRewardsHistory. |
| titleConfig | TitleConfig | Title and doc link config. |
| statusInfo | StatusInfo \| undefined | From useTradingRewardsStatus. |
| showEpochPauseCountdown | boolean | Whether to show pause countdown. |

### TradingRewardsProvider (component)

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| type | TWType | No | TWType.normal | normal or mm. |
| titleConfig | TitleConfig | No | doc link to trading-rewards docs | Title and doc open options. |
| showEpochPauseCountdown | boolean | No | false | Show countdown when epoch paused. |
| children | ReactNode | Yes | — | Child tree. |

### useTradingRewardsContext

Hook that returns `TradingRewardsState` from context.

## Usage example

```tsx
import { TradingRewardsProvider, useTradingRewardsContext } from "./provider";

<TradingRewardsProvider type={TWType.normal} titleConfig={{ ... }}>
  <YourContent />
</TradingRewardsProvider>

// In child:
const { epochList, curEpochEstimate } = useTradingRewardsContext();
```
