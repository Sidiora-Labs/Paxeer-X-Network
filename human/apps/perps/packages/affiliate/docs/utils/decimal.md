# utils/decimal.ts

## Responsibility of utils/decimal.ts

Formats numeric values as dollar strings with thousand separators (commify). Used for displaying referral/rebate amounts in the affiliate UI.

## Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| refCommify | function | Formatter | Returns `$` + commify(value, fix) or "-" for null/undefined |

## refCommify Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| value | string \| number \| undefined \| null | No | Value to format |
| fix | number | No | Decimal places (passed to commify) |

Returns: `string` — e.g. `"$1,234.56"` or `"-"`.

## Dependencies

- @paxeer-network/utils (commify)

## utils/decimal.ts Example

```typescript
import { refCommify } from "./decimal";

refCommify(1234.5, 2);   // "$1,234.50"
refCommify(undefined);   // "-"
```
