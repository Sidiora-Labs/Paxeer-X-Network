# provider/provider.tsx

## Responsibility of ReferralProvider

Provides referral context: fetches `/v1/referral/info`, auto_referral progress, daily volume, user volume stats, and multi-level data (volume prerequisite, max rebate rate, rebate info). Computes isAffiliate / isTrader, userVolume, tab state, and a single mutate that revalidates all. Persists tab to localStorage and reads ref from URL into localStorage.

## ReferralProvider Props

Same as ReferralContextProps: onBecomeAnAffiliate, becomeAnAffiliateUrl, bindReferralCodeState, onLearnAffiliate, learnAffiliateUrl, referralLinkUrl, showReferralPage, showDashboard, chartConfig, overwrite, splashPage, children.

## ReferralProvider Output (Context Value)

- referralInfo, generateCode, isAffiliate, isTrader, mutate, userVolume, dailyVolume, isLoading, showHome, setShowHome, tab, setTab, wrongNetwork, disabledConnect
- Plus all ReferralContextProps and MultiLevelReferralData (volumePrerequisite, multiLevelRebateInfo, isMultiLevelEnabled, isMultiLevelReferralUnlocked, multiLevelRebateInfoMutate, maxRebateRate)

## Dependencies

- react, date-fns
- @paxeer-network/hooks (RefferalAPI, usePrivateQuery, useDaily, useAccount, useMemoizedFn, noCacheConfig)
- @paxeer-network/react-app (useAppContext)
- @paxeer-network/types (AccountStatusEnum)
- useMultiLevelReferralData, ReferralContext and types from ./context

## Execution Flow

1. Read props and useAccount state.
2. Fetch referral info, auto_referral progress, daily volume, user volume stats; run useMultiLevelReferralData.
3. Derive isAffiliate (has referral_codes), isTrader (has referer_code), userVolume from dailyVolume + volumeStatistics.
4. On account status change, call mutate after 1s delay.
5. Persist ref from URL to localStorage; tab from localStorage.
6. Memoize context value and render ReferralContext.Provider.

## ReferralProvider Example

```tsx
import { ReferralProvider } from "@paxeer-network/affiliate";

<ReferralProvider referralLinkUrl="https://app.example.com">
  <App />
</ReferralProvider>
```
