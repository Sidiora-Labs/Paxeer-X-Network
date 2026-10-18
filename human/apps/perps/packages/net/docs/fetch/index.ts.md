# fetch/index.ts

## fetch/index.ts 的职责

封装 HTTP 请求：提供统一 URL 校验、RequestInit、Headers（Content-Type）、响应 JSON 解析与错误转换；对外暴露 get、post、del、put、mutate，供上层调用 paxeer-network API。

## fetch/index.ts 对外导出内容

| 名称 | 类型 | 角色 | 说明 |
|------|------|------|------|
| get | function | GET 请求，支持 formatter、rows 结构 | get\<R\>(url, options?, formatter?) => Promise\<R\> |
| post | function | POST 请求 | post(url, data, options?) => Promise\<any\> |
| del | function | DELETE 请求 | del(url, options?) => Promise\<any\> |
| put | function | PUT 请求 | put(url, data, options?) => Promise\<any\> |
| mutate | function | 通用请求 | mutate(url, init) => Promise\<any\> |
| request | function | 内部请求实现 | request(url, options) => Promise\<any\> |

## get 的输入与输出

- 输入：`url: string`，可选 `options?: RequestInit`，可选 `formatter?: (data: any) => R`
- 输出：`Promise<R>`。若 `res.success` 且存在 `res.data.rows` 则返回 `res.data.rows`，否则返回 `res.data`；若提供 formatter 则对 `res.data` 做转换后返回。失败时抛出 Error 或 ApiError。

## post / put / del / mutate 的输入与输出

- post/put：url、data（JSON 序列化为 body）、可选 options；返回 Promise\<any\>（response.json()）。
- del：url、可选 options；返回 Promise\<any\>。
- mutate：url、RequestInit；返回 Promise\<any\>（response.json()）。

## request 的执行流程

1. 校验 url 以 `http` 开头，否则 throw Error。
2. 使用 _createHeaders 合并/设置 Content-Type（DELETE 为 application/x-www-form-urlencoded，其余为 application/json）。
3. fetch(url, options)，若 response.ok 则 return response.json()。
4. 否则读取 error body，若 status === 400 则 throw new ApiError(message, code)，否则 throw new Error(...)。

## _createHeaders 行为

- 若未设置 Content-Type：DELETE 为 `application/x-www-form-urlencoded`，其它为 `application/json;charset=utf-8`。
- 返回合并后的 HeadersInit。

## get 的 rows 兼容

若响应为 `{ success: true, data: { rows: [...] } }` 且未传 formatter，get 返回 `res.data.rows`；否则返回 `res.data`。

## 错误与边界

| 场景 | 条件 | 表现 | 处理方式 |
|------|------|------|----------|
| URL 非法 | 非 http(s) | throw Error("url must start with http(s)") | 调用方保证 url 合法 |
| 400 | response.status === 400 | throw ApiError(errorMsg.message \|\| code, errorMsg.code) | 使用 instanceof ApiError 区分 |
| 其他非 2xx | 非 400 | throw Error(message \|\| code \|\| statusText) | 按 message 处理 |
| 非 JSON 错误体 | 无法 response.json() | 异常向上抛出 | try/catch 统一处理 |

## 依赖与调用关系

- 上游调用方：使用 net 包 get/post/del/put/mutate 的业务或 API 层
- 下游依赖：`../errors/apiError`（ApiError）

## fetch Example

```typescript
import { get, post, del, put, mutate, ApiError } from "@paxeer-network/net";

const rows = await get<MyType[]>("https://api.example.com/v1/list");
const created = await post("https://api.example.com/v1/create", { name: "x" });
await put("https://api.example.com/v1/1", { name: "y" });
await del("https://api.example.com/v1/1");
const raw = await mutate("https://api.example.com/v1/custom", { method: "PATCH", body: "..." });

// With formatter
const list = await get("https://api.example.com/v1/list", undefined, (data) => data.rows as Item[]);
```
