# version.ts

## version.ts responsibility

Exposes the portfolio package version and attaches it to `window.__PAXEER_NETWORK_VERSION__["@paxeer-network/portfolio"]` for runtime version reporting.

## version.ts exports

| Name | Type | Role | Description |
|------|------|------|--------------|
| default | string | Version string | `"2.10.2"` |
| (side effect) | - | Global | Sets `window.__PAXEER_NETWORK_VERSION__["@paxeer-network/portfolio"]` when in browser |

## Version value and global

- **Default export**: `"2.10.2"`.
- **Global**: On `typeof window !== "undefined"`, assigns `window.__PAXEER_NETWORK_VERSION__["@paxeer-network/portfolio"]` (initializes `__PAXEER_NETWORK_VERSION__` if missing).

## version.ts dependency and usage

- **Dependency**: None (no imports).
- **Usage**: Host app or tooling can read version from `window.__PAXEER_NETWORK_VERSION__?.["@paxeer-network/portfolio"]` or from the default export.

## version.ts Example

```typescript
import version from "@paxeer-network/portfolio/version";
// version === "2.10.2"

// In browser:
// window.__PAXEER_NETWORK_VERSION__["@paxeer-network/portfolio"] === "2.10.2"
```
