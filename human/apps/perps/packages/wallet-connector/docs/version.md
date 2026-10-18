# version.ts

## Overview

Registers the package version on `window.__PAXEER_NETWORK_VERSION__` in browser environments and exports the version string. Used for diagnostics and version reporting across paxeer-network SDK packages.

## Exports

### Default export

**Type:** `string`  
**Value:** `"2.9.1"` (matches package.json).

## Global augmentation

Declares a global `Window` interface extension:

```ts
interface Window {
  __PAXEER_NETWORK_VERSION__?: {
    [key: string]: string;
  };
}
```

When running in a browser, sets `window.__PAXEER_NETWORK_VERSION__["@paxeer-network/wallet-connector"] = "2.9.1"` (or merges into existing object).

## Usage example

```ts
import version from "@paxeer-network/wallet-connector/version";

console.log(version); // "2.9.1"

// In browser:
// window.__PAXEER_NETWORK_VERSION__?.["@paxeer-network/wallet-connector"] === "2.9.1"
```
