# types

## 目录职责与边界

- **负责**：WebSocket 相关类型、订阅/取消订阅消息形状、发送函数与消息处理接口。
- **不负责**：HTTP 请求类型、业务 DTO、UI 组件 Props。

## 本目录文件列表

| 文件名 | 语言 | 简介 | 入口符号 | 链接 |
|--------|------|------|----------|------|
| ws.ts | TypeScript | WS 消息观察参数、发送函数、MessageHandler 接口 | MessageObserveTopic, MessageObserveParams, SendFunc, MessageHandler | [ws.md](ws.md) |

## 关键实体表

| 实体名 | 文件 | 职责 | 依赖/上下游 |
|--------|------|------|-------------|
| MessageObserveTopic | ws.ts | 订阅/取消订阅消息的 topic 形态 | 被 WS 订阅逻辑使用 |
| MessageObserveParams | ws.ts | 订阅参数：string 或 MessageObserveTopic | 同上 |
| SendFunc | ws.ts | 发送任意消息的函数类型 | 类型占位 |
| MessageHandler | ws.ts | 消息处理器接口（handle(message, webSocket)） | 被 ws/handler 实现 |
