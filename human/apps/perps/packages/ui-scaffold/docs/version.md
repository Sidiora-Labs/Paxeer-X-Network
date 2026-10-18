# version

> Location: `packages/ui-scaffold/src/version.ts`

## Overview

Exposes the package version and injects it into `window.__PAXEER_NETWORK_VERSION__` for runtime checks. Default export is the version string.

## Exports

- **Default export**: `string` — e.g. `"2.9.1"`.

## Global extension

Extends the global `Window` interface:

```ts
interface Window {
  __PAXEER_NETWORK_VERSION__?: {
    [key: string]: string;
  };
}
```

When running in a browser, `window.__PAXEER_NETWORK_VERSION__["@paxeer-network/ui-scaffold"]` is set to the current package version.

## Usage example

```typescript
import version from "@paxeer-network/ui-scaffold/version";
// version === "2.9.1"

// In browser:
// window.__PAXEER_NETWORK_VERSION__?.["@paxeer-network/ui-scaffold"] === "2.9.1"
```
