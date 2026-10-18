# version

## Overview

Registers the package version on `window.__PAXEER_NETWORK_VERSION__` when running in a browser and exports the current version string as default.

## Exports

### Default export (string)

The current package version (e.g. `"2.9.1"`).

### Global augmentation

Extends the global `Window` interface with an optional `__PAXEER_NETWORK_VERSION__` map:

```ts
declare global {
  interface Window {
    __PAXEER_NETWORK_VERSION__?: { [key: string]: string };
  }
}
```

When `window` is defined, it sets `window.__PAXEER_NETWORK_VERSION__["@paxeer-network/utils"]` to the package version.

## Usage example

```typescript
import version from "@paxeer-network/utils";

console.log(version); // "2.9.1"

// In browser: window.__PAXEER_NETWORK_VERSION__?.["@paxeer-network/utils"] === "2.9.1"
```
