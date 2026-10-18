# AssetAreaChart

## Overview

Asset value area chart with gradient fill and tooltip. Same data shape as AssetLineChart (AssetChartDataItem); exports PnlAreaChartProps and AssetChartDataItem.

## Props (PnlAreaChartProps)

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| data | AssetChartDataItem[] | Yes | — | date and account_value. |
| colors | { profit: string; loss: string } | No | theme | profit used for stroke/fill. |
| invisible | boolean | No | — | Hides tooltip and area. |
| responsiveContainerProps | Omit&lt;ResponsiveContainerProps, "children"&gt; | No | — | Passed to ResponsiveContainer. |

## Usage example

```tsx
import { AssetAreaChart } from "@paxeer-network/chart";

<AssetAreaChart data={accountValueData} />
```
