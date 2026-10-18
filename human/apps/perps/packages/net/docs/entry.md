# index.ts

## index.ts 的职责

包入口文件，聚合并 re-export 版本、HTTP 方法、常量、WebSocket 类与事件枚举，供上层通过 `@paxeer-network/net` 单一入口使用。

## index.ts 对外导出内容

| 名称 | 类型 | 角色 | 说明 |
|------|------|------|------|
| version | default export (string) | 版本 | 来自 `./version` |
| get, post, del, put, mutate | named function | HTTP 方法 | 来自 `./fetch` |
| __PAXEER_NETWORK_API_URL_KEY__ | string constant | 常量 | 来自 `./constants` |
| WS | class | WebSocket 客户端 | 来自 `./ws/ws` |
| WebSocketEvent | enum | WebSocket 事件枚举 | 来自 `./ws/ws` |

## index.ts 依赖与关联关系

- 上游调用方：使用 `@paxeer-network/net` 的应用或包
- 下游依赖：`./version`、`./fetch`、`./constants`、`./ws/ws`
- 无业务逻辑，仅做转发

## index.ts Example

```typescript
import {
  version,
  get,
  post,
  WS,
  WebSocketEvent,
  __PAXEER_NETWORK_API_URL_KEY__,
} from "@paxeer-network/net";

console.log(version); // "2.10.2"
const data = await get("/api/v1/...");
const ws = new WS({ privateUrl: "...", publicUrl: "..." });
ws.on("status:change", (e) => { /* e.type === WebSocketEvent.OPEN */ });
```
