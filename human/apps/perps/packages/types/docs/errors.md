# errors

## Overview

Error classes for API and SDK failures. Used to distinguish server errors (with code) from client/SDK errors.

## Exports

### ApiError

Extends `Error`. Thrown when the API returns an error response.

| Member | Type | Description |
|--------|------|-------------|
| name | string | `"ApiError"` |
| message | string | Error message |
| code | number | API error code (readonly) |

Constructor: `new ApiError(message: string, code: number)`.

### SDKError

Extends `Error`. Thrown for SDK-side issues (validation, network, etc.).

| Member | Type | Description |
|--------|------|-------------|
| name | string | `"SDKError"` |
| message | string | `"[PAXEER_NETWORK SDK ERROR]:" + message` |

Constructor: `new SDKError(message: string)`.

## Usage example

```typescript
import { ApiError, SDKError } from "@paxeer-network/types";
try {
  await placeOrder(data);
} catch (e) {
  if (e instanceof ApiError) {
    console.error(e.code, e.message);
  } else if (e instanceof SDKError) {
    console.error(e.message);
  }
}
```
