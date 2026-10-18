# errors

## 目录职责与边界

- **负责**：网络层错误类型定义（如 API 返回错误封装）。
- **不负责**：业务错误码、国际化文案、UI 错误展示。

## 本目录文件列表

| 文件名 | 语言 | 简介 | 入口符号 | 链接 |
|--------|------|------|----------|------|
| apiError.ts | TypeScript | API 错误类，带 message 与 code | ApiError | [apiError.md](apiError.md) |

## 关键实体表

| 实体名 | 文件 | 职责 | 依赖/上下游 |
|--------|------|------|-------------|
| ApiError | apiError.ts | 封装 HTTP 错误信息与状态码，供 fetch 抛出 | 被 fetch/index 使用；继承 Error |
