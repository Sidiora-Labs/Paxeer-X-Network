# version

## Overview

Sets the package version on `window.__PAXEER_NETWORK_VERSION__["@paxeer-network/wallet-connector-privy"]` for runtime version reporting. Exports the version string as default.

## Exports

- **default** – Version string (e.g. `"2.9.1"`).

## Global

- **window.__PAXEER_NETWORK_VERSION__** – Optional object; after load, `[packageName]` is set to the current version.

## Usage example

```typescript
import version from "@paxeer-network/wallet-connector-privy/version";
// or read from window.__PAXEER_NETWORK_VERSION__["@paxeer-network/wallet-connector-privy"]
```
