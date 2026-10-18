# PaxeerNetworkChartTooltip

## Overview

Shared tooltip UI for paxeer-network charts: label, formatted value (Text.numeral with unit, optional coloring and dp/rm), and optional prefix/titleClassName.

## Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| label | string | Yes | — | Tooltip label (e.g. date or "Now"). |
| value | string \| number | Yes | — | Value to display. |
| unit | string | No | "USDC" | Unit shown with value. |
| prefix | ReactNode | No | — | Optional prefix content. |
| titleClassName | string | No | — | Class for the row containing prefix and value. |
| coloring | boolean | No | false | Whether to color value (e.g. profit/loss). |
| dp | number | No | — | Decimal places for value. |
| rm | number | No | — | Rounding mode for value. |

## Usage example

```tsx
import { PaxeerNetworkChartTooltip } from "./customTooltip";

<PaxeerNetworkChartTooltip label="2024-01-01" value={1234.56} unit="USDC" coloring />
```
