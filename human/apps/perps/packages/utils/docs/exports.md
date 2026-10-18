# package exports (index)

## Overview

Barrel file for `@paxeer-network/utils`. Re-exports all public APIs from submodules and re-exports `dayjs` as default from `dayjs`.

## Exports

| Export | Source |
|--------|--------|
| `Decimal` (default) | `./decimal` |
| `*` from decimal | `./decimal` |
| `*` from dateTime | `./dateTime` |
| `*` from chain | `./chain` |
| `*` from string | `./string` |
| `windowGuard`, `getGlobalObject`, `getTimestamp` | `./window` |
| `dayjs` (default) | `dayjs` |
| `*` from symbol | `./symbol` |
| `*` from order | `./order` |
| `*` from formatNum | `./formatNum` |

## Usage example

```typescript
import {
  Decimal,
  formatNum,
  formatSymbol,
  getTrailingStopPrice,
  windowGuard,
  getTimestamp,
  commify,
  timestampToString,
  hex2int,
  isTestnet,
} from "@paxeer-network/utils";
import dayjs from "@paxeer-network/utils";
```
