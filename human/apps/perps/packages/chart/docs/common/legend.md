# Legend

## Overview

Simple SVG legend group. Renders children in a translated `<g>`, or a default "Legend" text if no children are provided.

## Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| children | ReactNode | No | — | Custom content; default is `<text>Legend</text>`. |

## Usage example

```tsx
import { Legend } from "./common/legend";

<Legend />
<Legend><text>PnL</text></Legend>
```
