# version

## Overview

Registers the package version for `@paxeer-network/ui-tpsl` on the global `window.__PAXEER_NETWORK_VERSION__` object (when running in a browser). Used for version reporting and debugging.

## Exports

### Default export

- **Default**: string `"2.9.1"` (current package version)

## Global side effect

- In browser: `window.__PAXEER_NETWORK_VERSION__["@paxeer-network/ui-tpsl"] = "2.9.1"`.

## Usage example

```typescript
import version from "@paxeer-network/ui-tpsl/version";
// version === "2.9.1"
```
