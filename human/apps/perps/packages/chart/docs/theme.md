# theme (root)

## Overview

Defines the chart theme type and default theme for bar charts: tick count and gap (number or function). Used by bar-related components that need consistent spacing.

## Exports

### ChartTheme

| Property | Type | Description |
|----------|------|-------------|
| bar | object | Bar-specific theme. |
| bar.tick | number | Tick count. |
| bar.gap | number \| ((data: any[], ctx: ChartContextState) => number) | Gap between bars or function to compute it. |

### defaultTheme

Default theme: `bar: { tick: 4, gap: (data) => 2 }`.

## Usage example

```ts
import { defaultTheme, type ChartTheme } from "./theme";

const theme: ChartTheme = defaultTheme;
const gap = typeof theme.bar.gap === "function" ? theme.bar.gap(data, ctx) : theme.bar.gap;
```
