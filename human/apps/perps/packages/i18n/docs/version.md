# version.ts

## version.ts responsibility

Exposes the package version string and attaches it to window.__PAXEER_NETWORK_VERSION__["@paxeer-network/i18n"] when running in a browser. Used for diagnostics and version reporting.

## version.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| default | string | Version | "2.10.2" |

## version.ts side effect

- If typeof window !== "undefined": window.__PAXEER_NETWORK_VERSION__ = window.__PAXEER_NETWORK_VERSION__ || {}; window.__PAXEER_NETWORK_VERSION__["@paxeer-network/i18n"] = "2.10.2".

## version.ts Example

```typescript
import version from "@paxeer-network/i18n/version";
console.log(version); // "2.10.2"
```
