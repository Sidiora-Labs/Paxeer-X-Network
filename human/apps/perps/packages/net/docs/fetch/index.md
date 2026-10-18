# fetch

## 目录职责与边界

- **负责**：基于 `fetch` 的 HTTP 请求封装（get/post/del/put/mutate）、统一 header 与错误转换（含 ApiError）。
- **不负责**：WebSocket、业务 API 路径、认证 token 注入（由调用方通过 options 传入）。

## 本目录文件列表

| 文件名 | 语言 | 简介 | 入口符号 | 链接 |
|--------|------|------|----------|------|
| index.ts | TypeScript | 请求核心与 get/post/del/put/mutate 导出 | request, get, post, del, put, mutate | [index.ts.md](index.ts.md) |

## 关键实体表

| 实体名 | 文件 | 职责 | 依赖/上下游 |
|--------|------|------|-------------|
| request | index.ts | 内部通用请求，处理 URL、headers、JSON、错误 | 被 get/post/del/put/mutate 调用 |
| get | index.ts | GET 请求，支持 formatter、rows 兼容 | 包入口导出 |
| post | index.ts | POST 请求，body 为 JSON | 包入口导出 |
| del | index.ts | DELETE 请求 | 包入口导出 |
| put | index.ts | PUT 请求，body 为 JSON | 包入口导出 |
| mutate | index.ts | 通用 RequestInit 请求 | 包入口导出 |
