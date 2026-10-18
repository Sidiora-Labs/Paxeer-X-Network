# swr.ts

## Overview

Exports a key generator for SWR/infinite query: builds paginated URL with optional `start_date`, `end_date`, and `size` for referral/volume APIs.

## Exports

| Export | Description |
|--------|-------------|
| `generateKeyFun` | Function that takes `{ path, startDate?, endDate?, size? }` and returns a function `(pageIndex, previousPageData) => string \| null` for SWR key |

## Usage Example

```ts
import { generateKeyFun } from "./swr";
const getKey = generateKeyFun({
  path: "/v1/referral/volume",
  startDate: "2025-01-01",
  endDate: "2025-01-31",
  size: 100,
});
// Use with useSWRInfinite or similar
```
