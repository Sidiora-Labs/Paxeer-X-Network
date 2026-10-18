# ws/handler/baseHandler.ts

## baseHandler.ts 的职责

提供 MessageHandler 的默认实现基类，handle 方法默认抛出 "Method not implemented."，供具体 handler（如 PingHandler）继承并重写 handle。

## baseHandler.ts 对外导出内容

| 名称 | 类型 | 角色 | 说明 |
|------|------|------|------|
| default | class BaseHandler | 基类 | implements MessageHandler，handle 未实现 |

## BaseHandler 的职责

统一 MessageHandler 接口实现入口，子类只需实现 handle(message, webSocket)。

## BaseHandler 依赖与调用关系

- 上游调用方：无（不被直接调用，由子类实例注册到 messageHandlers）
- 下游依赖：../../types/ws（MessageHandler）

## BaseHandler 的扩展或修改入口

- 新增 handler：新建类 extends BaseHandler，重写 handle，并在 handler.ts 中注册到 messageHandlers。

## BaseHandler Example

```typescript
import BaseHandler from "@paxeer-network/net/src/ws/handler/baseHandler";
import { MessageHandler } from "@paxeer-network/net/src/types/ws";

class MyHandler extends BaseHandler implements MessageHandler {
  handle(message: any, webSocket: WebSocket) {
    // custom logic
  }
}
```
