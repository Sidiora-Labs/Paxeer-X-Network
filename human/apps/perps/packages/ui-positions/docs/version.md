# version

## Overview

Exposes the package version and injects it into `window.__PAXEER_NETWORK_VERSION__["@paxeer-network/ui-positions"]` for runtime checks. Default export is the version string (e.g. `"2.9.1"`).

## Exports

### Default export

- **Type**: `string`
- **Description**: Current package version.

### Global augmentation

- **`Window.__PAXEER_NETWORK_VERSION__`**: Optional record of package names to version strings; this package sets `["@paxeer-network/ui-positions"]`.

## Usage example

```typescript
import version from "@paxeer-network/ui-positions/version";
// or from source
import version from "./version";
console.log(version); // "2.9.1"
```
