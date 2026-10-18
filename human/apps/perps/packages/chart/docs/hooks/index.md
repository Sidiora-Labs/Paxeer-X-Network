# hooks — Chart context and scale state

React context for chart dimensions, margin, data, and scale, plus a hook to compute scale from data/size/margin/x/y.

## Files

| File | Language | Description | Link |
|------|----------|-------------|------|
| chartContext.ts | TypeScript | ChartContext and useChartContext: margin, size, data, scale, registerScale | [chartContext.md](chartContext.md) |
| useChartState.ts | TypeScript | useChartState: builds x (scaleUtc) and y (scaleLinear) from options, returns { scale } | [useChartState.md](useChartState.md) |
