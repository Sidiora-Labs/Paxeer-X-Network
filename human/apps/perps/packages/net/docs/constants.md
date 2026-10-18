# constants.ts

## constants.ts 的职责

定义 net 包使用的全局常量键名，当前仅导出 API base URL 的 key，供配置或注入 base URL 时使用。

## constants.ts 对外导出内容

| 名称 | 类型 | 角色 | 说明 |
|------|------|------|------|
| __PAXEER_NETWORK_API_URL_KEY__ | string constant | 常量 | 值为 `"__PAXEER_NETWORK_API_URL__"`，用作 API 根地址的 key |

## __PAXEER_NETWORK_API_URL_KEY__ 的输入与输出

- 输入：无（常量）
- 输出：字符串 `"__PAXEER_NETWORK_API_URL__"`

## __PAXEER_NETWORK_API_URL_KEY__ 依赖与关联关系

- 上游调用方：需要读取或设置 paxeer-network API base URL 的模块（如配置、fetch 封装）
- 下游依赖：无

## __PAXEER_NETWORK_API_URL_KEY__ Example

```typescript
import { __PAXEER_NETWORK_API_URL_KEY__ } from "@paxeer-network/net";

const baseUrl = (window as any)[__PAXEER_NETWORK_API_URL_KEY__] ?? "https://api.paxeer.network";
const url = `${baseUrl}/v1/...`;
```
