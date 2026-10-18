# version

## Overview

Registers the package version on `window.__PAXEER_NETWORK_VERSION__["@paxeer-network/ui-tradingview"]` when running in a browser, and exports the version string as default.

## Exports

### Default export

- **Type**: `string`
- **Value**: `"2.9.1"` (from package.json)

## Usage example

```ts
import version from "@paxeer-network/ui-tradingview/src/version";
// version === "2.9.1"
// In browser: window.__PAXEER_NETWORK_VERSION__["@paxeer-network/ui-tradingview"] === "2.9.1"
```
