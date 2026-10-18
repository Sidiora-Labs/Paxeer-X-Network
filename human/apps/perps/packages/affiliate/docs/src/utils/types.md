# types.ts

## Overview

Shared TypeScript types and interfaces used across the affiliate package: date range, filter types, and icon props extending React SVG props.

## Exports

### Types

| Name | Description |
|------|-------------|
| `DateRange` | `{ from: Date \| undefined; to?: Date \| undefined }` |
| `SummaryFilter` | `"All" \| "1D" \| "7D" \| "30D"` |
| `BarDayFilter` | `"7" \| "30" \| "90"` |
| `IconProps` | Extends `SVGProps<SVGSVGElement>` with optional `size?: number`, `className?: string` |

## Usage Example

```ts
import { DateRange, IconProps } from "../utils/types";
const range: DateRange = { from: new Date() };
const iconProps: IconProps = { size: 24, className: "icon" };
```
