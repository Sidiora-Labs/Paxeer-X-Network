# Affiliate Package Documentation Index

## Directory Overview

`packages/affiliate/src` is the paxeer-network Affiliate and Trader front-end module. It provides referral links, rebates, multi-level referral, dashboard, and landing pages.

## Module Responsibility Summary

| Responsibility | Description |
|----------------|-------------|
| Referral context and state | `ReferralProvider` exposes referral info, identity (isAffiliate / isTrader), tab, and data refresh |
| Multi-level referral | Volume prerequisite, rebate rates, statistics, and referral code management |
| Pages and layout | Dashboard, Home, Affiliate, Trader, MultiLevel pages and shared layout |
| Data and API | Hooks for referral and multi-level referral APIs and SWR pagination |

## Key Entities

| Entity | Type | Responsibility | Entry |
|--------|------|----------------|-------|
| ReferralProvider | React Provider | Global referral state and API data | `provider/provider.tsx` |
| ReferralContext | Context | Referral context type and `useReferralContext` | `provider/context.ts` |
| AffiliateLayoutWidget | Component | Affiliate module layout | `layout/layout.widget.tsx` |
| useReferralCode | Hook | Create/edit referral code, update rebate rate | `hooks/useReferralCode.ts` |
| useMultiLevelReferralData | Hook | Multi-level eligibility and rebate info | `hooks/useMultiLevelReferralData.ts` |
| StatisticsTimeRange | enum | Time range for statistics (1d / 7d / 30d / All) | `types.ts` |
| ReferralCodeFormType | enum | Form type (Create / Edit / Reset) | `types.ts` |

## Subdirectories and Top-Level Files

### Subdirectories

| Directory | Description | Index |
|-----------|-------------|--------|
| [components](components/index.md) | Shared UI (AutoHideText, EditCode, PinButton, etc.) | [components/index.md](components/index.md) |
| [hooks](hooks/index.md) | Referral and multi-level referral API/data hooks | [hooks/index.md](hooks/index.md) |
| [icons](icons/index.md) | Icon components | [icons/index.md](icons/index.md) |
| [layout](layout/index.md) | Layout and sidebar context | [layout/index.md](layout/index.md) |
| [pages](pages/index.md) | Pages: dashboard, home, affiliate, trader, multiLevel | [pages/index.md](pages/index.md) |
| [provider](provider/index.md) | ReferralProvider and ReferralContext | [provider/index.md](provider/index.md) |
| [utils](utils/index.md) | Utils, types, SWR, chart helpers | [utils/index.md](utils/index.md) |

### Top-Level Code Files

| File | Language | Responsibility | Entry symbols | Link |
|------|----------|-----------------|---------------|------|
| index.ts | TypeScript | Package entry: re-exports Dashboard, multiLevel, provider, layout | Dashboard, layout, provider | [entry.md](entry.md) |
| types.ts | TypeScript | Statistics time range, referral code form enums | StatisticsTimeRange, ReferralCodeFormType, ReferralCodeFormField | [types.md](types.md) |
| version.ts | TypeScript | Package version and global `__PAXEER_NETWORK_VERSION__` | default export | [version.md](version.md) |

## Search Keywords

affiliate, referral, referrer, referee, rebate, multi-level, dashboard, trader, referral code, referral link, commission, volume prerequisite, chart config, overwrite
