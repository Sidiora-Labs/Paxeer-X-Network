# parseError

## Overview

Utility to decode and normalize errors from ethers and contract calls. Uses `ethers-decode-error` to produce a decoded error with a user-friendly `message` (with ethers-specific prefixes stripped).

## Exports

### `parseError(rawError: any): Promise<ParsedError>`

Decodes a raw error (e.g. from a rejected contract call or transaction) and returns a `ParsedError` object.

#### Parameters

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `rawError` | `any` | Yes | The thrown error from ethers or the provider (e.g. contract revert, user rejection). |

#### Returns

`Promise<ParsedError>` where `ParsedError` extends `DecodedError` (from `ethers-decode-error`) with:

| Property | Type | Description |
|----------|------|-------------|
| `message` | `string` | Human-readable message with prefixes like `"ethers-user-denied: "` and `"ethers-unsupported: "` removed. |
| (other) | from `DecodedError` | e.g. `reason`, decoded revert data, etc. |

#### Internal behavior

- Uses `ErrorDecoder.create()` from `ethers-decode-error` to decode `rawError`.
- `message` is set by stripping the prefixes `"ethers-user-denied: "` and `"ethers-unsupported: "` from the decoded `reason` (or empty string if no reason).
- Logs the decoded error with `console.error("parsedError", error)`.

## Usage example

```typescript
import { parseError } from "@paxeer-network/web3-provider-ethers";

try {
  await contract.approve(spender, amount);
} catch (rawError) {
  const parsed = await parseError(rawError);
  console.error(parsed.message);
  throw parsed;
}
```
