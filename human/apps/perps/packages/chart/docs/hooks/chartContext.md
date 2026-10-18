# chartContext

## Overview

React context that holds chart layout (margin, size), data, scale, and a callback to register the scale. Used by Chart to provide state to ChartRenderer, Axis, Line, Bar, and useChartState consumers.

## Exports

### ChartContextState

| Property | Type | Description |
|----------|------|-------------|
| margin | Margin | Chart margins. |
| size | Size | Chart dimensions (width, height). |
| data | any[] | Chart data. |
| scale | ChartScale | Current x/y scales (from useChartState or children). |
| registerScale | (scale: ChartScale) => void | Callback to set the scale in context. |

### ChartContext

React context instance: `createContext<ChartContextState>({} as ChartContextState)`.

### useChartContext

Hook that returns the current `ChartContextState`. Must be used within `ChartContext.Provider`.

## Usage example

```tsx
import { ChartContext, useChartContext } from "./hooks/chartContext";

function Child() {
  const { size, margin, scale, registerScale } = useChartContext();
  return <g transform={`translate(${margin.left},${margin.top})`}>...</g>;
}
```
