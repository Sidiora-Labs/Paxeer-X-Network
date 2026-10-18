# provider.tsx（ReferralProvider）

## ReferralProvider 的职责

为推荐模块提供全局状态：拉取推荐信息与多级推荐数据、计算 isAffiliate/isTrader、聚合 userVolume、管理 showHome 与 tab，并通过 ReferralContext 向下传递。

## ReferralProvider 的输入与输出

- 输入：ReferralContextProps（children、referralLinkUrl、chartConfig、overwrite、splashPage、回调等）。
- 输出：无 UI 直接输出；通过 ReferralContext.Provider 提供 value。

## ReferralProvider Props

| 属性名 | 类型 | 必需 | 默认值 | 说明 |
|--------|------|------|--------|------|
| children | ReactNode | 是 | - | 子节点 |
| becomeAnAffiliateUrl | string | 否 | https://paxeer.network/ | 成为推荐人链接 |
| learnAffiliateUrl | string | 否 | https://paxeer.network/ | 了解推荐链接 |
| referralLinkUrl | string | 是 | - | 推荐链接基础 URL |
| chartConfig | ChartConfig | 否 | - | 图表配置 |
| overwrite | Overwrite | 否 | - | 覆盖节点 |
| splashPage | () => ReactNode | 否 | - | 自定义闪屏 |
| onBecomeAnAffiliate | () => void | 否 | - | 成为推荐人回调 |
| bindReferralCodeState | function | 否 | - | 绑定推荐码回调 |
| onLearnAffiliate | () => void | 否 | - | 了解推荐回调 |
| showReferralPage | () => void | 否 | - | 显示推荐页回调 |

## ReferralProvider 依赖与渲染关系

- 上游调用方：应用根或路由层，包裹推荐相关页面。
- 下游依赖：usePrivateQuery（/v1/referral/info、auto_referral/progress、/v1/volume/user/stats）、useDaily、useMultiLevelReferralData、useAccount、ReferralContext。

## ReferralProvider 的执行流程

1. 从 props 读取 URL、config、overwrite、splashPage、回调。
2. 请求 /v1/referral/info、auto_referral/progress、/v1/volume/user/stats、useDaily、useMultiLevelReferralData。
3. 根据 referral_codes / referer_code 计算 isAffiliate、isTrader。
4. 聚合 userVolume（1d/7d/30d/all），从 localStorage 读取/写入 tab。
5. 若 URL 带 ref 参数则写入 localStorage referral_code。
6. 账号状态变化后延迟 1s 执行 mutate 刷新数据。
7. useMemo 生成 context value，通过 ReferralContext.Provider 注入。

## ReferralProvider 的错误与边界

| 场景 | 条件 | 表现 | 处理方式 |
|------|------|------|----------|
| 未连接 | useAccount 未连接 | wrongNetwork/disabledConnect 透传 | 由子组件处理 |
| 接口失败 | usePrivateQuery 错误 | isLoading/数据为空 | errorRetryCount 等由 hooks 控制 |

## ReferralProvider Example

```tsx
import { ReferralProvider } from "@paxeer-network/affiliate";

<ReferralProvider
  referralLinkUrl="https://paxeer.network/"
  chartConfig={{ affiliate: {}, trader: {} }}
>
  <App />
</ReferralProvider>
```
