# version

## Overview

Registers the package version on `window.__PAXEER_NETWORK_VERSION__` for the `@paxeer-network/ui-notification` key and exports the version string as default.

## Exports

### Default export

- **Type**: `string`
- **Value**: `"2.9.1"` (or current package version)

## Global augmentation

Extends `Window` with:

```ts
interface Window {
  __PAXEER_NETWORK_VERSION__?: {
    [key: string]: string;
  };
}
```

When running in a browser, sets `window.__PAXEER_NETWORK_VERSION__["@paxeer-network/ui-notification"]` to the package version.

## Usage example

```typescript
import version from "@paxeer-network/ui-notification/version";
// or from the package entry that re-exports it
console.log(version); // "2.9.1"
```
