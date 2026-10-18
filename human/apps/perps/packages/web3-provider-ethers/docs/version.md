# version

## Overview

Defines the package version and registers it on `window.__PAXEER_NETWORK_VERSION__` for the `@paxeer-network/web3-provider-ethers` key when running in a browser. Used for version reporting and debugging.

## Exports

### Default export: version string

- **Type**: `string`
- **Value**: `"2.9.1"` (at doc generation time).

## Global augmentation

When `window` is defined (browser), the module:

1. Ensures `window.__PAXEER_NETWORK_VERSION__` is an object (creates `{}` if missing).
2. Sets `window.__PAXEER_NETWORK_VERSION__["@paxeer-network/web3-provider-ethers"]` to the same version string.

### Type declaration

```typescript
declare global {
  interface Window {
    __PAXEER_NETWORK_VERSION__?: {
      [key: string]: string;
    };
  }
}
```

## Usage example

```typescript
import version from "@paxeer-network/web3-provider-ethers/version";

console.log(version); // "2.9.1"

// In browser:
// window.__PAXEER_NETWORK_VERSION__["@paxeer-network/web3-provider-ethers"] === "2.9.1"
```
