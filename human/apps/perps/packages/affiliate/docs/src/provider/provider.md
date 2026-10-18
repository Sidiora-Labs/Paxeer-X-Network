# provider.tsx

## Overview

`ReferralProvider` wraps the app (or affiliate section) and provides referral state: referral info from API, generate code, daily volume, volume stats, tab state, and callbacks. Uses `ReferralContext.Provider` and hooks from `@paxeer-network/hooks` and `@paxeer-network/react-app`.

## Props

| Name | Type | Required | Description |
|------|------|----------|-------------|
| (extends ReferralContextProps) | | | becomeAnAffiliateUrl, learnAffiliateUrl, referralLinkUrl, chartConfig, overwrite, onBecomeAnAffiliate, bindReferralCodeState, onLearnAffiliate, showReferralPage, showDashboard, splashPage, etc. |
| children | ReactNode | Yes | App/content to wrap |

## Usage Example

```tsx
import { ReferralProvider } from "./provider";
<ReferralProvider referralLinkUrl="https://paxeer.network/">
  <App />
</ReferralProvider>
```
