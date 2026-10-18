# version

## Overview

Sets the package version on `window.__PAXEER_NETWORK_VERSION__["@paxeer-network/chart"]` when running in a browser, and exports the version string as default.

## Exports

- **Default**: `"2.9.1"` (string).

## Global extension

| Global | Description |
|--------|-------------|
| `Window.__PAXEER_NETWORK_VERSION__` | Optional record of package names to version strings; this file sets the key `@paxeer-network/chart`. |

## Usage example

```ts
import version from "@paxeer-network/chart/src/version";
// version === "2.9.1"
// In browser: window.__PAXEER_NETWORK_VERSION__?.["@paxeer-network/chart"] === "2.9.1"
```
