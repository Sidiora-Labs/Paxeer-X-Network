# Bar

## Overview

D3-based bar series for use inside Chart. Expects data with `date` and `pnl`. Registers scaleBand for x and scaleLinear for y (symmetric around zero), then uses d3-selection to draw rects with optional color.

## Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| color | string \| ((d: any, ctx: ChartContextState) => string) | No | "steelblue" | Fill for bars. |

## Usage example

```tsx
import { Chart } from "../chart";
import { Bar } from "./bar";
import { Axis } from "../common/axis";

<Chart data={pnlData} x="date" y="pnl">
  <Bar color={(d) => (d.pnl >= 0 ? "green" : "red")} />
  <Axis orientation="bottom" />
  <Axis orientation="left" />
</Chart>
```
