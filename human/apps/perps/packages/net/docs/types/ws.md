# ws.ts (types)

## ws.ts 的职责

定义 WebSocket 层使用的类型与接口：订阅/取消订阅消息结构、发送函数类型、以及可插拔的 MessageHandler 接口，供 WS 与 handler 模块使用。

## ws.ts 对外导出内容

| 名称 | 类型 | 角色 | 说明 |
|------|------|------|------|
| MessageObserveTopic | type | 订阅/取消订阅消息体 | event + topic |
| MessageObserveParams | type | 订阅参数（string 或 MessageObserveTopic） | 用于 subscribe 入参 |
| SendFunc | type | 发送函数 | (message: any) => void |
| MessageHandler | interface | 消息处理器 | handle(message, webSocket) |

## MessageObserveTopic 字段说明

| 字段名 | 类型 | 必需 | 说明 |
|--------|------|------|------|
| event | "subscribe" \| "unsubscribe" | 是 | 订阅或取消订阅 |
| topic | string | 是 | 主题标识 |

## MessageHandler 的职责

抽象 WebSocket 消息处理器，由具体 handler（如 PingHandler）实现，WS 收到消息后根据 `message.event` 查找并调用对应 handler。

## MessageHandler 参数或属性说明

| 名称 | 类型 | 必需 | 说明 |
|------|------|------|------|
| handle | (message: any, webSocket: WebSocket) => void | 是 | 处理单条消息，可向 webSocket 回写 |

## MessageHandler 依赖与调用关系

- 上游调用方：`ws/ws.ts` 的 onMessage 中通过 `messageHandlers.get(message.event)` 调用
- 下游实现：`ws/handler/baseHandler.ts`、`ws/handler/ping.ts` 等

## MessageHandler Example

```typescript
import type { MessageHandler } from "@paxeer-network/net";

const MyHandler: MessageHandler = {
  handle(message: any, webSocket: WebSocket) {
    if (message.event === "my_event") {
      webSocket.send(JSON.stringify({ ack: message.id }));
    }
  },
};
```

## MessageObserveTopic / MessageObserveParams Example

```typescript
import type { MessageObserveTopic, MessageObserveParams } from "@paxeer-network/net";

const sub: MessageObserveTopic = { event: "subscribe", topic: "orderbook" };
const params: MessageObserveParams = "orderbook"; // or sub
```
