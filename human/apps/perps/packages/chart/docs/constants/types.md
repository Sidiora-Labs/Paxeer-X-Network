# types

## Overview

Shared TypeScript types for chart layout (Margin, Size), axis mapping (LineChart), and the generic chart container props (ChartProps).

## Exports

### Margin

| Property | Type | Description |
|----------|------|-------------|
| top | number | Top margin. |
| right | number | Right margin. |
| bottom | number | Bottom margin. |
| left | number | Left margin. |

### Size

| Property | Type | Description |
|----------|------|-------------|
| width | number | Chart width. |
| height | number | Chart height. |

### LineChart

| Property | Type | Description |
|----------|------|-------------|
| x | string \| ((d: any) => any) | The name of the x-axis field in the data or accessor. |
| y | string \| ((d: any) => any) | The name of the y-axis field in the data or accessor. |

### ChartProps&lt;T&gt;

| Property | Type | Required | Description |
|----------|------|----------|-------------|
| id | string | No | Optional chart id. |
| margin | Margin | No | Chart margins. |
| data | T[] | Yes | Data array. |
| width | number | No | Fixed width. |
| height | number | No | Fixed height. |
| loading | boolean | No | Loading state. |
| x | string \| ((d: any) => any) | Yes | X field or accessor (from LineChart). |
| y | string \| ((d: any) => any) | Yes | Y field or accessor (from LineChart). |

## Usage example

```ts
import type { Margin, Size, ChartProps, LineChart } from "./constants/types";

const margin: Margin = { top: 20, right: 12, bottom: 20, left: 30 };
const size: Size = { width: 400, height: 200 };
const props: ChartProps<{ date: string; value: number }> = {
  data: [],
  x: "date",
  y: "value",
  margin,
};
```
