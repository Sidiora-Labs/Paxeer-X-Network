# version.ts

## version.ts 的职责

提供当前包版本号字符串，并在浏览器环境下将版本写入 `window.__PAXEER_NETWORK_VERSION__['@paxeer-network/net']`，便于运行时与调试时查看 net 包版本。

## version.ts 对外暴露的类型与入口

| 名称 | 类型 | 角色 | 说明 |
|------|------|------|------|
| default | string | 唯一导出 | 当前版本号，如 "2.10.2" |

## version 的输入与输出

- 输入：无（模块加载即执行）
- 输出：默认导出为版本字符串；副作用为在 `window` 上写入版本信息（仅当 `typeof window !== "undefined"`）

## version 依赖与关联关系

- 上游调用方：包入口 `index.ts`、可能依赖版本号的其他模块
- 下游依赖：无
- 关联：`Window` 全局类型扩展（`__PAXEER_NETWORK_VERSION__`）

## version 的错误与边界

| 场景 | 条件 | 表现 | 处理方式 |
|------|------|------|----------|
| 非浏览器 | `typeof window === "undefined"` | 不写入 window，仅导出字符串 | 正常使用 default export |

## version Example

```typescript
import version from "@paxeer-network/net";

console.log(version); // "2.10.2"

// In browser: window.__PAXEER_NETWORK_VERSION__?.["@paxeer-network/net"] === "2.10.2"
```
