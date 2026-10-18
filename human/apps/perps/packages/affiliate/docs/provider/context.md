# provider/context.ts

## Responsibility of context.ts

Defines the referral context: types (TabTypes, UserVolumeType, BuildNode, Overwrite, ChartConfig, ReferralContextProps, ReferralContextReturns), the React context instance, and `useReferralContext` hook. No data fetching; that is in provider.tsx.

## Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| TabTypes | enum | Tab | affiliate \| trader |
| UserVolumeType | type | Volume | 1d_volume, 7d_volume, 30d_volume, all_volume |
| BuildNode | type | Render | (state: ReferralContextReturns) => ReactNode |
| Overwrite | type | UI override | shortBrokerName, brokerName, ref.{ top, card, step } as BuildNode |
| ChartConfig | type | Chart | affiliate/trader each: bar, yAxis, xAxis (any) |
| ReferralContextProps | type | Provider props | Callbacks and URLs (becomeAnAffiliate, learnAffiliate, referralLinkUrl, showDashboard, chartConfig, overwrite, splashPage, etc.) |
| ReferralContextReturns | type | Context value | referralInfo, isAffiliate, isTrader, mutate, userVolume, dailyVolume, isLoading, showHome, setShowHome, tab, setTab, wrongNetwork, disabledConnect, generateCode, plus ReferralContextProps and MultiLevelReferralData |
| ReferralContext | Context | React context | createContext<ReferralContextReturns> |
| useReferralContext | function | Hook | useContext(ReferralContext) |

## ReferralContextProps Main Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| onBecomeAnAffiliate | () => void | No | Override become-affiliate click |
| becomeAnAffiliateUrl | string | No | Default https://paxeer.network/ |
| bindReferralCodeState | (success, error, hide, queryParams) => void | No | Bind referral code callback |
| onLearnAffiliate | () => void | No | Override learn-affiliate click |
| learnAffiliateUrl | string | No | Learn affiliate URL |
| referralLinkUrl | string | Yes | Base URL for referral links |
| showReferralPage | () => void | No | Open referral index |
| showDashboard | () => void | No | Tab + content |
| chartConfig | ChartConfig | No | Bar/yAxis/xAxis for affiliate and trader |
| overwrite | Overwrite | No | Custom nodes for ref/trader |
| splashPage | () => ReactNode | No | Custom splash instead of referral page |

## Dependencies

- react (createContext, useContext)
- @paxeer-network/hooks (RefferalAPI)
- ../hooks/useMultiLevelReferralData (MultiLevelReferralData)

## provider/context.ts Example

```typescript
import { useReferralContext, TabTypes } from "@paxeer-network/affiliate";

const { isAffiliate, tab, setTab, referralInfo } = useReferralContext();
setTab(TabTypes.trader);
```
