# version

## Overview

Sets the package version on `window.__PAXEER_NETWORK_VERSION__["@paxeer-network/ui-orders"]` for runtime checks and exports the version string as default.

## Exports

- **Default export**: Version string (e.g. `"2.9.1"`).

## Global

- Extends `Window` with `__PAXEER_NETWORK_VERSION__?: { [key: string]: string }` and assigns the current package version when running in a browser.

## Usage example

```typescript
import pkgVersion from "@paxeer-network/ui-orders/version";
// or
const version = window.__PAXEER_NETWORK_VERSION__?.["@paxeer-network/ui-orders"];
```
