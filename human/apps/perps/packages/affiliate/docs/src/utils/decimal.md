# decimal.ts

## Overview

Formats numbers as dollar strings using `@paxeer-network/utils` commify. Returns `"-"` for null/undefined.

## Exports

| Function | Description |
|----------|-------------|
| `refCommify(value?, fix?)` | Returns `$` + commified number, or `"-"` if value is null/undefined |

## Usage Example

```ts
import { refCommify } from "./decimal";
refCommify(1234.56, 2); // "$1,234.56"
refCommify(undefined);  // "-"
```
