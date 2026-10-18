# version.ts

## version.ts Responsibilities

Sets the package version string and registers it on `window.__PAXEER_NETWORK_VERSION__["@paxeer-network/markets"]` in browser environments. Used for runtime version reporting.

## version.ts Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| default | string | Version | Package version "2.10.2" |

## Input/Output

- **Input**: None.
- **Output**: Default export is the version string. Side effect: when `window` is defined, sets `window.__PAXEER_NETWORK_VERSION__["@paxeer-network/markets"]` to the same version.

## Global Augmentation

- **Type**: Extends `Window` with `__PAXEER_NETWORK_VERSION__?: { [key: string]: string }`.
- **Runtime**: Object is created if missing; then the markets package key is set.

## Dependencies

- **Upstream**: None.
- **Downstream**: Apps or tooling that read `window.__PAXEER_NETWORK_VERSION__` for diagnostics.

## version.ts Example

```typescript
import version from "@paxeer-network/markets/version";
// or from package entry if re-exported
console.log(version); // "2.10.2"
// In browser: window.__PAXEER_NETWORK_VERSION__["@paxeer-network/markets"] === "2.10.2"
```
