# yTickFormatter

## Overview

Formats a number for Y-axis tick labels using `numberToHumanStyle` from `@paxeer-network/utils`, with decimal places based on magnitude and preserves sign (e.g. negative values get a minus prefix).

## Exports

### tickFormatter

| Parameter | Type | Description |
|-----------|------|-------------|
| value | number | Raw tick value. |

**Returns:** string — e.g. `"1.23K"`, `"-500"`.

Logic: `dp = 0 if abs === 0, else abs <= 10 ? 2 : abs <= 100 ? 1 : 0`; then human-style format with sign.

## Usage example

```ts
import { tickFormatter } from "./utils/yTickFormatter";

<YAxis tickFormatter={tickFormatter} />
// tickFormatter(1234) => "1.23K", tickFormatter(-50) => "-50"
```
