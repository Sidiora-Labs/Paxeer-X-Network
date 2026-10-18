# sum.ts

## sum.ts 的职责

提供纯函数 `sum(a, b)`，用于两数相加。通常用于测试或工具场景，与 net 包核心网络能力无直接关系。

## sum.ts 对外导出内容

| 名称 | 类型 | 角色 | 说明 |
|------|------|------|------|
| sum | function | 工具函数 | (a: number, b: number) => number |

## sum 的输入与输出

- 输入：`a: number`, `b: number`
- 输出：`number`（a + b）

## sum 依赖与关联关系

- 上游调用方：测试或工具代码（未从包入口 index 导出）
- 下游依赖：无

## sum Example

```typescript
import { sum } from "@paxeer-network/net/src/sum";

sum(1, 2); // 3
```
