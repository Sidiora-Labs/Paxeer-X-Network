# index.ts (Package entry)

## Overview

Main package entry. Re-exports layout and all page modules as namespaces.

## Exports

- `./layout` — layout widget, layout component, script (sidebar paths)
- `OverviewModule` — `./pages/overview`
- `FeeTierModule` — `./pages/feeTier`
- `PositionsModule` — `./pages/positions/page`
- `OrdersModule` — `./pages/orders/page`
- `APIManagerModule` — `./pages/api`
- `SettingModule` — `./pages/setting`
- `AssetsModule` — `./pages/assets`
- `HistoryModule` — `./pages/history`

## Usage example

```ts
import {
  PortfolioLayoutWidget,
  OverviewModule,
  AssetsModule,
  SettingModule,
} from "@paxeer-network/portfolio";
```
