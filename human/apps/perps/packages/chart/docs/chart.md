# Chart

## Overview

Root chart container that provides layout (margin, size) and scale registration via `ChartContext`. Uses a wrapper div with `ResizeObserver` to derive width/height when not passed as props, then renders `ChartRenderer` with context so children (e.g. Axis, Line, Bar) can consume scale and dimensions.

## Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| margin | Margin | No | DeafultMargin | Chart margins (top, right, bottom, left). |
| data | T[] | Yes | — | Data array for the chart. |
| width | number | No | — | Fixed width; if omitted, size comes from ResizeObserver. |
| height | number | No | — | Fixed height; if omitted, size comes from ResizeObserver. |
| x | string \| ((d: any) => any) | Yes* | — | X field name or accessor (from ChartProps). |
| y | string \| ((d: any) => any) | Yes* | — | Y field name or accessor (from ChartProps). |
| id | string | No | — | Optional chart id. |
| loading | boolean | No | — | Optional loading flag. |
| children | ReactNode | No | — | Rendered inside ChartRenderer. |

*Required by ChartProps; typically used by children that call useChartState with x/y.

## Usage example

```tsx
import { Chart } from "./chart";
import { Axis } from "./common/axis";
import { Line } from "./line/line";

<Chart data={data} x="date" y="value" margin={{ top: 20, right: 12, bottom: 20, left: 30 }}>
  <Line dataKey="value" color="#00B49E" />
  <Axis orientation="bottom" />
  <Axis orientation="left" />
</Chart>
```
