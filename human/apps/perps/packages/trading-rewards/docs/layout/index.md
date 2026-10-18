# layout

## Overview

Layout for the Trading Rewards area: scaffold with optional left sidebar, routing paths, and a widget that wires script state into the UI.

## Files

| File | Language | Description |
|------|----------|-------------|
| [context](context.md) | TSX | `LayoutProvider` and `LayoutContext` for sidebar open state. |
| [layout.script.tsx](layout.script.md) | TSX | `useTradingRewardsLayoutScript`, sidebar items, and `TradingRewardsLeftSidebarPath`. |
| [layout.ui.tsx](layout.ui.md) | TSX | `TradingRewardsLayout` component and `TradingRewardsLayoutProps`. |
| [layout.widget.tsx](layout.widget.md) | TSX | `TradingRewardsLayoutWidget` that composes script + layout. |

## Exports (from layout barrel)

- `TradingRewardsLayoutWidget`
- `TradingRewardsLayout`
- `useTradingRewardsLayoutScript`
- `TradingRewardsLeftSidebarPath` (enum)
