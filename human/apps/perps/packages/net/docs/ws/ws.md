# ws/ws.ts

## ws/ws.ts 的职责

提供 paxeer-network WebSocket 客户端：建立公网/私网两条连接，支持订阅与取消订阅、自动重连、心跳超时检测、页面可见性与网络状态监听，并通过 EventEmitter 派发 `status:change` 与可配置的 topic 事件。

## ws/ws.ts 对外导出内容

| 名称 | 类型 | 角色 | 说明 |
|------|------|------|------|
| WS | class | WebSocket 客户端 | 继承 EventEmitter，公/私连接、subscribe/privateSubscribe、close/openPrivate |
| WebSocketEvent | enum | 事件类型 | OPEN, CLOSE, ERROR, MESSAGE, CONNECTING, RECONNECTING |
| WSOptions | type | 构造选项 | privateUrl, publicUrl?, networkId?, accountId?, onSigntureRequest? |
| MessageParams | type | 订阅参数 | event, topic, onMessage?, params? |
| unsubscribe | type | 取消订阅函数 | () => void |

## WSOptions 参数或属性说明

| 属性名 | 类型 | 必需 | 说明 |
|--------|------|------|------|
| privateUrl | string | 是 | 私网 WS base URL |
| publicUrl | string | 否 | 公网 WS base URL，无则不建公网连接 |
| networkId | "testnet" \| "mainnet" | 否 | 网络标识 |
| accountId | string | 否 | 账户 ID，有则建立私网连接并鉴权 |
| onSigntureRequest | (accountId: string) => Promise\<any\> | 否 | 鉴权时调用，返回 { publicKey, signature, timestamp } |

## MessageParams 参数或属性说明

| 属性名 | 类型 | 必需 | 说明 |
|--------|------|------|------|
| event | string | 是 | 事件名 |
| topic | string | 是 | 主题 |
| onMessage | (message: any) => any | 否 | 收到该 topic 消息的回调 |
| params | any | 否 | 额外参数 |

## WS 的输入与输出

- 输入：构造时 WSOptions；subscribe/privateSubscribe 的 params 与 callback。
- 输出：UI 通过 `on("status:change", ...)` 与 topic 事件获取状态与消息；subscribe 返回 unsubscribe 函数用于取消订阅。

## WS 的渲染与状态流程

1. 构造：若存在 publicUrl 则创建公网连接（/ws/stream/{COMMON_ID}）；若存在 accountId 则创建私网连接（/v2/ws/private/stream/{accountId}）。
2. 私网 onopen：调用 onSigntureRequest 后发送 auth 消息，成功后 authenticated = true，处理 _pendingPrivateSubscribe。
3. 收到消息：根据 message.event 查 messageHandlers，有则交给 handler；否则按 topic 派发到对应 callback 并 emit 匹配的 _eventContainer key。
4. 公/私连接 close：将当前订阅移入 pending，emit status:change CLOSE，setTimeout 触发 checkSocketStatus。
5. checkSocketStatus（可见且在线）：若连接 CLOSED 则重连；若距上次心跳超过 TIME_OUT（2 分钟）则主动 close(3888) 触发重连。
6. 重连：reconnectPublic/reconnectPrivate 设置 reconnecting 标志，递增 retry count，emit RECONNECTING，延迟后重新 createPublicSC/createPrivateSC。

## WS 依赖与渲染关系

- 上游调用方：使用 WS 实例的业务层（交易、订单簿等）
- 下游依赖：eventemitter3、./handler/handler（messageHandlers）、types/ws
- 使用的浏览器 API：WebSocket、document.visibilitychange、window online、navigator.onLine

## WS 的错误与边界

| 场景 | 条件 | 表现 | 处理方式 |
|------|------|------|----------|
| 公网连接未就绪 | 调用 subscribe 时 readyState !== OPEN | 订阅入 _pendingPublicSubscribe，open 后补发 | 正常 |
| 私网未鉴权 | 私网 open 后未收到 auth success | 私网订阅入 _pendingPrivateSubscribe，auth 成功后补发 | 正常 |
| 重连次数超限 | _publicRetryCount 或 _privateRetryCount > CONNECT_LIMIT(5) | 不再自动重连 | 可重新 new WS 或提示用户 |
| close code 3887 | 私网 onPrivateClose | 直接 return 不处理 | 内部约定 |
| 非浏览器 | typeof window/document 未定义 | 部分监听与重连不绑定 | 仅 Node 等环境需注意 |

## WS 的扩展或修改入口

- 新增通用 event 处理：在 `ws/handler` 中新增 Handler 并注册到 messageHandlers。
- 修改心跳超时：常量 TIME_OUT、CONNECT_LIMIT、reconnectInterval。
- 修改连接 URL 路径：createPublicSC/createPrivateSC 中的 URL 拼接。

## WS Example

```typescript
import { WS, WebSocketEvent } from "@paxeer-network/net";

const ws = new WS({
  publicUrl: "wss://stream.paxeer.network",
  privateUrl: "wss://private-stream.paxeer.network",
  accountId: "my-account-id",
  onSigntureRequest: async (accountId) => ({
    publicKey: "...",
    signature: "...",
    timestamp: Date.now(),
  }),
});

ws.on("status:change", (e) => {
  if (e.type === WebSocketEvent.OPEN) console.log("open", e.isPrivate, e.isReconnect);
  if (e.type === WebSocketEvent.CLOSE) console.log("close");
});

const unsub = ws.subscribe(
  { event: "subscribe", topic: "orderbook" },
  {
    onMessage: (data) => console.log(data),
    onUnsubscribe: (topic) => ({ event: "unsubscribe", topic }),
  },
);
// later: unsub();

ws.openPrivate("other-account-id");
ws.closePrivate();
ws.close();
```
