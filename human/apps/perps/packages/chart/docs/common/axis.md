# Axis

## Overview

D3 axis component that reads scale and size from ChartContext. Renders either a bottom (x) or left (y) axis with styled ticks and no domain line.

## Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| orientation | "top" \| "bottom" \| "left" \| "right" | No | "bottom" | Axis position. |
| tickValues | any[] | No | — | Override tick values (e.g. for x-axis). |

## Usage example

```tsx
import { Axis } from "./common/axis";

<Axis orientation="bottom" tickValues={[t1, t2]} />
<Axis orientation="left" />
```
