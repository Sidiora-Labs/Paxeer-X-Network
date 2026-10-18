# version.ts

## Overview

Registers the package version on `window.__PAXEER_NETWORK_VERSION__` for the key `@paxeer-network/trading-leaderboard` and exports the version string. Used for runtime version reporting.

## Exports

### Default export

- **`default`**: `"2.9.1"` — Current package version string.

### Global augmentation

Extends `Window` with:

```ts
interface Window {
  __PAXEER_NETWORK_VERSION__?: {
    [key: string]: string;
  };
}
```

When running in a browser, `window.__PAXEER_NETWORK_VERSION__["@paxeer-network/trading-leaderboard"]` is set to the package version.

## Usage example

```typescript
import version from "@paxeer-network/trading-leaderboard/version";
// or from source
import version from "./version";

console.log(version); // "2.9.1"
```
