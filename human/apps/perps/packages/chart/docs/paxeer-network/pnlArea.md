# PnlAreaChart

## Overview

Cumulative PnL area chart. Same data transformation as PnlLineChart; renders AreaChart with Area using a linear gradient fill and baseValue set to minimum pnl. Supports invisible mode and ResponsiveContainer props.

## Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| data | any[] | Yes | — | Items with date and pnl (cumulated internally). |
| colors | { profit: string; loss: string } | No | theme | Used for stroke/fill. |
| invisible | boolean | No | — | Hides tooltip and area. |
| responsiveContainerProps | Omit&lt;ResponsiveContainerProps, "children"&gt; | No | — | Passed to ResponsiveContainer. |

## Usage example

```tsx
import { PnlAreaChart } from "@paxeer-network/chart";

<PnlAreaChart data={cumulativePnlData} />
```
