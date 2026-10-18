# decimal

## Overview

Re-exports `Decimal` from `decimal.js-light` (default rounding `ROUND_DOWN`) and provides number formatting: commify, optional commify with fallback, human-style (K/M/B/T), precision, trailing zeros, and NaN check.

## Exports

### Default: Decimal

`Decimal` from `decimal.js-light` with default rounding set to `ROUND_DOWN`.

### Type

- **Numeric** — re-exported from `decimal.js-light`.

### Constants

- **zero** — `new Decimal(0)`.

### Functions

| Name | Description |
|------|-------------|
| `cutNumber(num, lenght)` | Placeholder (no implementation). |
| `commifyOptional(num?, options?)` | Format number with optional fallback; supports `fix`, `fallback`, `padEnd`, `fillString`, `prefix`. Returns `options?.fallback \|\| '--'` when invalid/undefined. |
| `commify(num, fix?)` | Add thousands separators; optional decimal `fix`. |
| `toNonExponential(num)` | Convert from exponential notation to fixed string. |
| `getPrecisionByNumber(num)` | Decimal places of the number. |
| `numberToHumanStyle(number, decimalPlaces?, options?)` | Format as K/M/B/T; `options.padding` supported. |
| `parseNumStr(str)` | Parse string/number with optional trailing k/K/m/M/b/B/t/T multiplier to `Decimal`. |
| `removeTrailingZeros(value, fixedCount?)` | Remove trailing zeros (default 16 decimal places). |
| `todpIfNeed(value, dp)` | Truncate decimal part to `dp` places if needed; keeps trailing dot. |
| `checkIsNaN(value)` | Returns `true` if value is undefined, null, empty string, or `Number(value)` is NaN. |

#### commifyOptional options

| Property | Type | Description |
|----------|------|-------------|
| fix | number | Decimal places |
| fallback | string | Value when num is undefined/invalid |
| padEnd | boolean | Pad decimal part to `fix` length |
| fillString | string | Pad character (default `'0'`) |
| prefix | string | Prefix (e.g. currency) |

## Usage example

```typescript
import Decimal, {
  zero,
  commify,
  commifyOptional,
  numberToHumanStyle,
  parseNumStr,
  removeTrailingZeros,
  todpIfNeed,
  checkIsNaN,
} from "@paxeer-network/utils";

const a = new Decimal("0.1").plus(0.2);
commify(1234.56, 2);                    // "1,234.56"
commifyOptional(undefined, { fallback: "-" }); // "-"
numberToHumanStyle(1500000, 1);         // "1.5M"
parseNumStr("1.5k");                    // Decimal 1500
removeTrailingZeros(0.000001);          // "0.000001"
todpIfNeed("3.14159", 2);               // "3.14"
checkIsNaN("");                         // true
```
