# ws

## 目录职责与边界

- **负责**：WebSocket 客户端（公/私连接）、订阅/取消订阅、重连、心跳、事件派发（status:change）；以及基于 MessageHandler 的通用消息处理（如 ping/pong）。
- **不负责**：业务 topic 语义、认证签名实现（由 onSigntureRequest 回调注入）。

## 本目录文件列表

| 文件名 | 语言 | 简介 | 入口符号 | 链接 |
|--------|------|------|----------|------|
| ws.ts | TypeScript | WS 类、WebSocketEvent、WSOptions、MessageParams 等 | WS, WebSocketEvent, WSOptions, unsubscribe, MessageParams | [ws.md](ws.md) |
| [handler/](handler/index.md) | 子目录 | 消息处理器注册与实现（ping 等） | messageHandlers, PingHandler, BaseHandler | handler/index.md |

## 关键实体表

| 实体名 | 文件 | 职责 | 依赖/上下游 |
|--------|------|------|-------------|
| WS | ws.ts | 公/私 WebSocket、订阅、重连、心跳、emit status:change | 使用 handler/messageHandlers、types/ws |
| WebSocketEvent | ws.ts | 事件名枚举（OPEN/CLOSE/ERROR/MESSAGE/CONNECTING/RECONNECTING） | 供 status:change 的 type 使用 |
| messageHandlers | handler/handler.ts | event -> MessageHandler 映射 | 被 WS.onMessage 使用 |
| BaseHandler | handler/baseHandler.ts | MessageHandler 基类 | 被 PingHandler 继承 |
| PingHandler | handler/ping.ts | 收到 ping 回 pong | 注册在 messageHandlers |
