# page.tsx — MarketsHomePage

## page.tsx Responsibilities

Exports `MarketsHomePage`, the main markets page component. It wraps content in `MarketsProvider`, renders a tabbed layout (Markets / Funding), and lazy-loads the markets header, markets data list, and funding widgets. Desktop and mobile layouts differ (Box + Tabs vs Tabs with leading nav).

## page.tsx Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| MarketsHomePageProps | type | Props | Extends MarketsProviderProps; adds className |
| MarketsHomePage | FC | Page component | Root markets page |

## MarketsHomePage Props

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| symbol | string | No | Current symbol (passed to MarketsProvider) |
| onSymbolChange | (symbol: API.Symbol) => void | No | Symbol change callback |
| navProps | object | No | Mobile: logo, routerAdapter, leftNav |
| comparisonProps | object | No | exchangesName, exchangesIconSrc |
| className | string | No | Root div class |

## MarketsHomePage Input/Output

- **Input**: MarketsHomePageProps (symbol, onSymbolChange, navProps, comparisonProps, className).
- **Output**: Renders MarketsProvider and a tabbed layout; Markets tab shows MarketsHeaderWidget + MarketsDataListWidget; Funding tab shows FundingWidget. Uses useScreen() to switch between desktop and mobile content.

## Render and State Flow

1. useScreen() determines isMobile.
2. activeTab state (MarketsPageTab) drives which tab is visible.
3. MarketsProvider receives symbol, onSymbolChange, navProps, comparisonProps.
4. Desktop: Box + Tabs with TabPanel for Markets and Funding; each panel uses React.Suspense around lazy widgets.
5. Mobile: Tabs with leading LeftNavUI when navProps.leftNav is set; same lazy widgets in each tab.

## Dependencies

- **Upstream**: React, @paxeer-network/i18n (useTranslation), @paxeer-network/types (RouterAdapter), @paxeer-network/ui (Box, cn, TabPanel, Tabs, useScreen), @paxeer-network/ui-scaffold (LeftNavProps, LeftNavUI), MarketsProvider from components, MarketsPageTab from type.
- **Downstream**: Apps that render the markets route (e.g. next or react-router).

## page.tsx Example

```tsx
import { MarketsHomePage } from "@paxeer-network/markets";

<MarketsHomePage
  symbol={symbol}
  onSymbolChange={setSymbol}
  className="oui-min-h-screen"
/>
```
