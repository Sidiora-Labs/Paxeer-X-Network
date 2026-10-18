# ChartRenderer

## Overview

Functional component that renders an SVG with `viewBox` set from `useChartContext().size`. Returns `null` if size is not yet available. Used by `Chart` and `ChartBase` to host axis/line/bar children.

## Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| children | ReactNode | No | — | Rendered inside the SVG. |

## Context

Consumes `size` from `ChartContext` (width and height). Must be used inside `ChartContext.Provider`.

## Usage example

```tsx
<ChartContext.Provider value={{ size: { width: 400, height: 200 }, ... }}>
  <ChartRenderer>
    <g>...</g>
  </ChartRenderer>
</ChartContext.Provider>
```
