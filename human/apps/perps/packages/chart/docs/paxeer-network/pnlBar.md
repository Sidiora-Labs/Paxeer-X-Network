# PnLBarChart

## Overview

Recharts-based PnL bar chart. Renders rounded bars (profit/loss colored), CartesianGrid, ReferenceLine at y=0, custom tooltip and cross cursor. Uses ResponsiveContainer; supports invisible mode and custom responsiveContainerProps.

## Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| data | ReadonlyArray&lt;PnLChartDataItem&gt; \| PnLChartDataItem[] | Yes | — | Items with date and pnl. |
| colors | { profit: string; loss: string } | No | theme | Override profit/loss colors. |
| invisible | boolean | No | — | If true, hides tooltip and bars (chart-invisible class). |
| responsiveContainerProps | Omit&lt;ResponsiveContainerProps, "children"&gt; | No | — | Passed to ResponsiveContainer. |

### PnLChartDataItem

| Field | Type | Description |
|-------|------|-------------|
| date | string | X-axis label (e.g. date). |
| pnl | number | Bar value (positive = profit, negative = loss). |

## Usage example

```tsx
import { PnLBarChart } from "@paxeer-network/chart";

<PnLBarChart
  data={[
    { date: "2024-01-01", pnl: 100 },
    { date: "2024-01-02", pnl: -50 },
  ]}
  colors={{ profit: "#00B49E", loss: "#FF6B6B" }}
/>
```
