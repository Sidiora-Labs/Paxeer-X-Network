# pages – directory index

## Directory responsibility

All portfolio page modules: overview (assets, charts, performance, history, funding, distribution), assets (and convert), fee tier, API key management, setting, history, positions, and orders. Each area may expose a Page component, Widget(s), and scripts/hooks.

## Key entities

| Entity | Directory | Responsibility | Entry |
|--------|-----------|----------------|--------|
| OverviewPage | overview | Overview grid (assets, chart, performance, history) | main.tsx |
| AssetsPage / AssetsWidget | assets | Assets table and convert | page.tsx, assetsPage |
| FeeTierPage | feeTier | Fee tier display and script | page.tsx, feeTier.script |
| APIManagerPage / APIManagerWidget | api | API key CRUD UI | apiManager.page, apiManager.widget |
| SettingPage / SettingWidget | setting | Settings UI | setting.page, setting.widget |
| HistoryPage / HistoryWidget | history | History page | history.page |
| PositionsModule | positions | Positions page (re-export) | page.tsx |
| OrdersModule | orders | Orders page (re-export) | page.tsx |

## Subdirectories and links

| Directory | Responsibility | Index |
|-----------|----------------|--------|
| [api](api/index.md) | API key management page and dialogs | [api/index.md](api/index.md) |
| [assets](assets/index.md) | Assets page, table, convert page | [assets/index.md](assets/index.md) |
| [feeTier](feeTier/index.md) | Fee tier page and script | [feeTier/index.md](feeTier/index.md) |
| [history](history/index.md) | History page | [history/index.md](history/index.md) |
| [overview](overview/index.md) | Overview page, assets, charts, performance, funding, distribution, mobile | [overview/index.md](overview/index.md) |
| [positions](positions/index.md) | Positions page | [positions/index.md](positions/index.md) |
| [orders](orders/index.md) | Orders page | [orders/index.md](orders/index.md) |
| [setting](setting/index.md) | Setting page and widget | [setting/index.md](setting/index.md) |

## Search keywords

overview, assets, fee tier, API key, setting, history, positions, orders, OverviewPage, AssetsPage, FeeTierPage, APIManagerPage, SettingPage, HistoryPage.
