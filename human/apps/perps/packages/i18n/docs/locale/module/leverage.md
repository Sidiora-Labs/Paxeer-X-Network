# leverage.ts

## leverage.ts responsibility

Provides leverage copy: max account leverage, account/current/max leverage, updated, adjusted leverage, tooltips (max position at leverage, max available leverage, over required margin, over max position leverage), confirm dialog content, disable confirmation, and actual position leverage tip.

## leverage.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| leverage | object | Key-value map | Keys under "leverage.*" |
| Leverage | type | typeof leverage | Type export |

## leverage.ts Example

```typescript
t("leverage.accountLeverage");
t("leverage.confirm.content");
t("leverage.maxAvailableLeverage.tips");
```
