# ws/handler

## 目录职责与边界

- **负责**：WebSocket 通用消息处理器的定义与注册（Map\<MessageType, MessageHandler\>）；当前实现 ping -> pong。
- **不负责**：业务 topic 解析、WS 连接生命周期（由 ws/ws 负责）。

## 本目录文件列表

| 文件名 | 语言 | 简介 | 入口符号 | 链接 |
|--------|------|------|----------|------|
| handler.ts | TypeScript | messageHandlers Map、MessageType | messageHandlers, MessageType | [handler.md](handler.md) |
| baseHandler.ts | TypeScript | MessageHandler 基类 | BaseHandler (default) | [baseHandler.md](baseHandler.md) |
| ping.ts | TypeScript | ping 事件回 pong | PingHandler (default) | [ping.md](ping.md) |

## 关键实体表

| 实体名 | 文件 | 职责 | 依赖/上下游 |
|--------|------|------|-------------|
| messageHandlers | handler.ts | event -> MessageHandler，供 WS 分发消息 | 被 ws/ws.ts 使用 |
| MessageType | handler.ts | 事件名字面量联合类型 | 用于 Map 键类型 |
| BaseHandler | baseHandler.ts | 实现 MessageHandler，handle 默认 throw | 被 PingHandler 继承 |
| PingHandler | ping.ts | handle 收到 ping 时发送 pong | 注册在 messageHandlers["ping"] |
