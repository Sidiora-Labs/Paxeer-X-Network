# en.ts

## en.ts responsibility

Aggregates all locale modules (common, markets, portfolio, trading, chart, positions, orders, tpsl, share, orderEntry, leverage, scaffold, tradingRewards, tradingView, connector, transfer, affiliate, ui, tradingLeaderboard, tradingPoints, widget, vaults, notification) into a single `en` object. This object is the default English bundle and defines the base shape of LocaleMessages for the default namespace.

## en.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| en | object | Default bundle | Merged key-value map from all module/*.ts |

## en.ts dependency

- **Upstream**: All locale/module/*.ts.
- **Downstream**: types (EnType), i18n.ts (resources), package index (re-export).

## en.ts Example

```typescript
import { en } from "@paxeer-network/i18n";

const confirmLabel = en["common.confirm"]; // "Confirm"
```
