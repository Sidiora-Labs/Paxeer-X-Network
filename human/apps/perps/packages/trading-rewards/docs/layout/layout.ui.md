# layout.ui

## Overview

Presentational layout component that wraps `Scaffold` from `@paxeer-network/ui-scaffold` with Trading Rewards–specific classNames and styling. Renders children inside the scaffold; left sidebar is passed as `null` (sidebar content is typically provided by the host app).

## Exports

### TradingRewardsLayoutProps (type)

Extends `ScaffoldProps` with:

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| hideSideBar | boolean | No | Hides left sidebar when true. |
| items | SideBarProps["items"] | No | Sidebar items (from script). |

### TradingRewardsLayout (component)

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| children | ReactNode | Yes | Page content. |
| classNames | ScaffoldProps["classNames"] | No | Override scaffold classNames. |
| ...rest | ScaffoldProps | No | Other scaffold props (e.g. routerAdapter). |

## Usage example

```tsx
import { TradingRewardsLayout } from "./layout.ui";

<TradingRewardsLayout routerAdapter={routerAdapter} hideSideBar={hideSideBar}>
  <TradingRewards.HomePage />
</TradingRewardsLayout>
```
