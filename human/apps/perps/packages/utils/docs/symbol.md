# symbol

## Overview

Utilities for formatting and displaying trading pair symbols (e.g. `PERP_BTC_USDT`): template-based formatting and human-readable abbreviation of leading numbers.

## Exports

### formatSymbol

Formats a symbol like `"PERP_BTC_USDT"` using a template string with placeholders `type`, `base`, and `quote`.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| symbol | string | Yes | Original symbol in form `type_base_quote` |
| formatString | string | No | Template (default `"base-type"`). Placeholders: `type`, `base`, `quote` |

**Returns:** Formatted string, or `""` if `symbol` is empty.

**Example:**

```typescript
formatSymbol("PERP_BTC_USDT");                 // "BTC"
formatSymbol("PERP_BTC_USDT", "base");         // "BTC"
formatSymbol("PERP_BTC_USDT", "base-type");    // "BTC-PERP"
formatSymbol("SPOT_ETH_USDC", "base-quote");   // "ETH-USDC"
```

---

### optimizeSymbolDisplay

Converts leading numbers in a symbol string to human-readable form (K, M, B, T).

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| symbol | string | Yes | Symbol string (e.g. `"1000000BABYDOGE"`) |
| decimalPlaces | number | No | Decimal places for abbreviated number (default `0`) |

**Returns:** Optimized string (e.g. `"1MBABYDOGE"`). If no leading number or number &lt; 1000, returns original.

**Example:**

```typescript
optimizeSymbolDisplay("1000000BABYDOGE");    // "1MBABYDOGE"
optimizeSymbolDisplay("5000ETH");            // "5KETH"
optimizeSymbolDisplay("1500000TOKEN", 1);   // "1.5MTOKEN"
optimizeSymbolDisplay("BITCOIN");            // "BITCOIN"
```

## Usage example

```typescript
import { formatSymbol, optimizeSymbolDisplay } from "@paxeer-network/utils";

const display = formatSymbol("PERP_ETH_USDC", "base-quote"); // "ETH-USDC"
const short = optimizeSymbolDisplay("1000000BABYDOGE", 1);   // "1.5MBABYDOGE"
```
