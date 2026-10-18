# provider/index.ts

## index.ts 的职责

重导出 `context` 与 `provider` 中的类型与组件，作为 provider 子包的入口。

## index.ts 对外导出内容

| 名称 | 类型 | 角色 | 说明 |
|------|------|------|------|
| (context 全部导出) | 多种 | Context、类型、useReferralContext | 见 context.ts |
| (provider 全部导出) | 组件 | ReferralProvider | 见 provider.tsx |

## index.ts Example

```typescript
import { ReferralProvider, useReferralContext, TabTypes } from "@paxeer-network/affiliate";
```
