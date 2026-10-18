# portfolio.ts

## portfolio.ts responsibility

Provides portfolio copy: fee tier, API keys, settings, overview (available to withdraw, portfolio value, performance ROI/PnL/volume, distribution, transfer history, vaults), fee tier table and headers, API key create/edit/delete dialogs and columns, IP restriction, created/deleted/updated messages, and settings (system upgrade, cancel open orders, sound alerts, theme).

## portfolio.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| portfolio | object | Key-value map | Keys under "portfolio.*" |
| Portfolio | type | typeof portfolio | Type export |

## portfolio.ts key groups (sample)

| Theme | Examples |
|-------|----------|
| Overview | portfolio.overview.availableWithdraw, portfolio.overview.performance.pnl |
| API keys | portfolio.apiKey.create.dialog.title, portfolio.apiKey.column.apiKey |
| Settings | portfolio.setting.systemUpgrade, portfolio.setting.soundAlerts |

## portfolio.ts Example

```typescript
t("portfolio.feeTier");
t("portfolio.apiKey.created.warning");
t("portfolio.setting.theme");
```
