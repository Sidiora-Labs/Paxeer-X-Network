# utils.ts

> Location: `packages/ui-transfer/src/utils.ts`

## Overview

Utility functions for token resolution, fee decimals, account ID validation, and user-facing error messages for transfer and deposit flows.

## Exports

### getTokenByTokenList

Builds a symbol-to-token map and returns USDC, USDbC, or the first token.

| Parameter | Type | Description |
| --------- | ---- | ----------- |
| tokens | API.TokenInfo[] | Token list (default `[]`) |

**Returns:** `API.TokenInfo`

### feeDecimalsOffset

Returns `(origin ?? 2) + 3` for fee display precision.

| Parameter | Type | Description |
| --------- | ---- | ----------- |
| origin | number | Optional base decimals |

**Returns:** `number`

### checkIsAccountId

Checks if a string is a valid account ID (0x + 64 hex chars).

| Parameter | Type | Description |
| --------- | ---- | ----------- |
| accountId | string | String to validate |

**Returns:** `boolean`

### getTransferErrorMessage

Maps transfer API error codes to localized messages (e.g. 34 = transfer in progress, 17 = withdrawal in progress, 35 = account not exist, 37 = transfer to self, 46 = transfer to sub-account).

| Parameter | Type | Description |
| --------- | ---- | ----------- |
| errorCode | number | API error code |

**Returns:** `string` (i18n key result)

### getDepositKnownErrorMessage

Maps known contract revert selectors (e.g. `0x6697b232` → AccessControlBadConfirmation) to a readable message including the selector.

| Parameter | Type | Description |
| --------- | ---- | ----------- |
| message | string | Error message from contract |

**Returns:** `string` or empty string

## Usage example

```ts
import {
  getTokenByTokenList,
  feeDecimalsOffset,
  checkIsAccountId,
  getTransferErrorMessage,
  getDepositKnownErrorMessage,
} from "@paxeer-network/ui-transfer";

const token = getTokenByTokenList(tokens);
const decimals = feeDecimalsOffset(2);
const ok = checkIsAccountId("0x" + "a".repeat(64));
const msg = getTransferErrorMessage(34);
const depositMsg = getDepositKnownErrorMessage(err.message);
```
