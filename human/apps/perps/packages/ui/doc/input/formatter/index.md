# input/formatter — Directory Index

## Directory Responsibility

Input formatters: format/parse for number, currency, identifier, and utilities (decimalPoint, dp, range, regex). Used by Input and other form controls.

## Files

| File              | Language   | Summary                              | Link                                   |
| ----------------- | ---------- | ------------------------------------ | -------------------------------------- |
| inputFormatter.ts | TypeScript | InputFormatter interface and options | [inputFormatter.md](inputFormatter.md) |
| index.ts          | TypeScript | Re-exports formatters                | [index.md](index.md)                   |
| number.ts         | TypeScript | Number formatter                     | [number.md](number.md)                 |
| currency.ts       | TypeScript | Currency formatter                   | [currency.md](currency.md)             |
| identifier.ts     | TypeScript | Identifier formatter                 | [identifier.md](identifier.md)         |
| decimalPoint.ts   | TypeScript | Decimal point helper                 | [decimalPoint.md](decimalPoint.md)     |
| dp.ts             | TypeScript | Decimal places helper                | [dp.md](dp.md)                         |
| range.ts          | TypeScript | Range formatter                      | [range.md](range.md)                   |
| regex.ts          | TypeScript | Regex formatter                      | [regex.md](regex.md)                   |

## Key Entities

| Entity                     | File              | Role                               |
| -------------------------- | ----------------- | ---------------------------------- |
| InputFormatter             | inputFormatter.ts | Interface for format/parse         |
| inputFormatter (namespace) | index.ts          | number, currency, identifier, etc. |
