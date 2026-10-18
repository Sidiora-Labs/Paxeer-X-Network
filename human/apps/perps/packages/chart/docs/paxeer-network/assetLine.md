# AssetLineChart

## Overview

Asset value over time: line chart on desktop and area chart on mobile (useScreen). Uses account_value as dataKey and XAxisLabel; tooltip shows value without coloring. Exports PnlLineChartProps and AssetChartDataItem for typing.

## Props (PnlLineChartProps)

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| data | AssetChartDataItem[] | Yes | — | date and account_value. |
| colors | { profit: string; loss: string } | No | theme | profit used for line/area stroke. |
| invisible | boolean | No | — | Hides tooltip and line/area. |
| responsiveContainerProps | Omit&lt;ResponsiveContainerProps, "children"&gt; | No | — | Passed to ResponsiveContainer. |

### AssetChartDataItem

| Field | Type | Description |
|-------|------|-------------|
| date | string | X-axis label. |
| account_value | number | Y value. |

## Usage example

```tsx
import { AssetLineChart } from "@paxeer-network/chart";

<AssetLineChart data={[{ date: "2024-01-01", account_value: 1000 }]} />
```
