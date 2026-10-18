# string

## Overview

String helpers: capitalize first letter, transform symbol form (`PERP_ETH_USDC` → `ETH-PERP`), camelCase to snake_case, and longest common prefix index.

## Exports

### capitalizeString

Lowercases the string and capitalizes the first character.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| str | string | Yes | Input string |

**Returns:** `string`

---

### transSymbolformString

Converts `"PERP_ETH_USDC"`-style to `"ETH-PERP"` (base-type). Expects three parts separated by `_` with first part starting with `"PERP"`. Throws on invalid format.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| input | string | Yes | Symbol string `type_base_quote` |

**Returns:** `string` — e.g. `"ETH-PERP"`

---

### camelCaseToUnderscoreCase

Converts camelCase to lowercase with underscores (e.g. `fooBar` → `foo_bar`).

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| str | string | Yes | camelCase string |

**Returns:** `string`

---

### findLongestCommonSubString

Finds the index where two strings first differ (character-by-character from the start). If they match for the whole length of `str1`, returns `-1`.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| str1 | string | Yes | First string |
| str2 | string | Yes | Second string |

**Returns:** `number` — first differing index, or `-1` if no difference found within `str1.length`.

## Usage example

```typescript
import {
  capitalizeString,
  transSymbolformString,
  camelCaseToUnderscoreCase,
  findLongestCommonSubString,
} from "@paxeer-network/utils";

capitalizeString("hello");              // "Hello"
transSymbolformString("PERP_ETH_USDC"); // "ETH-PERP"
camelCaseToUnderscoreCase("fooBar");   // "foo_bar"
findLongestCommonSubString("abc", "abd"); // 2
```
