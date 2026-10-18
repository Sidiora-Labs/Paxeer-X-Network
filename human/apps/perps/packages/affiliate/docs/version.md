# version.ts

## Responsibility of version.ts

Exports the package version string and attaches it to `window.__PAXEER_NETWORK_VERSION__["@paxeer-network/affiliate"]` in browser for runtime version reporting.

## Exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| default | string | Version | Semver string, e.g. "2.10.2" |

## Input and Output

- **Input**: None.
- **Output**: Version string; side effect: sets `window.__PAXEER_NETWORK_VERSION__` when `window` is defined.

## Dependencies

- None.

## version.ts Example

```typescript
import version from "@paxeer-network/affiliate/version";
// version === "2.10.2"

// In browser: window.__PAXEER_NETWORK_VERSION__?.["@paxeer-network/affiliate"] === "2.10.2"
```
