# main.tsx (OverviewPage)

## main.tsx responsibility

Overview page component: desktop renders a 2-column grid with lazy-loaded AssetWidget, AssetsChartWidget, PerformanceWidget, and HistoryDataGroupWidget; mobile renders MobileOverview. Wraps in OverviewProvider. Exports OverviewPage and OverviewPageProps (= MobileOverviewProps).

## main.tsx exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| OverviewPage | FC | Page component | Overview grid (desktop) or MobileOverview (mobile) |
| OverviewPageProps | type | Props | MobileOverviewProps (hideAffiliateCard?, hideTraderCard?) |

## OverviewPage Props

| Prop | Type | Required | Description |
|------|------|----------|-------------|
| hideAffiliateCard | boolean | No | Hide affiliate card on mobile |
| hideTraderCard | boolean | No | Hide trader card on mobile |

## OverviewPage dependency and rendering

- **Upstream**: Grid, useScreen from @paxeer-network/ui; MobileOverview, OverviewProvider; lazy imports for AssetWidget, AssetsChartWidget, PerformanceWidget, HistoryDataGroupWidget.
- **Downstream**: Router mounts OverviewPage under portfolio layout.

## OverviewPage rendering flow

1. OverviewProvider wraps content.
2. useScreen() → isMobile.
3. If isMobile: render MobileOverview with hideAffiliateCard, hideTraderCard.
4. Else: Grid cols={2}, two Suspense blocks (AssetWidget, AssetsChartWidget), then full-width PerformanceWidget and HistoryDataGroupWidget.

## OverviewPage Example

```tsx
import { OverviewModule } from "@paxeer-network/portfolio";

<OverviewModule.OverviewPage hideAffiliateCard={false} hideTraderCard={false} />
```
