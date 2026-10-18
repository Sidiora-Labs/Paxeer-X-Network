# utils/types.ts

## Responsibility of utils/types.ts

Shared type definitions for date ranges, summary filters, bar filters, and icon props used across affiliate UI and charts.

## Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| DateRange | type | Filter | `from` and optional `to` dates |
| SummaryFilter | type | Filter | "All" \| "1D" \| "7D" \| "30D" |
| BarDayFilter | type | Filter | "7" \| "30" \| "90" (days) |
| IconProps | interface | Props | Extends SVGProps with size, className |

## DateRange Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| from | Date \| undefined | Yes | Start date |
| to | Date \| undefined | No | End date |

## IconProps Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| size | number | No | Icon size |
| className | string | No | CSS class |
| (rest) | SVGProps<SVGSVGElement> | — | Pass-through SVG props |

## utils/types.ts Example

```typescript
import type { DateRange, SummaryFilter, IconProps } from "../utils/types";

const range: DateRange = { from: new Date(), to: new Date() };
const filter: SummaryFilter = "7D";
```
