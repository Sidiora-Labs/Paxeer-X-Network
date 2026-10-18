# overview – directory index

## Directory responsibility

Overview page and sub-modules: assets widget, asset chart, performance, history data group, funding history, distribution history, transfer history, vaults history, asset history, and mobile overview. Exposes OverviewPage, OverviewProvider, AssetWidget, AssetsUI, HistoryDataGroupWidget, PerformanceUI/PerformanceWidget, FundingHistoryWidget, DistributionHistoryWidget, etc.

## Key entities

| Entity | Location | Responsibility |
|--------|----------|----------------|
| OverviewPage | main.tsx | Page: grid (desktop) or MobileOverview (mobile) |
| OverviewProvider | provider/overviewProvider.tsx | Context for overview |
| AssetWidget / AssetsUI | assets/* | Overview assets block |
| AssetsChartWidget | assetChart/* | Chart widget |
| PerformanceWidget / PerformanceUI | performance/* | Performance block |
| HistoryDataGroupWidget | historyDataGroup/* | History group |
| FundingHistoryWidget | funding/* | Funding history |
| DistributionHistoryWidget | distribution/* | Distribution history |
| TransferHistory | TransferHistory/* | Transfer history |
| VaultsHistory | VaultsHistory/* | Vaults history |
| AssetHistory | assetHistory/* | Asset history |
| MobileOverview | mobile/* | Mobile overview layout |

## Subdirectories and links

| Directory | Responsibility | Index |
|-----------|----------------|--------|
| [assets](assets/index.md) | Overview assets widget, header, chart header | [assets/index.md](assets/index.md) |
| [assetChart](assetChart/index.md) | Assets chart widget and UI | [assetChart/index.md](assetChart/index.md) |
| [assetHistory](assetHistory/index.md) | Asset history widget and UI | [assetHistory/index.md](assetHistory/index.md) |
| [distribution](distribution/index.md) | Distribution history | [distribution/index.md](distribution/index.md) |
| [funding](funding/index.md) | Funding history | [funding/index.md](funding/index.md) |
| [helper](helper/index.md) | Date helper | [helper/index.md](helper/index.md) |
| [historyDataGroup](historyDataGroup/index.md) | History data group widget | [historyDataGroup/index.md](historyDataGroup/index.md) |
| [mobile](mobile/index.md) | Mobile overview, cards, handle | [mobile/index.md](mobile/index.md) |
| [performance](performance/index.md) | Performance widget and UI | [performance/index.md](performance/index.md) |
| [performanceMobileDialog](performanceMobileDialog/index.md) | Performance mobile dialog | [performanceMobileDialog/index.md](performanceMobileDialog/index.md) |
| [portfolioChartsMobile](portfolioChartsMobile/index.md) | Portfolio charts mobile | [portfolioChartsMobile/index.md](portfolioChartsMobile/index.md) |
| [provider](provider/index.md) | OverviewProvider, overviewContext | [provider/index.md](provider/index.md) |
| [shared](shared/index.md) | periodHeader, useAssetHistory | [shared/index.md](shared/index.md) |
| [TransferHistory](TransferHistory/index.md) | Transfer history | [TransferHistory/index.md](TransferHistory/index.md) |
| [VaultsHistory](VaultsHistory/index.md) | Vaults history | [VaultsHistory/index.md](VaultsHistory/index.md) |

## Top-level files

| File | Language | Link |
|------|----------|------|
| index.tsx | TSX | [index.md](index.md) |
| main.tsx | TSX | [main.md](main.md) |
