# context.ts

## Overview

Defines the referral context: types for props and return value, `ReferralContext`, and `useReferralContext` hook. Used by `ReferralProvider` and consumed by affiliate/trader UI.

## Exports

### Enums

| Name | Values |
|------|--------|
| `TabTypes` | `affiliate`, `trader` |

### Types

| Name | Description |
|------|-------------|
| `UserVolumeType` | Optional `1d_volume`, `7d_volume`, `30d_volume`, `all_volume` |
| `BuildNode` | `(state: ReferralContextReturns) => ReactNode` |
| `Overwrite` | Optional overwrites for shortBrokerName, brokerName, ref (top, card, step) |
| `ChartConfig` | `affiliate` and `trader` configs with bar, yAxis, xAxis |
| `ReferralContextProps` | Props for provider: onBecomeAnAffiliate, becomeAnAffiliateUrl, bindReferralCodeState, learnAffiliateUrl, referralLinkUrl, showReferralPage, showDashboard, chartConfig, overwrite, splashPage, etc. |
| `ReferralContextReturns` | Context value: referralInfo, isAffiliate, isTrader, mutate, userVolume, dailyVolume, isLoading, showHome, setShowHome, tab, setTab, wrongNetwork, disabledConnect, generateCode, plus ReferralContextProps |

### Context and hook

| Name | Description |
|------|-------------|
| `ReferralContext` | React context for ReferralContextReturns |
| `useReferralContext` | Hook that returns `useContext(ReferralContext)` |

## Usage Example

```ts
import { useReferralContext, TabTypes } from "./context";
const { isAffiliate, tab, setTab } = useReferralContext();
setTab(TabTypes.trader);
```
