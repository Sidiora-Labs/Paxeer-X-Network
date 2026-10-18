# XAxisLabel

## Overview

Custom X-axis tick component for Recharts. Renders the first tick’s payload value and the last tick as translated "Now" (chart.now). Used by PnL/Asset charts for consistent time-axis labels.

## Props (from Recharts tick)

| Prop | Type | Description |
|------|------|-------------|
| x, y | number | Position. |
| payload | { value: string } | Tick value. |
| index | number | Tick index (0 = first, last = "Now"). |

## Usage example

```tsx
<XAxis dataKey="date" tick={<XAxisLabel />} />
```
