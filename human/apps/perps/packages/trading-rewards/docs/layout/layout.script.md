# layout.script

## Overview

Hook and enum for the Trading Rewards layout: sidebar navigation items (Trading / Affiliate), current path, and responsive hide behavior. Integrates with `@paxeer-network/ui-scaffold` sidebar contract.

## Exports

### TradingRewardsLeftSidebarPath (enum)

| Member | Value | Description |
|--------|-------|-------------|
| Trading | `/rewards/trading` | Trading rewards route. |
| Affiliate | `/rewards/affiliate` | Affiliate rewards route. |

### useTradingRewardsLayoutScript

Returns sidebar props plus `hideSideBar` for the layout.

#### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| props | object | No | Optional config. |
| props.current | string | No | Current path; defaults to `/rewards/affiliate`. |

#### Returns

`SideBarProps` from `@paxeer-network/ui-scaffold` plus:

| Field | Type | Description |
|-------|------|-------------|
| hideSideBar | boolean | True when viewport is ≤768px (sidebar hidden). |
| items | array | Sidebar items (Trading, Affiliate) with name, href, icon. |
| current | string | Current path. |
| onItemSelect | (item) => void | Sets current path on item click. |

## Usage example

```tsx
import { useTradingRewardsLayoutScript, TradingRewardsLeftSidebarPath } from "./layout.script";

const state = useTradingRewardsLayoutScript({ current: "/rewards/trading" });
// state.items, state.current, state.hideSideBar, state.onItemSelect
```
