# VolBarChart

## Overview

Volume bar chart with rounded rects, optional per-cell opacity, custom tooltip (rm/dp), and CustomizedCross cursor. Uses ResponsiveContainer and BarChart; Y-axis formatter uses internal numberToHumanStyle when not empty.

## Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| data | ReadonlyArray&lt;VolChartDataItem&gt; \| VolChartDataItem[] | Yes | — | date, volume, optional opacity. |
| colors | { fill: string } | No | theme | Single fill color for bars (mapped to profit/loss). |
| tooltip | VolChartTooltip | No | — | rm/dp for PaxeerNetworkChartTooltip. |
| className | string | No | — | Applied to wrapper Box. |

### VolChartDataItem

| Field | Type | Description |
|-------|------|-------------|
| date | string | X-axis label. |
| volume | number | Bar value. |
| opacity | string \| number | Optional cell opacity. |

### VolChartTooltip

| Field | Type | Description |
|-------|------|-------------|
| rm | number | Optional rounding mode for tooltip. |
| dp | number | Optional decimal places. |

## Usage example

```tsx
import { VolBarChart } from "@paxeer-network/chart";

<VolBarChart data={[{ date: "2024-01-01", volume: 1000 }]} tooltip={{ dp: 2 }} />
```
