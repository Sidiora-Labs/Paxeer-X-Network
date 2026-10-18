# src/index.ts

## Overview

Main entry of the affiliate package. Re-exports the dashboard page module, provider, and layout. Also side-effect imports `./install` for extension setup.

## Other files in this directory

| Item | Description |
|------|-------------|
| [version.ts](version.md) | Package version and window.__PAXEER_NETWORK_VERSION__ |
| [install.tsx](install.md) | Side-effect extension setup |
| [layout](layout/index.md) | Affiliate layout and sidebar |
| [provider](provider/index.md) | Referral context and ReferralProvider |
| [utils](utils/index.md) | Types, date/format, SWR, decimal, chart, mock data |
| [components](components/index.md) | Icons and small UI components |
| [pages](pages/index.md) | home, dashboard, affiliate, trader pages |

## Exports

| Export | Description |
|--------|-------------|
| `Dashboard` | Namespace from `./pages/dashboard` |
| `*` from `./provider` | Referral context and provider |
| `*` from `./layout` | Layout widget (e.g. `AffiliateLayoutWidget`) |

## Usage Example

```ts
import { Dashboard, ReferralProvider, useReferralContext, AffiliateLayoutWidget } from "@paxeer-network/affiliate";
```
