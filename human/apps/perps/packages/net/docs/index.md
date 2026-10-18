# @paxeer-network/net

## Package 职责

`@paxeer-network/net` 提供 paxeer-network 前端的网络层能力：HTTP 请求封装、WebSocket 客户端、版本与常量。不负责业务路由或 UI，仅负责与后端的通信与流式数据。

## 关键实体表

| 实体名 | 类型 | 职责 | 入口 |
|--------|------|------|------|
| version | string (default export) | 包版本号，并写入 `window.__PAXEER_NETWORK_VERSION__` | `./version` |
| get, post, del, put, mutate | function | HTTP GET/POST/DELETE/PUT 及通用请求 | `./fetch` |
| __PAXEER_NETWORK_API_URL_KEY__ | string constant | API base URL 的全局 key | `./constants` |
| WS | class | 公/私 WebSocket 连接、订阅、重连、心跳 | `./ws/ws` |
| WebSocketEvent | enum | WebSocket 事件名（open/close/error/message/connecting/reconnecting） | `./ws/ws` |
| ApiError | class | HTTP 错误封装（message + code） | `./errors/apiError` |

## 子目录与顶层文件

| 路径 | 类型 | 职责 | 链接 |
|------|------|------|------|
| [errors/](errors/index.md) | 目录 | API 错误类型 | errors/index.md |
| [types/](types/index.md) | 目录 | WebSocket 相关类型与接口 | types/index.md |
| [fetch/](fetch/index.md) | 目录 | HTTP 请求封装（get/post/del/put/mutate） | fetch/index.md |
| [ws/](ws/index.md) | 目录 | WebSocket 客户端与事件处理 | ws/index.md |
| version.ts | TypeScript | 包版本与全局版本注册 | [version.md](version.md) |
| constants.ts | TypeScript | API URL key 常量 | [constants.md](constants.md) |
| sum.ts | TypeScript | 工具函数 sum(a, b) | [sum.md](sum.md) |
| index.ts | TypeScript | 包入口，聚合导出 | [entry.md](entry.md) |

## 检索关键词

- paxeer-network net, network, HTTP, fetch, WebSocket, WS, API, version, ApiError, get, post, del, put, mutate, subscribe, reconnect, mainnet, testnet
