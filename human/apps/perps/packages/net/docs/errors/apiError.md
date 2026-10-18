# apiError.ts

## apiError.ts 的职责

定义 API 请求失败时的错误类型，继承 `Error`，增加 `code` 字段，便于调用方区分 HTTP 或业务错误码。

## apiError.ts 对外导出内容

| 名称 | 类型 | 角色 | 说明 |
|------|------|------|------|
| ApiError | class | 错误类 | 构造函数 (message, code)；name 为 "ApiError" |

## ApiError 的职责

封装服务端返回的错误信息与错误码，供 `fetch` 模块在 `response.ok === false` 时抛出（如 400 时抛出 ApiError）。

## ApiError 的输入与输出

- 输入：`message: string`，`code: number`
- 输出：实例，`name === "ApiError"`，可读属性 `message`，只读 `code`

## ApiError 参数或属性说明

| 名称 | 类型 | 必需 | 说明 |
|------|------|------|------|
| message | string | 是 | 错误信息（同 Error.message） |
| code | number | 是 | 错误码（如 HTTP 或业务 code），只读 |

## ApiError 依赖与调用关系

- 上游调用方：`fetch/index.ts` 在 400 响应时 `throw new ApiError(...)`
- 下游依赖：无（仅继承 Error）

## ApiError 的错误与边界

| 场景 | 条件 | 表现 | 处理方式 |
|------|------|------|----------|
| 非 400 错误 | fetch 中 status !== 400 | 抛出普通 Error，非 ApiError | 调用方可根据 `e instanceof ApiError` 区分 |

## ApiError Example

```typescript
import { ApiError } from "@paxeer-network/net";

try {
  await get("/api/v1/...");
} catch (e) {
  if (e instanceof ApiError) {
    console.log(e.message, e.code);
  }
}
```
