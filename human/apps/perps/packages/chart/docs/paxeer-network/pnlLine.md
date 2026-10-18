# PnlLineChart

## Overview

Cumulative PnL line chart built with Recharts. Transforms data to cumulative series (pnl as running sum, _pnl as daily value), then renders LineChart with XAxis (XAxisLabel), YAxis (tickFormatter), CartesianGrid, Tooltip, and Line. Supports invisible mode and ResponsiveContainer props.

## Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| data | any[] | Yes | — | Items with date and pnl (will be cumulated). |
| colors | { profit: string; loss: string } | No | theme | Used for line stroke. |
| invisible | boolean | No | — | Hides tooltip and line; adds chart-invisible class. |
| responsiveContainerProps | Omit&lt;ResponsiveContainerProps, "children"&gt; | No | — | Passed to ResponsiveContainer. |

## Usage example

```tsx
import { PnlLineChart } from "@paxeer-network/chart";

<PnlLineChart data={[{ date: "2024-01-01", pnl: 10 }, { date: "2024-01-02", pnl: -5 }]} />
```
