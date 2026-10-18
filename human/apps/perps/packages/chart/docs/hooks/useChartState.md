# useChartState

## Overview

Hook that builds D3 scales from data, size, margin, and x/y field names. Returns an object with `scale` (x: scaleUtc or scaleLinear, y: scaleLinear). Used by Chart children that need to register or use scales.

## Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| options | object | Yes | Configuration. |
| options.data | any[] | Yes | Chart data. |
| options.size | Size | Yes | Width and height. |
| options.margin | Margin | Yes | Margins. |
| options.x | string | Yes | X field name (e.g. "date"). |
| options.y | string | Yes | Y field name. |

## Returns

| Property | Type | Description |
|----------|------|-------------|
| scale | ChartScale \| undefined | x: scaleUtc(extent dates, horizontal range), y: scaleLinear([0, max], vertical range). |

## ChartScale

- `x`: ScaleTime | ScaleLinear | ScaleBand
- `y`: ScaleLinear

## Usage example

```tsx
const { scale } = useChartState({
  data,
  size: { width: 400, height: 200 },
  margin: { top: 20, right: 12, bottom: 20, left: 30 },
  x: "date",
  y: "value",
});
```
