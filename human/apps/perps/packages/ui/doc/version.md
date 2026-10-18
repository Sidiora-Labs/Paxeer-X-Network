# version.ts

## version.ts 的职责

Exposes the package version for `@paxeer-network/ui` and attaches it to `window.__PAXEER_NETWORK_VERSION__` in browser environments. Used for runtime version checks and debugging.

## version.ts 对外导出内容

| Name    | Type   | Role           | Description |
| ------- | ------ | -------------- | ----------- |
| default | string | Version string | `"2.10.2"`  |

## version.ts 的输入与输出

- **Input**: None (no parameters).
- **Output**: Default export is the version string. Side effect: `window.__PAXEER_NETWORK_VERSION__["@paxeer-network/ui"]` is set to the same version when `window` is defined.

## version.ts 依赖与调用关系

- **Upstream**: None.
- **Downstream**: Any code that imports version or reads `window.__PAXEER_NETWORK_VERSION__`.

## version.ts 的错误与边界

| Scenario | Condition                       | Behavior                | Handling               |
| -------- | ------------------------------- | ----------------------- | ---------------------- |
| SSR      | `typeof window === "undefined"` | No assignment to window | Safe; no-op for server |

## version.ts Example

```ts
import version from "@paxeer-network/ui/version";

console.log(version); // "2.10.2"

// In browser:
console.log(window.__PAXEER_NETWORK_VERSION__?.["@paxeer-network/ui"]); // "2.10.2"
```
