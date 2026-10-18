# Portfolio package – docs index

## Module responsibility

The `packages/portfolio/src` directory implements the **portfolio** UI and logic for the paxeer-network app: layout, overview, assets, fee tier, API key management, settings, positions, orders, and history. It re-exports layout and page modules for use by the host app.

## Key entities

| Entity | Type | Responsibility | Entry |
|--------|------|----------------|--------|
| PortfolioLayoutWidget | Component | Root layout with sidebar; desktop/mobile switch | `layout/layout.widget` |
| PortfolioLayout | Component | Desktop scaffold + left sidebar | `layout/layout.ui` |
| usePortfolioLayoutScript | Hook | Sidebar items, current path, layout state | `layout/layout.script` |
| OverviewModule | Namespace | Overview page (assets, charts, performance, history) | `pages/overview` |
| FeeTierModule | Namespace | Fee tier page | `pages/feeTier` |
| AssetsModule | Namespace | Assets page and convert | `pages/assets` |
| APIManagerModule | Namespace | API key management page | `pages/api` |
| SettingModule | Namespace | Settings page | `pages/setting` |
| HistoryModule | Namespace | History page | `pages/history` |
| useAccountFilter | Hook | Filter list by selected account | `hooks/useAccountFilter` |
| useAssetsAccountFilter | Hook | Assets account filter + selector state | `hooks/useAssetsAccountFilter` |
| useAccountsData | Hook | Account list with holdings for display | `hooks/useAccountsData` |
| useAssetTotalValue | Hook | Total portfolio value (main + sub-accounts) | `hooks/useAssetTotalValue` |

## Top-level exports (from `src/index.ts`)

- `layout`: PortfolioLayoutWidget, PortfolioLayout, usePortfolioLayoutScript, PortfolioLeftSidebarPath
- `OverviewModule`: overview page and sub-components
- `FeeTierModule`: fee tier page
- `PositionsModule`, `OrdersModule`: positions/orders pages (from shared pages)
- `APIManagerModule`: API manager page
- `SettingModule`: setting page
- `AssetsModule`: assets page and widgets
- `HistoryModule`: history page

## Directory structure and links

| Directory | Responsibility | Index |
|-----------|----------------|--------|
| [hooks](hooks/index.md) | Account filtering, accounts data, asset total value | [hooks/index.md](hooks/index.md) |
| [layout](layout/index.md) | Portfolio layout widget, sidebar, script, context | [layout/index.md](layout/index.md) |
| [pages](pages/index.md) | All portfolio pages (overview, assets, api, feeTier, setting, history, positions, orders) | [pages/index.md](pages/index.md) |

## Top-level files

| File | Language | Responsibility | Entry symbol(s) | Link |
|------|----------|-----------------|-----------------|------|
| index.ts | TypeScript | Package re-exports (layout, OverviewModule, etc.) | (re-exports) | See "Top-level exports" above |
| version.ts | TypeScript | Package version on window | `__PAXEER_NETWORK_VERSION__["@paxeer-network/portfolio"]` | [version.md](version.md) |

## Search keywords

portfolio, layout, overview, assets, fee tier, API key, setting, history, positions, orders, account filter, useAccountFilter, useAssetsAccountFilter, useAccountsData, useAssetTotalValue, PortfolioLayoutWidget, usePortfolioLayoutScript.
