# Line

## Overview

D3-based line series component for use inside Chart. Registers x (scaleUtc) and y (scaleLinear) with context, optionally applies dataTransform, then draws a single path using d3-shape line with curveBasis.

## Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| dataKey | string | Yes | — | Key in data for y values. |
| color | string | No | "white" | Stroke color. |
| symbol | string | No | — | Unused (reserved). |
| dataTransform | (data: any) => any | No | — | Optional transform before computing scale/path. |

## Usage example

```tsx
import { Chart } from "../chart";
import { Line } from "./line";
import { Axis } from "../common/axis";

<Chart data={data} x="date" y="value">
  <Line dataKey="value" color="#00B49E" />
  <Axis orientation="bottom" />
  <Axis orientation="left" />
</Chart>
```
