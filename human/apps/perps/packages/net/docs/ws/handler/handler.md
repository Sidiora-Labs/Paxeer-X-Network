# ws/handler/handler.ts

## handler.ts 的职责

定义 WebSocket 通用消息类型（MessageType）并维护 messageHandlers 映射表，将 event 名与 MessageHandler 实例关联，供 WS 在 onMessage 中按 message.event 分发。

## handler.ts 对外导出内容

| 名称 | 类型 | 角色 | 说明 |
|------|------|------|------|
| MessageType | type | 事件名字面量联合 | "ping" \| "pong" \| "subscribe" \| ... |
| messageHandlers | Map\<MessageType, MessageHandler\> | 事件到处理器的映射 | 当前注册 "ping" -> PingHandler |

## MessageType 取值

| 取值 | 说明 |
|------|------|
| "ping" | 心跳 ping，由 PingHandler 回 pong |
| "pong" | 心跳 pong |
| "subscribe" \| "unsubscribe" | 订阅/取消订阅 |
| "authenticate" \| "auth" | 鉴权相关 |
| "message" \| "error" \| "close" | 通用消息/错误/关闭 |

## messageHandlers 的职责

作为 WS 与具体 Handler 的桥梁：WS 收到消息后执行 `messageHandlers.get(message.event)?.handle(message, socket)`，实现可扩展的 event 处理。

## messageHandlers 依赖与调用关系

- 上游调用方：ws/ws.ts 的 onMessage
- 下游依赖：../../types/ws（MessageHandler）、./ping（PingHandler）

## messageHandlers 的扩展或修改入口

- 新增 event 处理：实现 MessageHandler，并执行 `messageHandlers.set("event_name", new MyHandler())`（当前在数组字面量中写死，扩展需修改本文件）。

## handler Example

```typescript
import { messageHandlers } from "@paxeer-network/net/src/ws/handler/handler";

const handler = messageHandlers.get("ping");
if (handler) handler.handle({ event: "ping" }, socket);
```
